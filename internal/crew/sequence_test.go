package crew

import (
	"reflect"
	"testing"
)

func lfgEnded(outcome Outcome, report VerdictReport) Fact {
	return SessionEnded{FactHead: fh(5), Action: "lfg", Outcome: outcome, Report: report, Usage: usage}
}

func lfgSession(outcome Outcome) RunEvent {
	return ActionSessionEnded{EventHead: eh(5), Action: "lfg", Outcome: outcome, Usage: usage}
}

// lfgEnd is lfg's end at minute 5 with j and target, after its session
// started at minute 4.
func lfgEnd(j Judged, target Target) RunEvent {
	return ActionEnded{
		EventHead: eh(5), Action: "lfg", End: j.End, Verdict: j.Verdict, Target: target,
		SessionStarted: Some(at(4)), Usage: usage,
	}
}

// chose is the run through route at minute n, with action at its cursor
// and the test rule's steps of route.
func chose(n int, route RouteName, action ActionName) RunEvent {
	r, _ := sequence().Rule.Route(route)
	return RouteChosen{EventHead: eh(n), Route: route, Action: action, Steps: plans(r)}
}

// asked is the route's step at index step asked at minute n.
func asked(n, step int) RunEvent { return StepAsked{EventHead: eh(n), Step: step} }

func badLfgPrompt(d RunDefinition) RunDefinition {
	spec, _ := d.Rule.Actions[1].Kind.(SessionSpec)
	spec.Prompt = badPrompt("lfg")
	d.Rule.Actions[1].Kind = spec
	return d
}

// failedToBlocked sends lfg's failed verdict to the route blocked.
func failedToBlocked(d RunDefinition) RunDefinition {
	d.Rule.Actions[1].On = On{Failed: ToRoute{Route: "blocked"}}
	return d
}

