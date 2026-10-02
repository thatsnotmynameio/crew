package crew

import "testing"

func TestStatusCloneSharesNoActions(t *testing.T) {
	s := Status{IssueKey: "74", Kind: StatusRunning, Actions: []ActionStatus{
		{Name: "development", State: ActionRunning, Said: "Starting U2."},
		{Name: "acceptance", State: ActionSucceeded},
	}}

	c := s.Clone()
	c.Actions[0].Said = "changed"
	c.Actions[1].State = ActionFailed

	if s.Actions[0].Said != "Starting U2." || s.Actions[1].State != ActionSucceeded {
		t.Errorf("changing the clone changed the original: %+v", s.Actions)
	}
}
