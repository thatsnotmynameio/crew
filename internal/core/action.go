package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// actionInput applies an input about one action's workspace, session or
// check.
func (s *step) actionInput(in Input) {
	m := s.m
	switch in := in.(type) {
	case WorkspaceReady:
		s.workspaceReady(in)
	case WorkspaceFailed:
		if h, a := m.action(in.IssueKey, in.Action, PhaseCreating, PhaseReopening); a != nil {
			s.end(h, a, crew.Outcome{Reason: in.Reason}, crew.CauseWorkspace)
		}
	case WorkspaceGone:
		s.workspaceGone(in)
	case SessionStarted:
		s.sessionStarted(in)
	case SessionFailedToStart:
		if h, a := m.action(in.IssueKey, in.Action, PhaseStarting); a != nil {
			s.end(h, a, crew.Outcome{Reason: in.Reason}, crew.CauseStart)
		}
	case SessionEnded:
		s.sessionEnded(in)
	case CheckEnded:
		s.checkEnded(in)
	case PullRequestFound:
		s.pullRequestFound(in)
	}
}

// workspaceGone creates a fresh workspace for an action whose failed run's
// workspace no longer exists (R4), or, after a stop, fails the action
// without one: its end is written without a workspace and not remembered, so
// the failed run stays resumable.
func (s *step) workspaceGone(in WorkspaceGone) {
	h, a := s.m.action(in.IssueKey, in.Action, PhaseReopening)
	if a == nil {
		return
	}
	s.emit(WorkspaceMissing{
		At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, Rule: s.m.rules[h.rule].Name,
		Action: a.name, Workspace: a.prev.Workspace,
	})
	if s.m.stopping {
		s.end(h, a, stopped(), crew.CauseStopped)
		return
	}
	a.prev = nil
	a.phase = PhaseCreating
	s.command(CreateWorkspace{Issue: h.issue.Clone(), Action: a.name})
}

// workspaceReady records the run's start and starts the action's session,
// with the resume paragraph after its prompt when the workspace is a failed
// run's (R5), or, after a stop, fails the action without starting it.
func (s *step) workspaceReady(in WorkspaceReady) {
	m := s.m
	h, a := m.action(in.IssueKey, in.Action, PhaseCreating, PhaseReopening)
	if a == nil {
		return
	}
	a.workspace, a.dir, a.branch = in.Workspace, in.Dir, in.Branch
	if !in.Resumed {
		a.since = in.At
	}
	if !m.stopping {
		// A session that never starts writes no log, so a stopped action
		// names none.
		a.log = in.Log
	}
	a.prev = nil
	if prev, ok := m.lastRun(h, a); ok {
		a.prev = &prev
	}
	s.record(h, a, RunStarted)
	if m.stopping {
		s.end(h, a, stopped(), crew.CauseStopped)
		return
	}
	if in.Resumed && a.prev != nil {
		a.resumed = true
		a.prompt += "\n\n" + resumeParagraph(*a.prev, a.branch, a.log, in.LogFromDir)
	}
	a.phase = PhaseStarting
	s.command(StartSession{
		IssueKey: h.issue.Key, Action: a.name, Dir: a.dir, Prompt: a.prompt, Log: a.log, Resumed: a.resumed,
		Agent: a.agent, Bot: a.bot,
	})
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
		At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, Rule: s.m.rules[h.rule].Name,
		Action: a.name, Workspace: a.workspace, Branch: a.branch, Log: a.log, Resumed: a.resumed,
	})
	if s.m.stopping {
		s.command(StopSession{IssueKey: h.issue.Key, Action: a.name})
	}
}

