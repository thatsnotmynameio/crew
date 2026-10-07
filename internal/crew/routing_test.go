package crew

import (
	"reflect"
	"testing"
)

// The decision tables below follow a run through its route, one step at a
// time: each row names the fact it decides and the branch it follows.

// lfgFailed is lfg's session that failed at minute 5, and the run through
// failed, whose report is asked.
func lfgFailed() []RunEvent {
	return seq(inSession(), []RunEvent{
		lfgSession(failedOutcome("gave up")),
		lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), toFailed),
		chose(5, FailedRoute, "lfg"), asked(5, 0),
	})
}

// reported is the failed route's report that landed at minute 6, and its
// move asked.
func reported() []RunEvent {
	return append(lfgFailed(), stepEnded(6, 0, StepLanded{}), asked(6, 1))
}

// releasedAs is the failed route's move that settled as o at minute 7, and
// the run released.
func releasedAs(o StepOutcome) []RunEvent {
	return append(reported(), stepEnded(7, 1, o), RunReleased{EventHead: eh(7)})
}

func stepEnded(n, step int, o StepOutcome) RunEvent {
	return StepEnded{EventHead: eh(n), Step: step, Outcome: o}
}

func settled(n, step int, o StepOutcome) Fact {
	return StepSettled{FactHead: fh(n), Step: step, Outcome: o}
}

func shellStepEnded(n, step int, o ShellOutcome) Fact {
	return StepShellEnded{FactHead: fh(n), Step: step, Outcome: o}
}

// notify is a shell action a route runs as a step.
var notify = ShellStep{Name: "notify", Shell: ShellSpec{Script: "./notify"}}

// failedThrough sets the steps of the test rule's failed route.
func failedThrough(steps ...Step) func(RunDefinition) RunDefinition {
	return func(d RunDefinition) RunDefinition {
		d.Rule.Routes = append([]Route(nil), d.Rule.Routes...)
		d.Rule.Routes[1] = Route{Name: FailedRoute, Steps: steps}
		return d
	}
}

// failedRoute is the run through failed at minute n, with lfg at its
// cursor and steps as its route's steps.
func failedRoute(n int, steps ...Step) RunEvent {
	return RouteChosen{EventHead: eh(n), Route: FailedRoute, Action: "lfg", Steps: plans(Route{Steps: steps})}
}

// lfgFailedThrough is lfg's session that failed at minute 5, and the run
// through a failed route of steps, whose first step is asked.
func lfgFailedThrough(steps ...Step) []RunEvent {
	return seq(inSession(), []RunEvent{
		lfgSession(failedOutcome("gave up")),
		lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), toFailed),
		failedRoute(5, steps...), asked(5, 0),
	})
}

// mustComment returns the parsed comment of the failed route, which must
// parse.
func mustComment(text string) CommentStep {
	t, err := ParseCommentTemplate(FailedRoute, text)
	if err != nil {
		panic(err)
	}
	return CommentStep{Template: t}
}

// badComment renders for the sample issue's title, and fails on the shorter
// "Issue 9".
func badComment() CommentStep { return mustComment("Stuck on {{index .Issue.Title 11}}") }

var moveFailed = MoveStep{To: labelFailed}

