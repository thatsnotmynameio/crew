package crew

import (
	"slices"
	"time"
)

// Status is where an issue crew took stands, for the tracker to show you
// in one place it edits in place. The tracker adapter formats it in its own
// markup and computes elapsed times from Updated.
//
// A Status cannot be changed once built: NewStatus copies what it is given,
// and its accessors return copies.
type Status struct {
	data StatusData
}

// StatusData is a status's fields as plain data, to build a Status from
// (NewStatus) or read one back whole (Status.Data).
type StatusData struct {
	// IssueID and IssueRef identify the issue, as ID and Ref in Issue.
	IssueID  IssueID
	IssueRef string
	// Rule is the rule that runs or ran on the issue.
	Rule RuleName
	// Progress says whether the rule runs or ended.
	Progress StatusProgress
	// Actions are the rule's actions, in its action order.
	Actions []ActionStatus
	// Steps are the steps of the route the run ends through, in the order
	// they run, each with how it settled; nil while the run takes the
	// issue or runs its actions.
	Steps []StepStatus
	// Updated is when crew computed this status.
	Updated time.Time
	// Run identifies the status comment's entry this status belongs to: the
	// id of the rule run that opened it, so it is global. It stays the same
	// from the issue's first running status for a rule until the status
	// after that rule ended. A tracker that keeps a history of rule runs
	// edits the entry, or starts a new one, and compares it only as text.
	Run RuleRunID
}

// NewStatus returns the status d describes. The status keeps its own copy
// of d's Actions and Steps.
func NewStatus(d StatusData) Status {
	d.Actions, d.Steps = slices.Clone(d.Actions), slices.Clone(d.Steps)
	return Status{data: d}
}

// Data returns the status's fields as plain data, with its own copy of the
// Actions and Steps.
func (s Status) Data() StatusData {
	d := s.data
	d.Actions, d.Steps = slices.Clone(d.Actions), slices.Clone(d.Steps)
	return d
}

// IssueID returns the identity of the issue.
func (s Status) IssueID() IssueID { return s.data.IssueID }

// IssueRef returns how humans write the issue, such as "#42".
func (s Status) IssueRef() string { return s.data.IssueRef }

// Rule returns the rule that runs or ran on the issue.
func (s Status) Rule() RuleName { return s.data.Rule }

// Progress returns whether the rule runs or ended.
func (s Status) Progress() StatusProgress { return s.data.Progress }

// Actions returns a copy of the rule's actions, in its action order.
func (s Status) Actions() []ActionStatus { return slices.Clone(s.data.Actions) }

// Steps returns a copy of the steps of the route the run ends through, in
// the order they run, or nil while it runs its actions.
func (s Status) Steps() []StepStatus { return slices.Clone(s.data.Steps) }

// Updated returns when crew computed the status.
func (s Status) Updated() time.Time { return s.data.Updated }

// Run returns the id of the status comment's entry the status belongs to.
func (s Status) Run() RuleRunID { return s.data.Run }

// StatusProgress is whether the rule of a Status runs or ended:
// StatusRunning or StatusEnded.
//
//sumtype:decl
type StatusProgress interface {
	statusProgress()
}

// StatusRunning is the progress of a rule that took the issue and whose
// actions run.
type StatusRunning struct{}

// StatusEnded is the progress of a rule whose run chose its route, Route,
// whose final step moves the issue to To, or closes it.
type StatusEnded struct {
	// Route is the route the run ends through.
	Route RouteName
	// To is the state the issue moves to; empty when the route closes it.
	To State
	// Move says whether the route's final move or close is under way,
	// landed or was given up.
	Move MoveProgress
}

func (StatusRunning) statusProgress() {}
func (StatusEnded) statusProgress()   {}

// StepStatus is one step of the route a Status's run ends through.
type StepStatus struct {
	// Step is what the step does.
	Step StepPlan
	// Outcome is how the step settled; nil while it has not.
	Outcome StepOutcome
}

// ActionStatus is one action in a Status.
type ActionStatus struct {
	// Name is the action's name.
	Name ActionName
	// State is how the action stands.
	State ActionState
	// Shell is how a shell action's script ended: crew's one line, followed
	// by the last line the script printed, without control characters;
	// empty for a session, or before the script ended. Only a script's
	// line goes in a status: a session's or a tool's own words never do,
	// since a tracker may show it in public, and those words can hold
	// commands, output and secrets.
	Shell ShellReason
	// Workspace is the workspace the action resumed in; empty when it did
	// not resume.
	Workspace WorkspaceName
}

