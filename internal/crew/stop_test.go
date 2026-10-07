package crew

import (
	"reflect"
	"testing"
)

// The decision tables below follow a stop and crew's run time being up
// through a run: between its actions they keep the next action from
// starting; in its route a stop skips the shell steps and time-up changes
// nothing.

func outOfTime(n int) []RunEvent { return []RunEvent{RunOutOfTime{EventHead: eh(n)}} }

// timeUpEnd is the end of an action crew's run time kept from starting.
var timeUpEnd = EndFailed{Reason: NewSessionText("crew's run time was up"), Cause: CauseTimeUp}

// haltedAt is the end of action at minute n, which end kept from starting,
// and the run through failed, whose report is asked.
func haltedAt(n int, action ActionName, end EndFailed) []RunEvent {
	return []RunEvent{
		ActionEnded{EventHead: eh(n), Action: action, End: end, Verdict: Failed, Target: toFailed},
		chose(n, FailedRoute, action), asked(n, 0),
	}
}

// notifying is the run through a failed route of two shell steps and a
// move, after lfg failed, whose first shell step runs.
func notifying() []RunEvent { return lfgFailedThrough(notify, notify, moveFailed) }

var throughNotify = failedThrough(notify, notify, moveFailed)

// stopBetweenDecisions decide a stop that reaches a run between or during
// its actions.
var stopBetweenDecisions = []decision{
	{
		name:  "stop: during action 2 of 3 the session is asked to stop",
		given: inSession(), fact: StopReached{FactHead: fh(4)},
		want: []RunEvent{RunStopped{EventHead: eh(4)}, ActionSessionStopAsked{EventHead: eh(4), Action: "lfg"}},
	},
	{
		name:  "stop: action 2 of 3 ends stopped, starts nothing more and chooses failed naming it",
		given: seq(inSession(), stopped(4)), fact: lfgEnded(succeeded("done"), nil),
		want: []RunEvent{
			lfgSession(succeeded("done")),
			lfgEnd(failedBy(NewSessionText("done"), CauseStopped), toFailed),
			chose(5, FailedRoute, "lfg"), asked(5, 0),
		},
	},
	{
		name:  "stop: between actions 1 and 2, action 2 ends at the cursor, not started, stopped before it started",
		given: seq(reopening(), stopped(1)), fact: ready(true),
		want: seq([]RunEvent{WorkspaceOpened{EventHead: eh(2), Workspace: runWS()}}, haltedAt(2, "lfg", unstarted)),
	},
	{
		name:  "stop: after time-up, a stop still stops the running script",
		given: seq(installing(), outOfTime(2)), fact: StopReached{FactHead: fh(3)},
		want: []RunEvent{RunStopped{EventHead: eh(3)}, ActionShellStopAsked{EventHead: eh(3), Action: "install"}},
	},
}

// stopRoutingDecisions decide a stop that reaches a routing run.
var stopRoutingDecisions = []decision{
	{
		name: "stop: a running shell step is asked to stop",
		def:  throughNotify, given: notifying(), fact: StopReached{FactHead: fh(6)},
		want: []RunEvent{RunStopped{EventHead: eh(6)}, StepShellStopAsked{EventHead: eh(6), Step: 0}},
	},
	{
		name: "stop: the stopped shell step is recorded stopped, the next one skipped, and the move still asked",
		def:  throughNotify, given: seq(notifying(), stopped(6)),
		fact: shellStepEnded(7, 0, ShellOutcome{Reason: NewShellReason("notify was stopped")}),
		want: []RunEvent{
			stepEnded(7, 0, StepStopped{Reason: NewShellReason("notify was stopped")}),
			stepEnded(7, 1, StepSkipped{}),
			asked(7, 2),
		},
	},
	{
		name: "stop: a script that exits 0 after the stop is still recorded stopped",
		def:  throughNotify, given: seq(notifying(), stopped(6)), fact: shellStepEnded(7, 0, exited(0, "notify passed")),
		want: []RunEvent{
			stepEnded(7, 0, StepStopped{Reason: NewShellReason("notify passed")}),
			stepEnded(7, 1, StepSkipped{}),
			asked(7, 2),
		},
	},
	{
		name:  "stop: a tracker step in flight goes on, only the run is marked stopping",
		given: lfgFailed(), fact: StopReached{FactHead: fh(6)},
		want: stopped(6),
	},
	{
		name:  "stop: a tracker step that lands after the stop leaves the shell steps after it skipped",
		def:   failedThrough(ReportStep{}, notify, moveFailed),
		given: seq(lfgFailedThrough(ReportStep{}, notify, moveFailed), stopped(6)),
		fact:  settled(7, 0, StepLanded{}),
		want:  []RunEvent{stepEnded(7, 0, StepLanded{}), stepEnded(7, 1, StepSkipped{}), asked(7, 2)},
	},
	{
		name:  "stop: a stop reaches a routing run once",
		given: seq(lfgFailed(), stopped(6)), fact: StopReached{FactHead: fh(7)},
	},
	{
		name:  "stop: a run waiting for its lookup is marked stopping",
		given: lookingUp(), fact: StopReached{FactHead: fh(7)},
		want: stopped(7),
	},
}

