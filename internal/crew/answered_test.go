package crew

import (
	"reflect"
	"slices"
	"testing"
)

// The tables below follow a run of the answered rule: its one action,
// answer, reads the item's comments and ends with the check's verdict, and
// its passed route moves the item to the label the check found (R9, R10,
// KTD2, KTD4, KTD6, KTD7).

// returnedEnd is answer's end at minute n, after a check that found
// crew:deps:ready.
func returnedEnd(n int) RunEvent {
	return ActionEnded{
		EventHead: ah(n), Action: "answer", Verdict: Passed, Target: Next{},
		End: EndSucceeded{Reason: NewSessionText(`an answer counts, so the item returns to "crew:deps:ready"`)},
	}
}

// returnedTo is the passed route chosen at minute n, whose one step moves
// the item to crew:deps:ready, and that move asked.
func returnedTo(n int) []RunEvent {
	return []RunEvent{
		RouteChosen{
			EventHead: ah(n), Route: PassedRoute, Action: "answer",
			Steps: []StepPlan{{Kind: StepMove, To: labelDepsReady}},
		},
		StepAsked{EventHead: ah(n), Step: 0},
	}
}

// checkFailedEnd is answer's end at minute n, after a check that failed
// with v, saying reason.
func checkFailedEnd(n int, v Verdict, reason string) RunEvent {
	return ActionEnded{
		EventHead: ah(n), Action: "answer", Verdict: v, Target: toFailed,
		End: EndSucceeded{Reason: NewSessionText(reason)},
	}
}

// answeredFailed is the failed route chosen at minute n, a report then the
// move to crew:answered:failed, and its report asked.
func answeredFailed(n int) []RunEvent {
	return []RunEvent{
		RouteChosen{
			EventHead: ah(n), Route: FailedRoute, Action: "answer",
			Steps: []StepPlan{{Kind: StepReport}, {Kind: StepMove, To: labelAnsweredFailed}},
		},
		StepAsked{EventHead: ah(n), Step: 0},
	}
}

// depsReady is a check that found crew:deps:ready.
var depsReady = ReturnCheck{To: labelDepsReady}

// answeredDecisions decide the answered rule's run.
var answeredDecisions = []decision{
	{
		name: "take: KTD2, a rule whose only action checks the return asks no workspace, session, script or function",
		def:  answeredRule, finds: true, given: []RunEvent{answeredTake()}, fact: landed(),
		want: []RunEvent{answeredMoved(), ActionReturnAsked{EventHead: ah(1), Action: "answer"}},
	},
	{
		name: "take: after a stop, answer ends without reading, and the run chooses failed",
		def:  answeredRule, given: []RunEvent{answeredTake(), RunStopped{EventHead: ah(0)}}, fact: landed(),
		want: seq([]RunEvent{
			answeredMoved(),
			ActionEnded{EventHead: ah(1), Action: "answer", End: unstarted, Verdict: Failed, Target: toFailed},
		}, answeredFailed(1)),
	},
	{
		name: "returnChecked: R9, KTD4, a label ends answer with passed, and passed moves the item to that label",
		def:  answeredRule, finds: true, given: checkingReturn(), fact: returnChecked(2, depsReady),
		want: seq([]RunEvent{returnedEnd(2)}, returnedTo(2)),
	},
	{
		name: "returnChecked: R10, unanswered ends answer with unanswered, and the run reports and fails",
		def:  answeredRule, given: checkingReturn(), fact: returnChecked(2, ReturnCheck{Failure: Unanswered}),
		want: seq(
			[]RunEvent{checkFailedEnd(2, Unanswered, "no answer counts after the question")}, answeredFailed(2),
		),
	},
	{
		name: "returnChecked: no-question ends answer with no-question, and the run reports and fails",
		def:  answeredRule, given: checkingReturn(), fact: returnChecked(2, ReturnCheck{Failure: NoQuestion}),
		want: seq(
			[]RunEvent{checkFailedEnd(2, NoQuestion, "crew found no open question the config declares")},
			answeredFailed(2),
		),
	},
	{
		name: "returnChecked: unread ends answer with unread, and the run reports and fails",
		def:  answeredRule, given: checkingReturn(), fact: returnChecked(2, ReturnCheck{Failure: Unread}),
		want: seq(
			[]RunEvent{checkFailedEnd(2, Unread, "crew could not read the item's comments")}, answeredFailed(2),
		),
	},
	{
		name: "stop: KTD6, a stop while crew reads asks nothing to stop",
		def:  answeredRule, given: checkingReturn(), fact: StopReached{FactHead: fh(2)},
		want: []RunEvent{RunStopped{EventHead: ah(2)}},
	},
	{
		name: "returnChecked: KTD6, the check's label stands after a stop",
		def:  answeredRule, given: seq(checkingReturn(), []RunEvent{RunStopped{EventHead: ah(2)}}),
		fact: returnChecked(3, depsReady),
		want: seq([]RunEvent{returnedEnd(3)}, returnedTo(3)),
	},
	{
		name: "returnChecked: the check's verdict stands after crew's run time was up",
		def:  answeredRule, given: seq(checkingReturn(), []RunEvent{RunOutOfTime{EventHead: ah(2)}}),
		fact: returnChecked(3, ReturnCheck{Failure: Unanswered}),
		want: seq(
			[]RunEvent{checkFailedEnd(3, Unanswered, "no answer counts after the question")}, answeredFailed(3),
		),
	},
}

func TestDecideTheAnsweredRule(t *testing.T) { decide(t, answeredDecisions) }

func TestAReturnStepPlansAMoveWhoseLabelTheCheckGives(t *testing.T) {
	got := plans(Route{Name: PassedRoute, Steps: []Step{ReturnStep{}}})
	if want := []StepPlan{{Kind: StepMove}}; !reflect.DeepEqual(got, want) {
		t.Errorf("plans = %#v, want %#v", got, want)
	}
}