// routingDecisions decide the steps of a route.
var routingDecisions = []decision{
	{
		name:  "routeChosen: a route of report then move asks the report alone",
		given: inSession(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: lfgFailed()[len(inSession()):],
	},
	{
		name:  "stepSettled: the report that landed asks the move",
		given: lfgFailed(), fact: settled(6, 0, StepLanded{}),
		want: []RunEvent{stepEnded(6, 0, StepLanded{}), asked(6, 1)},
	},
	{
		name:  "stepSettled: the final move that landed releases the run",
		given: reported(), fact: settled(7, 1, StepLanded{}),
		want: []RunEvent{stepEnded(7, 1, StepLanded{}), RunReleased{EventHead: eh(7)}},
	},
	{
		name:  "stepSettled: a final move given up releases the run with the move given up",
		given: reported(), fact: settled(7, 1, StepGivenUp{Reason: "refused"}),
		want: []RunEvent{stepEnded(7, 1, StepGivenUp{Reason: "refused"}), RunReleased{EventHead: eh(7)}},
	},
	{
		name:  "stepSettled: a final move dropped as the issue moved meanwhile releases the run with that outcome",
		given: reported(), fact: settled(7, 1, StepDropped{Reason: "issue closed"}),
		want: []RunEvent{stepEnded(7, 1, StepDropped{Reason: "issue closed"}), RunReleased{EventHead: eh(7)}},
	},
	{
		name: "stepSettled: a comment given up is recorded, and the route goes on to its move",
		def:  failedThrough(mustComment("Stuck"), moveFailed), given: lfgFailedThrough(mustComment("Stuck"), moveFailed),
		fact: settled(6, 0, StepGivenUp{Reason: "refused"}),
		want: []RunEvent{stepEnded(6, 0, StepGivenUp{Reason: "refused"}), asked(6, 1)},
	},
	{
		name: "stepSettled: a route ending in close releases the run once the close settles",
		def:  failedThrough(ReportStep{}, CloseStep{}),
		given: append(lfgFailedThrough(ReportStep{}, CloseStep{}),
			stepEnded(6, 0, StepLanded{}), asked(6, 1)),
		fact: settled(7, 1, StepLanded{}),
		want: []RunEvent{stepEnded(7, 1, StepLanded{}), RunReleased{EventHead: eh(7)}},
	},
	{
		name: "stepShellEnded: AE7, a failing shell step is recorded, and the move is still asked",
		def:  failedThrough(notify, moveFailed), given: lfgFailedThrough(notify, moveFailed),
		fact: shellStepEnded(6, 0, exited(1, "notify failed: no token")),
		want: []RunEvent{
			stepEnded(6, 0, StepFailed{Reason: NewShellReason("notify failed: no token")}), asked(6, 1),
		},
	},
	{
		name: "stepShellEnded: a shell step that exits 0 ran, and the route goes on",
		def:  failedThrough(notify, moveFailed), given: lfgFailedThrough(notify, moveFailed),
		fact: shellStepEnded(6, 0, exited(0, "notify passed")),
		want: []RunEvent{stepEnded(6, 0, StepRan{Reason: NewShellReason("notify passed")}), asked(6, 1)},
	},
	{
		name: "stepShellEnded: a shell step that did not run to its end failed",
		def:  failedThrough(notify, moveFailed), given: lfgFailedThrough(notify, moveFailed),
		fact: shellStepEnded(6, 0, ShellOutcome{Reason: NewShellReason("notify ran out of time")}),
		want: []RunEvent{
			stepEnded(6, 0, StepFailed{Reason: NewShellReason("notify ran out of time")}), asked(6, 1),
		},
	},
	{
		name:  "routeChosen: a comment that renders is asked",
		def:   failedThrough(mustComment("{{.Action}} ended {{.Verdict}}"), moveFailed),
		given: inSession(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: lfgFailedThrough(mustComment("{{.Action}} ended {{.Verdict}}"), moveFailed)[len(inSession()):],
	},
	{
		name: "routeChosen: a comment that does not render for the run fails unasked, and the route goes on",
		def:  failedThrough(badComment(), moveFailed), given: inSession(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: []RunEvent{
			lfgSession(failedOutcome("gave up")),
			lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), toFailed),
			failedRoute(5, badComment(), moveFailed),
			stepEnded(5, 0, StepFailed{Reason: commentError()}),
			asked(5, 1),
		},
	},
}

// commentError returns the reason a comment that does not render gives.
func commentError() ShellReason {
	_, err := badComment().Template.Render(CommentData{Issue: NewIssue(testIssue())})
	if err == nil {
		panic("the bad comment rendered")
	}
	return NewShellReason(err.Error())
}

func TestDecideTheRoute(t *testing.T) { decide(t, routingDecisions) }

func TestAReleasedRunKeepsItsRouteAndHowItsStepsSettled(t *testing.T) {
	run := given(t, releasedAs(StepGivenUp{Reason: "refused"}))
	phase, ok := run.Phase().(ReleasedPhase)
	route, routed := phase.Route.Get()
	if !ok || !routed || route.Route != FailedRoute {
		t.Fatalf("phase = %#v, want released through failed", run.Phase())
	}
	final, settled := route.Final()
	end, _ := route.End()
	if !settled || final != (StepGivenUp{Reason: "refused"}) || end != (StepPlan{Kind: StepMove, To: labelFailed}) {
		t.Errorf("final step %#v settled as %#v, want the move to failed given up", end, final)
	}
	inFlight, _ := given(t, reported()).Phase().(RoutingPhase)
	if _, settled := inFlight.Final(); settled {
		t.Error("a route whose move is in flight has a final outcome")
	}
	if _, ok := (RoutingPhase{}).End(); ok {
		t.Error("a route without steps has an end")
	}
}

// A route asks its steps in order, one at a time. An event of a step that
// already settled, or past the route, changes nothing; one of a later step
// means the journal lost the ends before it, which settle as not recorded.
func TestARouteStepsInOrderAndOneAtATime(t *testing.T) {
	run := given(t, lfgFailed())
	p, _ := run.Phase().(RoutingPhase)
	want := RoutingPhase{
		Route: FailedRoute, Chosen: at(5), Asked: true,
		Steps: []StepPlan{{Kind: StepReport}, {Kind: StepMove, To: labelFailed}},
	}
	if i, inFlight := p.InFlight(); !reflect.DeepEqual(p, want) || i != 0 || !inFlight {
		t.Errorf("phase = %#v, in flight %d %v, want the report alone in flight", p, i, inFlight)
	}
	reported := want
	reported.Settled, reported.Asked = []StepOutcome{StepLanded{}}, false
	stale := given(t, append(lfgFailed(), stepEnded(6, 0, StepLanded{}), stepEnded(7, 0, StepGivenUp{}), asked(7, 2)))
	if !reflect.DeepEqual(stale.Phase(), reported) {
		t.Errorf("events of settled steps or past the route changed the phase: %#v", stale.Phase())
	}
	lost := want
	lost.Settled, lost.Asked = []StepOutcome{StepGivenUp{Reason: "not recorded"}, StepLanded{}}, false
	if ahead := given(t, append(lfgFailed(), stepEnded(6, 1, StepLanded{}))); !reflect.DeepEqual(ahead.Phase(), lost) {
		t.Errorf("phase after a later step's end = %#v, want the report not recorded and the move landed", ahead.Phase())
	}
}

func TestACommentRendersWhatTheRunKnows(t *testing.T) {
	got := given(t, lfgFailed()).CommentData()
	want := CommentData{
		Issue: NewIssue(testIssue()), Rule: "implement", Action: "lfg", Verdict: Failed, Route: FailedRoute, Log: runLog,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CommentData = %#v, want %#v", got, want)
	}
	bare := given(t, []RunEvent{takenWithoutActions(), takeMoved(), chose(1, PassedRoute, "")}).CommentData()
	if bare.Action != "" || bare.Verdict != "" || bare.Log != "" || bare.Route != PassedRoute {
		t.Errorf("CommentData of a rule without actions = %#v, want only its route", bare)
	}
}

func TestJudgeFunctionStep(t *testing.T) {
	tests := []struct {
		name     string
		outcome  FunctionOutcome
		stopping bool
		want     StepOutcome
	}{
		{"passed ran", returned(Passed, "check returned passed"), false,
			StepRan{Reason: NewShellReason("check returned passed")}},
		{"any other verdict failed", returned("blocked", "check returned blocked"), false,
			StepFailed{Reason: NewShellReason("check returned blocked")}},
		{"no verdict failed", FunctionOutcome{Reason: NewShellReason("check failed")}, false,
			StepFailed{Reason: NewShellReason("check failed")}},
		{"a stop stopped it, whatever it returned", returned(Passed, "check was stopped"), true,
			StepStopped{Reason: NewShellReason("check was stopped")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := judgeFunctionStep(tt.outcome, tt.stopping); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("judgeFunctionStep = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAFunctionStepPlansItsName(t *testing.T) {
	step := FunctionStep{Name: "check", Function: FunctionSpec{Function: "check-pr"}}
	want := []StepPlan{{Kind: StepFunction, Function: "check"}, {Kind: StepShell, Shell: "notify"}}
	if got := plans(Route{Steps: []Step{step, notify}}); !reflect.DeepEqual(got, want) {
		t.Errorf("plans = %#v, want %#v", got, want)
	}
}
