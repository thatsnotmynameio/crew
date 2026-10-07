package jsonl

import "github.com/thatsnotmynameio/crew/internal/crew"

// The types of the journal's lines, one per run event.
const (
	typeRunTaken         = "run_taken"
	typeTakeMoved        = "take_moved"
	typeRunStopped       = "run_stopped"
	typeRunOutOfTime     = "run_out_of_time"
	typeRunReleased      = "run_released"
	typeWorkspaceAsked   = "workspace_asked"
	typeWorkspaceMissing = "workspace_missing"
	typeWorkspaceOpened  = "workspace_opened"
	typeSessionAsked     = "action_session_asked"
	typeSessionStarted   = "action_session_started"
	typeSessionStopAsked = "action_session_stop_asked"
	typeSessionEnded     = "action_session_ended"
	typeShellAsked       = "action_shell_asked"
	typeShellStopAsked   = "action_shell_stop_asked"
	typeShellEnded       = "action_shell_ended"
	typeFunctionAsked    = "action_function_asked"
	typeFunctionStop     = "action_function_stop_asked"
	typeFunctionEnded    = "action_function_ended"
	typeActionEnded      = "action_ended"
	typeRouteChosen      = "route_chosen"
	typeLookupAsked      = "lookup_asked"
	typeLookupDone       = "lookup_done"
	typeStepAsked        = "step_asked"
	typeStepShellStop    = "step_shell_stop_asked"
	typeStepFunctionStop = "step_function_stop_asked"
	typeStepEnded        = "step_ended"
)

// encode returns the line of e, written in the crew process run. An event
// of the run's worktree, of one action or of its route goes to its own
// family's encoder.
func encode(e crew.RunEvent, run string) line {
	switch e := e.(type) {
	case crew.RunTaken:
		return takenLine(e, run)
	case crew.TakeMoved:
		l := headLine(e.EventHead, typeTakeMoved, run)
		l.From, l.To = e.From, e.To
		return l
	case crew.RunStopped:
		return headLine(e.EventHead, typeRunStopped, run)
	case crew.RunOutOfTime:
		return headLine(e.EventHead, typeRunOutOfTime, run)
	case crew.RunReleased:
		return headLine(e.EventHead, typeRunReleased, run)
	case crew.WorkspaceAsked, crew.WorkspaceMissing, crew.WorkspaceOpened:
		return encodeWorkspace(e, run)
	case crew.ActionSessionAsked, crew.ActionSessionStarted, crew.ActionSessionStopAsked, crew.ActionSessionEnded,
		crew.ActionShellAsked, crew.ActionShellStopAsked, crew.ActionShellEnded, crew.ActionEnded:
		return encodeAction(e, run)
	case crew.ActionFunctionAsked, crew.ActionFunctionStopAsked, crew.ActionFunctionEnded:
		return encodeFunction(e, run)
	case crew.RouteChosen, crew.RunLookupAsked, crew.RunLookupDone, crew.StepAsked, crew.StepShellStopAsked,
		crew.StepFunctionStopAsked, crew.StepEnded:
		return encodeRoute(e, run)
	}
	return line{}
}

// takenLine returns the line of e: the issue as the rule took it, the run
// it continues, its actions, how it starts and the questions it inherits.
func takenLine(e crew.RunTaken, run string) line {
	l := headLine(e.EventHead, typeRunTaken, run)
	l.From, l.To = e.From, e.To
	d := e.Issue
	l.Title, l.URL, l.Priority, l.States, l.Blocked = d.Title, d.URL, d.Priority, d.States, d.Blocked
	if !d.Created.IsZero() {
		l.Created = timeOf(d.Created)
	}
	l.Kind = kindIssue
	if d.Kind == crew.KindPullRequest {
		l.Kind = kindPullRequest
	}
	l.Continues, _ = e.Continues.Get()
	for _, a := range e.Actions {
		l.Actions = append(l.Actions, takenAction{Name: a})
	}
	l.Start = startOf(e.Start)
	for _, q := range e.Questions {
		l.Questions = append(l.Questions, question{RuleRun: q.Run, Action: q.Action, Login: q.Login})
	}
	return l
}

// startOf returns the keys of s; nil counts as StartFresh.
func startOf(s crew.Start) *start {
	switch s := s.(type) {
	case crew.StartAt:
		out := &start{
			Kind: startAt, Workspace: s.Workspace.Name, Branch: s.Workspace.Branch, Log: s.Log, Action: s.Action,
			Route: s.Route, Reason: s.Reason.String(),
		}
		out.Session, out.Bot = sessionOf(s.Session)
		return out
	case crew.StartPassedRoute:
		out := &start{Kind: startPassedRoute, Log: s.Log}
		if w, ok := s.Workspace.Get(); ok {
			out.Workspace, out.Branch = w.Name, w.Branch
		}
		out.Session, out.Bot = sessionOf(s.Session)
		return out
	case crew.StartWithoutAction:
		return &start{Kind: startWithoutAction, Action: s.Action}
	case crew.StartFresh:
	}
	return &start{Kind: startFresh}
}