func TestTheAnsweredRuleNeedsNoWorkspace(t *testing.T) {
	if answeredRule(sequence()).Rule.needsWorkspace() {
		t.Error("the answered rule needs a workspace, want none")
	}
}

func TestAnAnswerActionRunsWhileCrewReads(t *testing.T) {
	run := given(t, checkingReturn())
	a, _ := run.Cursor()
	if a.State() != (InReturnCheck{Started: at(1)}) || !a.running() || !a.started() {
		t.Errorf("answer = %#v, want it reading since minute 1", a)
	}
	want := []ActionStatus{{Name: "answer", State: ActionRunning{Started: at(1)}}}
	if got := run.Status(at(2), nil, false).Data().Actions; !reflect.DeepEqual(got, want) {
		t.Errorf("Status actions = %#v, want %#v", got, want)
	}
}

// answeredPast is a run of the answered rule that decided facts.
func answeredPast(facts ...Fact) past {
	return past{take: answeredTake(), change: answeredRule, facts: facts}
}

// answeredStarts are the starts after a run of the answered rule: always
// fresh, checking again, since its passed route never runs alone (KTD4).
func answeredStarts() []startCase {
	returned := returnChecked(2, depsReady)
	crashedBeforeRoute := answeredPast(landed(), returned)
	crashedBeforeRoute.drop = 2
	return []startCase{
		{
			name: "KTD4: returned, its move given up: fresh, checking again",
			past: answeredPast(landed(), returned, settled(3, 0, StepGivenUp{Reason: "refused"})),
			want: StartFresh{},
		},
		{
			name: "returned, crashed before its move settled: fresh",
			past: answeredPast(landed(), returned), want: StartFresh{},
		},
		{name: "answer passed and crew crashed before the route: fresh", past: crashedBeforeRoute, want: StartFresh{}},
		{
			name: "returned: fresh",
			past: answeredPast(landed(), returned, settled(3, 0, StepLanded{})), want: StartFresh{},
		},
		{
			name: "failed with unanswered: fresh",
			past: answeredPast(
				landed(), returnChecked(2, ReturnCheck{Failure: Unanswered}),
				settled(3, 0, StepLanded{}), settled(4, 1, StepLanded{}),
			),
			want: StartFresh{},
		},
		{
			name: "failed with unread, crashed before its report: fresh",
			past: answeredPast(landed(), returnChecked(2, ReturnCheck{Failure: Unread})), want: StartFresh{},
		},
		{name: "crashed while crew read: fresh", past: answeredPast(landed()), want: StartFresh{}},
		{
			name: "stopped before answer started: fresh",
			past: answeredPast(StopReached{FactHead: fh(0)}, landed()), want: StartFresh{},
		},
	}
}

func TestTheStartOfARunAfterTheAnsweredRule(t *testing.T) {
	rule := answeredRule(sequence()).Rule
	for _, tc := range answeredStarts() {
		t.Run(tc.name, func(t *testing.T) {
			h := folded(tc.past.events(t))
			if got := h.Start(testID, rule); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Start =\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}

func TestTheAnsweredRulesReportNamesWhyItsCheckFailed(t *testing.T) {
	report := func(route RouteName, v Verdict, reason FailureReason) FailureReport {
		return FailureReport{
			IssueID: testID, IssueRef: "#9", Rule: "answered", Route: route,
			Failures: []ActionFailure{{Action: "answer", Verdict: v, Reason: reason}},
		}
	}
	tests := []struct {
		name string
		past past
		want FailureReport
	}{
		{
			name: "no-question", past: answeredPast(landed(), returnChecked(2, ReturnCheck{Failure: NoQuestion})),
			want: report(FailedRoute, NoQuestion, ReasonNoQuestion),
		},
		{
			name: "unanswered", past: answeredPast(landed(), returnChecked(2, ReturnCheck{Failure: Unanswered})),
			want: report(FailedRoute, Unanswered, ReasonUnanswered),
		},
		{
			name: "unread", past: answeredPast(landed(), returnChecked(2, ReturnCheck{Failure: Unread})),
			want: report(FailedRoute, Unread, ReasonUnread),
		},
		{
			name: "returned", past: answeredPast(landed(), returnChecked(2, depsReady)),
			want: report(PassedRoute, Passed, NoFailureReason),
		},
		{
			name: "stopped before it read", past: answeredPast(StopReached{FactHead: fh(0)}, landed()),
			want: report(FailedRoute, Failed, NoFailureReason),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := given(t, tt.past.events(t)).FailureReport(); !ok || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FailureReport = %#v, %v, want %#v", got, ok, tt.want)
			}
		})
	}
}

// A session that reports a verdict of the check's name still carries no
// reason: only the answered rule's check words one.
func TestASessionsReportCarriesNoReasonWhateverItsVerdict(t *testing.T) {
	onUnanswered := func(d RunDefinition) RunDefinition {
		d.Rule.Actions = slices.Clone(d.Rule.Actions)
		d.Rule.Actions[1].On = On{Unanswered: ToRoute{Route: "blocked"}}
		return d
	}
	p := past{change: onUnanswered, facts: slices.Concat(toJudge()[:4], []Fact{
		lfgEnded(succeeded("done"), VerdictReported{Verdict: Unanswered}),
	})}
	want := failure("lfg")
	want.Verdict = Unanswered
	got, ok := given(t, p.events(t)).FailureReport()
	if !ok || !reflect.DeepEqual(got.Failures, []ActionFailure{want}) {
		t.Errorf("FailureReport = %#v, %v, want lfg's failure without a reason", got, ok)
	}
}
