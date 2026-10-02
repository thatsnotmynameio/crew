package crew

import "testing"

func TestPullRequestReportCloneSharesNoMemory(t *testing.T) {
	r := PullRequestReport{
		ID: "7", IssueKey: "42", IssueRef: "#42", State: "crew:failed",
		End: &StageEnd{Stage: "development", Actions: []ActionStatus{{Name: "lfg", State: ActionFailed}}},
	}
	c := r.Clone()
	c.End.Stage = "review"
	c.End.Actions[0].Name = "other"
	if r.End.Stage != "development" || r.End.Actions[0].Name != "lfg" {
		t.Fatalf("changing the clone changed the original: %+v", r.End)
	}
}

func TestPullRequestReportCloneKeepsANilEnd(t *testing.T) {
	if c := (PullRequestReport{ID: "1", State: "crew:in progress"}).Clone(); c.End != nil {
		t.Fatalf("End = %+v, want nil", c.End)
	}
}

func TestStageEndFailedWhenAnyActionFailed(t *testing.T) {
	ok := StageEnd{Actions: []ActionStatus{{State: ActionSucceeded}, {State: ActionSucceeded}}}
	failed := StageEnd{Actions: []ActionStatus{{State: ActionSucceeded}, {State: ActionFailed}}}
	if ok.Failed() || !failed.Failed() {
		t.Fatalf("Failed() = %v, %v, want false, true", ok.Failed(), failed.Failed())
	}
}