// timeUpDecisions decide crew's run time being up.
var timeUpDecisions = []decision{
	{
		name:  "timeUp: a run taking is marked out of time",
		given: []RunEvent{taken()}, fact: TimeUp{FactHead: fh(0)},
		want: outOfTime(0),
	},
	{
		name:  "timeUp: the running script is not asked to stop",
		given: installing(), fact: TimeUp{FactHead: fh(2)},
		want: outOfTime(2),
	},
	{
		name: "timeUp: AE22, action 1 of 3 finishes, action 2 does not start, and failed's shell step is asked",
		def:  failedThrough(notify, moveFailed), given: seq(installing(), outOfTime(2)),
		fact: shellEnded(3, "install", exited(0, "install passed")),
		want: []RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
			passedEnd(3, "install", "install passed"),
			ActionEnded{EventHead: eh(3), Action: "lfg", End: timeUpEnd, Verdict: Failed, Target: toFailed},
			failedRoute(3, notify, moveFailed), asked(3, 0),
		},
	},
	{
		name: "timeUp: AE22, the route's shell step runs to its end and the move is asked",
		def:  failedThrough(notify, moveFailed),
		given: seq(installing(), outOfTime(2), []RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
			passedEnd(3, "install", "install passed"),
			ActionEnded{EventHead: eh(3), Action: "lfg", End: timeUpEnd, Verdict: Failed, Target: toFailed},
			failedRoute(3, notify, moveFailed), asked(3, 0),
		}),
		fact: shellStepEnded(4, 0, exited(0, "notify passed")),
		want: []RunEvent{stepEnded(4, 0, StepRan{Reason: NewShellReason("notify passed")}), asked(4, 1)},
	},
	{
		name:  "timeUp: a take that lands after it starts no action and chooses failed with the time-up cause",
		given: seq([]RunEvent{taken()}, outOfTime(0)), fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: seq([]RunEvent{takeMoved()}, haltedAt(1, "install", timeUpEnd)),
	},
	{
		name:  "timeUp: a workspace ready after it names no log and starts no action",
		given: seq(asking(), outOfTime(1)), fact: ready(false),
		want: seq([]RunEvent{WorkspaceOpened{EventHead: eh(2), Workspace: runWS()}}, haltedAt(2, "install", timeUpEnd)),
	},
	{
		name:  "timeUp: a reopened workspace that is gone after it starts no action",
		given: seq(reopening(), outOfTime(1)), fact: WorkspaceGone{FactHead: fh(2)},
		want: seq([]RunEvent{WorkspaceMissing{EventHead: eh(2), Workspace: runWS()}}, haltedAt(2, "install", timeUpEnd)),
	},
	{
		name: "timeUp: a rule without actions chooses passed at the take", def: withoutActions,
		given: seq([]RunEvent{takenWithoutActions()}, outOfTime(0)), fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{takeMoved(), chose(1, PassedRoute, ""), asked(1, 0)},
	},
	{
		name:  "timeUp: an action whose verdict chooses its own route keeps it",
		given: seq(inSession(), outOfTime(4)), fact: lfgEnded(succeeded("done"), VerdictReported{Verdict: "blocked"}),
		want: []RunEvent{
			lfgSession(succeeded("done")),
			lfgEnd(Judged{Verdict: "blocked", End: EndSucceeded{Reason: NewSessionText("done")}}, ToRoute{Route: "blocked"}),
			chose(5, "blocked", "lfg"), asked(5, 0),
		},
	},
	{
		name:  "timeUp: the last action going next still chooses passed",
		given: seq(judging(), outOfTime(5)), fact: shellEnded(6, "judge", exited(0, "judge passed")),
		want: passedAll()[len(judging()):],
	},
	{name: "timeUp: a routing run runs every step", given: lfgFailed(), fact: TimeUp{FactHead: fh(6)}},
	{name: "timeUp: a stopping run changes nothing", given: seq(installing(), stopped(2)), fact: TimeUp{FactHead: fh(3)}},
	{name: "timeUp: it reaches a run once", given: seq(installing(), outOfTime(2)), fact: TimeUp{FactHead: fh(3)}},
}

func TestDecideTheStopBetweenActions(t *testing.T) { decide(t, stopBetweenDecisions) }
func TestDecideTheStopWhileRouting(t *testing.T)   { decide(t, stopRoutingDecisions) }
func TestDecideTheTimeUp(t *testing.T)             { decide(t, timeUpDecisions) }

func TestAStopLeavesTheActionsAfterTheStoppedOneNotRun(t *testing.T) {
	run := given(t, seq(inSession(), stopped(4), stopBetweenDecisions[1].want))
	if got := states(run); !reflect.DeepEqual(got[2], NotRun{}) || !run.Stopping() {
		t.Errorf("actions = %#v, want judge not run, and the run stopping", got)
	}
	if a, _ := run.Cursor(); a.Name() != "lfg" {
		t.Errorf("cursor = %s, want lfg", a.Name())
	}
	timedOut := given(t, seq(installing(), outOfTime(2)))
	if !timedOut.TimeUp() || timedOut.Stopping() {
		t.Errorf("a run out of time: time up %v, stopping %v, want only time up", timedOut.TimeUp(), timedOut.Stopping())
	}
}
