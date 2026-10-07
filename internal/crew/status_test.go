package crew

import "testing"

func TestStatusCloneSharesNoActions(t *testing.T) {
	s := Status{IssueKey: "74", Kind: StatusRunning, Actions: []ActionStatus{
		{Name: "development", State: ActionRunning, Said: NewSaid("Starting U2.")},
		{Name: "acceptance", State: ActionSucceeded, Checks: []CheckResult{{Name: "judge", Passed: true}}},
	}}

	c := s.Clone()
	c.Actions[0].Said = NewSaid("changed")
	c.Actions[1].State = ActionFailed
	c.Actions[1].Checks[0].Passed = false

	if s.Actions[0].Said.String() != "Starting U2." ||
		s.Actions[1].State != ActionSucceeded || !s.Actions[1].Checks[0].Passed {
		t.Errorf("changing the clone changed the original: %+v", s.Actions)
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
		{name: "check", action: ActionStatus{Cause: CauseCheck, Checks: checks}, want: checks[1].Reason},
		{name: "check without results", action: ActionStatus{Cause: CauseCheck}},
		{name: "stopped while checking", action: ActionStatus{Cause: CauseStopped, Checks: checks[:1]}},
	}
	for _, tt := range tests {
		if got := tt.action.FailedCheck(); got != tt.want {
			t.Errorf("%s: FailedCheck() = %q, want %q", tt.name, got.String(), tt.want.String())
		}
	}
}