// sessionEnded ends the action whose session ended, or, when the session
// succeeded and the action has checks, runs the first (R2, R3). After a
// stop, no check is started and the action counts as stopped (R8). It
// keeps what the session used and its last message, and looks up the pull
// request the action opened, whatever its outcome (R5, KTD3).
func (s *step) sessionEnded(in SessionEnded) {
	h, a := s.m.action(in.IssueKey, in.Action, PhaseStarting, PhaseRunning)
	if a == nil {
		return
	}
	a.usage, a.lastMessage = in.Usage, in.LastMessage
	if s.m.finding {
		a.finding = true
		s.command(FindPullRequest{IssueKey: h.issue.Key, Action: a.name, Branch: a.branch, Since: a.since})
	}
	cause := crew.CauseSession
	if s.m.stopping {
		cause = crew.CauseStopped
	}
	switch {
	case !in.Outcome.Succeeded || len(a.checks) == 0:
		s.end(h, a, in.Outcome, cause)
	case s.m.stopping:
		s.end(h, a, stopped(), crew.CauseStopped)
	default:
		a.phase = PhaseChecking
		s.runCheck(h, a)
	}
}

// runCheck runs a's next check, checks[len(results)].
func (s *step) runCheck(h *heldIssue, a *actionRun) {
	c := a.checks[len(a.results)]
	s.command(RunCheck{
		IssueKey: h.issue.Key, Action: a.name, Dir: a.dir, Name: c.Name, Command: c.Script, Log: a.log,
		IssueRef: h.issue.Ref, IssueURL: h.issue.URL, Branch: a.branch, Bot: a.bot,
		Prompt: a.prompt, LastMessage: a.lastMessage,
	})
}

// checkEnded keeps how the action's running check ended. A check that
// passed starts the next, or ends the action as succeeded when it was the
// last; one that did not pass ends it as failed (R4). Either way the
// action's reason is the check's (KTD7). A stop ends it as stopped, whatever
// the check returned (R8).
func (s *step) checkEnded(in CheckEnded) {
	h, a := s.m.action(in.IssueKey, in.Action, PhaseChecking)
	if a == nil {
		return
	}
	a.results = append(a.results, crew.CheckResult{
		Name: a.checks[len(a.results)].Name, Passed: in.Passed, Reason: in.Reason,
	})
	switch {
	case a.stopped:
		s.end(h, a, stopped(), crew.CauseStopped)
	case !in.Passed || len(a.results) == len(a.checks):
		outcome := crew.Outcome{Succeeded: in.Passed, Reason: crew.NewSessionText(in.Reason.String())}
		s.end(h, a, outcome, crew.CauseCheck)
	default:
		s.runCheck(h, a)
	}
}

// pullRequestFound keeps the pull request the lookup found, and ends the
// action when its outcome was waiting for it (KTD3).
func (s *step) pullRequestFound(in PullRequestFound) {
	h, a := s.m.action(in.IssueKey, in.Action, PhaseChecking, PhaseFinishing)
	if a == nil || !a.finding {
		return
	}
	a.finding, a.pr = false, in.PullRequest
	if a.phase == PhaseFinishing {
		s.end(h, a, a.outcome, a.cause)
	}
}

// end ends action a of h with outcome, records the end of its run, and
// judges h once every action ended. cause says what made it fail when the
// outcome is a failure. While its pull request is being looked up, the
// action waits in PhaseFinishing instead, and the lookup's result ends it.
func (s *step) end(h *heldIssue, a *actionRun, outcome crew.Outcome, cause crew.FailureCause) {
	a.outcome = outcome
	if !outcome.Succeeded {
		a.cause = cause
	}
	if a.finding {
		a.phase = PhaseFinishing
		return
	}
	a.phase = PhaseEnded
	s.m.spent = s.m.spent.Add(a.spend())
	s.m.bots.credit(s.m.bots.identity(a.bot), a.spend())
	s.record(h, a, RunEnded)
	s.emit(ActionEnded{
		At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, Rule: s.m.rules[h.rule].Name,
		Action: a.name, Outcome: outcome, Workspace: a.workspace, Log: a.log,
	})
	if h.ended() {
		s.judge(h)
	}
}