// sessionOf returns the name and bot of the latest session s holds, both
// empty when it holds none.
func sessionOf(s crew.Optional[crew.LatestSession]) (crew.ActionName, crew.BotName) {
	latest, _ := s.Get()
	return latest.Action, latest.Bot.Name
}

// encodeWorkspace returns the line of e, an event of the run's worktree.
func encodeWorkspace(e crew.RunEvent, run string) line {
	switch e := e.(type) {
	case crew.WorkspaceAsked:
		l := headLine(e.EventHead, typeWorkspaceAsked, run)
		if w, ok := e.Reopen.Get(); ok {
			l.Workspace, l.Branch = w.Name, w.Branch
		}
		return l
	case crew.WorkspaceMissing:
		l := headLine(e.EventHead, typeWorkspaceMissing, run)
		l.Workspace, l.Branch = e.Workspace.Name, e.Workspace.Branch
		return l
	case crew.WorkspaceOpened:
		l := headLine(e.EventHead, typeWorkspaceOpened, run)
		l.Workspace, l.Branch, l.Log, l.Resumed = e.Workspace.Name, e.Workspace.Branch, e.Log, e.Resumed
		return l
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunOutOfTime, crew.RunReleased,
		crew.ActionSessionAsked, crew.ActionSessionStarted, crew.ActionSessionStopAsked, crew.ActionSessionEnded,
		crew.ActionShellAsked, crew.ActionShellStopAsked, crew.ActionShellEnded, crew.ActionFunctionAsked,
		crew.ActionFunctionStopAsked, crew.ActionFunctionEnded, crew.ActionEnded, crew.RouteChosen,
		crew.RunLookupAsked, crew.RunLookupDone, crew.StepAsked, crew.StepShellStopAsked,
		crew.StepFunctionStopAsked, crew.StepEnded:
		// Not of the worktree: encode words them.
	}
	return line{}
}

// encodeAction returns the line of e, an event of one action. An action's
// start, the ask for its session or its script, is marked started, and
// its end ended.
func encodeAction(e crew.RunEvent, run string) line {
	switch e := e.(type) {
	case crew.ActionSessionAsked:
		l := actionLine(e.EventHead, typeSessionAsked, run, e.Action)
		l.Event = eventStarted
		return l
	case crew.ActionSessionStarted:
		l := actionLine(e.EventHead, typeSessionStarted, run, e.Action)
		l.Bot, l.Login, l.Asks = e.Bot.Name, e.Login, e.Asks
		return l
	case crew.ActionSessionStopAsked:
		return actionLine(e.EventHead, typeSessionStopAsked, run, e.Action)
	case crew.ActionSessionEnded:
		l := actionLine(e.EventHead, typeSessionEnded, run, e.Action)
		l.outcome = outcome{Succeeded: new(e.Outcome.Succeeded), Reason: e.Outcome.Reason.String()}
		l.usage = usageOf(e.Usage)
		return l
	case crew.ActionShellAsked:
		l := actionLine(e.EventHead, typeShellAsked, run, e.Action)
		l.Event, l.Bot = eventStarted, e.Bot.Name
		return l
	case crew.ActionShellStopAsked:
		return actionLine(e.EventHead, typeShellStopAsked, run, e.Action)
	case crew.ActionShellEnded:
		l := actionLine(e.EventHead, typeShellEnded, run, e.Action)
		l.Reason = e.Outcome.Reason.String()
		if status, ok := e.Outcome.Status.Get(); ok {
			l.ExitStatus = new(status)
		}
		return l
	case crew.ActionEnded:
		return endedLine(e, run)
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunOutOfTime, crew.RunReleased,
		crew.WorkspaceAsked, crew.WorkspaceMissing, crew.WorkspaceOpened, crew.ActionFunctionAsked,
		crew.ActionFunctionStopAsked, crew.ActionFunctionEnded, crew.RouteChosen, crew.RunLookupAsked,
		crew.RunLookupDone, crew.StepAsked, crew.StepShellStopAsked, crew.StepFunctionStopAsked, crew.StepEnded:
		// Not of a session or shell action: encode words them.
	}
	return line{}
}

