package crew

import (
	"reflect"
	"slices"
	"testing"
)

// The decision tables below follow a function through a run: as an action
// in the sequence, with check in place of judge or as the rule's only
// action, and as a step of a route.

// checkEnded is check's function that ended as outcome says at minute n,
// as the run records it.
func checkEnded(n int, outcome FunctionOutcome) RunEvent {
	return ActionFunctionEnded{EventHead: eh(n), Action: "check", Outcome: outcome}
}

// checkEnd is check's end at minute n with j and target.
func checkEnd(n int, j Judged, target Target) RunEvent {
	return ActionEnded{EventHead: eh(n), Action: "check", End: j.End, Verdict: j.Verdict, Target: target}
}

// noVerdict is how a function that returned an error ended.
var noVerdict = FunctionOutcome{Reason: NewShellReason("check failed: no pull request")}

// checkWasStopped is how a function crew stopped ended.
var checkWasStopped = FunctionOutcome{Reason: NewShellReason("check was stopped")}

// functionDecisions decide check, an action of the sequence.
var functionDecisions = []decision{
	{
		name: "sessionEnded: a function after a session runs as its bot once the session went next",
		def:  withCheck, given: checked(inSession()), fact: lfgEnded(succeeded("done"), nil),
		want: checking()[len(inSession()):],
	},
	{
		name: "functionEnded: a declared verdict its on maps ends the run through that route",
		def:  withCheck, given: checking(), fact: functionEnded(6, "check", returned("blocked", "check returned blocked")),
		want: []RunEvent{
			checkEnded(6, returned("blocked", "check returned blocked")),
			checkEnd(6, Judged{Verdict: "blocked", End: EndSucceeded{Reason: NewSessionText("check returned blocked")}},
				ToRoute{Route: "blocked"}),
			chose(6, "blocked", "check"), asked(6, 0),
		},
	},
	{
		name: "functionEnded: passed, the last action, chooses passed",
		def:  withCheck, given: checking(), fact: functionEnded(6, "check", returned(Passed, "check returned passed")),
		want: []RunEvent{
			checkEnded(6, returned(Passed, "check returned passed")),
			passedEnd(6, "check", "check returned passed"),
			chose(6, PassedRoute, "check"), asked(6, 0),
		},
	},
	{
		name: "functionEnded: a function that returned no verdict fails by its function",
		def:  withCheck, given: checking(), fact: functionEnded(6, "check", noVerdict),
		want: []RunEvent{
			checkEnded(6, noVerdict),
			checkEnd(6, failedBy(NewSessionText("check failed: no pull request"), CauseFunction), toFailed),
			chose(6, FailedRoute, "check"), asked(6, 0),
		},
	},
	{
		name: "functionEnded: a verdict it does not declare chooses failed",
		def:  withCheck, given: checking(), fact: functionEnded(6, "check", returned("too-big", "check returned too-big")),
		want: []RunEvent{
			checkEnded(6, returned("too-big", "check returned too-big")),
			checkEnd(6, judgeFunction(checkSpec(), nil, returned("too-big", ""), false), toFailed),
			chose(6, FailedRoute, "check"), asked(6, 0),
		},
	},
	{
		name: "sessionEnded: a function whose text does not render fails without being asked",
		def:  withBadCheck, given: checked(inSession()), fact: lfgEnded(succeeded("done"), nil),
		want: []RunEvent{
			lfgSession(succeeded("done")), lfgPassed(),
			checkEnd(5, failedBy(NewSessionText(checkError()), CauseFunction), toFailed),
			chose(5, FailedRoute, "check"), asked(5, 0),
		},
	},
	{
		name: "take: R25, a rule whose only action is a function asks no workspace and asks its function",
		def:  onlyCheck, finds: true, given: []RunEvent{onlyCheckTake()}, fact: landed(),
		want: []RunEvent{takeMoved(), ActionFunctionAsked{EventHead: eh(1), Action: "check"}},
	},
	{
		name: "take: a rule whose only action is a function ends it without starting after a stop",
		def:  onlyCheck, given: seq([]RunEvent{onlyCheckTake()}, stopped(0)), fact: landed(),
		want: seq([]RunEvent{takeMoved()}, stoppedAt(1, "check")),
	},
	{
		name: "functionEnded: a rule whose only action is a function looks up no pull request",
		def:  onlyCheck, finds: true,
		given: []RunEvent{onlyCheckTake(), takeMoved(), ActionFunctionAsked{EventHead: eh(1), Action: "check"}},
		fact:  functionEnded(2, "check", returned(Passed, "check returned passed")),
		want: []RunEvent{
			checkEnded(2, returned(Passed, "check returned passed")),
			passedEnd(2, "check", "check returned passed"),
			chose(2, PassedRoute, "check"), asked(2, 0),
		},
	},
}

