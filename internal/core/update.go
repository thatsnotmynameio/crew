package core

import (
	"cmp"
	"slices"
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// stoppedReason is the reason of an action that never ran because crew
// stopped first (R9).
const stoppedReason = "crew stopped"

// stopped is the outcome of an action crew stopped before it could end
// on its own.
func stopped() crew.Outcome {
	return crew.Outcome{Reason: crew.NewSessionText(stoppedReason)}
}

// Update applies in to the model and returns the commands to run and the
// domain events to publish, in order. It is deterministic: the same model
// and input always give the same result. Inputs that answer nothing the
// core is waiting for, such as a result for a released issue, change
// nothing.
func (m *Model) Update(in Input) ([]Command, []Event) {
	s := &step{m: m, at: in.arrival()}
	if !s.runInput(in) {
		s.actionInput(in)
	}
	s.windDown()
	if m.Stopped() && !m.stopped {
		m.stopped = true
		s.emit(Stopped{At: s.at})
	}
	return s.cmds, s.events
}

// runInput applies an input about the run as a whole or its tracker calls,
// and reports whether in was one.
func (s *step) runInput(in Input) bool {
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
	default:
		return false
	}
	return true
}

// step is one Update in progress: the input's time and what it produced.
type step struct {
	m      *Model
	at     time.Time
	cmds   []Command
	events []Event
	// seed is the input's seed, from which the rule runs it takes get their
	// ids, and runs counts those runs (KTD5).
	seed uuid.UUID
	runs int
}

