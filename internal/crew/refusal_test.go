package crew

import (
	"errors"
	"reflect"
	"testing"
)

// The tables below are facts a run refuses: each row names the fact and
// why the run does not wait for it.

// released is a run released at minute 7, after its failed route's move
// landed.
func released() []RunEvent { return releasedAs(StepLanded{}) }

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
	{name: "a released run refuses a stop", given: released(), fact: StopReached{FactHead: fh(9)}},
	{name: "a released run refuses a time-up", given: released(), fact: TimeUp{FactHead: fh(9)}},
	{name: "a time-up for another run", given: asking(), fact: TimeUp{FactHead: FactHead{Run: "run-1", At: at(2)}}},
	{name: "a take that already landed", given: asking(), fact: TakeSettled{FactHead: fh(2), Landed: true}},
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
	{name: "functionEnded while a script runs", given: installing(), fact: functionEnded(3, "install", noVerdict)},
	{name: "functionEnded while a session runs", given: inSession(), fact: functionEnded(5, "lfg", noVerdict)},
	{name: "functionEnded while routing", given: passedAll(), fact: functionEnded(7, "judge", noVerdict)},
	{name: "shellEnded while a function runs", given: checking(), fact: shellEnded(6, "check", exited(0, ""))},
	{name: "returnChecked while taking", given: []RunEvent{answeredTake()}, fact: returnChecked(1, depsReady)},
	{
		name: "returnChecked while a function runs", given: checking(),
		fact: ReturnChecked{FactHead: fh(6), Action: "check", Check: depsReady},
	},
	{
		name: "returnChecked for a check that ended", given: seq(checkingReturn(), []RunEvent{returnedEnd(2)}, returnedTo(2)),
		fact: returnChecked(3, depsReady),
	},
	{name: "functionEnded while crew reads", given: checkingReturn(), fact: functionEnded(2, "answer", noVerdict)},
}

// lookupRefusals are lookups the run did not ask for, or that it already
// answered.
var lookupRefusals = []refusal{
	{name: "pullRequestLookedUp while taking", given: []RunEvent{taken()}, fact: PullRequestLookedUp{FactHead: fh(1)}},
	{name: "pullRequestLookedUp while the actions run", given: inSession(), fact: PullRequestLookedUp{FactHead: fh(5)}},
	{name: "pullRequestLookedUp never asked", given: passedAll(), fact: PullRequestLookedUp{FactHead: fh(7)}},
	{
		name:  "pullRequestLookedUp already answered",
		given: append(lookingUp(), RunLookupDone{EventHead: eh(7)}), fact: PullRequestLookedUp{FactHead: fh(8)},
	},
}

// stepRefusals are facts of a route's step that is not in flight, or that
// settles in a way its kind cannot.
var stepRefusals = []refusal{
	{name: "stepSettled while taking", given: []RunEvent{taken()}, fact: settled(1, 0, StepLanded{})},
	{name: "stepSettled while the actions run", given: inSession(), fact: settled(5, 0, StepLanded{})},
	{name: "stepSettled while the lookup is pending", given: lookingUp(), fact: settled(7, 0, StepLanded{})},
	{name: "stepSettled for a step not in flight", given: lfgFailed(), fact: settled(6, 1, StepLanded{})},
	{name: "stepSettled for a step that settled", given: reported(), fact: settled(7, 0, StepLanded{})},
	{name: "stepSettled for a shell step", given: notifying(), fact: settled(6, 0, StepLanded{})},
	{name: "stepSettled with a shell step's outcome", given: lfgFailed(), fact: settled(6, 0, StepRan{})},
	{name: "stepSettled skipped", given: lfgFailed(), fact: settled(6, 0, StepSkipped{})},
	{name: "stepSettled with no outcome", given: lfgFailed(), fact: settled(6, 0, nil)},
	{name: "stepSettled for a released run", given: released(), fact: settled(9, 0, StepLanded{})},
	{name: "stepShellEnded while the actions run", given: installing(), fact: shellStepEnded(3, 0, exited(0, ""))},
	{name: "stepShellEnded for a tracker step", given: lfgFailed(), fact: shellStepEnded(6, 0, exited(0, ""))},
	{name: "stepShellEnded for a step not in flight", given: notifying(), fact: shellStepEnded(6, 1, exited(0, ""))},
	{name: "stepShellEnded while the lookup is pending", given: lookingUp(), fact: shellStepEnded(7, 0, exited(0, ""))},
	{name: "stepShellEnded for a released run", given: released(), fact: shellStepEnded(9, 0, exited(0, ""))},
	{name: "stepSettled for a function step", given: checkingStep(), fact: settled(6, 0, StepLanded{})},
	{name: "stepShellEnded for a function step", given: checkingStep(), fact: shellStepEnded(6, 0, exited(0, ""))},
	{name: "stepFunctionEnded for a shell step", given: notifying(), fact: functionStepEnded(6, 0, noVerdict)},
	{name: "stepFunctionEnded for a tracker step", given: lfgFailed(), fact: functionStepEnded(6, 0, noVerdict)},
	{name: "stepFunctionEnded while the actions run", given: checking(), fact: functionStepEnded(6, 0, noVerdict)},
	{name: "stepFunctionEnded for a step not in flight", given: checkingStep(), fact: functionStepEnded(6, 1, noVerdict)},
}

// checkingStep is the run through a failed route of a function step and a
// move, after lfg failed, whose function step runs.
func checkingStep() []RunEvent { return lfgFailedThrough(checkStep, moveFailed) }

func TestDecideRefusesWhatTheRunDoesNotWaitFor(t *testing.T) {
	for _, table := range [][]refusal{runRefusals, workspaceRefusals, actionRefusals, lookupRefusals, stepRefusals} {
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
