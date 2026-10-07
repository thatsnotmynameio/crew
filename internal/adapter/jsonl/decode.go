package jsonl

import (
	"maps"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// decoder returns the run event a line of one type holds, with head h.
type decoder func(l line, h crew.EventHead) crew.RunEvent

// decoders returns the decoders of the journal's line types: those of a
// run's take, stop, worktree and release, and those of its actions and
// route (actionDecoders).
func decoders() map[string]decoder {
	d := map[string]decoder{
		typeRunTaken: line.runTaken,
		typeTakeMoved: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.TakeMoved{EventHead: h, From: l.From, To: l.To}
		},
		typeRunStopped:   func(_ line, h crew.EventHead) crew.RunEvent { return crew.RunStopped{EventHead: h} },
		typeRunOutOfTime: func(_ line, h crew.EventHead) crew.RunEvent { return crew.RunOutOfTime{EventHead: h} },
		typeRunReleased:  func(_ line, h crew.EventHead) crew.RunEvent { return crew.RunReleased{EventHead: h} },
		typeWorkspaceAsked: func(l line, h crew.EventHead) crew.RunEvent {
			e := crew.WorkspaceAsked{EventHead: h}
			if l.Workspace != "" {
				e.Reopen = crew.Some(l.workspace())
			}
			return e
		},
		typeWorkspaceMissing: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.WorkspaceMissing{EventHead: h, Workspace: l.workspace()}
		},
		typeWorkspaceOpened: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.WorkspaceOpened{EventHead: h, Workspace: l.workspace(), Log: l.Log, Resumed: l.Resumed}
		},
		typeRouteChosen: line.routeChosen,
		typeLookupAsked: func(_ line, h crew.EventHead) crew.RunEvent { return crew.RunLookupAsked{EventHead: h} },
		typeLookupDone: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.RunLookupDone{EventHead: h, PullRequest: l.found()}
		},
		typeStepAsked: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.StepAsked{EventHead: h, Step: deref(l.Step)}
		},
		typeStepShellStop: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.StepShellStopAsked{EventHead: h, Step: deref(l.Step)}
		},
		typeStepFunctionStop: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.StepFunctionStopAsked{EventHead: h, Step: deref(l.Step)}
		},
		typeStepEnded: line.stepEnded,
	}
	maps.Copy(d, actionDecoders())
	return d
}

// actionDecoders returns the decoders of the line types of one action.
func actionDecoders() map[string]decoder {
	return map[string]decoder{
		typeSessionAsked: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionSessionAsked{EventHead: h, Action: l.Action}
		},
		typeSessionStarted: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionSessionStarted{
				EventHead: h, Action: l.Action, Bot: crew.Bot{Name: l.Bot}, Login: l.Login, Asks: l.Asks,
			}
		},
		typeSessionStopAsked: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionSessionStopAsked{EventHead: h, Action: l.Action}
		},
		typeSessionEnded: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionSessionEnded{EventHead: h, Action: l.Action, Outcome: l.end().Outcome(), Usage: l.reported()}
		},
		typeShellAsked: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionShellAsked{EventHead: h, Action: l.Action, Bot: crew.Bot{Name: l.Bot}}
		},
		typeShellStopAsked: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionShellStopAsked{EventHead: h, Action: l.Action}
		},
		typeShellEnded: line.shellEnded,
		typeFunctionAsked: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionFunctionAsked{EventHead: h, Action: l.Action, Bot: crew.Bot{Name: l.Bot}}
		},
		typeFunctionStop: func(l line, h crew.EventHead) crew.RunEvent {
			return crew.ActionFunctionStopAsked{EventHead: h, Action: l.Action}
		},
		typeFunctionEnded: line.functionEnded,
		typeActionEnded:   line.ended,
	}
}

// decode returns the run event l holds, with head h, by the decoder of its
// type in decoders, and false when its type has none.
func (l line) decode(decoders map[string]decoder, h crew.EventHead) (crew.RunEvent, bool) {
	d, ok := decoders[l.Type]
	if !ok {
		return nil, false
	}
	return d(l, h), true
}

// workspace returns the worktree l names.
func (l line) workspace() crew.Workspace {
	return crew.Workspace{Name: l.Workspace, Branch: l.Branch}
}