func (s *step) command(c Command) { s.cmds = append(s.cmds, c) }
func (s *step) emit(e Event)      { s.events = append(s.events, e) }

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
		if _, a := m.action(x.IssueID, x.Action, PhaseRunning); a != nil {
			a.said = x.Text
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
		s.retryRun(h.issue.ID, false)
		if h.claim == ClaimRunning {
			s.running(h)
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

// stop starts nothing new from now on, stops the running sessions and
// checks, and gives each owed call, status and pull request report not in
// flight its final try (R9). Issues whose actions have all ended are already
// being judged, so their verdicts go on.
func (s *step) stop() {
	m := s.m
	if m.stopping {
		return
	}
	m.stopping = true
	for _, h := range m.issues {
		switch h.claim {
		case ClaimTaking:
			h.claim = ClaimStopping
		case ClaimRunning:
			h.claim = ClaimStopping
			s.stopActions(h)
		case ClaimJudging, ClaimStopping, ClaimOwed:
			// Judging goes on; only stop sets stopping, and it runs once;
			// owed is never stored, the view derives it.
		}
		s.retryRun(h.issue.ID, true)
	}
	s.retryStatuses()
	s.retryPullRequests()
}

// stopActions stops h's running sessions and checks.
func (s *step) stopActions(h *heldIssue) {
	for _, a := range h.actions {
		switch a.phase {
		case PhaseRunning:
			s.command(StopSession{IssueID: h.issue.ID, Action: a.name})
		case PhaseChecking:
			a.stopped = true
			s.command(StopCheck{IssueID: h.issue.ID, Action: a.name})
		case PhaseWaiting, PhaseCreating, PhaseReopening, PhaseStarting, PhaseFinishing, PhaseEnded:
			// No session or check runs: its next input sees the stop.
		}
	}
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
		if !h.ended() {
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
		if len(issue.States) > 1 && s.m.held(issue.ID) == nil {
			s.emit(IssueSkipped{At: s.at, IssueID: issue.ID, IssueRef: issue.Ref, States: slices.Clone(issue.States)})
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
			inLabel := len(issue.States) == 1 && issue.States[0] == rule.Labels.Ready
			if inLabel && issue.Kind == rule.Takes && !issue.Blocked {
				candidates = append(candidates, candidate{si, issue})
			}
		}
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int {
		if c := comparePriority(a.issue.Priority, b.issue.Priority); c != 0 {
			return c
		}
		if c := cmp.Compare(b.rule, a.rule); c != 0 {
			return c
		}
		return a.issue.Created.Compare(b.issue.Created)
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
		if m.held(c.issue.ID) != nil || m.queueFull(m.queueOf[c.rule]) {
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

// take holds issue for rule si and moves it to the rule's running label.
func (s *step) take(si int, issue crew.Issue) {
	m := s.m
	rule := m.rules[si]
	s.runs++
	h := &heldIssue{
		issue: issue.Clone(), rule: si, run: crew.NewRuleRunID(s.seed, s.runs), claim: ClaimTaking, taken: s.at,
	}
	for _, a := range rule.Actions {
		h.actions = append(h.actions, &actionRun{
			name: a.Name, prompt: a.Prompt, checks: a.Checks, agent: a.Agent, bot: a.Bot,
		})
	}
	m.issues = append(m.issues, h)
	s.emit(IssueTaken{At: s.at, Issue: issue.Clone(), Rule: rule.Name, From: rule.Labels.Ready, To: rule.Labels.Running})
	s.deliver(h, &delivery{purpose: purposeTake, call: h.move(rule.Labels.Ready, rule.Labels.Running)})
}

// move returns the move of h's issue from one state to another, as a Call.
func (h *heldIssue) move(from, to crew.State) Call {
	return Call{Kind: CallMove, IssueID: h.issue.ID, IssueRef: h.issue.Ref, From: from, To: to}
}

// taken applies the take to the board (KTD4), reports it on h's pull
// requests and starts h's actions once its take move is done, or, after a
// stop, ends them unstarted so the issue moves to its rule's failure label
// (R9). A rule without actions is judged at once, after a stop too, so the
// issue moves on to its rule's success (R8, KTD5).
func (s *step) taken(h *heldIssue, c Call) {
	m := s.m
	s.emit(IssueMoved{At: s.at, IssueID: h.issue.ID, IssueRef: h.issue.Ref, From: c.From, To: c.To})
	m.boardMoved(h.issue, c.To)
	s.reportPullRequests(h, c.To, false)
	switch {
	case len(h.actions) == 0:
		s.judge(h)
	case m.stopping:
		for _, a := range h.actions {
			s.end(h, a, stopped(), crew.CauseStopped)
		}
	default:
		s.start(h)
	}
}

// start starts h's actions, each in a new workspace or in its failed run's
// (R5), and reports h running unless every action already ended.
func (s *step) start(h *heldIssue) {
	m := s.m
	h.claim = ClaimRunning
	for _, a := range h.actions {
		prompt, err := crew.Action{Name: a.name, Prompt: a.prompt}.Render(h.issue)
		if err != nil {
			s.end(h, a, crew.Outcome{Reason: crew.NewSessionText(err.Error())}, crew.CausePrompt)
			continue
		}
		a.prompt = prompt
		if prev, ok := m.resumable(h, a); ok {
			a.prev = &prev
			a.phase = PhaseReopening
			s.command(ReopenWorkspace{IssueID: h.issue.ID, Action: a.name, Workspace: prev.Workspace, Branch: prev.Branch})
			continue
		}
		a.phase = PhaseCreating
		s.command(CreateWorkspace{Issue: h.issue.Clone(), Action: a.name})
	}
	if h.claim == ClaimRunning {
		s.running(h)
	}
}

// judge moves h to its rule's success label when every action succeeded,
// and otherwise to its failure label with a failure report (R7).
func (s *step) judge(h *heldIssue) {
	rule := s.m.rules[h.rule]
	h.claim = ClaimJudging
	report := crew.FailureReport{IssueID: h.issue.ID, IssueRef: h.issue.Ref}
	for _, a := range h.actions {
		if !a.outcome.Succeeded {
			report.Failures = append(report.Failures, crew.ActionFailure{
				Action: a.name, Workspace: a.workspace, Log: a.log,
			})
		}
	}
	h.verdict = &HandledView{
		Issue: h.issue.Clone(), Rule: rule.Name, To: rule.Labels.Success, Taken: h.taken, Ended: s.at,
	}
	for _, a := range h.actions {
		h.verdict.Actions = append(h.verdict.Actions, HandledAction{Name: a.name, Spend: a.spend(), PullRequest: a.pr})
	}
	if len(report.Failures) == 0 {
		s.deliver(h, &delivery{purpose: purposeVerdict, call: h.move(rule.Labels.Running, rule.Labels.Success)})
		s.ended(h, rule.Labels.Success, crew.MovePending)
		return
	}
	h.verdict.To, h.verdict.Failures = rule.Labels.Failure, slices.Clone(report.Failures)
	s.deliver(h, &delivery{purpose: purposeVerdict, call: h.move(rule.Labels.Running, rule.Labels.Failure)})
	s.deliver(h, &delivery{
		purpose: purposeReport, report: report,
		call: Call{Kind: CallReport, IssueID: h.issue.ID, IssueRef: h.issue.Ref},
	})
	s.ended(h, rule.Labels.Failure, crew.MovePending)
}

// received applies to h the outcome of one of its deliveries: a
// landed take starts its actions, a landed verdict move reports the move, a
// landed failure report is reported, and a verdict move given up ends the
// status with the move dropped. A take given up leaves nothing to do.
func (s *step) received(h *heldIssue, o outcome) {
	switch {
	case o.purpose == purposeTake && o.landed:
		s.taken(h, o.call)
	case o.purpose == purposeVerdict && o.landed:
		s.emit(IssueMoved{At: s.at, IssueID: h.issue.ID, IssueRef: h.issue.Ref, From: o.call.From, To: o.call.To})
		s.m.boardMoved(h.issue, o.call.To)
		s.ended(h, o.call.To, crew.MoveDone)
		s.reportPullRequests(h, o.call.To, true)
		h.verdict.Move, h.landed = crew.MoveDone, s.m.listings
	case o.purpose == purposeVerdict:
		s.ended(h, o.call.To, crew.MoveDropped)
		h.verdict.Move, h.verdict.DropReason = crew.MoveDropped, o.reason
		h.landed = s.m.listings
	case o.purpose == purposeReport && o.landed:
		s.emit(FailureReported{At: s.at, IssueID: h.issue.ID, IssueRef: h.issue.Ref})
	}
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

// held returns the held issue identified by id, or nil.
func (m *Model) held(id crew.IssueID) *heldIssue {
	for _, h := range m.issues {
		if h.issue.ID == id {
			return h
		}
	}
	return nil
}

// action returns the named action of the held issue identified by id, when
// it is in one of phases, or nils.
func (m *Model) action(id crew.IssueID, name crew.ActionName, phases ...Phase) (*heldIssue, *actionRun) {
	h := m.held(id)
	if h == nil {
		return nil, nil
	}
	for _, a := range h.actions {
		if a.name == name && slices.Contains(phases, a.phase) {
			return h, a
		}
	}
	return nil, nil
}

// release forgets h, keeping its handled entry, which replaces the issue's
// earlier one, when its rule ended. A rule without actions that ended well
// keeps an earlier entry that ended well too, marked Gone: its move took the
// issue out of the entry's To (#109, R10, KTD6).
func (m *Model) release(h *heldIssue) {
	m.issues = slices.DeleteFunc(m.issues, func(x *heldIssue) bool { return x == h })
	if h.verdict == nil {
		return
	}
	view := *h.verdict
	i := slices.IndexFunc(m.handled, func(e handledEntry) bool { return e.view.Issue.ID == h.issue.ID })
	if i >= 0 {
		old := m.handled[i].view
		if len(m.rules[h.rule].Actions) == 0 && !h.verdict.NeedsAttention() && !old.NeedsAttention() {
			m.handled[i].view.Gone = true
			return
		}
		view.Earlier = old.Spend().Add(old.Earlier)
		m.handled = slices.Delete(m.handled, i, i+1)
	}
	m.handled = append(m.handled, handledEntry{view: view, landed: h.landed})
}

// ended reports whether every action of h has ended.
func (h *heldIssue) ended() bool {
	for _, a := range h.actions {
		if a.phase != PhaseEnded {
			return false
		}
	}
	return true
}
