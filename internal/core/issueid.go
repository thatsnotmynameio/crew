package core

import (
	"maps"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// sortedIssueIDs returns m's keys in crew.IssueID.Compare order.
func sortedIssueIDs[V any](m map[crew.IssueID]V) []crew.IssueID {
	return slices.SortedFunc(maps.Keys(m), crew.IssueID.Compare)
}
