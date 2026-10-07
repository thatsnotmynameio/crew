package jsonl

import "github.com/thatsnotmynameio/crew/internal/crew"

// The types of version 2 lines, one per run event.
const (
	typeRunTaken             = "run_taken"
	typeTakeMoved            = "take_moved"
	typeRunStopped           = "run_stopped"
	typeRunJudged            = "run_judged"
	typeVerdictMoved         = "verdict_moved"
	typeVerdictDropped       = "verdict_dropped"
	typeFailureReported      = "failure_reported"
	typeFailureReportDropped = "failure_report_dropped"
	typeRunReleased          = "run_released"
	typeWorkspaceAsked       = "action_workspace_asked"
	typeWorkspaceMissing     = "workspace_missing"
	typeActionOpened         = "action_opened"
	typeSessionAsked         = "action_session_asked"
	typeSessionStarted       = "action_session_started"
	typeSessionStopAsked     = "action_session_stop_asked"
	typeSessionEnded         = "action_session_ended"
	typeLookupAsked          = "action_lookup_asked"
	typeCheckAsked           = "action_check_asked"
	typeCheckStopAsked       = "action_check_stop_asked"
	typeCheckEnded           = "action_check_ended"
	typeLookupDone           = "action_lookup_done"
	typeActionFinishing      = "action_finishing"
	typeActionEnded          = "action_ended"
)

// encode returns the version 2 line of e, written in the crew process
// run. An event about one action goes to encodeAction.
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
	case crew.RunJudged:
		l := headLine(e.EventHead, typeRunJudged, run)
		l.To = e.Verdict.To
		for _, f := range e.Verdict.Failures {
			l.Failures = append(l.Failures, failure{Action: f.Action, Workspace: f.Workspace, Log: f.Log})
		}
		return l
	case crew.VerdictMoved:
		l := headLine(e.EventHead, typeVerdictMoved, run)
		l.From, l.To = e.From, e.To
		return l
	case crew.VerdictDropped:
		l := headLine(e.EventHead, typeVerdictDropped, run)
		l.To, l.Reason = e.To, e.Reason
		return l
	case crew.FailureReported:
		return headLine(e.EventHead, typeFailureReported, run)
	case crew.FailureReportDropped:
		return headLine(e.EventHead, typeFailureReportDropped, run)
	case crew.RunReleased:
		return headLine(e.EventHead, typeRunReleased, run)
	case crew.ActionWorkspaceAsked, crew.WorkspaceMissing, crew.ActionOpened, crew.ActionSessionAsked,
		crew.ActionSessionStarted, crew.ActionSessionStopAsked, crew.ActionSessionEnded, crew.ActionLookupAsked,
		crew.ActionCheckAsked, crew.ActionCheckStopAsked, crew.ActionCheckEnded, crew.ActionLookupDone,
		crew.ActionFinishing, crew.ActionEnded:
		return encodeAction(e, run)
	}
	return line{}
}

// takenLine returns the line of e: the issue as the rule took it, the run
// it continues, and each action with the resume point it inherited.
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
		t := takenAction{Name: a.Name}
		if p, ok := a.Resume.Get(); ok {
			t.Workspace, t.Branch, t.Log, t.Reason = p.Workspace.Name, p.Workspace.Branch, p.Log, p.Reason.String()
		}
		l.Actions = append(l.Actions, t)
	}
	return l
}

