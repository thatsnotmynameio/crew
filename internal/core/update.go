package core

import (
	"cmp"
	"slices"
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Update applies in to the model and returns the commands to run and the
// events to publish, in order. It is deterministic: the same model
// and input always give the same result. Inputs that answer nothing the
// core is waiting for, such as a result for a released run, change
// nothing.
func (m *Model) Update(in Input) ([]Command, []Published) {
	s := &step{m: m, at: in.arrival()}
	switch in := in.(type) {
	case RunInput:
		s.runInput(in)
	case SchedulerInput:
		s.schedulerInput(in)
	}
	s.windDown()
	if m.Stopped() && !m.stopped {
		m.stopped = true
		s.emit(Stopped{At: s.at})
	}
	return s.cmds, s.events
}

// schedulerInput applies an input about what spans rule runs: a tick, a
// stop, the run time, a listing, a board read, the bots, or a tracker
// write's or a record's result.
func (s *step) schedulerInput(in SchedulerInput) {
	switch in := in.(type) {
	case Tick:
		s.tick(in.Said)
	case StopRequested:
		s.m.requested = true
		s.stop()
	case TimeUp:
		s.timeUp(in.Limit)
	case IssuesListed:
		s.seed = in.Seed
		s.listed(in.Issues)
	case ListFailed:
		s.m.listing = false
		s.m.listingFailed(in.Reason)
		s.emit(ListingFailed{At: s.at, Reason: in.Reason})
	case BoardListed:
		s.m.boardListed(in.Issues)
	case BoardListFailed:
		s.m.boardListFailed(in.Reason)
	case BotsChecked:
		s.botsChecked(in)
	case CallResult:
		s.callResult(in)
	case StatusResult:
		s.statusResult(in)
	case PullRequestsResult:
		s.pullRequestsResult(in)
	case RecordFailed:
		r := in.Record
		s.emit(RunNotRecorded{
			At: s.at, IssueID: r.IssueID, IssueRef: r.IssueRef, Rule: r.Rule, Action: r.Action, Reason: in.Reason,
		})
	}
}

// step is one Update in progress: the input's time and what it produced.
type step struct {
	m      *Model
	at     time.Time
	cmds   []Command
	events []Published
	// seed is the input's seed, from which the rule runs it takes get their
	// ids, and runs counts those runs (KTD5).
	seed uuid.UUID
	runs int
}

func (s *step) command(c Command) { s.cmds = append(s.cmds, c) }
func (s *step) emit(e Published)  { s.events = append(s.events, e) }

// tick reads the board (KTD4), then lists issues, unless a listing is
// outstanding or the run time is up; when every slot is busy it says it
// skipped the listing instead (R1, R3). It then retries the owed calls,
// statuses and pull request reports that are not in flight (KTD8, KTD5), and
// reports the status of each running issue with what its sessions last said
// (R6).
func (s *step) tick(said []Said) {
	m := s.m
	if m.stopping {
		return
	}
	for _, x := range said {
		if h := m.heldRun(x.Run); h != nil {
			h.said(x.Action, x.Text)
		}
	}
	s.readBoard()
	if !m.listing && !m.timeUp {
		if m.full() {
			m.skipped++
			s.emit(PollSkipped{At: s.at, Busy: len(m.issues), Slots: m.slots})
		} else {
			s.listIssues()
		}
	}
	for _, h := range m.issues {
		s.retryRun(h.id(), false)
		if h.claim() == ClaimRunning {
			s.reportRun(h)
		}
	}
	s.retryStatuses()
	s.retryPullRequests()
}

// listIssues asks for the items in every rule's ready and running labels,
// of both kinds, each once, and starts the count of skipped listings again
// (R6). Only the ready labels are taken from; the running ones fill the
// default board (KTD10).
func (s *step) listIssues() {
	m := s.m
	m.listing = true
	m.listings++
	m.skipped = 0
	m.listingAsked()
	var states []crew.State
	for _, r := range m.rules {
		for _, st := range []crew.State{r.Labels.Ready, r.Labels.Running} {
			if !slices.Contains(states, st) {
				states = append(states, st)
			}
		}
	}
	s.command(ListIssues{States: states})
}

// freed lists at once when a released issue freed a slot after a tick
// skipped its listing (R4); otherwise the next tick lists (R5).
func (s *step) freed() {
	m := s.m
	if m.skipped > 0 && !m.listing && !m.timeUp && !m.stopping {
		s.listIssues()
	}
}

// stop starts nothing new from now on, hands every held run the stop, which
// stops its running sessions and checks, and gives each owed call, status
// and pull request report not in flight its final try (R9). Runs whose
// actions have all ended are already being judged, so their verdicts go on.
func (s *step) stop() {
	m := s.m
	if m.stopping {
		return
	}
	m.stopping = true
	for _, h := range m.issues {
		s.decide(h, crew.StopReached{FactHead: s.head(h)})
		s.retryRun(h.id(), true)
	}
	s.retryStatuses()
	s.retryPullRequests()
}

// timeUp ends the run time (R2): from now on nothing new is taken, while the
// held issues, a take in flight or owed included, run and are judged as
// usual (R4). windDown stops once they have all ended.
func (s *step) timeUp(limit time.Duration) {
	m := s.m
	if m.stopping || m.timeUp {
		return
	}
	m.timeUp = true
	s.emit(WindingDown{At: s.at, Limit: limit})
}

// windDown starts the stop sequence once the run time is up and no held
// issue has an action left to end, so owed calls get their final try (R5).
func (s *step) windDown() {
	m := s.m
	if !m.timeUp || m.stopping {
		return
	}
	for _, h := range m.issues {
		if !h.run.ActionsEnded() {
			return
		}
	}
	s.stop()
}

// listed marks the handled entries whose issue left its state (KTD4), fills
// a board filled from the listings (KTD10), skips issues in two states
// (R15), reports the items in the label of a rule of the other kind (#92),
// and takes free slots' worth of issues, each while its rule's queue has a
// free slot (R6): the highest priority first, an issue without one last;
// then, at the same priority, later rules first; then the oldest issue
// first (KTD8). It reports nothing for the issues it leaves, a blocked one
// included: a later listing with a free slot takes them. It takes nothing
// once the run time is up.
func (s *step) listed(issues []crew.Issue) {
	m := s.m
	m.listing = false
	m.gone(issues)
	m.boardFromListing(issues)
	if m.stopping || m.timeUp {
		return
	}
	s.skipped(issues)
	s.otherKind(issues)
	taken := s.takeWaiting(s.waiting(issues))
	s.emit(PollDone{At: s.at, Listed: len(issues), Taken: taken})
}

// skipped reports each issue in more than one state that crew does not hold
// (R15).
func (s *step) skipped(issues []crew.Issue) {
	for _, issue := range issues {
		if len(issue.States()) > 1 && s.m.held(issue.ID()) == nil {
			s.emit(IssueSkipped{At: s.at, IssueID: issue.ID(), IssueRef: issue.Ref(), States: issue.States()})
		}
	}
}

// candidate is an issue waiting in the state of rule, which crew may take.
type candidate struct {
	rule  int
	issue crew.Issue
}

// waiting returns the unblocked items of a rule's kind waiting in its state,
// in the order listed takes them.
func (s *step) waiting(issues []crew.Issue) []candidate {
	var candidates []candidate
	for si, rule := range s.m.rules {
		for _, issue := range issues {
			inLabel := len(issue.States()) == 1 && issue.States()[0] == rule.Labels.Ready
			if inLabel && issue.Kind() == rule.Takes && !issue.Blocked() {
				candidates = append(candidates, candidate{si, issue})
			}
		}
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int {
		if c := comparePriority(a.issue.Priority(), b.issue.Priority()); c != 0 {
			return c
		}
		if c := cmp.Compare(b.rule, a.rule); c != 0 {
			return c
		}
		return a.issue.Created().Compare(b.issue.Created())
	})
	return candidates
}

// takeWaiting takes candidates in order while slots are free, passing over
// each one whose rule's queue is full, and returns how many it took
// (KTD3). It reports nothing for the rest.
func (s *step) takeWaiting(candidates []candidate) int {
	m := s.m
	taken := 0
	for _, c := range candidates {
		if m.full() {
			break
		}
		if m.held(c.issue.ID()) != nil || m.queueFull(m.queueOf[c.rule]) {
			continue
		}
		s.take(c.rule, c.issue)
		taken++
	}
	return taken
}

// comparePriority orders two issue priorities, the higher first: 1 before
// 2, and any priority before 0, which is none.
func comparePriority(a, b int) int {
	switch {
	case a == b:
		return 0
	case a == 0:
		return 1
	case b == 0:
		return -1
	}
	return cmp.Compare(a, b)
}

// take holds issue for rule si, as a new rule run that inherits today's
// resume points (KTD-P4), and moves it to the rule's running label.
func (s *step) take(si int, issue crew.Issue) {
	m := s.m
	rule := m.rules[si]
	s.runs++
	taken := crew.RunTaken{
		Run: crew.NewRuleRunID(s.seed, s.runs), At: s.at, IssueID: issue.ID(), IssueRef: issue.Ref(), Rule: rule.Name,
		Issue: issue.Data(), From: rule.Labels.Ready, To: rule.Labels.Running,
	}
	for _, a := range rule.Actions {
		taken.Actions = append(taken.Actions, crew.ActionTaken{
			Name: a.Name, Resume: m.resumable(issue.ID(), rule.Name, a.Name),
		})
	}
	// Applied to the zero run, a RunTaken event is never refused.
	run, _ := crew.Apply(crew.RuleRun{}, taken)
	h := &heldIssue{run: run, rule: si}
	m.issues = append(m.issues, h)
	s.emit(taken)
	s.deliver(h, &delivery{purpose: purposeTake, call: h.move(rule.Labels.Ready, rule.Labels.Running)})
}

// full reports whether every slot is busy, so a listing could take nothing:
// the issues held, in any claim, reach max_parallel_issues, or every queue
// some rule runs in is full (R1, R7, KTD4).
func (m *Model) full() bool {
	if len(m.issues) >= m.maxParallel {
		return true
	}
	for q := range m.queues {
		if !m.queueFull(q) {
			return false
		}
	}
	return true
}

// queueFull reports whether queue q has no free slot: its busy slots reach
// its slots. A queue of 0 slots is always full.
func (m *Model) queueFull(q int) bool {
	return m.busy(q) >= m.queues[q].Slots
}

// busy returns how many slots of queue q are busy: the held issues, in any
// claim, whose rule runs in q (KTD3).
func (m *Model) busy(q int) int {
	held := 0
	for _, h := range m.issues {
		if m.queueOf[h.rule] == q {
			held++
		}
	}
	return held
}

// heldRun returns the held issue whose rule run is identified by id, or
// nil.
func (m *Model) heldRun(id crew.RuleRunID) *heldIssue {
	for _, h := range m.issues {
		if h.run.ID() == id {
			return h
		}
	}
	return nil
}

// held returns the held issue identified by id, or nil.
func (m *Model) held(id crew.IssueID) *heldIssue {
	for _, h := range m.issues {
		if h.id() == id {
			return h
		}
	}
	return nil
}