// encodeFunction returns the line of e, an event of one function action.
// Its ask is marked started, and its end holds the verdict the function
// returned, left out when it returned none.
func encodeFunction(e crew.RunEvent, run string) line {
	switch e := e.(type) {
	case crew.ActionFunctionAsked:
		l := actionLine(e.EventHead, typeFunctionAsked, run, e.Action)
		l.Event, l.Bot = eventStarted, e.Bot.Name
		return l
	case crew.ActionFunctionStopAsked:
		return actionLine(e.EventHead, typeFunctionStop, run, e.Action)
	case crew.ActionFunctionEnded:
		l := actionLine(e.EventHead, typeFunctionEnded, run, e.Action)
		l.Reason = e.Outcome.Reason.String()
		l.Verdict, _ = e.Outcome.Verdict.Get()
		l.Log = e.Outcome.Log
		return l
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunOutOfTime, crew.RunReleased,
		crew.WorkspaceAsked, crew.WorkspaceMissing, crew.WorkspaceOpened, crew.ActionSessionAsked,
		crew.ActionSessionStarted, crew.ActionSessionStopAsked, crew.ActionSessionEnded, crew.ActionShellAsked,
		crew.ActionShellStopAsked, crew.ActionShellEnded, crew.ActionEnded, crew.RouteChosen, crew.RunLookupAsked,
		crew.RunLookupDone, crew.StepAsked, crew.StepShellStopAsked, crew.StepFunctionStopAsked, crew.StepEnded:
		// Not of a function action: encode words them.
	}
	return line{}
}

// endedLine returns the line of e, an action run's end: its outcome, its
// verdict and where it leads, how long its session took and what it used.
// It is the boss's cost record, which tools find by its event, ended.
func endedLine(e crew.ActionEnded, run string) line {
	l := actionLine(e.EventHead, typeActionEnded, run, e.Action)
	l.Event = eventEnded
	l.outcome = outcomeOf(e.End)
	l.Verdict = e.Verdict
	switch t := e.Target.(type) {
	case crew.Next:
		l.Target = "next"
	case crew.ToRoute:
		l.Target = string(t.Route)
	}
	if started, ok := e.SessionStarted.Get(); ok {
		l.SessionStarted = timeOf(started)
		l.DurationMS = new(e.At.Sub(started).Milliseconds())
	}
	l.usage = usageOf(e.Usage)
	return l
}

// encodeRoute returns the line of e, an event of the run's route.
func encodeRoute(e crew.RunEvent, run string) line {
	switch e := e.(type) {
	case crew.RouteChosen:
		l := actionLine(e.EventHead, typeRouteChosen, run, e.Action)
		l.Route = e.Route
		for _, s := range e.Steps {
			l.Steps = append(l.Steps, step{Kind: stepKinds()[s.Kind], To: s.To, Shell: s.Shell, Function: s.Function})
		}
		return l
	case crew.RunLookupAsked:
		return headLine(e.EventHead, typeLookupAsked, run)
	case crew.RunLookupDone:
		l := headLine(e.EventHead, typeLookupDone, run)
		l.pullRequest = pullRequestOf(e.PullRequest)
		return l
	case crew.StepAsked:
		return stepLine(e.EventHead, typeStepAsked, run, e.Step)
	case crew.StepShellStopAsked:
		return stepLine(e.EventHead, typeStepShellStop, run, e.Step)
	case crew.StepFunctionStopAsked:
		return stepLine(e.EventHead, typeStepFunctionStop, run, e.Step)
	case crew.StepEnded:
		l := stepLine(e.EventHead, typeStepEnded, run, e.Step)
		l.Settled, l.Reason = settledOf(e.Outcome)
		return l
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunOutOfTime, crew.RunReleased,
		crew.WorkspaceAsked, crew.WorkspaceMissing, crew.WorkspaceOpened, crew.ActionSessionAsked,
		crew.ActionSessionStarted, crew.ActionSessionStopAsked, crew.ActionSessionEnded, crew.ActionShellAsked,
		crew.ActionShellStopAsked, crew.ActionShellEnded, crew.ActionFunctionAsked, crew.ActionFunctionStopAsked,
		crew.ActionFunctionEnded, crew.ActionEnded:
		// Not of the route: encode words them.
	}
	return line{}
}

// The names of the step outcomes on the wire.
const (
	settledLanded  = "landed"
	settledRan     = "ran"
	settledFailed  = "failed"
	settledGivenUp = "given_up"
	settledDropped = "dropped"
	settledSkipped = "skipped"
	settledStopped = "stopped"
)

// settledOf returns the name of o on the wire, then its reason.
func settledOf(o crew.StepOutcome) (string, string) {
	switch o := o.(type) {
	case crew.StepLanded:
		return settledLanded, ""
	case crew.StepRan:
		return settledRan, o.Reason.String()
	case crew.StepFailed:
		return settledFailed, o.Reason.String()
	case crew.StepGivenUp:
		return settledGivenUp, o.Reason
	case crew.StepDropped:
		return settledDropped, o.Reason
	case crew.StepSkipped:
		return settledSkipped, ""
	case crew.StepStopped:
		return settledStopped, o.Reason.String()
	}
	return "", ""
}

// actionLine returns the line of an event with head h about the action
// named action, of type kind, written in the crew process run.
func actionLine(h crew.EventHead, kind, run string, action crew.ActionName) line {
	l := headLine(h, kind, run)
	l.Action = action
	return l
}

// stepLine returns the line of an event with head h about the route's
// step at index i, of type kind, written in the crew process run.
func stepLine(h crew.EventHead, kind, run string, i int) line {
	l := headLine(h, kind, run)
	l.Step = new(i)
	return l
}
