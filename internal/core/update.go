package core

import (
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
	switch in := in.(type) {
	case Tick:
		s.tick(in.Said)
	case StopRequested:
		m.requested = true
		s.stop()
	case TimeUp:
		s.timeUp(in.Limit)
	case IssuesListed:
		s.listed(in.Issues)
	case ListFailed:
		m.listing = false
		s.emit(ListingFailed{At: s.at, Reason: in.Reason})
	case CallResult:
		s.callResult(in)
	case StatusResult:
		s.statusResult(in)
	case WorkspaceReady:
		s.workspaceReady(in)
	case WorkspaceFailed:
		if h, a := m.action(in.IssueKey, in.Action, PhaseCreating); a != nil {
			s.end(h, a, crew.Outcome{Reason: in.Reason})
		}
	case SessionStarted:
		s.sessionStarted(in)
	case SessionFailedToStart:
		if h, a := m.action(in.IssueKey, in.Action, PhaseStarting); a != nil {
			s.end(h, a, crew.Outcome{Reason: in.Reason})
		}
	case SessionEnded:
		if h, a := m.action(in.IssueKey, in.Action, PhaseStarting, PhaseRunning); a != nil {
			s.end(h, a, in.Outcome)
		}
	}
	s.windDown()
	if m.Stopped() && !m.stopped {
		m.stopped = true
		s.emit(Stopped{At: s.at})
	}
	return s.cmds, s.events
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

// tick lists issues, unless a listing is outstanding or the run time is up,
// retries the owed calls and statuses that are not in flight (KTD8, KTD5),
// and reports the status of each running issue with what its sessions last
// said (R6).
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
		m.listing = true
		states := make([]crew.State, len(m.stages))
		for i, st := range m.stages {
			states[i] = st.Label
		}
		s.command(ListIssues{States: states})
	}
	for _, h := range m.issues {
		for _, c := range h.calls {
			if c.owed && !c.inFlight {
				s.attempt(h, c)
			}
		}
		if h.claim == ClaimRunning {
			s.running(h)
		}
	}
	s.retryStatuses()
}

// stop starts nothing new from now on, stops the running sessions and gives
// each owed call not in flight its final try (R9). Issues whose actions have
// all ended are already being judged, so their verdicts go on.
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
			for _, a := range h.actions {
				if a.phase == PhaseRunning {
					s.command(StopSession{IssueKey: h.issue.Key, Action: a.name})
				}
			}
		case ClaimJudging, ClaimOwed:
			for _, c := range h.calls {
				if c.owed && !c.inFlight {
					c.final = true
					s.attempt(h, c)
				}
			}
		}
	}
	s.retryStatuses()
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
// issues: later stages first, then the oldest issue first (KTD8). It takes
// nothing once the run time is up.
func (s *step) listed(issues []crew.Issue) {
	m := s.m
	m.listing = false
	if m.stopping || m.timeUp {
		return
	}
	for _, issue := range issues {
		if len(issue.States) > 1 && m.held(issue.Key) == nil {
			s.emit(IssueSkipped{At: s.at, IssueKey: issue.Key, IssueRef: issue.Ref, States: slices.Clone(issue.States)})
		}
	}
	taken := 0
	for si := len(m.stages) - 1; si >= 0; si-- {
		var candidates []crew.Issue
		for _, issue := range issues {
			if len(issue.States) == 1 && issue.States[0] == m.stages[si].Label {
				candidates = append(candidates, issue)
			}
		}
		slices.SortStableFunc(candidates, func(a, b crew.Issue) int { return a.Created.Compare(b.Created) })
		for _, issue := range candidates {
			if len(m.issues) >= m.maxParallel {
				break
			}
			if m.held(issue.Key) != nil {
				continue
			}
			s.take(si, issue)
			taken++
		}
		for _, issue := range candidates {
			if m.held(issue.Key) == nil {
				s.queued(si, issue)
			}
		}
	}
	s.emit(PollDone{At: s.at, Listed: len(issues), Taken: taken})
}

