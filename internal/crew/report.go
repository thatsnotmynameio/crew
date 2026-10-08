package crew

import "time"

// Status returns the run's status as it stands at at: running while it
// takes the issue or runs its actions, and ended once it chose its route,
// with the route, the state it moves the issue to or its close, how that
// final step stands, and how each of its steps settled. said holds what
// each running session last said, by action. With showUsage, each ended
// action whose session started carries what it spent and its pull request.
func (r RuleRun) Status(at time.Time, said map[ActionName]Said, showUsage bool) Status {
	return NewStatus(StatusData{
		IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), Rule: r.rule, Progress: r.progress(),
		Actions: r.actionStatuses(said, showUsage), Steps: r.stepStatuses(), Updated: at, Run: r.id,
	})
}

// FailureReport returns the report a report step of the run's route posts,
// once the run chose its route: the rule, the route, and the action at the
// run's cursor, whose verdict ended the sequence, with that verdict, the
// run's workspace and its log (KTD5), and, for the answered rule's check,
// the reason its verdict names (KTD7). A run without actions names no
// action.
func (r RuleRun) FailureReport() (FailureReport, bool) {
	route, routed := r.route()
	if !routed {
		return FailureReport{}, false
	}
	report := FailureReport{IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), Rule: r.rule, Route: route.Route}
	if a, ok := r.Cursor(); ok {
		w, _ := r.Workspace().Get()
		f := ActionFailure{Action: a.name, Workspace: w.Workspace.Name, Log: r.ActionLog(a)}
		if ended, ok := a.state.(Finished); ok {
			f.Verdict = ended.Verdict
			if a.checked {
				f.Reason = failureReason(ended.Verdict)
			}
		}
		report.Failures = []ActionFailure{f}
	}
	return report, true
}

// TakeReport returns the pull request report that follows the run's take
// move to to.
func (r RuleRun) TakeReport(to State) PullRequestReport {
	return NewPullRequestReport(PullRequestReportData{
		ID: r.id.TakeReport(), IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), State: to,
	})
}

// EndingReport returns the pull request report that follows the final move
// or close of the run's route, once it chose a route. It carries how the
// rule ended, unless the rule has no actions: nobody watched anything, so
// there is nothing to tell, and a rule without actions whose route closes
// the issue has no report at all. After a close it carries no state: the
// close took crew's labels off the pull requests (R51). With showUsage, its
// ended actions carry what they spent and their pull requests, as the run's
// status does.
func (r RuleRun) EndingReport(showUsage bool) (PullRequestReport, bool) {
	route, routed := r.route()
	end, ok := route.End()
	if !routed || !ok || end.Kind == StepClose && len(r.actions) == 0 {
		return PullRequestReport{}, false
	}
	d := PullRequestReportData{ID: r.id.EndingReport(), IssueID: r.issue.ID(), IssueRef: r.issue.Ref(), State: end.To}
	if len(r.actions) > 0 {
		d.End = Some(NewRuleEnd(r.rule, route.Route, r.actionStatuses(nil, showUsage)))
	}
	return NewPullRequestReport(d), true
}

// progress returns whether the run's status shows it running or ended: it
// ended once it chose its route, and the final step's outcome says how its
// move or close stands. A route that closes the issue moves it to no state.
func (r RuleRun) progress() StatusProgress {
	route, ok := r.route()
	if !ok {
		return StatusRunning{}
	}
	end, _ := route.End()
	move := MovePending
	if final, settled := route.Final(); settled {
		move = moveProgress(final)
	}
	return StatusEnded{Route: route.Route, To: end.To, Move: move}
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

// stepStatuses returns the steps of the run's route, each with how it
// settled, once it chose a route; nil before.
func (r RuleRun) stepStatuses() []StepStatus {
	route, ok := r.route()
	if !ok {
		return nil
	}
	out := make([]StepStatus, 0, len(route.Steps))
	for i, step := range route.Steps {
		s := StepStatus{Step: step}
		if i < len(route.Settled) {
			s.Outcome = route.Settled[i]
		}
		out = append(out, s)
	}
	return out
}

// actionStatuses returns the run's actions as a status shows them: each
// one's state, and a shell action's line once its script ended, or a
// function action's once its function ended. A session's or a tool's own
// words never go with them. When the run resumed, each action that ran in
// it names its workspace.
func (r RuleRun) actionStatuses(said map[ActionName]Said, showUsage bool) []ActionStatus {
	out := make([]ActionStatus, 0, len(r.actions))
	w, _ := r.Workspace().Get()
	for i, a := range r.actions {
		s := ActionStatus{Name: a.name, State: r.actionState(i, said[a.name], showUsage)}
		if o, ok := a.shell.Get(); ok {
			s.Shell = o.Reason
		}
		if o, ok := a.function.Get(); ok {
			s.Shell = o.Reason
		}
		if w.Resumed && ran(s.State) {
			s.Workspace = w.Workspace.Name
		}
		out = append(out, s)
	}
	return out
}

// ran reports whether an action that stands at state ran in its run.
func ran(state ActionState) bool {
	switch state.(type) {
	case ActionRunning, ActionSucceeded, ActionFailed:
		return true
	case ActionPending, ActionAwaitingTurn, ActionNotRun, ActionDoneInEarlierRun:
	}
	return false
}

// actionState returns how the action at index i stands in a status. A
// session, a script, a function or a read of the item's comments that
// runs is running. The action at the
// cursor that has none running yet is pending, and those after it await
// their turn. A failed action carries its cause and the run's log.
func (r RuleRun) actionState(i int, said Said, showUsage bool) ActionState {
	a := r.actions[i]
	switch s := a.state.(type) {
	case InSession:
		started, _ := a.session.Get()
		return ActionRunning{Started: started, Said: said}
	case InShell:
		return ActionRunning{Started: s.Started}
	case InFunction:
		return ActionRunning{Started: s.Started}
	case InReturnCheck:
		return ActionRunning{Started: s.Started}
	case Finished:
		return r.endState(a, s, showUsage)
	case DoneInEarlierRun:
		return ActionDoneInEarlierRun{}
	case NotRun:
		return ActionNotRun{}
	case AwaitingTurn:
		if i != r.cursor {
			return ActionAwaitingTurn{}
		}
	case StartingSession:
	}
	return ActionPending{}
}

// endState returns how a, which ended as f says, stands in a status: with
// showUsage and a session that started, with what it spent and the run's
// pull request.
func (r RuleRun) endState(a ActionRun, f Finished, showUsage bool) ActionState {
	var usage Optional[ShownUsage]
	if _, started := a.session.Get(); showUsage && started {
		usage = Some(ShownUsage{Spend: a.Spend(), PullRequest: r.PullRequest()})
	}
	if failed, ok := f.End.(EndFailed); ok {
		return ActionFailed{Cause: failed.Cause, Log: r.ActionLog(a), Usage: usage}
	}
	return ActionSucceeded{Verdict: f.Verdict, Usage: usage}
}

// ActionLog returns the log a wrote into: its run's, or for a run without
// a workspace, such as one whose actions are all functions, the log its
// function wrote into; empty when there is none.
func (r RuleRun) ActionLog(a ActionRun) string {
	if w, ok := r.Workspace().Get(); ok && w.Log != "" {
		return w.Log
	}
	if f, ok := a.function.Get(); ok {
		return f.Log
	}
	return ""
}
