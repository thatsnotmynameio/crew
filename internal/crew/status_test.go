package crew_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestStatusCloneSharesNoActions(t *testing.T) {
	s := crew.Status{IssueKey: "74", Kind: crew.StatusRunning, Actions: []crew.ActionStatus{
		{Name: "development", State: crew.ActionRunning, Said: "Starting U2."},
		{Name: "acceptance", State: crew.ActionSucceeded},
	}}

	c := s.Clone()
	c.Actions[0].Said = "changed"
	c.Actions[1].State = crew.ActionFailed

	if s.Actions[0].Said != "Starting U2." || s.Actions[1].State != crew.ActionSucceeded {
		t.Errorf("changing the clone changed the original: %+v", s.Actions)
	}
}