// encodeAction returns the line of e, an event about one action. An event
// of an action's check, lookup or end goes to encodeActionEnd.
func encodeAction(e crew.RunEvent, run string) line {
	switch e := e.(type) {
	case crew.ActionWorkspaceAsked:
		l := actionLine(e.EventHead, typeWorkspaceAsked, run, e.Action)
		if w, ok := e.Reopen.Get(); ok {
			l.Workspace, l.Branch = w.Name, w.Branch
		}
		return l
	case crew.WorkspaceMissing:
		l := actionLine(e.EventHead, typeWorkspaceMissing, run, e.Action)
		l.Workspace, l.Branch = e.Workspace.Name, e.Workspace.Branch
		return l
	case crew.ActionOpened:
		l := actionLine(e.EventHead, typeActionOpened, run, e.Action)
		l.Event = eventStarted
		l.Workspace, l.Branch, l.Log, l.Resumed = e.Workspace.Name, e.Workspace.Branch, e.Log, e.Resumed
		return l
	case crew.ActionSessionAsked:
		return actionLine(e.EventHead, typeSessionAsked, run, e.Action)
	case crew.ActionSessionStarted:
		l := actionLine(e.EventHead, typeSessionStarted, run, e.Action)
		l.Workspace, l.Branch, l.Log, l.Resumed = e.Workspace.Name, e.Workspace.Branch, e.Log, e.Resumed
		return l
	case crew.ActionSessionStopAsked:
		return actionLine(e.EventHead, typeSessionStopAsked, run, e.Action)
	case crew.ActionSessionEnded:
		l := actionLine(e.EventHead, typeSessionEnded, run, e.Action)
		l.outcome = outcome{Succeeded: new(e.Outcome.Succeeded), Reason: e.Outcome.Reason.String()}
		l.usage = usageOf(e.Usage)
		return l
	case crew.ActionLookupAsked, crew.ActionCheckAsked, crew.ActionCheckStopAsked, crew.ActionCheckEnded,
		crew.ActionLookupDone, crew.ActionFinishing, crew.ActionEnded:
		return encodeActionEnd(e, run)
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunJudged, crew.VerdictMoved, crew.VerdictDropped,
		crew.FailureReported, crew.FailureReportDropped, crew.RunReleased:
		// About no one action: encode words them.
	}
	return line{}
}

// encodeActionEnd returns the line of e, an event of an action's check,
// lookup or end.
func encodeActionEnd(e crew.RunEvent, run string) line {
	switch e := e.(type) {
	case crew.ActionLookupAsked:
		return actionLine(e.EventHead, typeLookupAsked, run, e.Action)
	case crew.ActionCheckAsked:
		l := actionLine(e.EventHead, typeCheckAsked, run, e.Action)
		l.Check = e.Check
		return l
	case crew.ActionCheckStopAsked:
		return actionLine(e.EventHead, typeCheckStopAsked, run, e.Action)
	case crew.ActionCheckEnded:
		l := actionLine(e.EventHead, typeCheckEnded, run, e.Action)
		l.Check, l.Passed, l.Reason = e.Result.Name, new(e.Result.Passed), e.Result.Reason.String()
		return l
	case crew.ActionLookupDone:
		l := actionLine(e.EventHead, typeLookupDone, run, e.Action)
		l.pullRequest = pullRequestOf(e.PullRequest)
		return l
	case crew.ActionFinishing:
		l := actionLine(e.EventHead, typeActionFinishing, run, e.Action)
		l.outcome = outcomeOf(e.End)
		return l
	case crew.ActionEnded:
		return endedLine(e, run)
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunJudged, crew.VerdictMoved, crew.VerdictDropped,
		crew.FailureReported, crew.FailureReportDropped, crew.RunReleased, crew.ActionWorkspaceAsked,
		crew.WorkspaceMissing, crew.ActionOpened, crew.ActionSessionAsked, crew.ActionSessionStarted,
		crew.ActionSessionStopAsked, crew.ActionSessionEnded:
		// Not of a check, a lookup or an end: encode and encodeAction word
		// them.
	}
	return line{}
}

// endedLine returns the line of e, an action run's end: its outcome, the
// workspace it worked in, how long its session took, what it used and the
// pull request it opened. It is the boss's cost record, which tools find
// by its event, ended.
func endedLine(e crew.ActionEnded, run string) line {
	l := actionLine(e.EventHead, typeActionEnded, run, e.Action)
	l.Event = eventEnded
	l.outcome = outcomeOf(e.End)
	if w, ok := e.Workspace.Get(); ok {
		l.Workspace, l.Branch, l.Log, l.Resumed = w.Workspace.Name, w.Workspace.Branch, w.Log, w.Resumed
		l.Opened = timeOf(w.Opened)
	}
	if started, ok := e.SessionStarted.Get(); ok {
		l.SessionStarted = timeOf(started)
		l.DurationMS = new(e.At.Sub(started).Milliseconds())
	}
	l.usage = usageOf(e.Usage)
	l.pullRequest = pullRequestOf(e.PullRequest)
	return l
}

// actionLine returns the line of an event with head h about the action
// named action, of type kind, written in the crew process run.
func actionLine(h crew.EventHead, kind, run string, action crew.ActionName) line {
	l := headLine(h, kind, run)
	l.Action = action
	return l
}
