package crew

import "slices"

// BoardColumn is a column of the live view's board as the config draws it:
// a name, the labels whose open items it shows, and the kind of those items.
// Every label is non-empty and written once, in one spelling across the
// board.
type BoardColumn struct {
	Name   string
	Labels []string
	// Takes is the kind of item the column shows. The zero Kind shows
	// issues.
	Takes Kind
}

// BoardIssue is an open issue as the board reads it: the issue, and the
// board labels it carries, spelled as the board writes them.
type BoardIssue struct {
	Issue  Issue
	Labels []string
}

// Clone returns a copy of b that shares no memory with it.
func (b BoardIssue) Clone() BoardIssue {
	b.Issue = b.Issue.Clone()
	b.Labels = slices.Clone(b.Labels)
	return b
}

// BoardLabels returns every label the board's columns name, column by
// column in board order, each once. The slice is new on every call.
func BoardLabels(board []BoardColumn) []string {
	labels := []string{}
	for _, c := range board {
		for _, l := range c.Labels {
			if !slices.Contains(labels, l) {
				labels = append(labels, l)
			}
		}
	}
	return labels
}