// ActionState is how an action in a Status stands: ActionPending,
// ActionAwaitingTurn, ActionRunning, ActionSucceeded, ActionFailed,
// ActionNotRun or ActionDoneInEarlierRun.
//
//sumtype:decl
type ActionState interface {
	actionState()
}

// ActionPending is the action the run is about to start, with no session
// or script to time yet: the run's take or workspace is under way, or its
// session starts.
type ActionPending struct{}

// ActionAwaitingTurn is an action after the one the run is about to start
// or runs: it starts once the actions before it went on to the next.
type ActionAwaitingTurn struct{}

// ActionNotRun is an action the run never reached: an action before it
// ended the sequence through a route.
type ActionNotRun struct{}

// ActionDoneInEarlierRun is an action before the one the run resumed at:
// it went on to the next action in the run this one continues.
type ActionDoneInEarlierRun struct{}

// ActionRunning is an action whose session or shell script runs.
type ActionRunning struct {
	// Started is when its session started, or crew asked for its script.
	Started time.Time
	// Said is the last thing its running session said, on one line with
	// local paths shortened and without control characters; empty when it
	// said nothing yet, said only control characters, its harness cannot
	// tell, or it is a shell action.
	Said Said
}

// ActionSucceeded is an action that ended with a verdict other than
// Failed: Passed, or one its on: names.
type ActionSucceeded struct {
	// Verdict is the action's verdict; empty counts as Passed.
	Verdict Verdict
	// Usage is what its session spent and the pull request it opened, when
	// its session started and crew is set to show them.
	Usage Optional[ShownUsage]
}

// ActionFailed is an action that ended in a failure.
type ActionFailed struct {
	// Cause says what made it fail.
	Cause FailureCause
	// Log is the repository-relative path of its log; empty when it failed
	// before it had one.
	Log string
	// Usage is what its session spent and the pull request it opened, when
	// its session started and crew is set to show them.
	Usage Optional[ShownUsage]
}

func (ActionPending) actionState()          {}
func (ActionAwaitingTurn) actionState()     {}
func (ActionRunning) actionState()          {}
func (ActionSucceeded) actionState()        {}
func (ActionFailed) actionState()           {}
func (ActionNotRun) actionState()           {}
func (ActionDoneInEarlierRun) actionState() {}

// ShownUsage is what an ended action's session spent and the pull request
// it opened, as a status shows them.
type ShownUsage struct {
	Spend       Spend
	PullRequest PullRequest
}

// FailureCause is what made an action fail, for a tracker to word itself.
type FailureCause int

// The causes of a failed action.
const (
	// CauseSession: its session ended in a failure.
	CauseSession FailureCause = iota
	// CauseStopped: crew stopped before the action could end on its own.
	CauseStopped
	// CauseWorkspace: its workspace could not be created.
	CauseWorkspace
	// CauseStart: its session could not start.
	CauseStart
	// CausePrompt: its prompt did not render.
	CausePrompt
	// CauseShell: its shell action's script exited with a status that gives
	// Failed, ran out of time or could not start.
	CauseShell
	// CauseVerdict: it reported, or its script's exit status gave, a
	// verdict its on: does not name, or its session reported text with no
	// verdict name.
	CauseVerdict
	// CauseStoppedBeforeStart: crew stopped before the action started, so
	// it never ran.
	CauseStoppedBeforeStart
	// CauseTimeUp: crew's run time was up before the action started, so it
	// never ran.
	CauseTimeUp
)

// MoveProgress is how the move or close that ends a rule stands.
type MoveProgress int

// The progress of the move or close that ends a rule.
const (
	// MovePending: the move is in flight or waits for a retry, or a step
	// before it runs.
	MovePending MoveProgress = iota
	// MoveDone: the issue is in To, or closed.
	MoveDone
	// MoveDropped: crew gave the move up, as the issue was closed or moved
	// meanwhile, the tracker refused it, or its last try after a stop failed.
	MoveDropped
)
