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
		Progress: StatusEnded{To: "crew:needs attention", Move: MoveDone},
		Actions: []ActionStatus{
			{Name: "acceptance", State: ActionSucceeded{}},
			{Name: "development", State: ActionFailed{Cause: CauseSession, Log: ".crew/logs/issue-74-development.log"}},
		},
		Updated: updated, Run: "run-1",
	}
	s := NewStatus(d)

	if s.IssueID() != d.IssueID || s.IssueRef() != "#74" || s.Rule() != "implement" ||
		s.Progress() != d.Progress || s.Updated() != updated || s.Run() != "run-1" {
		t.Errorf("status = %+v, want the fields of %+v", s, d)
	}
	if !reflect.DeepEqual(s.Actions(), d.Actions) || !reflect.DeepEqual(s.Data(), d) {
		t.Errorf("Actions() = %+v, Data() = %+v, want %+v", s.Actions(), s.Data(), d)
	}
}

func TestStatusSharesNoActions(t *testing.T) {
	built := []ActionStatus{
		{Name: "development", State: ActionRunning{Said: NewSaid("Starting U2.")}},
		{Name: "acceptance", State: ActionSucceeded{}, Checks: []CheckResult{{Name: "judge", Passed: true}}},
	}
	s := NewStatus(StatusData{IssueID: IssueID{Key: "74"}, Progress: StatusRunning{}, Actions: built})
	built[0].Name = "changed"
	built[1].Checks[0].Passed = false

	returned := s.Actions()
	returned[0].State = ActionFailed{Cause: CauseStopped}
	returned[1].Checks[0].Passed = false
	data := s.Data()
	data.Actions[1].Checks[0].Passed = false

	got := s.Actions()
	if got[0].Name != "development" || got[0].State != (ActionRunning{Said: NewSaid("Starting U2.")}) ||
		!got[1].Checks[0].Passed {
		t.Errorf("changing the actions it was built from or returned changed the status: %+v", got)
	}
}

func TestFailedCheckIsTheReasonOfTheLastCheckOfACheckFailure(t *testing.T) {
	checks := []CheckResult{
		{Name: "judge", Passed: true, Reason: NewCheckReason("the check judge passed: done (0.97)")},
		{Name: "pr-closes-issue", Reason: NewCheckReason("the check pr-closes-issue failed: no open pull request")},
	}
	tests := []struct {
		name   string
		action ActionStatus
		want   CheckReason
	}{
		{name: "check", action: ActionStatus{State: ActionFailed{Cause: CauseCheck}, Checks: checks}, want: checks[1].Reason},
		{name: "check without results", action: ActionStatus{State: ActionFailed{Cause: CauseCheck}}},
		{name: "stopped while checking", action: ActionStatus{State: ActionFailed{Cause: CauseStopped}, Checks: checks[:1]}},
		{name: "succeeded", action: ActionStatus{State: ActionSucceeded{}, Checks: checks}},
		{name: "running its checks", action: ActionStatus{State: ActionRunning{}, Checks: checks}},
	}
	for _, tt := range tests {
		if got := tt.action.FailedCheck(); got != tt.want {
			t.Errorf("%s: FailedCheck() = %q, want %q", tt.name, got.String(), tt.want.String())
		}
	}
}
