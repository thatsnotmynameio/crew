package core

import (
	"cmp"
	"slices"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// stoppedReason is the reason of an action that never ran because crew
// stopped first (R9).
const stoppedReason = "crew stopped"

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
		s.listed(in.Issues)
	case ListFailed:
		s.m.listing = false
		s.emit(ListingFailed{At: s.at, Reason: in.Reason})
	case CallResult:
		s.callResult(in)
	case StatusResult:
		s.statusResult(in)
	case PullRequestsResult:
		s.pullRequestsResult(in)
	case RecordFailed:
		r := in.Record
		s.emit(RunNotRecorded{
			At: s.at, IssueKey: r.IssueKey, IssueRef: r.IssueRef, Stage: r.Stage, Action: r.Action, Reason: in.Reason,
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
}

func (s *step) command(c Command) { s.cmds = append(s.cmds, c) }
func (s *step) emit(e Event)      { s.events = append(s.events, e) }

// tick lists issues, unless a listing is outstanding or the run time is up;
// when every slot is busy it says it skipped the listing instead (R1, R3). It
// then retries the owed calls, statuses and pull request reports that are not in
// flight (KTD8, KTD5), and reports the status of each running issue with what
// its sessions last said (R6).
func (s *step) tick(said []Said) {
	m := s.m
	if m.stopping {
		return
	}
	for _, x := range said {
		if _, a := m.action(x.IssueKey, x.Action, PhaseRunning); a != nil {
			a.said = x.Text
		}
	}
	if !m.listing && !m.timeUp {
		if m.full() {
			m.skipped++
			s.emit(PollSkipped{At: s.at, Busy: len(m.issues), Slots: m.slots})
		} else {
			s.listIssues()
		}
	}
	for _, h := range m.issues {
		s.retryOwed(h, false)
		if h.claim == ClaimRunning {
			s.running(h)
		}
	}
	s.retryStatuses()
	s.retryPullRequests()
}

// listIssues asks for the issues in every stage's state, and starts the count
// of skipped listings again (R6).
func (s *step) listIssues() {
	m := s.m
	m.listing = true
	m.skipped = 0
	states := make([]crew.State, len(m.stages))
	for i, st := range m.stages {
		states[i] = st.Label
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

// retryOwed attempts h's owed calls that are not in flight, each as its final
// try when final is set.
func (s *step) retryOwed(h *heldIssue, final bool) {
	for _, c := range h.calls {
		if c.owed && !c.inFlight {
			if final {
				c.final = true
			}
			s.attempt(h, c)
		}
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
		case ClaimJudging, ClaimOwed:
			s.retryOwed(h, true)
		case ClaimStopping:
			// Only stop sets this claim, and stop runs once.
		}
	}
	s.retryStatuses()
	s.retryPullRequests()
}

// stopActions stops h's running sessions and checks.
func (s *step) stopActions(h *heldIssue) {
	for _, a := range h.actions {
		switch a.phase {
		case PhaseRunning:
			s.command(StopSession{IssueKey: h.issue.Key, Action: a.name})
		case PhaseChecking:
			a.stopped = true
			s.command(StopCheck{IssueKey: h.issue.Key, Action: a.name})
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

// listed skips issues in two states (R15) and takes free slots' worth of
// issues, each while its stage's queue has a free slot (R6): the highest
// priority first, an issue without one last; then, at the same priority,
// later stages first; then the oldest issue first (KTD8). It reports nothing
// for the issues it leaves, a blocked one included: a later listing with a
// free slot takes them. It takes nothing once the run time is up.
func (s *step) listed(issues []crew.Issue) {
	m := s.m
	m.listing = false
	if m.stopping || m.timeUp {
		return
	}
	s.skipped(issues)
	taken := s.takeWaiting(s.waiting(issues))
	s.emit(PollDone{At: s.at, Listed: len(issues), Taken: taken})
}

// skipped reports each issue in more than one state that crew does not hold
// (R15).
func (s *step) skipped(issues []crew.Issue) {
	for _, issue := range issues {
		if len(issue.States) > 1 && s.m.held(issue.Key) == nil {
			s.emit(IssueSkipped{At: s.at, IssueKey: issue.Key, IssueRef: issue.Ref, States: slices.Clone(issue.States)})
		}
	}
}

// candidate is an issue waiting in the state of stage, which crew may take.
type candidate struct {
	stage int
	issue crew.Issue
}

// waiting returns the unblocked issues waiting in a stage's state, in the
// order listed takes them.
func (s *step) waiting(issues []crew.Issue) []candidate {
	var candidates []candidate
	for si, stage := range s.m.stages {
		for _, issue := range issues {
			if len(issue.States) == 1 && issue.States[0] == stage.Label && !issue.Blocked {
				candidates = append(candidates, candidate{si, issue})
			}
		}
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int {
		if c := comparePriority(a.issue.Priority, b.issue.Priority); c != 0 {
			return c
		}
		if c := cmp.Compare(b.stage, a.stage); c != 0 {
			return c
		}
		return a.issue.Created.Compare(b.issue.Created)
	})
	return candidates
}

// takeWaiting takes candidates in order while slots are free, passing over
// each one whose stage's queue is full, and returns how many it took
// (KTD3). It reports nothing for the rest.
func (s *step) takeWaiting(candidates []candidate) int {
	m := s.m
	taken := 0
	for _, c := range candidates {
		if m.full() {
			break
		}
		if m.held(c.issue.Key) != nil || m.queueFull(m.queueOf[c.stage]) {
			continue
		}
		s.take(c.stage, c.issue)
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

// take holds issue for stage si and moves it to the stage's moves_to.
func (s *step) take(si int, issue crew.Issue) {
	m := s.m
	stage := m.stages[si]
	h := &heldIssue{issue: issue.Clone(), stage: si, claim: ClaimTaking, taken: s.at}
	for _, a := range stage.Actions {
		h.actions = append(h.actions, &actionRun{name: a.Name, prompt: a.Prompt, check: a.Check})
	}
	m.issues = append(m.issues, h)
	s.emit(IssueTaken{At: s.at, Issue: issue.Clone(), Stage: stage.Name, From: stage.Label, To: stage.MovesTo})
	s.call(h, &call{kind: CallMove, take: true, from: stage.Label, to: stage.MovesTo})
}

// call registers c on h under a new ID and makes its first attempt.
func (s *step) call(h *heldIssue, c *call) {
	s.m.lastID++
	c.id = s.m.lastID
	h.calls = append(h.calls, c)
	s.attempt(h, c)
}

// attempt issues c's command.
func (s *step) attempt(h *heldIssue, c *call) {
	c.inFlight = true
	if c.kind == CallReport {
		s.command(ReportFailure{ID: c.id, Report: cloneReport(c.report)})
		return
	}
	s.command(Move{ID: c.id, IssueKey: h.issue.Key, From: c.from, To: c.to})
}

// callResult settles, owes or retries the call r answers. A take and a
// verdict call are owed alike when they fail transiently: a take may have
// landed although it failed, so releasing its issue could strand it in
// moves_to with no session, and the tracker makes the retry idempotent.
func (s *step) callResult(r CallResult) {
	m := s.m
	h, c := m.findCall(r.ID)
	if c == nil || !c.inFlight {
		return
	}
	c.inFlight = false
	switch r.Result {
	case ResultDone:
		if c.take {
			s.taken(h, c)
			return
		}
		if c.kind == CallReport {
			s.emit(FailureReported{At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref})
		} else {
			s.emit(IssueMoved{At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, From: c.from, To: c.to})
			s.ended(h, c.to, crew.MoveDone)
			s.reportPullRequests(h, c.to, true)
			h.verdict.Move = crew.MoveDone
		}
		h.settle(c)
	case ResultFailed:
		switch {
		case m.stopping && c.final:
			s.dropped(h, c, r)
		default:
			c.owed = true
			h.claim = ClaimOwed
			s.emit(CallOwed{At: s.at, Call: h.describe(c), Reason: r.Reason})
			if m.stopping {
				c.final = true
				s.attempt(h, c)
			}
		}
	default:
		s.dropped(h, c, r)
	}
	if len(h.calls) == 0 {
		m.release(h)
		s.freed()
	}
}

// dropped gives up c, which r answered, and says so on h's status when c is
// the verdict move.
func (s *step) dropped(h *heldIssue, c *call, r CallResult) {
	s.emit(CallDropped{At: s.at, Call: h.describe(c), Result: r.Result, Reason: r.Reason})
	if c.kind == CallMove && !c.take {
		s.ended(h, c.to, crew.MoveDropped)
		h.verdict.Move, h.verdict.DropReason = crew.MoveDropped, r.Reason
	}
	h.settle(c)
}

// taken reports the take on h's pull requests and starts h's actions once
// its take move is done, or, after a stop, ends them unstarted so the issue
// moves to its stage's on_failure (R9).
func (s *step) taken(h *heldIssue, c *call) {
	m := s.m
	s.emit(IssueMoved{At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, From: c.from, To: c.to})
	s.reportPullRequests(h, c.to, false)
	h.settle(c)
	if m.stopping {
		for _, a := range h.actions {
			s.end(h, a, crew.Outcome{Reason: stoppedReason}, crew.CauseStopped)
		}
		return
	}
	h.claim = ClaimRunning
	for _, a := range h.actions {
		prompt, err := crew.Action{Name: a.name, Prompt: a.prompt}.Render(h.issue)
		if err != nil {
			s.end(h, a, crew.Outcome{Reason: err.Error()}, crew.CausePrompt)
			continue
		}
		a.prompt = prompt
		if prev, ok := m.resumable(h, a); ok {
			a.prev = &prev
			a.phase = PhaseReopening
			s.command(ReopenWorkspace{IssueKey: h.issue.Key, Action: a.name, Workspace: prev.Workspace, Branch: prev.Branch})
			continue
		}
		a.phase = PhaseCreating
		s.command(CreateWorkspace{Issue: h.issue.Clone(), Action: a.name})
	}
	if h.claim == ClaimRunning {
		s.running(h)
	}
}

// judge moves h to its stage's on_success when every action succeeded, and
// otherwise to its stage's on_failure with a failure report (R7).
func (s *step) judge(h *heldIssue) {
	stage := s.m.stages[h.stage]
	h.claim = ClaimJudging
	report := crew.FailureReport{IssueKey: h.issue.Key, IssueRef: h.issue.Ref}
	for _, a := range h.actions {
		if !a.outcome.Succeeded {
			report.Failures = append(report.Failures, crew.ActionFailure{
				Action: a.name, Reason: a.outcome.Reason, Workspace: a.workspace, Log: a.log,
			})
		}
	}
	h.verdict = &HandledView{
		Issue: h.issue.Clone(), Stage: stage.Name, To: stage.OnSuccess, Taken: h.taken, Ended: s.at,
	}
	for _, a := range h.actions {
		h.verdict.Actions = append(h.verdict.Actions, HandledAction{Name: a.name, Spend: a.spend(), PullRequest: a.pr})
	}
	if len(report.Failures) == 0 {
		s.call(h, &call{kind: CallMove, from: stage.MovesTo, to: stage.OnSuccess})
		s.ended(h, stage.OnSuccess, crew.MovePending)
		return
	}
	h.verdict.To, h.verdict.Failures = stage.OnFailure, slices.Clone(report.Failures)
	s.call(h, &call{kind: CallMove, from: stage.MovesTo, to: stage.OnFailure})
	s.call(h, &call{kind: CallReport, report: report})
	s.ended(h, stage.OnFailure, crew.MovePending)
}

// full reports whether every slot is busy, so a listing could take nothing:
// the issues held, in any claim, reach max_parallel_issues, or every queue
// some stage runs in is full (R1, R7, KTD4).
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
// claim, whose stage runs in q (KTD3).
func (m *Model) busy(q int) int {
	held := 0
	for _, h := range m.issues {
		if m.queueOf[h.stage] == q {
			held++
		}
	}
	return held
}

// held returns the held issue keyed key, or nil.
func (m *Model) held(key string) *heldIssue {
	for _, h := range m.issues {
		if h.issue.Key == key {
			return h
		}
	}
	return nil
}

// action returns the named action of the held issue keyed key, when it is in
// one of phases, or nils.
func (m *Model) action(key, name string, phases ...Phase) (*heldIssue, *actionRun) {
	h := m.held(key)
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

// findCall returns the unsettled call with id and its issue, or nils.
func (m *Model) findCall(id CallID) (*heldIssue, *call) {
	for _, h := range m.issues {
		for _, c := range h.calls {
			if c.id == id {
				return h, c
			}
		}
	}
	return nil, nil
}

// release forgets h, keeping its handled entry, which replaces the issue's
// earlier one, when its stage ended.
func (m *Model) release(h *heldIssue) {
	m.issues = slices.DeleteFunc(m.issues, func(x *heldIssue) bool { return x == h })
	if h.verdict == nil {
		return
	}
	m.handled = slices.DeleteFunc(m.handled, func(e HandledView) bool { return e.Issue.Key == h.issue.Key })
	m.handled = append(m.handled, *h.verdict)
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

// settle forgets c, which needs no further attempt.
func (h *heldIssue) settle(c *call) {
	h.calls = slices.DeleteFunc(h.calls, func(x *call) bool { return x == c })
}

// cloneReport copies r, so the copy shares no slice with it.
func cloneReport(r crew.FailureReport) crew.FailureReport {
	r.Failures = slices.Clone(r.Failures)
	return r
}
