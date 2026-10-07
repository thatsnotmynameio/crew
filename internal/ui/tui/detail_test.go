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
	head := crew.EventHead{IssueID: id}
	for _, e := range []core.Published{
		crew.RunTaken{EventHead: head, Issue: crew.IssueData{ID: id}},
		crew.ActionSessionStarted{EventHead: head},
		crew.WorkspaceMissing{EventHead: head},
		core.RunNotRecorded{IssueID: id},
		crew.ActionEnded{EventHead: head},
		crew.TakeMoved{EventHead: head},
		crew.VerdictMoved{EventHead: head},
		crew.FailureReported{EventHead: head},
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
