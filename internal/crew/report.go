package crew

import "time"

// Status returns the run's status as it stands at at: running while it
// takes the issue or runs its actions, and ended once it chose its route,
// with the state its route moves the issue to and how that final move
// stands. said holds what each running session last said, by action. With
// showUsage, each ended action whose session started carries what it spent
// and its pull request.
func (r RuleRun) Status(at time.Time, said map[ActionName]Said, showUsage bool) Status {
	return NewStatus(StatusData{
		IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), Rule: r.rule, Progress: r.progress(),
		Actions: r.actionStatuses(said, showUsage), Updated: at, Run: r.id,
	})
}

// FailureReport returns the report a report step of the run's route posts,
// once the run chose its route: it names the action at the run's cursor,
// whose verdict ended the sequence, with the run's workspace and log. A run
// without actions has none.
func (r RuleRun) FailureReport() (FailureReport, bool) {
	a, ok := r.Cursor()
	if _, routed := r.route(); !routed || !ok {
		return FailureReport{}, false
	}
	w, _ := r.Workspace().Get()
	return FailureReport{
		IssueID: r.issue.ID(), IssueRef: r.issue.Ref(),
		Failures: []ActionFailure{{Action: a.name, Workspace: w.Workspace.Name, Log: w.Log}},
	}, true
}

// TakeReport returns the pull request report that follows the run's take
// move to to.
func (r RuleRun) TakeReport(to State) PullRequestReport {
	return NewPullRequestReport(PullRequestReportData{
		ID: r.id.TakeReport(), IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), State: to,
	})
}

// EndingReport returns the pull request report that follows the final move
// of the run's route, once it chose a route that ends with a move. It
// carries how the rule ended, unless the rule has no actions: nobody
// watched anything, so there is nothing to tell. With showUsage, its ended
// actions carry what they spent and their pull requests, as the run's
// status does.
func (r RuleRun) EndingReport(showUsage bool) (PullRequestReport, bool) {
	to, ok := r.endingMove()
	if !ok {
		return PullRequestReport{}, false
	}
	d := PullRequestReportData{ID: r.id.EndingReport(), IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), State: to}
	if len(r.actions) > 0 {
		d.End = Some(NewRuleEnd(r.rule, r.actionStatuses(nil, showUsage)))
	}
	return NewPullRequestReport(d), true
}

// endingMove returns the state the final move of the run's route moves the
// issue to, once it chose a route that ends with a move.
func (r RuleRun) endingMove() (State, bool) {
	route, _ := r.route()
	if end, ok := route.End(); ok && end.Kind == StepMove {
		return end.To, true
	}
	return "", false
}

// progress returns whether the run's status shows it running or ended: it
// ended once it chose its route, and the final step's outcome says how its
// move stands. A route that closes the issue moves it to no state.
func (r RuleRun) progress() StatusProgress {
	route, ok := r.route()
	if !ok {
		return StatusRunning{}
	}
	to, _ := r.endingMove()
	move := MovePending
	if final, settled := route.Final(); settled {
		move = moveProgress(final)
	}
	return StatusEnded{To: to, Move: move}
}

// moveProgress returns how a final step that settled as o stands.
func moveProgress(o StepOutcome) MoveProgress {
	switch o.(type) {
	case StepLanded:
		return MoveDone
	case StepGivenUp, StepDropped, StepRan, StepFailed, StepSkipped, StepStopped:
	}
	return MoveDropped
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
