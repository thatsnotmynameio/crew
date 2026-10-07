package tui

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Covers KTD8 of #151: every event about one issue belongs to that
// issue's popup, and one about no issue to none.
func TestEachEventAboutAnIssueIsThatIssues(t *testing.T) {
	id := issueID("1")
	for _, e := range []core.Event{
		core.IssueTaken{Issue: crew.NewIssue(crew.IssueData{ID: id})},
		core.ActionStarted{IssueID: id},
		core.WorkspaceMissing{IssueID: id},
		core.RunNotRecorded{IssueID: id},
		core.ActionEnded{IssueID: id},
		core.IssueMoved{IssueID: id},
		core.FailureReported{IssueID: id},
		core.IssueSkipped{IssueID: id},
		core.IssueOfOtherKind{IssueID: id},
		core.StatusFailed{IssueID: id},
		core.CallOwed{Call: core.Call{IssueID: id}},
		core.CallDropped{Call: core.Call{IssueID: id}},
	} {
		if got := eventIssue(e); got != id {
			t.Errorf("eventIssue(%T) = %v, want %v", e, got, id)
		}
	}
	if got := eventIssue(core.PollDone{}); got != (crew.IssueID{}) {
		t.Errorf("eventIssue(PollDone) = %v, want the zero id", got)
	}
}