// runTaken returns the RunTaken l holds, with head h; without questions
// when l, a line of an earlier crew, holds none.
func (l line) runTaken(h crew.EventHead) crew.RunEvent {
	e := crew.RunTaken{
		EventHead: h, From: l.From, To: l.To, Start: l.start(),
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
		e.Actions = append(e.Actions, a.Name)
	}
	for _, q := range l.Questions {
		e.Questions = append(e.Questions, crew.Question{Run: q.RuleRun, Action: q.Action, Login: q.Login})
	}
	return e
}

// start returns the start l holds: StartFresh when it holds none, or one
// of a kind crew does not know.
func (l line) start() crew.Start {
	s := l.Start
	if s == nil {
		return crew.StartFresh{}
	}
	var session crew.Optional[crew.LatestSession]
	if s.Session != "" {
		session = crew.Some(crew.LatestSession{Action: s.Session, Bot: crew.Bot{Name: s.Bot}})
	}
	switch s.Kind {
	case startAt:
		return crew.StartAt{
			Workspace: crew.Workspace{Name: s.Workspace, Branch: s.Branch}, Log: s.Log, Action: s.Action,
			Route: s.Route, Reason: crew.NewSessionText(s.Reason), Session: session,
		}
	case startPassedRoute:
		out := crew.StartPassedRoute{Log: s.Log, Session: session}
		if s.Workspace != "" {
			out.Workspace = crew.Some(crew.Workspace{Name: s.Workspace, Branch: s.Branch})
		}
		return out
	case startWithoutAction:
		return crew.StartWithoutAction{Action: s.Action}
	}
	return crew.StartFresh{}
}

// shellEnded returns the ActionShellEnded l holds, with head h.
func (l line) shellEnded(h crew.EventHead) crew.RunEvent {
	outcome := crew.ShellOutcome{Reason: crew.NewShellReason(l.Reason)}
	if l.ExitStatus != nil {
		outcome.Status = crew.Some(*l.ExitStatus)
	}
	return crew.ActionShellEnded{EventHead: h, Action: l.Action, Outcome: outcome}
}

// functionEnded returns the ActionFunctionEnded l holds, with head h: no
// verdict when the line holds none.
func (l line) functionEnded(h crew.EventHead) crew.RunEvent {
	outcome := crew.FunctionOutcome{Reason: crew.NewShellReason(l.Reason), Log: l.Log}
	if l.Verdict != "" {
		outcome.Verdict = crew.Some(l.Verdict)
	}
	return crew.ActionFunctionEnded{EventHead: h, Action: l.Action, Outcome: outcome}
}

// ended returns the ActionEnded l holds, with head h. A target that names
// neither the next action nor a route is left out.
func (l line) ended(h crew.EventHead) crew.RunEvent {
	e := crew.ActionEnded{EventHead: h, Action: l.Action, End: l.end(), Verdict: l.Verdict, Usage: l.reported()}
	if target, err := crew.ParseTarget(l.Target); err == nil {
		e.Target = target
	}
	if l.SessionStarted != nil {
		e.SessionStarted = crew.Some[time.Time](*l.SessionStarted)
	}
	return e
}

// routeChosen returns the RouteChosen l holds, with head h. A step of a
// kind crew does not know is a move.
func (l line) routeChosen(h crew.EventHead) crew.RunEvent {
	e := crew.RouteChosen{EventHead: h, Route: l.Route, Action: l.Action}
	for _, s := range l.Steps {
		kind, _ := named(stepKinds(), s.Kind)
		e.Steps = append(e.Steps, crew.StepPlan{Kind: kind, To: s.To, Shell: s.Shell, Function: s.Function})
	}
	return e
}

// stepEnded returns the StepEnded l holds, with head h: a step given up
// when its outcome is none crew knows, which leaves its route unfinished.
func (l line) stepEnded(h crew.EventHead) crew.RunEvent {
	var o crew.StepOutcome
	switch l.Settled {
	case settledLanded:
		o = crew.StepLanded{}
	case settledRan:
		o = crew.StepRan{Reason: crew.NewShellReason(l.Reason)}
	case settledFailed:
		o = crew.StepFailed{Reason: crew.NewShellReason(l.Reason)}
	case settledDropped:
		o = crew.StepDropped{Reason: l.Reason}
	case settledSkipped:
		o = crew.StepSkipped{}
	case settledStopped:
		o = crew.StepStopped{Reason: crew.NewShellReason(l.Reason)}
	default:
		o = crew.StepGivenUp{Reason: l.Reason}
	}
	return crew.StepEnded{EventHead: h, Step: deref(l.Step), Outcome: o}
}