// sessionDecisions decide lfg's session.
var sessionDecisions = []decision{
	{
		name:  "sessionStarted: the session runs as its action's bot",
		given: starting(), fact: SessionStarted{FactHead: fh(4), Action: "lfg"},
		want: []RunEvent{ActionSessionStarted{EventHead: eh(4), Action: "lfg", Bot: developer}},
	},
	{
		name:  "sessionStarted: a session that starts after a stop is asked to stop",
		given: seq(starting(), stopped(3)), fact: SessionStarted{FactHead: fh(4), Action: "lfg"},
		want: []RunEvent{
			ActionSessionStarted{EventHead: eh(4), Action: "lfg", Bot: developer},
			ActionSessionStopAsked{EventHead: eh(4), Action: "lfg"},
		},
	},
	{
		name:  "sessionFailedToStart: the action fails by its start, and the run chooses failed",
		given: starting(), fact: SessionFailedToStart{FactHead: fh(4), Action: "lfg", Reason: NewSessionText("not found")},
		want: []RunEvent{
			ActionEnded{
				EventHead: eh(4), Action: "lfg", Verdict: Failed, Target: toFailed,
				End: EndFailed{Reason: NewSessionText("not found"), Cause: CauseStart},
			},
			chose(4, FailedRoute, "lfg"), asked(4, 0),
		},
	},
	{
		name:  "sessionEnded: a session that succeeded with no report passes, and the next action runs as its bot",
		given: inSession(), fact: lfgEnded(succeeded("done"), NoVerdictReported{}),
		want: []RunEvent{
			lfgSession(succeeded("done")), lfgPassed(),
			ActionShellAsked{EventHead: eh(5), Action: "judge", Bot: developer},
		},
	},
	{
		name:  "sessionEnded: AE3, a reported verdict its on names chooses its route, and judge never runs",
		given: inSession(), fact: lfgEnded(succeeded("done"), VerdictReported{Verdict: "blocked"}),
		want: []RunEvent{
			lfgSession(succeeded("done")),
			lfgEnd(Judged{Verdict: "blocked", End: EndSucceeded{Reason: NewSessionText("done")}}, ToRoute{Route: "blocked"}),
			chose(5, "blocked", "lfg"), asked(5, 0),
		},
	},
	{
		name:  "sessionEnded: AE3, a reported verdict its on does not name chooses failed",
		given: inSession(), fact: lfgEnded(succeeded("done"), VerdictReported{Verdict: "too-big"}),
		want: []RunEvent{
			lfgSession(succeeded("done")),
			lfgEnd(judgeSession(nil, succeeded("done"), VerdictReported{Verdict: "too-big"}, false), toFailed),
			chose(5, FailedRoute, "lfg"), asked(5, 0),
		},
	},
	{
		name:  "sessionEnded: a report with no verdict name chooses failed",
		given: inSession(), fact: lfgEnded(succeeded("done"), VerdictUnreadable{}),
		want: []RunEvent{
			lfgSession(succeeded("done")),
			lfgEnd(judgeSession(nil, succeeded("done"), VerdictUnreadable{}, false), toFailed),
			chose(5, FailedRoute, "lfg"), asked(5, 0),
		},
	},
	{
		name:  "sessionEnded: a failed harness chooses failed whatever the session reported",
		given: inSession(), fact: lfgEnded(failedOutcome("gave up"), VerdictReported{Verdict: "blocked"}),
		want: []RunEvent{
			lfgSession(failedOutcome("gave up")),
			lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), toFailed),
			chose(5, FailedRoute, "lfg"), asked(5, 0),
		},
	},
	{
		name:  "sessionEnded: a stop chooses failed whatever the session reported",
		given: seq(inSession(), stopped(4)), fact: lfgEnded(succeeded("done"), VerdictReported{Verdict: "blocked"}),
		want: []RunEvent{
			lfgSession(succeeded("done")),
			lfgEnd(failedBy(NewSessionText("done"), CauseStopped), toFailed),
			chose(5, FailedRoute, "lfg"), asked(5, 0),
		},
	},
	{
		name:  "sessionEnded: a failed session follows its on's entry for failed",
		given: inSession(), def: failedToBlocked, fact: lfgEnded(failedOutcome("gave up"), nil),
		want: []RunEvent{
			lfgSession(failedOutcome("gave up")),
			lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), ToRoute{Route: "blocked"}),
			chose(5, "blocked", "lfg"), asked(5, 0),
		},
	},
	{
		name:  "sessionEnded: a stop chooses failed even when its on sends failed elsewhere",
		given: seq(inSession(), stopped(4)), def: failedToBlocked, fact: lfgEnded(failedOutcome("killed"), nil),
		want: []RunEvent{
			lfgSession(failedOutcome("killed")),
			lfgEnd(failedBy(NewSessionText("killed"), CauseStopped), toFailed),
			chose(5, FailedRoute, "lfg"), asked(5, 0),
		},
	},
	{
		name:  "sessionEnded: a session that ends before its start was seen keeps no start",
		given: starting(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: []RunEvent{
			lfgSession(failedOutcome("gave up")),
			ActionEnded{
				EventHead: eh(5), Action: "lfg", Verdict: Failed, Target: toFailed, Usage: usage,
				End: EndFailed{Reason: NewSessionText("gave up"), Cause: CauseSession},
			},
			chose(5, FailedRoute, "lfg"), asked(5, 0),
		},
	},
}

func shellEnded(n int, action ActionName, outcome ShellOutcome) Fact {
	return ShellEnded{FactHead: fh(n), Action: action, Outcome: outcome}
}

