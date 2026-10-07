package crew

import "time"

// Status returns the run's status as it stands at at: running while it
// takes the issue or runs its actions, and ended once it ended, with its
// ending's state and how the ending move stands. said holds what each
// running session last said, by action. With showUsage, each ended action
// whose session started carries what it spent and its pull request.
func (r RuleRun) Status(at time.Time, said map[ActionName]Said, showUsage bool) Status {
	return NewStatus(StatusData{
		IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), Rule: r.rule, Progress: r.progress(),
		Actions: r.actionStatuses(said, showUsage), Updated: at, Run: r.id,
	})
}

// FailureReport returns the report of the run's failed actions, once it
// ended in failure.
func (r RuleRun) FailureReport() (FailureReport, bool) {
	v, ok := r.ending()
	if !ok || !v.Failed() {
		return FailureReport{}, false
	}
	return FailureReport{IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), Failures: v.clone().Failures}, true
}

// TakeReport returns the pull request report that follows the run's take
// move to to.
func (r RuleRun) TakeReport(to State) PullRequestReport {
	return NewPullRequestReport(PullRequestReportData{
		ID: r.id.TakeReport(), IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), State: to,
	})
}

// EndingReport returns the pull request report that follows the run's
// ending move, once it ended. It carries how the rule ended, unless
// the rule has no actions: nobody watched anything, so there is nothing to
// tell. With showUsage, its ended actions carry what they spent and their
// pull requests, as the run's status does.
func (r RuleRun) EndingReport(showUsage bool) (PullRequestReport, bool) {
	v, ok := r.ending()
	if !ok {
		return PullRequestReport{}, false
	}
	d := PullRequestReportData{ID: r.id.EndingReport(), IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), State: v.To}
	if len(r.actions) > 0 {
		d.End = Some(NewRuleEnd(r.rule, r.actionStatuses(nil, showUsage)))
	}
	return NewPullRequestReport(d), true
}

// ending returns how the run ended, once it ended.
func (r RuleRun) ending() (RunEnding, bool) {
	switch p := r.phase.(type) {
	case EndingPhase:
		return p.Ending, true
	case ReleasedPhase:
		if v, ok := p.Ending.Get(); ok {
			return v.Ending, true
		}
	case TakingPhase, RunningPhase, RoutingPhase:
	}
	return RunEnding{}, false
}

// progress returns whether the run's status shows it running or ended.
func (r RuleRun) progress() StatusProgress {
	switch p := r.phase.(type) {
	case EndingPhase:
		move := MovePending
		if m, ok := p.Move.Get(); ok {
			move = moveProgress(m)
		}
		return StatusEnded{To: p.Ending.To, Move: move}
	case ReleasedPhase:
		if v, ok := p.Ending.Get(); ok {
			return StatusEnded{To: v.Ending.To, Move: moveProgress(v.Move)}
		}
	case TakingPhase, RunningPhase, RoutingPhase:
	}
	return StatusRunning{}
}

// moveProgress returns how a settled ending move stands.
func moveProgress(m EndingMove) MoveProgress {
	if _, givenUp := m.(EndingGivenUp); givenUp {
		return MoveDropped
	}
	return MoveDone
}

// actionStatuses returns the run's actions as a status shows them: each
// one's state. A session's or a tool's own words never go with them. When
// the run resumed, each action that started names its workspace.
func (r RuleRun) actionStatuses(said map[ActionName]Said, showUsage bool) []ActionStatus {
	out := make([]ActionStatus, 0, len(r.actions))
	w, _ := r.Workspace().Get()
	for _, a := range r.actions {
		s := ActionStatus{Name: a.name, State: r.actionState(a, said[a.name], showUsage)}
		if _, pending := s.State.(ActionPending); w.Resumed && !pending {
			s.Workspace = w.Workspace.Name
		}
		out = append(out, s)
	}
	return out
}

// actionState returns how a stands in a status. A session or a script
// that runs is running; an action that has none running, has not started
// or will not start is pending. A failed action carries its cause and the
// run's log.
func (r RuleRun) actionState(a ActionRun, said Said, showUsage bool) ActionState {
	switch s := a.state.(type) {
	case InSession:
		started, _ := a.session.Get()
		return ActionRunning{Started: started, Said: said}
	case InShell:
		return ActionRunning{Started: s.Started}
	case Finished:
		return r.endState(a, s.End, showUsage)
	case AwaitingTurn, DoneInEarlierRun, StartingSession, NotRun:
		// No session or script runs: the action has no start time to
		// report.
	}
	return ActionPending{}
}

// endState returns how a, which ended with end, stands in a status: with
// showUsage and a session that started, with what it spent and the run's
// pull request.
func (r RuleRun) endState(a ActionRun, end ActionEnd, showUsage bool) ActionState {
	var usage Optional[ShownUsage]
	if _, started := a.session.Get(); showUsage && started {
		usage = Some(ShownUsage{Spend: a.Spend(), PullRequest: r.PullRequest()})
	}
	if failed, ok := end.(EndFailed); ok {
		w, _ := r.Workspace().Get()
		return ActionFailed{Cause: failed.Cause, Log: w.Log, Usage: usage}
	}
	return ActionSucceeded{Usage: usage}
}
