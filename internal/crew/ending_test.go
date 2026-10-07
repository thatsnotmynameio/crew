package crew

import (
	"errors"
	"reflect"
	"testing"
)

// The decision tables below cover the run's ending move and failure report,
// which a run reaches through RunEnded, and the facts a run refuses; each
// row names the fact it decides and the branch it follows.

// endedFailed is a run whose lfg failed at minute 5, and that ended in
// failure at minute 7.
var endedFailed = seq(inSession(), []RunEvent{
	lfgSession(failedOutcome("gave up")),
	lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), toFailed),
	RunEnded{EventHead: eh(7), Ending: RunEnding{To: labelFailed, Failures: []ActionFailure{failure("lfg")}}},
})

// endedDone is a run whose actions passed, and that ended in success at
// minute 7.
var endedDone = seq(judging(), []RunEvent{
	ActionShellEnded{EventHead: eh(6), Action: "judge", Outcome: exited(0, "judge passed")},
	passedEnd(6, "judge", "judge passed"),
	RunEnded{EventHead: eh(7), Ending: RunEnding{To: labelDone}},
})

// endingDecisions decide the ending move and the failure report.
var endingDecisions = []decision{
	{
		name:  "endingMoveSettled: a landed ending move without a report releases the run",
		given: endedDone, fact: EndingMoveSettled{FactHead: fh(8), Move: EndingLanded{}},
		want: []RunEvent{
			EndingMoved{EventHead: eh(8), From: labelRunning, To: labelDone},
			RunReleased{EventHead: eh(8)},
		},
	},
	{
		name:  "endingMoveSettled: a landed ending move waits for the failure report",
		given: endedFailed, fact: EndingMoveSettled{FactHead: fh(8), Move: EndingLanded{}},
		want: []RunEvent{EndingMoved{EventHead: eh(8), From: labelRunning, To: labelFailed}},
	},
	{
		name:  "failureReportSettled: a landed failure report waits for the ending move",
		given: endedFailed, fact: FailureReportSettled{FactHead: fh(8), Landed: true},
		want: []RunEvent{FailureReported{EventHead: eh(8)}},
	},
	{
		name: "failureReportSettled: the failure report landing after the ending move releases the run",
		given: seq(endedFailed, []RunEvent{
			EndingMoved{EventHead: eh(8), From: labelRunning, To: labelFailed},
		}),
		fact: FailureReportSettled{FactHead: fh(9), Landed: true},
		want: []RunEvent{FailureReported{EventHead: eh(9)}, RunReleased{EventHead: eh(9)}},
	},
	{
		name:  "endingMoveSettled: the ending move landing after the failure report releases the run",
		given: seq(endedFailed, []RunEvent{FailureReported{EventHead: eh(8)}}),
		fact:  EndingMoveSettled{FactHead: fh(9), Move: EndingLanded{}},
		want: []RunEvent{
			EndingMoved{EventHead: eh(9), From: labelRunning, To: labelFailed},
			RunReleased{EventHead: eh(9)},
		},
	},
	{
		name:  "endingMoveSettled: an ending move given up keeps its reason",
		given: endedDone, fact: EndingMoveSettled{FactHead: fh(8), Move: EndingGivenUp{Reason: "issue closed"}},
		want: []RunEvent{
			EndingDropped{EventHead: eh(8), To: labelDone, Reason: "issue closed"},
			RunReleased{EventHead: eh(8)},
		},
	},
	{
		name: "failureReportSettled: a failure report given up still settles",
		given: seq(endedFailed, []RunEvent{
			EndingMoved{EventHead: eh(8), From: labelRunning, To: labelFailed},
		}),
		fact: FailureReportSettled{FactHead: fh(9)},
		want: []RunEvent{FailureReportDropped{EventHead: eh(9)}, RunReleased{EventHead: eh(9)}},
	},
}

// released is a run released at minute 8, after its ending move landed.
var released = seq(endedDone, []RunEvent{
	EndingMoved{EventHead: eh(8), From: labelRunning, To: labelDone}, RunReleased{EventHead: eh(8)},
})

// lookingUp is a run through passed whose pull requests are looked up.
var lookingUp = append(passedAll(), RunLookupAsked{EventHead: eh(6)})

type refusal struct {
	name  string
	given []RunEvent
	fact  Fact
}

// runRefusals are facts no run in this state waits for: a fact of another
// run, for no run, for a released one, or a delivery not asked for.
var runRefusals = []refusal{
	{
		name: "a fact for another run", given: asking(),
		fact: WorkspaceReady{FactHead: FactHead{Run: "run-1", At: at(2)}, Workspace: runWS()},
	},
	{name: "a fact for no run", fact: StopReached{FactHead: fh(1)}},
	{name: "a released run refuses a stop", given: released, fact: StopReached{FactHead: fh(9)}},
	{
		name: "a released run refuses an ending move", given: released,
		fact: EndingMoveSettled{FactHead: fh(9), Move: EndingLanded{}},
	},
	{name: "a take that already landed", given: asking(), fact: TakeSettled{FactHead: fh(2), Landed: true}},
	{
		name: "an ending move while the actions run", given: inSession(),
		fact: EndingMoveSettled{FactHead: fh(5), Move: EndingLanded{}},
	},
	{
		name: "an ending move while routing", given: passedAll(),
		fact: EndingMoveSettled{FactHead: fh(7), Move: EndingLanded{}},
	},
	{
		name:  "an ending move that already settled",
		given: seq(endedDone, []RunEvent{EndingDropped{EventHead: eh(8), To: labelDone}}),
		fact:  EndingMoveSettled{FactHead: fh(9), Move: EndingLanded{}},
	},
	{
		name: "a failure report the ending does not post", given: endedDone,
		fact: FailureReportSettled{FactHead: fh(8), Landed: true},
	},
	{
		name: "a failure report while routing", given: passedAll(),
		fact: FailureReportSettled{FactHead: fh(7), Landed: true},
	},
}

