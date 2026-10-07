package crew

import (
	"slices"
	"time"
)

// Issue is an issue as a tracker reports it. Its identity is its ID, its
// repository plus the opaque key its tracker knows it by; its Ref is how it
// is displayed. The tracker adapter sets the key and the Ref, such as key
// "42" and Ref "#42" on GitHub, key and Ref "PROJ-123" on Jira, and the
// engine sets the repository (WithRepository). Nothing outside the adapter
// assumes an integer issue number.
//
// An Issue cannot be changed once built: NewIssue copies what it is given,
// and its accessors return copies. The zero Issue is an issue with no
// fields set.
type Issue struct {
	data IssueData
}

// IssueData is an issue's fields as plain data, to build an Issue from
// (NewIssue) or read one back whole (Issue.Data).
type IssueData struct {
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
	// Blocked is whether an open issue blocks this one. A blocked issue is
	// not taken until every issue blocking it is closed. A tracker that
	// knows no dependencies leaves it false.
	Blocked bool
	// Kind is whether the item is an issue or a pull request. Only a rule
	// that takes its kind takes it. A tracker that knows no pull requests
	// leaves it KindIssue.
	Kind Kind
}

// NewIssue returns the issue d describes. The issue keeps its own copy of
// d's States.
func NewIssue(d IssueData) Issue {
	d.States = slices.Clone(d.States)
	return Issue{data: d}
}

// Data returns the issue's fields as plain data, with its own copy of the
// States.
func (i Issue) Data() IssueData {
	d := i.data
	d.States = slices.Clone(d.States)
	return d
}

// WithRepository returns a copy of i in repository r: the same key in
// another repository.
func (i Issue) WithRepository(r RepositoryID) Issue {
	i.data.ID.Repository = r
	return i
}

// ID returns the identity of the issue.
func (i Issue) ID() IssueID { return i.data.ID }

// Ref returns how humans write the issue, such as "#42".
func (i Issue) Ref() string { return i.data.Ref }

// Title returns the issue's title.
func (i Issue) Title() string { return i.data.Title }

// URL returns the issue's web address.
func (i Issue) URL() string { return i.data.URL }

// Created returns when the issue was opened.
func (i Issue) Created() time.Time { return i.data.Created }

// Priority returns the issue's rank, 1 the highest and 0 none.
func (i Issue) Priority() int { return i.data.Priority }

// States returns a copy of the crew states the issue is in.
func (i Issue) States() []State { return slices.Clone(i.data.States) }

// OnlyState returns the one crew state the issue is in, or false when it
// is in none or in more than one.
func (i Issue) OnlyState() (State, bool) {
	if len(i.data.States) != 1 {
		return "", false
	}
	return i.data.States[0], true
}

// Blocked reports whether an open issue blocks this one.
func (i Issue) Blocked() bool { return i.data.Blocked }

// Kind returns whether the item is an issue or a pull request.
func (i Issue) Kind() Kind { return i.data.Kind }

// Kind is the kind of item a tracker lists: an issue or a pull request.
type Kind int

// The kinds of item. The zero Kind is an issue.
const (
	// KindIssue is an issue.
	KindIssue Kind = iota
	// KindPullRequest is a pull request.
	KindPullRequest
)