// shellDecisions decide install's and judge's scripts.
var shellDecisions = []decision{
	{
		name:  "shellEnded: a script that exits 0 passes, and the next action starts",
		given: installing(), fact: shellEnded(3, "install", exited(0, "install passed")),
		want: []RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
			passedEnd(3, "install", "install passed"),
			ActionSessionAsked{EventHead: eh(3), Action: "lfg"},
		},
	},
	{
		name:  "shellEnded: AE1, a script with no on that exits 2 chooses failed, and the next action never starts",
		given: installing(), fact: shellEnded(3, "install", exited(2, "install failed")),
		want: []RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(2, "install failed")},
			ActionEnded{
				EventHead: eh(3), Action: "install", Verdict: Failed, Target: toFailed,
				End: EndFailed{Reason: NewSessionText("install failed"), Cause: CauseShell},
			},
			chose(3, FailedRoute, "install"), asked(3, 0),
		},
	},
	{
		name:  "shellEnded: AE2, an exit status its verdicts name chooses the route its on maps",
		given: judging(), fact: shellEnded(6, "judge", exited(3, "judge: ask")),
		want: []RunEvent{
			ActionShellEnded{EventHead: eh(6), Action: "judge", Outcome: exited(3, "judge: ask")},
			ActionEnded{
				EventHead: eh(6), Action: "judge", Verdict: "needs_person", Target: ToRoute{Route: "needs-person"},
				End: EndSucceeded{Reason: NewSessionText("judge: ask")},
			},
			chose(6, "needs-person", "judge"), asked(6, 0),
		},
	},
	{
		name:  "shellEnded: F1, the last action going next chooses passed",
		given: judging(), fact: shellEnded(6, "judge", exited(0, "judge passed")),
		want: passedAll()[len(judging()):],
	},
	{
		name:  "shellEnded: a stop chooses failed whatever the script exited with",
		given: seq(installing(), stopped(2)), fact: shellEnded(3, "install", exited(0, "install was stopped")),
		want: []RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install was stopped")},
			ActionEnded{
				EventHead: eh(3), Action: "install", Verdict: Failed, Target: toFailed,
				End: EndFailed{Reason: NewSessionText("install was stopped"), Cause: CauseStopped},
			},
			chose(3, FailedRoute, "install"), asked(3, 0),
		},
	},
	{
		name: "shellEnded: a session whose prompt does not render chooses failed when its turn comes",
		def:  badLfgPrompt, given: installing(), fact: shellEnded(3, "install", exited(0, "install passed")),
		want: []RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
			passedEnd(3, "install", "install passed"),
			ActionEnded{
				EventHead: eh(3), Action: "lfg", Verdict: Failed, Target: toFailed,
				End: EndFailed{Reason: renderError("lfg"), Cause: CausePrompt},
			},
			chose(3, FailedRoute, "lfg"), asked(3, 0),
		},
	},
}

func onlyShells(d RunDefinition) RunDefinition {
	d.Rule.Actions = []Action{d.Rule.Actions[0], d.Rule.Actions[2]}
	return d
}

// lookupDecisions decide the lookup of the run's pull requests, once it
// chose its route.
var lookupDecisions = []decision{
	{
		name:  "routeChosen: with lookups, a chosen route asks for the run's pull requests",
		given: judging(), finds: true, fact: shellEnded(6, "judge", exited(0, "judge passed")),
		want: lookingUp()[len(judging()):],
	},
	{
		name:  "routeChosen: a rule without a session asks for none",
		given: installing(), finds: true, def: onlyShells, fact: shellEnded(3, "install", exited(2, "install failed")),
		want: []RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(2, "install failed")},
			ActionEnded{
				EventHead: eh(3), Action: "install", Verdict: Failed, Target: toFailed,
				End: EndFailed{Reason: NewSessionText("install failed"), Cause: CauseShell},
			},
			chose(3, FailedRoute, "install"), asked(3, 0),
		},
	},
	{
		name:  "routeChosen: a run without a workspace asks for none",
		given: seq([]RunEvent{taken()}, stopped(0)), finds: true, fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: seq([]RunEvent{takeMoved()}, stoppedAt(1, "install")),
	},
	{
		name:  "pullRequestLookedUp: a run rebuilt with a lookup before its route asks no step",
		given: append(installing(), RunLookupAsked{EventHead: eh(2)}), finds: true,
		fact: PullRequestLookedUp{FactHead: fh(3), PullRequest: foundPR},
		want: []RunEvent{RunLookupDone{EventHead: eh(3), PullRequest: foundPR}},
	},
	{
		name:  "pullRequestLookedUp: the route keeps what the lookup found, and asks its first step",
		given: lookingUp(), finds: true,
		fact: PullRequestLookedUp{FactHead: fh(7), PullRequest: foundPR},
		want: []RunEvent{RunLookupDone{EventHead: eh(7), PullRequest: foundPR}, asked(7, 0)},
	},
}

