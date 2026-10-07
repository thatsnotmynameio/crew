package port_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestWorkspaceBaseNamesTheIssueAndTheRuleSafelyForAFileName(t *testing.T) {
	tests := []struct {
		key  string
		rule crew.RuleName
		want crew.WorkspaceName
	}{
		{"42", "implement", "issue-42-implement"},
		{"ABC-7", "Code Review", "issue-abc-7-code-review"},
		{"9", "lfg_2/../x", "issue-9-lfg-2----x"},
	}
	for _, tt := range tests {
		if got := port.WorkspaceBase(crew.IssueID{Repository: "repo", Key: tt.key}, tt.rule); got != tt.want {
			t.Errorf("WorkspaceBase(%q, %q) = %q, want %q", tt.key, tt.rule, got, tt.want)
		}
	}
}
