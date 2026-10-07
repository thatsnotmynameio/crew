package crew

import (
	"reflect"
	"testing"
)

// The statuses and reports below mirror internal/core's status,
// actionState, the report step's report and reportPullRequests.

func TestARunningStatusShowsEachActionAsItStands(t *testing.T) {
	said := map[ActionName]Said{"lfg": NewSaid("Reading the diff.")}
	tests := []struct {
		name  string
		given []RunEvent
		want  []ActionStatus
	}{
		{
			name: "in a session", given: inSession(),
			want: []ActionStatus{
				{Name: "install", State: ActionSucceeded{}},
				{Name: "lfg", State: ActionRunning{Started: at(4), Said: NewSaid("Reading the diff.")}},
				{Name: "judge", State: ActionPending{}},
			},
		},
		{
			name: "in a shell", given: judging(),
			want: []ActionStatus{
				{Name: "install", State: ActionSucceeded{}},
				{Name: "lfg", State: ActionSucceeded{Usage: Some(ShownUsage{Spend: usage.Spend()})}},
				{Name: "judge", State: ActionRunning{Started: at(5)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := given(t, tt.given).Status(at(9), said, true)
			want := NewStatus(StatusData{
				IssueID: testID, IssueRef: "#9", Rule: "implement", Progress: StatusRunning{}, Updated: at(9),
				Run: testRun, Actions: tt.want,
			})
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Status =\n%#v\nwant\n%#v", got.Data(), want.Data())
			}
		})
	}
}

func TestAnActionWithoutASessionOrScriptToTimeIsPending(t *testing.T) {
	failedAtInstall := seq(installing(), []RunEvent{
		ActionEnded{
			EventHead: eh(3), Action: "install", Verdict: Failed, Target: toFailed,
			End: EndFailed{Cause: CauseShell},
		},
		chose(3, FailedRoute, "install"),
	})
	for name, events := range map[string][]RunEvent{
		"awaiting the take":       {taken()},
		"preparing":               asking(),
		"starting":                starting(),
		"done in an earlier run":  reopening(),
		"not run after a failure": failedAtInstall,
	} {
		run := given(t, events)
		for i, a := range run.Status(at(9), nil, true).Actions() {
			if !run.Actions()[i].Ended() && a.State != (ActionPending{}) {
				t.Errorf("%s: %s is %#v, want it pending", name, a.Name, a.State)
			}
		}
	}
}

func TestAnEndedStatusCarriesTheRoutesMoveAndHowItStands(t *testing.T) {
	tests := []struct {
		name  string
		given []RunEvent
		want  StatusEnded
	}{
		{name: "routing", given: lfgFailed(), want: StatusEnded{To: labelFailed, Move: MovePending}},
		{name: "moved", given: releasedAs(StepLanded{}), want: StatusEnded{To: labelFailed, Move: MoveDone}},
		{name: "given up", given: releasedAs(StepGivenUp{}), want: StatusEnded{To: labelFailed, Move: MoveDropped}},
		{name: "dropped", given: releasedAs(StepDropped{}), want: StatusEnded{To: labelFailed, Move: MoveDropped}},
		{
			name: "closed", given: append(lfgFailedThrough(CloseStep{}), stepEnded(6, 0, StepLanded{})),
			want: StatusEnded{Move: MoveDone},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := given(t, tt.given).Status(at(9), nil, false)
			want := []ActionStatus{
				{Name: "install", State: ActionSucceeded{}},
				{Name: "lfg", State: ActionFailed{Cause: CauseSession, Log: runLog}},
				{Name: "judge", State: ActionPending{}},
			}
			if got.Progress() != tt.want || !reflect.DeepEqual(got.Actions(), want) {
				t.Errorf("Status = %#v, want %#v with %#v", got.Data(), tt.want, want)
			}
		})
	}
}

func TestAnEndedActionShowsItsUsageOnlyWhenItsSessionStartedAndUsageIsShown(t *testing.T) {
	run := given(t, seq(lookingUp(), []RunEvent{RunLookupDone{EventHead: eh(7), PullRequest: foundPR}}))
	shown := Some(ShownUsage{Spend: usage.Spend(), PullRequest: foundPR})
	want := []ActionStatus{
		{Name: "install", State: ActionSucceeded{}},
		{Name: "lfg", State: ActionSucceeded{Usage: shown}},
		{Name: "judge", State: ActionSucceeded{}},
	}
	if got := run.Status(at(9), nil, true).Actions(); !reflect.DeepEqual(got, want) {
		t.Errorf("with usage = %#v, want %#v", got, want)
	}
	want[1].State = ActionSucceeded{}
	if got := run.Status(at(9), nil, false).Actions(); !reflect.DeepEqual(got, want) {
		t.Errorf("without usage = %#v, want %#v", got, want)
	}
}

func TestAResumedRunNamesItsWorkspaceOnTheActionsThatRan(t *testing.T) {
	run := given(t, seq(reopening(), []RunEvent{
		WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog, Resumed: true},
		ActionSessionAsked{EventHead: eh(2), Action: "lfg"},
		ActionSessionStarted{EventHead: eh(3), Action: "lfg", Bot: developer},
	}))
	actions := run.Status(at(9), nil, false).Actions()
	if actions[1].Workspace != runWS().Name || actions[0].Workspace != "" || actions[2].Workspace != "" {
		t.Errorf("actions = %#v, want lfg's workspace named, the others' not", actions)
	}
}

func TestTheReportNamesTheActionThatEndedTheSequence(t *testing.T) {
	got, ok := given(t, lfgFailed()).FailureReport()
	want := FailureReport{
		IssueID: testID, IssueRef: "#9", Failures: []ActionFailure{failure("lfg")},
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("FailureReport = %#v, %v, want %#v", got, ok, want)
	}
	if got, ok := given(t, passedAll()).FailureReport(); !ok || got.Failures[0].Action != "judge" {
		t.Errorf("FailureReport through passed = %#v, %v, want judge named", got, ok)
	}
	actionless := []RunEvent{takenWithoutActions(), takeMoved(), chose(1, PassedRoute, "")}
	for name, events := range map[string][]RunEvent{"a rule without actions": actionless, "a running run": asking()} {
		if got, ok := given(t, events).FailureReport(); ok {
			t.Errorf("%s has a report: %#v", name, got)
		}
	}
}

func TestThePullRequestReportsOfTheTakeAndTheEnding(t *testing.T) {
	run := given(t, passedAll())
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
	if _, ok := given(t, asking()).EndingReport(true); ok {
		t.Error("a running run has an ending report")
	}
	if _, ok := given(t, lfgFailedThrough(CloseStep{})).EndingReport(true); ok {
		t.Error("a route that closes the issue has an ending report")
	}
	actionless, _ := given(t, []RunEvent{
		takenWithoutActions(), takeMoved(), chose(1, PassedRoute, ""),
	}).EndingReport(true)
	if _, ended := actionless.End().Get(); ended {
		t.Error("the ending report of a rule without actions carries an end")
	}
}

func TestAReleasedRunKeepsItsEndingOnlyWhenItChoseARoute(t *testing.T) {
	if _, ok := given(t, released()).EndingReport(false); !ok {
		t.Error("a released run lost its ending")
	}
	givenUp := given(t, []RunEvent{taken(), RunReleased{EventHead: eh(1)}})
	if r, ok := givenUp.EndingReport(false); ok || givenUp.Status(at(1), nil, false).Progress() != (StatusRunning{}) {
		t.Errorf("a take given up has the ending report %#v", r)
	}
}