// functionStopDecisions decide a stop and crew's run time being up while
// check runs or before it starts.
var functionStopDecisions = []decision{
	{
		name: "stop: R53, a running function is asked to stop",
		def:  withCheck, given: checking(), fact: StopReached{FactHead: fh(6)},
		want: []RunEvent{RunStopped{EventHead: eh(6)}, ActionFunctionStopAsked{EventHead: eh(6), Action: "check"}},
	},
	{
		name: "functionEnded: a stopped function fails by the stop, and the run chooses failed",
		def:  withCheck, given: seq(checking(), stopped(6)), fact: functionEnded(7, "check", checkWasStopped),
		want: []RunEvent{
			checkEnded(7, checkWasStopped),
			checkEnd(7, failedBy(NewSessionText("check was stopped"), CauseStopped), toFailed),
			chose(7, FailedRoute, "check"), asked(7, 0),
		},
	},
	{
		name: "timeUp: R52, a running function is not asked to stop",
		def:  withCheck, given: checking(), fact: TimeUp{FactHead: fh(6)},
		want: outOfTime(6),
	},
	{
		name: "timeUp: a function that finishes after it keeps the route its verdict chose",
		def:  withCheck, given: seq(checking(), outOfTime(5)),
		fact: functionEnded(6, "check", returned("blocked", "check returned blocked")),
		want: []RunEvent{
			checkEnded(6, returned("blocked", "check returned blocked")),
			checkEnd(6, Judged{Verdict: "blocked", End: EndSucceeded{Reason: NewSessionText("check returned blocked")}},
				ToRoute{Route: "blocked"}),
			chose(6, "blocked", "check"), asked(6, 0),
		},
	},
	{
		name: "timeUp: a function after the session that ran out of time does not start",
		def:  withCheck, given: seq(checked(inSession()), outOfTime(4)), fact: lfgEnded(succeeded("done"), nil),
		want: seq([]RunEvent{lfgSession(succeeded("done")), lfgPassed()}, haltedAt(5, "check", timeUpEnd)),
	},
}

// throughCheck makes the failed route two function steps, then the move.
var throughCheck = failedThrough(checkStep, checkStep, moveFailed)

// badCheckStep is check's call as a route's step, whose text does not
// render for the test issue.
var badCheckStep = FunctionStep{Name: "check", Function: badCheckSpec()}

// functionStepDecisions decide check as a step of the failed route.
var functionStepDecisions = []decision{
	{
		name: "routeChosen: a function step whose text renders is asked",
		def:  throughCheck, given: inSession(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: lfgFailedThrough(checkStep, checkStep, moveFailed)[len(inSession()):],
	},
	{
		name: "stepFunctionEnded: R16, a function step returning blocked failed, and the next step is asked",
		def:  throughCheck, given: lfgFailedThrough(checkStep, checkStep, moveFailed),
		fact: functionStepEnded(6, 0, returned("blocked", "check returned blocked")),
		want: []RunEvent{stepEnded(6, 0, StepFailed{Reason: NewShellReason("check returned blocked")}), asked(6, 1)},
	},
	{
		name: "stepFunctionEnded: a function step returning passed ran",
		def:  throughCheck, given: lfgFailedThrough(checkStep, checkStep, moveFailed),
		fact: functionStepEnded(6, 0, returned(Passed, "check returned passed")),
		want: []RunEvent{stepEnded(6, 0, StepRan{Reason: NewShellReason("check returned passed")}), asked(6, 1)},
	},
	{
		name: "stop: a running function step is asked to stop",
		def:  throughCheck, given: lfgFailedThrough(checkStep, checkStep, moveFailed), fact: StopReached{FactHead: fh(6)},
		want: []RunEvent{RunStopped{EventHead: eh(6)}, StepFunctionStopAsked{EventHead: eh(6), Step: 0}},
	},
	{
		name: "stepFunctionEnded: R53, the stopped function step is recorded stopped, the next one skipped",
		def:  throughCheck, given: seq(lfgFailedThrough(checkStep, checkStep, moveFailed), stopped(6)),
		fact: functionStepEnded(7, 0, returned(Passed, "check was stopped")),
		want: []RunEvent{
			stepEnded(7, 0, StepStopped{Reason: NewShellReason("check was stopped")}),
			stepEnded(7, 1, StepSkipped{}),
			asked(7, 2),
		},
	},
	{
		name: "routeChosen: a function step whose text does not render fails unasked, and the route goes on",
		def:  failedThrough(badCheckStep, moveFailed), given: inSession(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: []RunEvent{
			lfgSession(failedOutcome("gave up")),
			lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), toFailed),
			failedRoute(5, badCheckStep, moveFailed),
			stepEnded(5, 0, StepFailed{Reason: NewShellReason(checkError())}),
			asked(5, 1),
		},
	},
	{
		name: "timeUp: R52, a route's function step still runs",
		def:  throughCheck, given: lfgFailedThrough(checkStep, checkStep, moveFailed), fact: TimeUp{FactHead: fh(6)},
	},
}

