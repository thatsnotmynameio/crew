package crew

import (
	"slices"
	"testing"
)

func TestBoardLabels(t *testing.T) {
	tests := []struct {
		name  string
		board []BoardColumn
		want  []State
	}{
		{
			name: "every column's labels, in board order, each once",
			board: []BoardColumn{
				{Name: "ideas", Labels: []State{"crew:brainstorm:ready"}},
				{Name: "bugs", Labels: []State{"bug", "crew:brainstorm:ready"}},
				{Name: "done", Labels: []State{"crew:brainstorm:done", "crew:triage:done"}},
			},
			want: []State{"crew:brainstorm:ready", "bug", "crew:brainstorm:done", "crew:triage:done"},
		},
		{
			name: "no board",
			want: []State{},
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

func TestABoardIssueKeepsItsLabels(t *testing.T) {
	labels := []State{"bug"}
	b := NewBoardIssue(NewIssue(IssueData{ID: IssueID{Key: "20"}}), labels)
	labels[0] = "changed"
	b.Labels()[0] = "changed"
	if got := b.Labels(); !slices.Equal(got, []State{"bug"}) {
		t.Errorf("Labels() = %q after changing the given and returned labels, want [bug]", got)
	}
	if got := b.Issue().ID(); got != (IssueID{Key: "20"}) {
		t.Errorf("Issue().ID() = %v, want 20", got)
	}
}
