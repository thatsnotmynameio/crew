package crew

import (
	"slices"
	"time"
)

// Status is where an issue crew took stands, for the tracker to show you
// in one place it edits in place. The tracker adapter formats it in its own
// markup and computes elapsed times from Updated.
type Status struct {
	// IssueKey and IssueRef identify the issue, as in Issue.
	IssueKey string
	IssueRef string
	// Rule is the rule that runs or ran on the issue.
	Rule RuleName
	// Kind says which of the fields below apply.
	Kind StatusKind
	// Actions are the rule's actions, in its action order.
	Actions []ActionStatus
	// To is the state the issue moves to once the rule ended; set when Kind
	// is StatusEnded.
	To State
	// Move says whether the move to To is under way, landed or was given up;
	// set when Kind is StatusEnded.
	Move MoveProgress
	// Updated is when crew computed this status.
	Updated time.Time
	// Run identifies the rule run this status belongs to: it stays the same
	// from the issue's first running status for a rule until the status
	// after that rule ended, and differs between crew processes. A tracker
	// that keeps a history of rule runs edits the run's entry, or starts a
	// new one.
	Run string
}

// StatusKind is the kind of a Status.
type StatusKind int

// The kinds of status.
const (
	// StatusRunning: the rule took the issue and its actions run.
	StatusRunning StatusKind = iota
	// StatusEnded: every action of the rule ended.
	StatusEnded
)

// ActionStatus is one action in a Status.
type ActionStatus struct {
	// Name is the action's name.
	Name ActionName
	// State is how the action stands.
	State ActionState
	// Started is when its session started; zero while its workspace is
	// created or its session starts, and once it ended.
	Started time.Time
	// Said is the last thing its running session said, on one line with
	// local paths shortened; empty when it said nothing yet or its harness
	// cannot tell.
	Said string
	// Cause says what made a failed action fail; set when State is
	// ActionFailed.
	Cause FailureCause
	// Checks are how its checks that ran so far ended, in the order they
	// ran; when Cause is CauseCheck, the last is the one that did not pass.
	// Only a check's reason goes in a status: a session's or a tool's own
	// words never do, since a tracker may show it in public, and those
	// words can hold commands, output and secrets.
	Checks []CheckResult
	// Log is the repository-relative path of its log, once it has one.
	Log string
	// Workspace is the workspace the action resumed in; empty when it did
	// not resume.
	Workspace WorkspaceName
	// Spend is what its session used, and PullRequest the pull request it
	// opened; set only for an ended action whose session started, when crew
	// is set to show them.
	Spend       Spend
	PullRequest PullRequest
}

// FailureCause is what made an action fail, for a tracker to word itself.
type FailureCause int

// The causes of a failed action. The zero value means the action did not
// fail.
const (
	CauseNone FailureCause = iota
	// CauseSession: its session ended in a failure.
	CauseSession
	// CauseCheck: its check failed, ran out of time or could not start.
	CauseCheck
	// CauseStopped: crew stopped before the action could end on its own.
	CauseStopped
	// CauseWorkspace: its workspace could not be created.
	CauseWorkspace
	// CauseStart: its session could not start.
	CauseStart
	// CausePrompt: its prompt did not render.
	CausePrompt
)

// ActionState is how an action in a Status stands.
type ActionState int

// The states of an action in a Status. An action whose session has not
// started yet is running.
const (
	ActionRunning ActionState = iota
	ActionSucceeded
	ActionFailed
)

// MoveProgress is how the move that ends a rule stands.
type MoveProgress int

// The progress of the move that ends a rule.
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
	s.Actions = cloneActions(s.Actions)
	return s
}

// cloneActions returns a copy of actions, each with its own Checks.
func cloneActions(actions []ActionStatus) []ActionStatus {
	out := slices.Clone(actions)
	for i := range out {
		out[i].Checks = slices.Clone(out[i].Checks)
	}
	return out
}

// FailedCheck returns the reason of the check that failed a, when its Cause
// is CauseCheck, or "".
func (a ActionStatus) FailedCheck() string {
	if a.Cause != CauseCheck || len(a.Checks) == 0 {
		return ""
	}
	return a.Checks[len(a.Checks)-1].Reason
}
