package crew

import "testing"

func TestKindString(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KindIssue, "issue"},
		{KindPullRequest, "pull request"},
		{Kind(7), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(tt.kind), got, tt.want)
		}
	}
}

func TestTheZeroIssueAndRuleAreOfKindIssue(t *testing.T) {
	if got := (Issue{}).Kind; got != KindIssue {
		t.Errorf("Issue{}.Kind = %v, want %v", got, KindIssue)
	}
	if got := (Rule{}).Takes; got != KindIssue {
		t.Errorf("Rule{}.Takes = %v, want %v", got, KindIssue)
	}
}
