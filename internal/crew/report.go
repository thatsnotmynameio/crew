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
	case TakingPhase, RunningPhase:
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
	case TakingPhase, RunningPhase:
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
// one's state and how its checks that ran so far ended. Only a check's
// reason goes with them: a session's or a tool's own words never do. An
// action that resumed also names its workspace.
func (r RuleRun) actionStatuses(said map[ActionName]Said, showUsage bool) []ActionStatus {
	out := make([]ActionStatus, 0, len(r.actions))
	for _, a := range r.actions {
		s := ActionStatus{Name: a.name, State: a.status(said[a.name], showUsage), Checks: a.Checks()}
		if w, ok := a.workspace.Get(); ok && w.Resumed {
			s.Workspace = w.Workspace.Name
		}
		out = append(out, s)
	}
	return out
}

// status returns how a stands in a status. An action whose check runs is
// still running, since its session started; its session's last words are
// no longer current. A failed action carries its cause and log.
func (a ActionRun) status(said Said, showUsage bool) ActionState {
	started, _ := a.session.Get()
	switch s := a.state.(type) {
	case InSession:
		return ActionRunning{Started: started, Said: said}
	case InChecks:
		return ActionRunning{Started: started}
	case Finished:
		return a.endState(s.End, showUsage)
	case AwaitingTake, CreatingWorkspace, ReopeningWorkspace, StartingSession, Finishing:
		// No session runs: the action has no start time to report.
	}
	return ActionPending{}
}

// endState returns how a, which ended with end, stands in a status: with
// showUsage and a session that started, with what it spent and its pull
// request.
func (a ActionRun) endState(end ActionEnd, showUsage bool) ActionState {
	var usage Optional[ShownUsage]
	if _, started := a.session.Get(); showUsage && started {
		usage = Some(ShownUsage{Spend: a.Spend(), PullRequest: a.PullRequest()})
	}
	if failed, ok := end.(EndFailed); ok {
		w, _ := a.workspace.Get()
		return ActionFailed{Cause: failed.Cause, Log: w.Log, Usage: usage}
	}
	return ActionSucceeded{Usage: usage}
}
