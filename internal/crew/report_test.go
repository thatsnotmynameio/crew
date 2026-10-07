package crew

import (
	"reflect"
	"testing"
)

// The statuses and reports below mirror internal/core's status,
// actionState, the report step's report and reportPullRequests.

// installRan is install's shell line as its status shows it.
var installRan = NewShellReason("install passed")

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
				{Name: "install", State: ActionSucceeded{Verdict: Passed}, Shell: installRan},
				{Name: "lfg", State: ActionRunning{Started: at(4), Said: NewSaid("Reading the diff.")}},
				{Name: "judge", State: ActionAwaitingTurn{}},
			},
		},
		{
			name: "in a shell", given: judging(),
			want: []ActionStatus{
				{Name: "install", State: ActionSucceeded{Verdict: Passed}, Shell: installRan},
				{Name: "lfg", State: ActionSucceeded{Verdict: Passed, Usage: Some(ShownUsage{Spend: usage.Spend()})}},
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

func TestAnActionNotRunningShowsWhyItHasNoTimeToShow(t *testing.T) {
	failedAtInstall := seq(installing(), []RunEvent{
		ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(2, "install exited with status 2")},
		ActionEnded{
			EventHead: eh(3), Action: "install", Verdict: Failed, Target: toFailed,
			End: EndFailed{Cause: CauseShell},
		},
		chose(3, FailedRoute, "install"),
	})
	tests := []struct {
		name  string
		given []RunEvent
		want  []ActionState
	}{
		{
			name: "awaiting the take", given: []RunEvent{taken()},
			want: []ActionState{ActionPending{}, ActionAwaitingTurn{}, ActionAwaitingTurn{}},
		},
		{
			name: "preparing", given: asking(),
			want: []ActionState{ActionPending{}, ActionAwaitingTurn{}, ActionAwaitingTurn{}},
		},
		{
			name:  "starting",
			given: starting(),
			want:  []ActionState{ActionSucceeded{Verdict: Passed}, ActionPending{}, ActionAwaitingTurn{}},
		},
		{
			name: "resumed at lfg", given: reopening(),
			want: []ActionState{ActionDoneInEarlierRun{}, ActionPending{}, ActionAwaitingTurn{}},
		},
		{
			name: "ended early", given: failedAtInstall,
			want: []ActionState{ActionFailed{Cause: CauseShell, Log: runLog}, ActionNotRun{}, ActionNotRun{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []ActionState
			for _, a := range given(t, tt.given).Status(at(9), nil, true).Actions() {
				got = append(got, a.State)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("states = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAShellActionShowsItsLineAndASessionNone(t *testing.T) {
	stripped := seq(judging(), []RunEvent{
		ActionShellEnded{
			EventHead: eh(6), Action: "judge",
			Outcome: exited(3, "judge exited with status 3: \x1b[31mneeds a person\x1b[0m"),
		},
		ActionEnded{
			EventHead: eh(6), Action: "judge", End: EndSucceeded{}, Verdict: "needs_person",
			Target: ToRoute{Route: "needs-person"},
		},
	})
	got := given(t, stripped).Status(at(9), nil, false).Actions()
	want := []ActionStatus{
		{Name: "install", State: ActionSucceeded{Verdict: Passed}, Shell: installRan},
		{Name: "lfg", State: ActionSucceeded{Verdict: Passed}},
		{
			Name: "judge", State: ActionSucceeded{Verdict: "needs_person"},
			Shell: NewShellReason("judge exited with status 3: needs a person"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Actions =\n%#v\nwant\n%#v", got, want)
	}
	if line := got[2].Shell.String(); line != "judge exited with status 3: needs a person" {
		t.Errorf("judge's line = %q, want it without control characters", line)
	}
}

func TestAnEndedStatusCarriesTheRouteItsMoveOrCloseAndHowItStands(t *testing.T) {
	tests := []struct {
		name  string
		given []RunEvent
		want  StatusEnded
	}{
		{name: "routing", given: lfgFailed(), want: StatusEnded{Route: FailedRoute, To: labelFailed, Move: MovePending}},
		{
			name: "moved", given: releasedAs(StepLanded{}),
			want: StatusEnded{Route: FailedRoute, To: labelFailed, Move: MoveDone},
		},
		{
			name: "given up", given: releasedAs(StepGivenUp{}),
			want: StatusEnded{Route: FailedRoute, To: labelFailed, Move: MoveDropped},
		},
		{
			name: "dropped", given: releasedAs(StepDropped{}),
			want: StatusEnded{Route: FailedRoute, To: labelFailed, Move: MoveDropped},
		},
		{
			name: "closed", given: append(lfgFailedThrough(CloseStep{}), stepEnded(6, 0, StepLanded{})),
			want: StatusEnded{Route: FailedRoute, Move: MoveDone},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := given(t, tt.given).Status(at(9), nil, false)
			want := []ActionStatus{
				{Name: "install", State: ActionSucceeded{Verdict: Passed}, Shell: installRan},
				{Name: "lfg", State: ActionFailed{Cause: CauseSession, Log: runLog}},
				{Name: "judge", State: ActionNotRun{}},
			}
			if got.Progress() != tt.want || !reflect.DeepEqual(got.Actions(), want) {
				t.Errorf("Status = %#v, want %#v with %#v", got.Data(), tt.want, want)
			}
		})
	}
}

func TestAnEndedStatusListsTheRoutesStepsAndHowEachSettled(t *testing.T) {
	failed := StepFailed{Reason: NewShellReason("the route's shell step notify exited with status 1")}
	run := given(t, append(lfgFailedThrough(notify, ReportStep{}, moveFailed),
		stepEnded(6, 0, failed), asked(6, 1)))
	want := []StepStatus{
		{Step: StepPlan{Kind: StepShell, Shell: "notify"}, Outcome: failed},
		{Step: StepPlan{Kind: StepReport}},
		{Step: StepPlan{Kind: StepMove, To: labelFailed}},
	}
	if got := run.Status(at(9), nil, false).Steps(); !reflect.DeepEqual(got, want) {
		t.Errorf("Steps =\n%#v\nwant\n%#v", got, want)
	}
	if got := given(t, inSession()).Status(at(9), nil, false).Steps(); got != nil {
		t.Errorf("a running run's Steps = %#v, want none", got)
	}
}

func TestAnEndedActionShowsItsUsageOnlyWhenItsSessionStartedAndUsageIsShown(t *testing.T) {
	run := given(t, seq(lookingUp(), []RunEvent{RunLookupDone{EventHead: eh(7), PullRequest: foundPR}}))
	shown := Some(ShownUsage{Spend: usage.Spend(), PullRequest: foundPR})
	judged := NewShellReason("judge passed")
	want := []ActionStatus{
		{Name: "install", State: ActionSucceeded{Verdict: Passed}, Shell: installRan},
		{Name: "lfg", State: ActionSucceeded{Verdict: Passed, Usage: shown}},
		{Name: "judge", State: ActionSucceeded{Verdict: Passed}, Shell: judged},
	}
	if got := run.Status(at(9), nil, true).Actions(); !reflect.DeepEqual(got, want) {
		t.Errorf("with usage = %#v, want %#v", got, want)
	}
	want[1].State = ActionSucceeded{Verdict: Passed}
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

func TestTheReportNamesTheActionAtTheCursorItsVerdictTheRouteAndTheLog(t *testing.T) {
	blocked := seq(inSession(), []RunEvent{
		lfgSession(succeeded("blocked on #12")),
		lfgEnd(Judged{Verdict: "blocked", End: EndSucceeded{}}, ToRoute{Route: "blocked"}),
		chose(5, "blocked", "lfg"),
	})
	lfg := failure("lfg")
	lfg.Verdict = "blocked"
	judge := failure("judge")
	judge.Verdict = Passed
	tests := []struct {
		name  string
		given []RunEvent
		want  FailureReport
	}{
		{
			name: "blocked", given: blocked,
			want: FailureReport{
				IssueID: testID, IssueRef: "#9", Rule: "implement", Route: "blocked", Failures: []ActionFailure{lfg},
			},
		},
		{
			name: "passed", given: passedAll(),
			want: FailureReport{
				IssueID: testID, IssueRef: "#9", Rule: "implement", Route: PassedRoute, Failures: []ActionFailure{judge},
			},
		},
		{
			name: "a rule without actions", given: []RunEvent{takenWithoutActions(), takeMoved(), chose(1, FailedRoute, "")},
			want: FailureReport{IssueID: testID, IssueRef: "#9", Rule: "implement", Route: FailedRoute},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := given(t, tt.given).FailureReport(); !ok || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FailureReport = %#v, %v, want %#v", got, ok, tt.want)
			}
		})
	}
	if got, ok := given(t, asking()).FailureReport(); ok {
		t.Errorf("a running run has a report: %#v", got)
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
		!reflect.DeepEqual(end, NewRuleEnd("implement", PassedRoute, run.Status(at(9), nil, true).Actions())) {
		t.Errorf("EndingReport = %#v, %v, want the ending with the actions as the ended status shows them", ending, ok)
	}
	if _, ok := given(t, asking()).EndingReport(true); ok {
		t.Error("a running run has an ending report")
	}
	actionless, _ := given(t, []RunEvent{
		takenWithoutActions(), takeMoved(), chose(1, PassedRoute, ""),
	}).EndingReport(true)
	if _, ended := actionless.End().Get(); ended {
		t.Error("the ending report of a rule without actions carries an end")
	}
}

// R51: after a close, the pull request report puts the pull requests in no
// state, and its end names the route.
func TestTheEndingReportOfACloseCarriesNoState(t *testing.T) {
	closing := given(t, lfgFailedThrough(CloseStep{}))
	report, ok := closing.EndingReport(false)
	end, ended := report.End().Get()
	if !ok || report.State() != "" || !ended || end.Route() != FailedRoute {
		t.Errorf("EndingReport = %#v, %v, want no state and the end through failed", report, ok)
	}
	actionless := given(t, []RunEvent{
		takenWithoutActions(), takeMoved(),
		RouteChosen{EventHead: eh(1), Route: PassedRoute, Steps: []StepPlan{{Kind: StepClose}}},
	})
	if r, ok := actionless.EndingReport(false); ok {
		t.Errorf("a rule without actions that closes has the ending report %#v", r)
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