// functionStarts are the branches of a run that ended at check, a
// function after a session, which resumes as a shell action does (R22,
// R54, KTD-F6).
func functionStarts() []startCase {
	return []startCase{
		{
			name: "blocked at check, a function after a session: at lfg",
			past: past{take: checkTake(), change: withCheck, facts: slices.Concat(toJudge(), []Fact{
				functionEnded(6, "check", returned("blocked", "check returned blocked")),
			})},
			rule: withCheck, want: atAction("lfg", "blocked", "check returned blocked"),
		},
		{
			name: "blocked at check, which resumes at itself: at check",
			past: past{take: checkTake(), change: checkResumesSelf, facts: slices.Concat(toJudge(), []Fact{
				functionEnded(6, "check", returned("blocked", "check returned blocked")),
			})},
			rule: checkResumesSelf, want: atAction("check", "blocked", "check returned blocked"),
		},
		{
			name: "check returned no verdict: at check",
			past: past{take: checkTake(), change: withCheck, facts: slices.Concat(toJudge(), []Fact{
				functionEnded(6, "check", noVerdict),
			})},
			rule: withCheck, want: atAction("check", FailedRoute, "check failed: no pull request"),
		},
		{
			name: "time up between lfg and check: at check, which never started",
			past: past{take: checkTake(), change: withCheck, facts: slices.Concat(toJudge()[:4], []Fact{
				TimeUp{FactHead: fh(4)}, lfgEnded(succeeded("done"), nil),
			})},
			rule: withCheck, want: atAction("check", FailedRoute, timeUpReason),
		},
		{
			name: "stopped while check ran: at check",
			past: past{take: checkTake(), change: withCheck, facts: slices.Concat(toJudge(), []Fact{
				StopReached{FactHead: fh(6)}, functionEnded(7, "check", returned("blocked", "check was stopped")),
			})},
			rule: withCheck, want: atAction("check", FailedRoute, "check was stopped"),
		},
		{
			name: "crashed during check, after a session: at check",
			past: past{take: checkTake(), change: withCheck, facts: toJudge()},
			rule: withCheck, want: crashedAt("check"),
		},
	}
}

func TestDecideTheFunction(t *testing.T)     { decide(t, functionDecisions) }
func TestDecideTheFunctionStop(t *testing.T) { decide(t, functionStopDecisions) }
func TestDecideTheFunctionStep(t *testing.T) { decide(t, functionStepDecisions) }

func TestAFunctionRunsWithoutAWorkspaceWhenItsRuleNeedsNone(t *testing.T) {
	run := given(t, []RunEvent{onlyCheckTake(), takeMoved(), ActionFunctionAsked{EventHead: eh(1), Action: "check"}})
	a, _ := run.Cursor()
	if _, ok := run.Workspace().Get(); ok || a.State() != (InFunction{Started: at(1)}) || !a.running() {
		t.Errorf("run = %#v, want check running without a workspace", run.Snapshot())
	}
	ended := given(t, seq(checking(), []RunEvent{checkEnded(6, noVerdict)}))
	if a, _ := ended.Action("check"); a.Function() != Some(noVerdict) {
		t.Errorf("check's function = %v, want %v", a.Function(), noVerdict)
	}
}

func TestAFunctionsOutcomeSurvivesASnapshot(t *testing.T) {
	run := given(t, seq(checking(), []RunEvent{checkEnded(6, noVerdict)}))
	restored, err := RestoreRuleRun(run.Snapshot())
	if err != nil || !reflect.DeepEqual(restored.Snapshot(), run.Snapshot()) {
		t.Errorf("RestoreRuleRun = %#v, %v, want %#v", restored.Snapshot(), err, run.Snapshot())
	}
}