// take holds issue for stage si and moves it to the stage's moves_to.
func (s *step) take(si int, issue crew.Issue) {
	m := s.m
	stage := m.stages[si]
	h := &heldIssue{issue: issue.Clone(), stage: si, claim: ClaimTaking}
	for _, a := range stage.Actions {
		h.actions = append(h.actions, &actionRun{name: a.Name, prompt: a.Prompt})
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
	}
}

// dropped gives up c, which r answered, and says so on h's status when c is
// the verdict move.
func (s *step) dropped(h *heldIssue, c *call, r CallResult) {
	s.emit(CallDropped{At: s.at, Call: h.describe(c), Result: r.Result, Reason: r.Reason})
	if c.kind == CallMove && !c.take {
		s.ended(h, c.to, crew.MoveDropped)
	}
	h.settle(c)
}

// taken starts h's actions once its take move is done, or, after a stop,
// ends them unstarted so the issue moves to its stage's on_failure (R9).
func (s *step) taken(h *heldIssue, c *call) {
	m := s.m
	s.emit(IssueMoved{At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, From: c.from, To: c.to})
	h.settle(c)
	if m.stopping {
		for _, a := range h.actions {
			s.end(h, a, crew.Outcome{Reason: stoppedReason})
		}
		return
	}
	h.claim = ClaimRunning
	for _, a := range h.actions {
		prompt, err := crew.Action{Name: a.name, Prompt: a.prompt}.Render(h.issue)
		if err != nil {
			s.end(h, a, crew.Outcome{Reason: err.Error()})
			continue
		}
		a.prompt = prompt
		a.phase = PhaseCreating
		s.command(CreateWorkspace{Issue: h.issue.Clone(), Action: a.name})
	}
	if h.claim == ClaimRunning {
		s.running(h)
	}
}

// workspaceReady starts the action's session, or, after a stop, fails the
// action without starting it.
func (s *step) workspaceReady(in WorkspaceReady) {
	h, a := s.m.action(in.IssueKey, in.Action, PhaseCreating)
	if a == nil {
		return
	}
	a.workspace, a.dir, a.branch = in.Workspace, in.Dir, in.Branch
	if s.m.stopping {
		s.end(h, a, crew.Outcome{Reason: stoppedReason})
		return
	}
	a.log = in.Log
	a.phase = PhaseStarting
	s.command(StartSession{IssueKey: h.issue.Key, Action: a.name, Dir: a.dir, Prompt: a.prompt, Log: a.log})
}

// sessionStarted records the action's start time, and stops the session at
// once when a stop arrived while it was starting.
func (s *step) sessionStarted(in SessionStarted) {
	h, a := s.m.action(in.IssueKey, in.Action, PhaseStarting)
	if a == nil {
		return
	}
	a.phase = PhaseRunning
	a.started = s.at
	s.emit(ActionStarted{
		At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, Stage: s.m.stages[h.stage].Name,
		Action: a.name, Workspace: a.workspace, Branch: a.branch, Log: a.log,
	})
	if s.m.stopping {
		s.command(StopSession{IssueKey: h.issue.Key, Action: a.name})
	}
}

// end ends action a of h with outcome, and judges h once every action ended.
func (s *step) end(h *heldIssue, a *actionRun, outcome crew.Outcome) {
	a.phase = PhaseEnded
	a.outcome = outcome
	s.emit(ActionEnded{
		At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, Stage: s.m.stages[h.stage].Name,
		Action: a.name, Outcome: outcome, Workspace: a.workspace, Log: a.log,
	})
	if h.ended() {
		s.judge(h)
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
	if len(report.Failures) == 0 {
		s.call(h, &call{kind: CallMove, from: stage.MovesTo, to: stage.OnSuccess})
		s.ended(h, stage.OnSuccess, crew.MovePending)
		return
	}
	s.call(h, &call{kind: CallMove, from: stage.MovesTo, to: stage.OnFailure})
	s.call(h, &call{kind: CallReport, report: report})
	s.ended(h, stage.OnFailure, crew.MovePending)
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

// release forgets h.
func (m *Model) release(h *heldIssue) {
	m.issues = slices.DeleteFunc(m.issues, func(x *heldIssue) bool { return x == h })
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