// workspaceRefusals are workspace facts while the run does not wait for
// them.
var workspaceRefusals = []refusal{
	{name: "workspaceReady while taking", given: []RunEvent{taken()}, fact: ready(false)},
	{name: "workspaceReady once it was ready", given: inSession(), fact: ready(false)},
	{name: "workspaceReady while routing", given: passedAll(), fact: ready(false)},
	{name: "workspaceGone while taking", given: []RunEvent{taken()}, fact: WorkspaceGone{FactHead: fh(1)}},
	{name: "workspaceGone for a new workspace", given: asking(), fact: WorkspaceGone{FactHead: fh(2)}},
	{name: "workspaceGone once it was ready", given: installing(), fact: WorkspaceGone{FactHead: fh(3)}},
	{name: "workspaceGone while routing", given: passedAll(), fact: WorkspaceGone{FactHead: fh(7)}},
	{name: "workspaceFailed while taking", given: []RunEvent{taken()}, fact: WorkspaceFailed{FactHead: fh(1)}},
	{name: "workspaceFailed once it was ready", given: installing(), fact: WorkspaceFailed{FactHead: fh(3)}},
	{name: "workspaceFailed while routing", given: passedAll(), fact: WorkspaceFailed{FactHead: fh(7)}},
}

// actionRefusals are facts of an action that is not at the cursor, or
// whose state does not wait for them.
var actionRefusals = []refusal{
	{
		name: "sessionStarted while taking", given: []RunEvent{taken()},
		fact: SessionStarted{FactHead: fh(1), Action: "lfg"},
	},
	{name: "sessionStarted before the workspace", given: asking(), fact: SessionStarted{FactHead: fh(2), Action: "lfg"}},
	{
		name: "sessionStarted for an action the run has not reached", given: installing(),
		fact: SessionStarted{FactHead: fh(3), Action: "lfg"},
	},
	{name: "sessionStarted while routing", given: passedAll(), fact: SessionStarted{FactHead: fh(7), Action: "lfg"}},
	{
		name: "sessionFailedToStart for a running session", given: inSession(),
		fact: SessionFailedToStart{FactHead: fh(5), Action: "lfg"},
	},
	{
		name: "sessionFailedToStart while a script runs", given: installing(),
		fact: SessionFailedToStart{FactHead: fh(3), Action: "install"},
	},
	{name: "sessionEnded before the workspace", given: asking(), fact: lfgEnded(succeeded("done"), nil)},
	{name: "sessionEnded while a script runs", given: installing(), fact: lfgEnded(succeeded("done"), nil)},
	{name: "sessionEnded for a session that ended", given: judging(), fact: lfgEnded(succeeded("again"), nil)},
	{name: "sessionEnded while routing", given: passedAll(), fact: lfgEnded(succeeded("again"), nil)},
	{name: "shellEnded while taking", given: []RunEvent{taken()}, fact: shellEnded(1, "install", exited(0, ""))},
	{name: "shellEnded before the workspace", given: asking(), fact: shellEnded(2, "install", exited(0, ""))},
	{name: "shellEnded while a session runs", given: inSession(), fact: shellEnded(5, "lfg", exited(0, ""))},
	{
		name: "shellEnded for an action the run has not reached", given: installing(),
		fact: shellEnded(3, "judge", exited(0, "")),
	},
	{name: "shellEnded for a script that ended", given: inSession(), fact: shellEnded(5, "install", exited(0, ""))},
	{name: "shellEnded while routing", given: passedAll(), fact: shellEnded(7, "judge", exited(0, ""))},
	{name: "an action the run does not have", given: installing(), fact: shellEnded(3, "deploy", exited(0, ""))},
}

// lookupRefusals are lookups the run did not ask for, or that it already
// answered.
var lookupRefusals = []refusal{
	{name: "pullRequestLookedUp while taking", given: []RunEvent{taken()}, fact: PullRequestLookedUp{FactHead: fh(1)}},
	{name: "pullRequestLookedUp while the actions run", given: inSession(), fact: PullRequestLookedUp{FactHead: fh(5)}},
	{name: "pullRequestLookedUp never asked", given: passedAll(), fact: PullRequestLookedUp{FactHead: fh(7)}},
	{
		name:  "pullRequestLookedUp already answered",
		given: append(lookingUp, RunLookupDone{EventHead: eh(7)}), fact: PullRequestLookedUp{FactHead: fh(8)},
	},
}

func TestDecideRefusesWhatTheRunDoesNotWaitFor(t *testing.T) {
	for _, table := range [][]refusal{runRefusals, workspaceRefusals, actionRefusals, lookupRefusals} {
		for _, tt := range table {
			t.Run(tt.name, func(t *testing.T) {
				run := given(t, tt.given)
				before := run.Snapshot()
				got, err := Decide(run, sequence(), tt.fact)
				if !errors.Is(err, ErrRefused) || got != nil {
					t.Errorf("Decide = %#v, %v, want a refusal", got, err)
				}
				if !reflect.DeepEqual(run.Snapshot(), before) {
					t.Errorf("the run changed: %#v, was %#v", run.Snapshot(), before)
				}
			})
		}
	}
}

func TestDecideTheEnding(t *testing.T) { decide(t, endingDecisions) }
