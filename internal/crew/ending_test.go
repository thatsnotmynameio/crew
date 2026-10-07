package crew

import (
	"errors"
	"reflect"
	"testing"
)

// The decision tables below mirror today's core handlers for checks, pull
// request lookups and the run's ending, and the inputs the core ignores; each
// row names the handler in internal/core and the branch it follows.

func checkEnded(passed bool, reason string) Fact {
	return CheckEnded{FactHead: fh(6), Action: "development", Passed: passed, Reason: NewCheckReason(reason)}
}

func result(check CheckName, passed bool, reason string) RunEvent {
	return ActionCheckEnded{EventHead: eh(6), Action: "development", Result: CheckResult{
		Name: check, Passed: passed, Reason: NewCheckReason(reason),
	}}
}

// testRunning is development's build passed and its test running.
var testRunning = seq(preparing(), inChecks(), []RunEvent{
	ActionCheckEnded{EventHead: eh(5), Action: "development", Result: CheckResult{Name: "build", Passed: true}},
	ActionCheckAsked{EventHead: eh(5), Action: "development", Check: "test"},
})

// lookingUp is development's build running while its pull request is
// looked up.
var lookingUp = seq(preparing(), inSession("development"), []RunEvent{
	ActionSessionEnded{EventHead: eh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
	ActionLookupAsked{EventHead: eh(5), Action: "development"},
	ActionCheckAsked{EventHead: eh(5), Action: "development", Check: "build"},
})

// checkDecisions mirror checkEnded, pullRequestFound and end's wait for the
// lookup.
var checkDecisions = []decision{
	{
		name:  "checkEnded: a passing check runs the next",
		given: seq(preparing(), inChecks()), fact: checkEnded(true, "build passed"),
		want: []RunEvent{
			result("build", true, "build passed"),
			ActionCheckAsked{EventHead: eh(6), Action: "development", Check: "test"},
		},
	},
	{
		name:  "checkEnded: the last passing check succeeds the action with its reason",
		given: testRunning, fact: checkEnded(true, "test passed"),
		want: []RunEvent{
			result("test", true, "test passed"),
			developmentEnded(6, EndSucceeded{Reason: NewSessionText("test passed")}),
		},
	},
	{
		name:  "checkEnded: a failing check fails the action by its check",
		given: seq(preparing(), inChecks()), fact: checkEnded(false, "build failed: exit 2"),
		want: []RunEvent{
			result("build", false, "build failed: exit 2"),
			developmentEnded(6, EndFailed{Reason: NewSessionText("build failed: exit 2"), Cause: CauseCheck}),
		},
	},
	{
		name: "checkEnded: a check that ends after its stop was sent ends the action stopped",
		given: seq(preparing(), inChecks(), stopped(5), []RunEvent{
			ActionCheckStopAsked{EventHead: eh(5), Action: "development"},
		}),
		fact: checkEnded(true, "build passed"),
		want: []RunEvent{result("build", true, "build passed"), developmentEnded(6, stopEnd)},
	},
	{
		name:  "pullRequestFound: a lookup answered during the checks is kept",
		given: lookingUp, finds: true,
		fact: PullRequestLookedUp{FactHead: fh(6), Action: "development", PullRequest: foundPR},
		want: []RunEvent{ActionLookupDone{EventHead: eh(6), Action: "development", PullRequest: foundPR}},
	},
	{
		name:  "end: a failing check while the lookup is pending waits for it",
		given: lookingUp, finds: true, fact: checkEnded(false, "build failed"),
		want: []RunEvent{
			result("build", false, "build failed"),
			ActionFinishing{EventHead: eh(6), Action: "development", End: EndFailed{
				Reason: NewSessionText("build failed"), Cause: CauseCheck,
			}},
		},
	},
	{
		name: "pullRequestFound: the lookup's answer ends an action that waits for it, with its pull request",
		given: seq(lookingUp, []RunEvent{
			ActionCheckEnded{EventHead: eh(6), Action: "development", Result: CheckResult{Name: "build"}},
			ActionFinishing{EventHead: eh(6), Action: "development", End: EndFailed{Cause: CauseCheck}},
		}),
		finds: true, fact: PullRequestLookedUp{FactHead: fh(7), Action: "development", PullRequest: foundPR},
		want: []RunEvent{
			ActionLookupDone{EventHead: eh(7), Action: "development", PullRequest: foundPR},
			ActionEnded{
				EventHead: eh(7), Action: "development", End: EndFailed{Cause: CauseCheck},
				Workspace: opened("development"), SessionStarted: Some(at(4)), Usage: usage,
				PullRequest: foundPR,
			},
		},
	},
}

var devFailed = EndFailed{Reason: NewSessionText("gave up"), Cause: CauseSession}

// devEnd is development's session failing at minute 7.
var devEnd = []RunEvent{
	ActionSessionEnded{EventHead: eh(7), Action: "development", Outcome: devFailed.Outcome(), Usage: usage},
	developmentEnded(7, devFailed),
}

// endedFailed is a run that ended in failure at minute 7.
var endedFailed = seq(preparing(), inSession("development"), reviewEnded(EndFailed{Cause: CauseSession}), devEnd,
	[]RunEvent{RunEnded{EventHead: eh(7), Ending: RunEnding{
		To: labelFailed, Failures: []ActionFailure{failure("development"), failure("review")},
	}}})

// endedDone is a run that ended in success at minute 7.
var endedDone = seq(preparing(), inSession("development"), reviewEnded(EndSucceeded{}), []RunEvent{
	developmentEnded(7, EndSucceeded{}), RunEnded{EventHead: eh(7), Ending: RunEnding{To: labelDone}},
})

func withoutChecks(d RunDefinition) RunDefinition {
	d.Rule.Actions[0].Checks = nil
	return d
}

// endingDecisions mirror end, endRun, received and callResult.
var endingDecisions = []decision{
	{
		name:  "end: the last action's end ends the run in success",
		given: seq(preparing(), inSession("development"), reviewEnded(EndSucceeded{})),
		fact:  SessionEnded{FactHead: fh(7), Action: "development", Outcome: succeeded("done"), Usage: usage},
		def:   withoutChecks,
		want: []RunEvent{
			ActionSessionEnded{EventHead: eh(7), Action: "development", Outcome: succeeded("done"), Usage: usage},
			developmentEnded(7, EndSucceeded{Reason: NewSessionText("done")}),
			RunEnded{EventHead: eh(7), Ending: RunEnding{To: labelDone}},
		},
	},
	{
		name:  "endRun: one failure per failed action, in action order, with its workspace and log",
		given: seq(preparing(), inSession("development"), reviewEnded(EndFailed{Cause: CauseSession})),
		fact:  SessionEnded{FactHead: fh(7), Action: "development", Outcome: devFailed.Outcome(), Usage: usage},
		want: seq(devEnd, []RunEvent{RunEnded{EventHead: eh(7), Ending: RunEnding{
			To: labelFailed, Failures: []ActionFailure{failure("development"), failure("review")},
		}}}),
	},
	{
		name:  "received: a landed ending move without a report releases the run",
		given: endedDone, fact: EndingMoveSettled{FactHead: fh(8), Move: EndingLanded{}},
		want: []RunEvent{
			EndingMoved{EventHead: eh(8), From: labelRunning, To: labelDone},
			RunReleased{EventHead: eh(8)},
		},
	},
	{
		name:  "received: a landed ending move waits for the failure report",
		given: endedFailed, fact: EndingMoveSettled{FactHead: fh(8), Move: EndingLanded{}},
		want: []RunEvent{EndingMoved{EventHead: eh(8), From: labelRunning, To: labelFailed}},
	},
	{
		name:  "received: a landed failure report waits for the ending move",
		given: endedFailed, fact: FailureReportSettled{FactHead: fh(8), Landed: true},
		want: []RunEvent{FailureReported{EventHead: eh(8)}},
	},
	{
		name: "received: the failure report landing after the ending move releases the run",
		given: seq(endedFailed, []RunEvent{
			EndingMoved{EventHead: eh(8), From: labelRunning, To: labelFailed},
		}),
		fact: FailureReportSettled{FactHead: fh(9), Landed: true},
		want: []RunEvent{FailureReported{EventHead: eh(9)}, RunReleased{EventHead: eh(9)}},
	},
	{
		name:  "received: the ending move landing after the failure report releases the run",
		given: seq(endedFailed, []RunEvent{FailureReported{EventHead: eh(8)}}),
		fact:  EndingMoveSettled{FactHead: fh(9), Move: EndingLanded{}},
		want: []RunEvent{
			EndingMoved{EventHead: eh(9), From: labelRunning, To: labelFailed},
			RunReleased{EventHead: eh(9)},
		},
	},
	{
		name:  "received: an ending move given up keeps its reason",
		given: endedDone, fact: EndingMoveSettled{FactHead: fh(8), Move: EndingGivenUp{Reason: "issue closed"}},
		want: []RunEvent{
			EndingDropped{EventHead: eh(8), To: labelDone, Reason: "issue closed"},
			RunReleased{EventHead: eh(8)},
		},
	},
	{
		name: "callResult: a failure report given up still settles",
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

type refusal struct {
	name  string
	given []RunEvent
	fact  Fact
}

// refusals are facts the core's handlers ignore: an input for an action in
// another phase, or for an issue no longer held.
var refusals = []refusal{
	{
		name:  "AE1: an ending run refuses an action's end",
		given: endedDone, fact: SessionEnded{FactHead: fh(8), Action: "review", Outcome: succeeded("again")},
	},
	{
		name:  "a fact for another run",
		given: preparing(),
		fact: WorkspaceReady{
			FactHead: FactHead{Run: "run-1", At: at(3)}, Action: "development", Workspace: ws("development"),
		},
	},
	{name: "a fact for no run", fact: StopReached{FactHead: fh(1)}},
	{name: "a released run refuses a stop", given: released, fact: StopReached{FactHead: fh(9)}},
	{
		name: "a released run refuses an ending move", given: released,
		fact: EndingMoveSettled{FactHead: fh(9), Move: EndingLanded{}},
	},
	{name: "a take that already landed", given: preparing(), fact: TakeSettled{FactHead: fh(3), Landed: true}},
	{
		name: "an ending move before the run ended", given: preparing(),
		fact: EndingMoveSettled{FactHead: fh(3), Move: EndingLanded{}},
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
	{name: "an action the run does not have", given: preparing(), fact: WorkspaceGone{FactHead: fh(3), Action: "deploy"}},
	{
		name:  "workspaceReady for an action whose session runs",
		given: seq(preparing(), inSession("review")),
		fact:  WorkspaceReady{FactHead: fh(5), Action: "review", Workspace: ws("review")},
	},
	{
		name: "workspaceGone for a new workspace", given: preparing(),
		fact: WorkspaceGone{FactHead: fh(3), Action: "review"},
	},
	{
		name:  "workspaceFailed for an action waiting for the take",
		given: []RunEvent{taken(bothActions()...)}, fact: WorkspaceFailed{FactHead: fh(1), Action: "review"},
	},
	{
		name: "sessionStarted before the workspace", given: preparing(),
		fact: SessionStarted{FactHead: fh(3), Action: "review"},
	},
	{
		name:  "sessionFailedToStart for a running session",
		given: seq(preparing(), inSession("review")), fact: SessionFailedToStart{FactHead: fh(5), Action: "review"},
	},
	{name: "sessionEnded before the workspace", given: preparing(), fact: SessionEnded{FactHead: fh(3), Action: "review"}},
	{
		name:  "checkEnded while the session runs",
		given: seq(preparing(), inSession("development")), fact: CheckEnded{FactHead: fh(5), Action: "development"},
	},
	{
		name:  "pullRequestFound for a lookup never asked",
		given: seq(preparing(), inChecks()), fact: PullRequestLookedUp{FactHead: fh(6), Action: "development"},
	},
}

func TestDecideRefusesWhatTheRunDoesNotWaitFor(t *testing.T) {
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			run := given(t, tt.given)
			before := run.Snapshot()
			got, err := Decide(run, definition(), tt.fact)
			if !errors.Is(err, ErrRefused) || got != nil {
				t.Errorf("Decide = %#v, %v, want a refusal", got, err)
			}
			if !reflect.DeepEqual(run.Snapshot(), before) {
				t.Errorf("the run changed: %#v, was %#v", run.Snapshot(), before)
			}
		})
	}
}

func TestDecideTheChecks(t *testing.T) { decide(t, checkDecisions) }
func TestDecideTheEnding(t *testing.T) { decide(t, endingDecisions) }
