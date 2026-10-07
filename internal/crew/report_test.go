package crew

import (
	"reflect"
	"testing"
)

// The statuses and reports below mirror internal/core's status,
// actionState, runEnded's failure report and reportPullRequests.

func TestARunningStatusShowsEachActionAsItStands(t *testing.T) {
	built := CheckResult{Name: "build", Passed: true, Reason: NewCheckReason("build passed")}
	run := given(t, seq(preparing(), inChecks(), []RunEvent{
		ActionCheckEnded{EventHead: eh(6), Action: "development", Result: built},
		ActionCheckAsked{EventHead: eh(6), Action: "development", Check: "test"},
	}, inSession("review")))
	said := map[ActionName]Said{"development": NewSaid("Building."), "review": NewSaid("Reading the diff.")}

	got := run.Status(at(9), said, true)
	want := NewStatus(StatusData{
		IssueID: testID, IssueRef: "#9", Rule: "implement", Progress: StatusRunning{}, Updated: at(9), Run: testRun,
		Actions: []ActionStatus{
			{Name: "development", State: ActionRunning{Started: at(4)}, Checks: []CheckResult{built}},
			{Name: "review", State: ActionRunning{Started: at(4), Said: NewSaid("Reading the diff.")}},
		},
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Status =\n%#v\nwant\n%#v", got.Data(), want.Data())
	}
}

func TestAnActionWithoutASessionToTimeIsPending(t *testing.T) {
	for name, events := range map[string][]RunEvent{
		"awaiting the take": {taken(bothActions()...)},
		"preparing":         preparing(),
		"starting":          seq(preparing(), starting("development"), starting("review")),
		"finishing": seq(preparing(), inSession("development"), inSession("review"), []RunEvent{
			ActionLookupAsked{EventHead: eh(5), Action: "development"},
			ActionFinishing{EventHead: eh(5), Action: "development", End: EndSucceeded{}},
			ActionLookupAsked{EventHead: eh(5), Action: "review"},
			ActionFinishing{EventHead: eh(5), Action: "review", End: EndSucceeded{}},
		}),
	} {
		for _, a := range given(t, events).Status(at(9), nil, true).Actions() {
			if a.State != (ActionPending{}) {
				t.Errorf("%s: %s is %#v, want it pending", name, a.Name, a.State)
			}
		}
	}
}

func TestAnEndedStatusCarriesTheEndingAndHowItsMoveStands(t *testing.T) {
	tests := []struct {
		name  string
		given []RunEvent
		want  StatusEnded
	}{
		{name: "ended", given: endedFailed, want: StatusEnded{To: labelFailed, Move: MovePending}},
		{
			name:  "moved",
			given: seq(endedFailed, []RunEvent{EndingMoved{EventHead: eh(8), From: labelRunning, To: labelFailed}}),
			want:  StatusEnded{To: labelFailed, Move: MoveDone},
		},
		{name: "dropped", given: snapshots["released"], want: StatusEnded{To: labelFailed, Move: MoveDropped}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := given(t, tt.given).Status(at(9), nil, false)
			want := []ActionStatus{
				{Name: "development", State: ActionFailed{Cause: CauseSession, Log: logOf("development")}},
				{Name: "review", State: ActionFailed{Cause: CauseSession, Log: logOf("review")}},
			}
			if got.Progress() != tt.want || !reflect.DeepEqual(got.Actions(), want) {
				t.Errorf("Status = %#v, want %#v with %#v", got.Data(), tt.want, want)
			}
		})
	}
}

func TestAnEndedActionShowsItsUsageOnlyWhenItsSessionStartedAndUsageIsShown(t *testing.T) {
	run := given(t, seq(preparing(), inSession("development"), []RunEvent{
		ActionLookupAsked{EventHead: eh(5), Action: "development"},
		ActionLookupDone{EventHead: eh(6), Action: "development", PullRequest: foundPR},
		developmentEnded(6, EndSucceeded{}),
		ActionEnded{EventHead: eh(6), Action: "review", End: EndFailed{Cause: CauseWorkspace}},
	}))
	shown := Some(ShownUsage{Spend: usage.Spend(), PullRequest: foundPR})
	want := []ActionStatus{
		{Name: "development", State: ActionSucceeded{Usage: shown}},
		{Name: "review", State: ActionFailed{Cause: CauseWorkspace}},
	}
	if got := run.Status(at(9), nil, true).Actions(); !reflect.DeepEqual(got, want) {
		t.Errorf("with usage = %#v, want %#v", got, want)
	}
	want[0].State = ActionSucceeded{}
	if got := run.Status(at(9), nil, false).Actions(); !reflect.DeepEqual(got, want) {
		t.Errorf("without usage = %#v, want %#v", got, want)
	}
}

func TestAResumedActionNamesItsWorkspace(t *testing.T) {
	run := given(t, seq(reopening(), []RunEvent{ActionOpened{
		EventHead: eh(3), Action: "development", Workspace: ws("development"), Log: logOf("development"), Resumed: true,
	}}))
	actions := run.Status(at(9), nil, false).Actions()
	if actions[0].Workspace != ws("development").Name || actions[1].Workspace != "" {
		t.Errorf("actions = %#v, want development's workspace named, review's not", actions)
	}
}

func TestTheFailureReportListsTheFailedActions(t *testing.T) {
	got, ok := given(t, endedFailed).FailureReport()
	want := FailureReport{
		IssueID: testID, IssueRef: "#9", Failures: []ActionFailure{failure("development"), failure("review")},
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("FailureReport = %#v, %v, want %#v", got, ok, want)
	}
	for name, events := range map[string][]RunEvent{"a success": endedDone, "a running run": preparing()} {
		if got, ok := given(t, events).FailureReport(); ok {
			t.Errorf("%s has a failure report: %#v", name, got)
		}
	}
}

func TestThePullRequestReportsOfTheTakeAndTheEnding(t *testing.T) {
	run := given(t, endedDone)
	take := run.TakeReport(labelRunning)
	if take.ID() != testRun.TakeReport() || take.IssueID() != testID || take.State() != labelRunning {
		t.Errorf("TakeReport = %#v", take)
	}
	if _, ended := take.End().Get(); ended {
		t.Error("the take report carries an end")
	}
	ending, ok := run.EndingReport(true)
	end, ended := ending.End().Get()
	if !ok || ending.ID() != testRun.EndingReport() || ending.State() != labelDone || !ended ||
		!reflect.DeepEqual(end, NewRuleEnd("implement", run.Status(at(9), nil, true).Actions())) {
		t.Errorf("EndingReport = %#v, %v, want the ending with the actions as the ended status shows them", ending, ok)
	}
	if _, ok := given(t, preparing()).EndingReport(true); ok {
		t.Error("a running run has an ending report")
	}
	actionless, _ := given(t, []RunEvent{taken(), takeMoved(), endedAtOnce}).EndingReport(true)
	if _, ended := actionless.End().Get(); ended {
		t.Error("the ending report of a rule without actions carries an end")
	}
}

func TestAReleasedRunKeepsItsEndingOnlyWhenItEnded(t *testing.T) {
	if _, ok := given(t, snapshots["released"]).EndingReport(false); !ok {
		t.Error("a released run lost its ending")
	}
	givenUp := given(t, []RunEvent{taken(bothActions()...), RunReleased{EventHead: eh(1)}})
	if r, ok := givenUp.EndingReport(false); ok || givenUp.Status(at(1), nil, false).Progress() != (StatusRunning{}) {
		t.Errorf("a take given up has the ending report %#v", r)
	}
}
