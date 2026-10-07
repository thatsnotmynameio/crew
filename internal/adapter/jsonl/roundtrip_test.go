package jsonl_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestEveryFailureCauseLoadsBackAsItself(t *testing.T) {
	causes := []crew.FailureCause{
		crew.CauseSession, crew.CauseStopped, crew.CauseWorkspace, crew.CauseStart, crew.CausePrompt,
		crew.CauseShell, crew.CauseVerdict, crew.CauseStoppedBeforeStart, crew.CauseTimeUp, crew.CauseFunction,
	}
	want := make([]crew.RunEvent, 0, len(causes))
	for i, cause := range causes {
		want = append(want, crew.ActionEnded{
			EventHead: head(i), Action: "lfg", Verdict: crew.Failed, Target: crew.ToRoute{Route: crew.FailedRoute},
			End: crew.EndFailed{Reason: crew.NewSessionText("broke"), Cause: cause},
		})
	}
	roundTrip(t, want)
}

func TestEveryStartLoadsBackAsItself(t *testing.T) {
	starts := []crew.Start{
		crew.StartFresh{}, resumed,
		crew.StartAt{Workspace: space, Action: "install", Reason: crew.NewSessionText("crashed")},
		crew.StartPassedRoute{
			Workspace: crew.Some(space), Log: logPath,
			Session: crew.Some(crew.LatestSession{Action: "lfg", Bot: developer}),
		},
		crew.StartPassedRoute{},
		crew.StartWithoutAction{Action: "deploy"},
	}
	want := make([]crew.RunEvent, 0, len(starts))
	for i, start := range starts {
		want = append(want, crew.RunTaken{
			EventHead: head(i), Issue: crew.IssueData{ID: head(i).IssueID, Ref: "#9"}, Start: start,
		})
	}
	roundTrip(t, want)
}

func TestEveryStepOutcomeLoadsBackAsItself(t *testing.T) {
	outcomes := []crew.StepOutcome{
		crew.StepLanded{}, crew.StepRan{Reason: crew.NewShellReason("notify: exit status 0")},
		crew.StepFailed{Reason: crew.NewShellReason("notify: exit status 2")},
		crew.StepGivenUp{Reason: "the tracker refused it"}, crew.StepDropped{Reason: "the issue moved meanwhile"},
		crew.StepSkipped{}, crew.StepStopped{Reason: crew.NewShellReason("stopped")},
	}
	want := make([]crew.RunEvent, 0, len(outcomes))
	for i, o := range outcomes {
		want = append(want, crew.StepEnded{EventHead: head(i), Step: i, Outcome: o})
	}
	roundTrip(t, want)
}

func TestAShellThatDidNotRunToItsEndLoadsWithoutAStatus(t *testing.T) {
	roundTrip(t, []crew.RunEvent{
		crew.ActionShellEnded{
			EventHead: head(1), Action: "judge", Outcome: crew.ShellOutcome{Reason: crew.NewShellReason("timed out")},
		},
		crew.ActionShellEnded{
			EventHead: head(2), Action: "judge", Outcome: crew.ShellOutcome{Status: crew.Some(0)},
		},
	})
}

func TestAReasonWithAControlByteLoadsWithASpaceInItsPlace(t *testing.T) {
	j, root := journal(t)
	writeJournal(t, root, `{"v":3,"type":"action_ended","time":"2026-10-07T09:00:01Z","rule_run":"development-1",`+
		`"issue":"9","ref":"#9","stage":"development","action":"lfg","succeeded":false,"reason":"bo\u0000om"}`)

	got, err := j.Load(repository)
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %#v, %v, want one event", got, err)
	}
	e, ok := got[0].(crew.ActionEnded)
	if !ok || e.End.Outcome().Reason.String() != "bo om" {
		t.Errorf("event = %#v, want an end whose reason is %q", got[0], "bo om")
	}
}

func TestEveryLookupLoadsBackAsItself(t *testing.T) {
	roundTrip(t, []crew.RunEvent{
		crew.RunLookupDone{EventHead: head(1), PullRequest: pr45},
		crew.RunLookupDone{EventHead: head(2), PullRequest: crew.PullRequestNone{}},
		crew.RunLookupDone{EventHead: head(3), PullRequest: crew.PullRequestNotLookedUp{}},
	})
}

func TestEveryFunctionEventLoadsBackAsItselfWithOrWithoutAVerdict(t *testing.T) {
	roundTrip(t, []crew.RunEvent{
		crew.ActionFunctionAsked{EventHead: head(1), Action: "label"},
		crew.ActionFunctionStopAsked{EventHead: head(2), Action: "label"},
		crew.ActionFunctionEnded{
			EventHead: head(3), Action: "label", Outcome: crew.FunctionOutcome{Reason: crew.NewShellReason("stopped")},
		},
		crew.ActionFunctionEnded{EventHead: head(4), Action: "label", Outcome: crew.FunctionOutcome{
			Verdict: crew.Some(crew.Verdict("needs-review")), Reason: crew.NewShellReason("label: needs-review"),
		}},
		crew.ActionEnded{
			EventHead: head(5), Action: "label", Verdict: crew.Failed, Target: crew.ToRoute{Route: crew.FailedRoute},
			End: crew.EndFailed{Reason: crew.NewSessionText("label: stopped"), Cause: crew.CauseFunction},
		},
		crew.RouteChosen{EventHead: head(6), Route: crew.FailedRoute, Action: "label", Steps: []crew.StepPlan{
			{Kind: crew.StepFunction, Function: "notify"},
		}},
		crew.StepAsked{EventHead: head(7), Step: 0},
		crew.StepFunctionStopAsked{EventHead: head(8), Step: 0},
		crew.StepEnded{EventHead: head(9), Step: 0, Outcome: crew.StepFailed{Reason: crew.NewShellReason("notify: failed")}},
	})
}