func TestDecideTheSession(t *testing.T) { decide(t, sessionDecisions) }
func TestDecideTheShell(t *testing.T)   { decide(t, shellDecisions) }
func TestDecideTheLookup(t *testing.T)  { decide(t, lookupDecisions) }

// step is one fact of a sequence walk and the action it should start.
type step struct {
	fact   Fact
	starts []ActionName
}

func TestF1ASequenceRunsItsActionsOneAtATimeInOneWorkspace(t *testing.T) {
	run := given(t, []RunEvent{taken()})
	steps := []step{
		{fact: TakeSettled{FactHead: fh(1), Landed: true}},
		{fact: ready(false), starts: []ActionName{"install"}},
		{fact: shellEnded(3, "install", exited(0, "install passed")), starts: []ActionName{"lfg"}},
		{fact: SessionStarted{FactHead: fh(4), Action: "lfg"}},
		{fact: lfgEnded(succeeded("done"), nil), starts: []ActionName{"judge"}},
		{fact: shellEnded(6, "judge", exited(0, "judge passed"))},
	}
	var workspaces int
	for _, s := range steps {
		var events []RunEvent
		run, events = walk(t, run, s.fact)
		var started []ActionName
		for _, e := range events {
			if _, ok := e.(WorkspaceAsked); ok {
				workspaces++
			}
			started = append(started, startedBy(e)...)
		}
		if !reflect.DeepEqual(started, s.starts) {
			t.Errorf("%T started %v, want %v", s.fact, started, s.starts)
		}
	}
	w, _ := run.Workspace().Get()
	want := RoutingPhase{
		Route: PassedRoute, Chosen: at(6), Steps: []StepPlan{{Kind: StepMove, To: labelDone}}, Asked: true,
	}
	if workspaces != 1 || w.Workspace != runWS() || !reflect.DeepEqual(run.Phase(), want) {
		t.Errorf("workspace asked %d times, workspace %#v, phase %#v; want one workspace and passed",
			workspaces, w, run.Phase())
	}
}

// walk decides fact on run and applies its events one by one, failing the
// test when more than one action runs after any of them.
func walk(t *testing.T, run RuleRun, fact Fact) (RuleRun, []RunEvent) {
	t.Helper()
	events, err := Decide(run, sequence(), fact)
	if err != nil {
		t.Fatalf("Decide(%#v) = %v", fact, err)
	}
	for _, e := range events {
		if run, err = Apply(run, e); err != nil {
			t.Fatalf("Apply(%#v) = %v", e, err)
		}
		if n := runningActions(run); n > 1 {
			t.Fatalf("after %#v, %d actions run, want at most one", e, n)
		}
	}
	return run, events
}

// startedBy returns the action e starts, if it starts one.
func startedBy(e RunEvent) []ActionName {
	if asked, ok := e.(ActionSessionAsked); ok {
		return []ActionName{asked.Action}
	}
	if asked, ok := e.(ActionShellAsked); ok {
		return []ActionName{asked.Action}
	}
	return nil
}

// runningActions counts the actions of run whose session or script runs.
func runningActions(run RuleRun) int {
	n := 0
	for _, a := range run.Actions() {
		if a.running() {
			n++
		}
	}
	return n
}
