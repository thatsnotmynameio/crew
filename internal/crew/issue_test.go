package crew

import "testing"

func TestTheZeroIssueAndRuleAreOfKindIssue(t *testing.T) {
	if got := (Issue{}).Kind; got != KindIssue {
		t.Errorf("Issue{}.Kind = %v, want %v", got, KindIssue)
	}
	if got := (Rule{}).Takes; got != KindIssue {
		t.Errorf("Rule{}.Takes = %v, want %v", got, KindIssue)
	}
}
