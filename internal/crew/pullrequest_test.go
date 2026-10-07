package crew

import "testing"

func TestPullRequestReportCloneSharesNoMemory(t *testing.T) {
	r := PullRequestReport{
		ID: "7", IssueID: IssueID{Key: "42"}, IssueRef: "#42", State: "crew:failed",
		End: &RuleEnd{Rule: "development", Actions: []ActionStatus{
			{Name: "lfg", State: ActionFailed, Checks: []CheckResult{{Name: "judge", Reason: NewCheckReason("unfinished")}}},
		}},
	}
	c := r.Clone()
	c.End.Rule = "review"
	c.End.Actions[0].Name = "other"
	c.End.Actions[0].Checks[0].Reason = NewCheckReason("changed")
	if r.End.Rule != "development" || r.End.Actions[0].Name != "lfg" ||
		r.End.Actions[0].Checks[0].Reason.String() != "unfinished" {
		t.Fatalf("changing the clone changed the original: %+v", r.End)
	}
}

func TestPullRequestReportCloneKeepsANilEnd(t *testing.T) {
	if c := (PullRequestReport{ID: "1", State: "crew:in progress"}).Clone(); c.End != nil {
		t.Fatalf("End = %+v, want nil", c.End)
	}
}

func TestRuleEndFailedWhenAnyActionFailed(t *testing.T) {
	ok := RuleEnd{Actions: []ActionStatus{{State: ActionSucceeded}, {State: ActionSucceeded}}}
	failed := RuleEnd{Actions: []ActionStatus{{State: ActionSucceeded}, {State: ActionFailed}}}
	if ok.Failed() || !failed.Failed() {
		t.Fatalf("Failed() = %v, %v, want false, true", ok.Failed(), failed.Failed())
	}
}
