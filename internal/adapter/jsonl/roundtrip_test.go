package jsonl_test

import (
	"reflect"
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

// Lines an earlier crew wrote, without a session's login or asks or a
// take's questions, load with none (KTD-W11).
func TestLinesWithoutQuestionsLoadWithNone(t *testing.T) {
	j, root := journal(t)
	writeJournal(t, root,
		`{"v":3,"type":"run_taken","time":"2026-10-07T09:00:00Z","rule_run":"development-1","issue":"9",`+
			`"ref":"#9","stage":"development","start":{"kind":"fresh"}}`,
		`{"v":3,"type":"action_session_started","time":"2026-10-07T09:00:07Z","rule_run":"development-1",`+
			`"issue":"9","ref":"#9","stage":"development","action":"lfg","bot":"crew-developer"}`,
	)

	got, err := j.Load(repository)
	want := []crew.RunEvent{
		crew.RunTaken{EventHead: head(0), Issue: crew.IssueData{ID: head(0).IssueID, Ref: "#9"}, Start: crew.StartFresh{}},
		crew.ActionSessionStarted{EventHead: head(7), Action: "lfg", Bot: developer},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load =\n%#v, %v\nwant\n%#v", got, err, want)
	}
}

func TestEveryQuestionLoadsBackAsItself(t *testing.T) {
	roundTrip(t, []crew.RunEvent{crew.RunTaken{
		EventHead: head(0), Issue: crew.IssueData{ID: head(0).IssueID, Ref: "#9"}, Start: crew.StartFresh{},
		Questions: []crew.Question{
			{Run: "development-0", Action: "lfg", Login: "crew-developer[bot]"},
			{Run: "development-1", Action: "lfg"},
			{Run: "development-1", Action: "acceptance", Login: "boss"},
			{ID: "blocks", Rule: "development"},
		},
	}})
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
			Log: ".crew/logs/issue-7-label.log",
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

// KTD12: a route that asks a question, or delegates one, loads back with
// its steps.
func TestEveryQuestionStepLoadsBackAsItself(t *testing.T) {
	roundTrip(t, []crew.RunEvent{
		crew.RouteChosen{EventHead: head(1), Route: "unsure", Action: "lfg", Steps: []crew.StepPlan{
			{Kind: crew.StepQuestion, Question: "blocks"}, {Kind: crew.StepMove, To: "crew:question"},
		}},
		crew.RouteChosen{EventHead: head(2), Route: crew.PassedRoute, Steps: []crew.StepPlan{
			{Kind: crew.StepDelegate}, {Kind: crew.StepMove, To: "crew:question:waiting answer"},
		}},
	})
}

// KTD12: the answered rule's check, its failure verdict and the return its
// passed route plans load back as themselves, so a replayed run still
// knows its action was the check.
func TestTheAnsweredRulesCheckLoadsBackAsItself(t *testing.T) {
	roundTrip(t, []crew.RunEvent{
		crew.ActionReturnAsked{EventHead: head(1), Action: "answer"},
		crew.ActionEnded{
			EventHead: head(2), Action: "answer", Verdict: crew.Unanswered, Target: crew.ToRoute{Route: crew.FailedRoute},
			End: crew.EndSucceeded{Reason: crew.NewSessionText("no answer counts after the question")},
		},
		crew.RouteChosen{EventHead: head(3), Route: crew.PassedRoute, Action: "answer", Steps: []crew.StepPlan{
			{Kind: crew.StepMove, To: "crew:deps:ready"},
		}},
	})
}

// KTD12: a version 3 line written before crew kept a question step's id
// or a rule's question loads as it did: a question step without an id,
// and a session's question without one.
func TestLinesWithoutQuestionIDsLoadAsBefore(t *testing.T) {
	j, root := journal(t)
	writeJournal(t, root,
		`{"v":3,"type":"run_taken","time":"2026-10-07T09:00:00Z","rule_run":"development-1","issue":"9",`+
			`"ref":"#9","stage":"development","start":{"kind":"fresh"},`+
			`"questions":[{"rule_run":"development-0","action":"lfg"}]}`,
		`{"v":3,"type":"route_chosen","time":"2026-10-07T09:00:01Z","rule_run":"development-1","issue":"9",`+
			`"ref":"#9","stage":"development","action":"lfg","route":"unsure","steps":[{"kind":"question"}]}`,
	)

	got, err := j.Load(repository)
	want := []crew.RunEvent{
		crew.RunTaken{
			EventHead: head(0), Issue: crew.IssueData{ID: head(0).IssueID, Ref: "#9"}, Start: crew.StartFresh{},
			Questions: []crew.Question{{Run: "development-0", Action: "lfg"}},
		},
		crew.RouteChosen{
			EventHead: head(1), Route: "unsure", Action: "lfg", Steps: []crew.StepPlan{{Kind: crew.StepQuestion}},
		},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load =\n%#v, %v\nwant\n%#v", got, err, want)
	}
}
