package crew

import (
	"slices"
	"testing"
)

func TestBoardLabels(t *testing.T) {
	tests := []struct {
		name  string
		board []BoardColumn
		want  []string
	}{
		{
			name: "every column's labels, in board order, each once",
			board: []BoardColumn{
				{Name: "ideas", Labels: []string{"crew:brainstorm:ready"}},
				{Name: "bugs", Labels: []string{"bug", "crew:brainstorm:ready"}},
				{Name: "done", Labels: []string{"crew:brainstorm:done", "crew:triage:done"}},
			},
			want: []string{"crew:brainstorm:ready", "bug", "crew:brainstorm:done", "crew:triage:done"},
		},
		{
			name: "no board",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BoardLabels(tt.board); !slices.Equal(got, tt.want) {
				t.Errorf("BoardLabels() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBoardIssueClone(t *testing.T) {
	b := BoardIssue{Issue: Issue{Key: "20", States: []State{"crew:fix:in progress"}}, Labels: []string{"bug"}}
	c := b.Clone()
	c.Labels[0] = "changed"
	c.Issue.States[0] = "changed"
	if b.Labels[0] != "bug" || b.Issue.States[0] != "crew:fix:in progress" {
		t.Errorf("Clone shares memory: %+v", b)
	}
}
