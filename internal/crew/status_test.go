package crew

import (
	"reflect"
	"testing"
	"time"
)

func TestStatusReturnsWhatItWasBuiltFrom(t *testing.T) {
	updated := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	d := StatusData{
		IssueID: IssueID{Key: "74"}, IssueRef: "#74", Rule: "implement",
		Progress: StatusEnded{Route: FailedRoute, To: "crew:needs attention", Move: MoveDone},
		Actions: []ActionStatus{
			{Name: "acceptance", State: ActionSucceeded{}},
			{Name: "development", State: ActionFailed{Cause: CauseSession, Log: ".crew/logs/issue-74-development.log"}},
		},
		Steps:   []StepStatus{{Step: StepPlan{Kind: StepMove, To: "crew:needs attention"}, Outcome: StepLanded{}}},
		Updated: updated, Run: "run-1",
	}
	s := NewStatus(d)

	if s.IssueID() != d.IssueID || s.IssueRef() != "#74" || s.Rule() != "implement" ||
		s.Progress() != d.Progress || s.Updated() != updated || s.Run() != "run-1" {
		t.Errorf("status = %+v, want the fields of %+v", s, d)
	}
	if !reflect.DeepEqual(s.Actions(), d.Actions) || !reflect.DeepEqual(s.Steps(), d.Steps) ||
		!reflect.DeepEqual(s.Data(), d) {
		t.Errorf("Actions() = %+v, Steps() = %+v, Data() = %+v, want %+v", s.Actions(), s.Steps(), s.Data(), d)
	}
}

func TestStatusSharesNoActionsOrSteps(t *testing.T) {
	built := []ActionStatus{
		{Name: "development", State: ActionRunning{Said: NewSaid("Starting U2.")}},
		{Name: "judge", State: ActionSucceeded{}, Shell: NewCheckReason("judge passed")},
	}
	steps := []StepStatus{{Step: StepPlan{Kind: StepReport}}}
	s := NewStatus(StatusData{IssueID: IssueID{Key: "74"}, Progress: StatusRunning{}, Actions: built, Steps: steps})
	built[0].Name = "changed"
	steps[0].Outcome = StepLanded{}

	returned := s.Actions()
	returned[0].State = ActionFailed{Cause: CauseStopped}
	s.Steps()[0].Outcome = StepLanded{}
	data := s.Data()
	data.Actions[1].Shell = NewCheckReason("changed")
	data.Steps[0].Outcome = StepLanded{}

	got := s.Actions()
	if got[0].Name != "development" || got[0].State != (ActionRunning{Said: NewSaid("Starting U2.")}) ||
		got[1].Shell.String() != "judge passed" || s.Steps()[0].Outcome != nil {
		t.Errorf("changing what it was built from or returned changed the status: %+v, %+v", got, s.Steps())
	}
}
