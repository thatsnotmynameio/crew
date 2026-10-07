package jsonl

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// decode returns the run event l, a version 2 line, holds, with head h,
// and false when its type is none crew knows. A line about one action goes
// to decodeAction.
func (l line) decode(h crew.EventHead) (crew.RunEvent, bool) {
	switch l.Type {
	case typeRunTaken:
		return l.runTaken(h), true
	case typeTakeMoved:
		return crew.TakeMoved{EventHead: h, From: l.From, To: l.To}, true
	case typeRunStopped:
		return crew.RunStopped{EventHead: h}, true
	case typeRunJudged:
		verdict := crew.Verdict{To: l.To}
		for _, f := range l.Failures {
			verdict.Failures = append(verdict.Failures, crew.ActionFailure{
				Action: f.Action, Workspace: f.Workspace, Log: f.Log,
			})
		}
		return crew.RunJudged{EventHead: h, Verdict: verdict}, true
	case typeVerdictMoved:
		return crew.VerdictMoved{EventHead: h, From: l.From, To: l.To}, true
	case typeVerdictDropped:
		return crew.VerdictDropped{EventHead: h, To: l.To, Reason: l.Reason}, true
	case typeFailureReported:
		return crew.FailureReported{EventHead: h}, true
	case typeFailureReportDropped:
		return crew.FailureReportDropped{EventHead: h}, true
	case typeRunReleased:
		return crew.RunReleased{EventHead: h}, true
	}
	return l.decodeAction(h)
}

// runTaken returns the RunTaken l holds, with head h.
func (l line) runTaken(h crew.EventHead) crew.RunTaken {
	e := crew.RunTaken{
		EventHead: h, From: l.From, To: l.To,
		Issue: crew.IssueData{
			ID: h.IssueID, Ref: h.IssueRef, Title: l.Title, URL: l.URL, Created: deref(l.Created),
			Priority: l.Priority, States: l.States, Blocked: l.Blocked,
		},
	}
	if l.Kind == kindPullRequest {
		e.Issue.Kind = crew.KindPullRequest
	}
	if l.Continues != "" {
		e.Continues = crew.Some(l.Continues)
	}
	for _, a := range l.Actions {
		t := crew.ActionTaken{Name: a.Name}
		if a.Workspace != "" {
			t.Resume = crew.Some(crew.ResumePoint{
				Workspace: crew.Workspace{Name: a.Workspace, Branch: a.Branch}, Log: a.Log,
				Reason: crew.NewSessionText(a.Reason),
			})
		}
		e.Actions = append(e.Actions, t)
	}
	return e
}

// decodeAction returns the run event l, a version 2 line about one action,
// holds, with head h, and false when its type is none crew knows. A line
// of an action's check, lookup or end goes to decodeActionEnd.
func (l line) decodeAction(h crew.EventHead) (crew.RunEvent, bool) {
	workspace := crew.Workspace{Name: l.Workspace, Branch: l.Branch}
	switch l.Type {
	case typeWorkspaceAsked:
		e := crew.ActionWorkspaceAsked{EventHead: h, Action: l.Action}
		if l.Workspace != "" {
			e.Reopen = crew.Some(workspace)
		}
		return e, true
	case typeWorkspaceMissing:
		return crew.WorkspaceMissing{EventHead: h, Action: l.Action, Workspace: workspace}, true
	case typeActionOpened:
		return crew.ActionOpened{EventHead: h, Action: l.Action, Workspace: workspace, Log: l.Log, Resumed: l.Resumed}, true
	case typeSessionAsked:
		return crew.ActionSessionAsked{EventHead: h, Action: l.Action}, true
	case typeSessionStarted:
		return crew.ActionSessionStarted{
			EventHead: h, Action: l.Action, Workspace: workspace, Log: l.Log, Resumed: l.Resumed,
		}, true
	case typeSessionStopAsked:
		return crew.ActionSessionStopAsked{EventHead: h, Action: l.Action}, true
	case typeSessionEnded:
		return crew.ActionSessionEnded{
			EventHead: h, Action: l.Action, Outcome: l.end().Outcome(), Usage: l.reported(),
		}, true
	}
	return l.decodeActionEnd(h)
}

// decodeActionEnd returns the run event l, a version 2 line of an action's
// check, lookup or end, holds, with head h, and false when its type is
// none crew knows.
func (l line) decodeActionEnd(h crew.EventHead) (crew.RunEvent, bool) {
	switch l.Type {
	case typeLookupAsked:
		return crew.ActionLookupAsked{EventHead: h, Action: l.Action}, true
	case typeCheckAsked:
		return crew.ActionCheckAsked{EventHead: h, Action: l.Action, Check: l.Check}, true
	case typeCheckStopAsked:
		return crew.ActionCheckStopAsked{EventHead: h, Action: l.Action}, true
	case typeCheckEnded:
		return crew.ActionCheckEnded{EventHead: h, Action: l.Action, Result: crew.CheckResult{
			Name: l.Check, Passed: deref(l.Passed), Reason: crew.NewCheckReason(l.Reason),
		}}, true
	case typeLookupDone:
		return crew.ActionLookupDone{EventHead: h, Action: l.Action, PullRequest: l.found()}, true
	case typeActionFinishing:
		return crew.ActionFinishing{EventHead: h, Action: l.Action, End: l.end()}, true
	case typeActionEnded:
		return l.ended(h), true
	}
	return nil, false
}

// ended returns the ActionEnded l holds, with head h.
func (l line) ended(h crew.EventHead) crew.ActionEnded {
	e := crew.ActionEnded{
		EventHead: h, Action: l.Action, End: l.end(), Usage: l.reported(), PullRequest: l.found(),
	}
	if l.Workspace != "" {
		e.Workspace = crew.Some(crew.OpenedWorkspace{
			Workspace: crew.Workspace{Name: l.Workspace, Branch: l.Branch}, Log: l.Log, Resumed: l.Resumed,
			Opened: deref(l.Opened),
		})
	}
	if l.SessionStarted != nil {
		e.SessionStarted = crew.Some[time.Time](*l.SessionStarted)
	}
	return e
}
