package core

import (
	"cmp"
	"maps"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// compareIssueIDs orders issue ids by repository, then by key as text.
func compareIssueIDs(a, b crew.IssueID) int {
	return cmp.Or(cmp.Compare(a.Repository, b.Repository), cmp.Compare(a.Key, b.Key))
}

// sortedIssueIDs returns m's keys in compareIssueIDs order.
func sortedIssueIDs[V any](m map[crew.IssueID]V) []crew.IssueID {
	return slices.SortedFunc(maps.Keys(m), compareIssueIDs)
}
