package crew

import (
	"slices"
	"time"
)

// Status is where an issue crew queued or took stands, for the tracker to
// show the boss in one place it edits in place. The tracker adapter formats
// it in its own markup and computes elapsed times from Updated.
type Status struct {
	// IssueKey and IssueRef identify the issue, as in Issue.
	IssueKey string
	IssueRef string
	// Stage is the name of the stage the issue is queued for, runs or ran.
	Stage string
	// Kind says which of the fields below apply.
	Kind StatusKind
	// Slots is how many issues crew runs at once; set when Kind is
	// StatusQueued, as the issue waits for one of them to free up.
	Slots int
	// Actions are the stage's actions, in its action order; set when Kind is
	// StatusRunning or StatusEnded.
	Actions []ActionStatus
	// To is the state the issue moves to once the stage ended; set when Kind
	// is StatusEnded.
	To State
	// Move says whether the move to To is under way, landed or was given up;
	// set when Kind is StatusEnded.
	Move MoveProgress
	// Updated is when crew computed this status.
	Updated time.Time
}

// StatusKind is the kind of a Status.
type StatusKind int

// The kinds of status.
const (
	// StatusQueued: the issue is in a stage's label and waits for a free slot.
	StatusQueued StatusKind = iota
	// StatusRunning: the stage took the issue and its actions run.
	StatusRunning
	// StatusEnded: every action of the stage ended.
	StatusEnded
)

// ActionStatus is one action in a Status.
type ActionStatus struct {
	// Name is the action's name.
	Name string
	// State is how the action stands.
	State ActionState
	// Started is when its session started; zero while its workspace is
	// created or its session starts, and once it ended.
	Started time.Time
	// Said is the last thing its running session said, on one line with
	// local paths shortened; empty when it said nothing yet or its harness
	// cannot tell.
	Said string
	// Workspace is the name of the workspace the action resumed in; empty
	// when it did not resume.
	Workspace string
}

// ActionState is how an action in a Status stands.
type ActionState int

// The states of an action in a Status. An action whose session has not
// started yet is running.
const (
	ActionRunning ActionState = iota
	ActionSucceeded
	ActionFailed
)

// MoveProgress is how the move that ends a stage stands.
type MoveProgress int

// The progress of the move that ends a stage.
const (
	// MovePending: the move is in flight or waits for a retry.
	MovePending MoveProgress = iota
	// MoveDone: the issue is in To.
	MoveDone
	// MoveDropped: crew gave the move up, as the issue was closed or moved
	// meanwhile, the tracker refused it, or its last try after a stop failed.
	MoveDropped
)

// Clone returns a copy of s with its own Actions, so the copy shares no
// slice with s.
func (s Status) Clone() Status {
	s.Actions = slices.Clone(s.Actions)
	return s
}
