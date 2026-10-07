package crew

import (
	"slices"
	"time"
)

// Issue is an issue as a tracker reports it. Its identity is ID, its
// repository plus the opaque key its tracker knows it by; Ref is how it is
// displayed. The tracker adapter sets ID.Key and Ref, such as key "42" and
// Ref "#42" on GitHub, key and Ref "PROJ-123" on Jira, and the engine sets
// ID.Repository. Nothing outside the adapter assumes an integer issue
// number.
type Issue struct {
	// ID identifies the issue everywhere. Its key is opaque to the engine.
	ID IssueID
	// Ref is how humans write the issue, such as "#42" or "PROJ-123".
	Ref string
	// Title is the issue's title.
	Title string
	// URL is the issue's web address.
	URL string
	// Created is when the issue was opened. Among issues of the same
	// priority and rule, the oldest is taken first.
	Created time.Time
	// Priority is the issue's rank as the tracker sets it: 1 is the highest
	// and larger numbers rank lower. 0 means no priority, which ranks after
	// every priority. A tracker that knows no priority leaves it 0.
	Priority int
	// States are the crew states the issue is in. A healthy issue is in
	// exactly one; an issue in two or more is skipped and reported.
	States []State
	// Blocked is set when an open issue blocks this one. A blocked issue is
	// not taken until every issue blocking it is closed. A tracker that
	// knows no dependencies leaves it false.
	Blocked bool
	// Kind is whether the item is an issue or a pull request. Only a rule
	// that takes its kind takes it. A tracker that knows no pull requests
	// leaves it KindIssue.
	Kind Kind
}

// Kind is the kind of item a tracker lists: an issue or a pull request.
type Kind int

// The kinds of item. The zero Kind is an issue.
const (
	// KindIssue is an issue.
	KindIssue Kind = iota
	// KindPullRequest is a pull request.
	KindPullRequest
)

// Clone returns a copy of i with its own States, so the copy shares no slice
// with i.
func (i Issue) Clone() Issue {
	i.States = slices.Clone(i.States)
	return i
}
