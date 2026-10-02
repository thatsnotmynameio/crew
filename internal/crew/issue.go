package crew

import (
	"slices"
	"time"
)

// Issue is an issue as a tracker reports it. Its identity is the opaque Key
// plus the display Ref, both set by the tracker adapter: Key "42" and Ref "#42"
// on GitHub, Key and Ref "PROJ-123" on Jira. Nothing outside the adapter
// assumes an integer issue number.
type Issue struct {
	// Key identifies the issue to its tracker. It is opaque to the engine.
	Key string
	// Ref is how humans write the issue, such as "#42" or "PROJ-123".
	Ref string
	// Title is the issue's title.
	Title string
	// URL is the issue's web address.
	URL string
	// Created is when the issue was opened; the oldest issue is taken first.
	Created time.Time
	// States are the crew states the issue is in. A healthy issue is in
	// exactly one; an issue in two or more is skipped and reported.
	States []State
	// Blocked is set when an open issue blocks this one. A blocked issue is
	// not taken until every issue blocking it is closed. A tracker that
	// knows no dependencies leaves it false.
	Blocked bool
}

// Clone returns a copy of i with its own States, so the copy shares no slice
// with i.
func (i Issue) Clone() Issue {
	i.States = slices.Clone(i.States)
	return i
}
