package crew

import "time"

// RunEvent is one change of a rule run, which Decide returns and Apply
// applies. Each carries its run's id, its time, the issue and the rule
// (Head), so it stands alone in a journal; the start and end of an action
// run also carry its workspace and log. An event's fields are plain values.
//
//sumtype:decl
type RunEvent interface {
	// Head returns what every event of the run carries.
	Head() EventHead
	// Time returns when the event happened.
	Time() time.Time
	// apply returns the run with the event applied, as far as its own
	// fields allow.
	apply(r RuleRun) RuleRun
}

// EventHead is what every run event carries: the run's id, when the event
// happened, the issue and the rule.
type EventHead struct {
	Run      RuleRunID
	At       time.Time
	IssueID  IssueID
	IssueRef string
	Rule     RuleName
}

// Head implements RunEvent for every event that embeds h.
func (h EventHead) Head() EventHead { return h }

// Time implements RunEvent for every event that embeds h: it returns At.
func (h EventHead) Time() time.Time { return h.At }

// RunTaken is a rule taking an issue: a new run, whose take move from From
// to To is delivered.
type RunTaken struct {
	EventHead

	// Issue is the issue as the rule took it.
	Issue IssueData
	// Continues is the id of the last run of the same issue and rule, when
	// there was one.
	Continues Optional[RuleRunID]
	From, To  State
	// Actions are the rule's actions, in its action order, each with the
	// resume point it inherited.
	Actions []ActionTaken
}

// ActionTaken is one action of a RunTaken.
type ActionTaken struct {
	Name ActionName
	// Resume is where the action resumes a failed run's work: its workspace
	// is reopened when the take lands.
	Resume Optional[ResumePoint]
}

// TakeMoved is the run's take move that landed: the issue moved from From
// to To.
type TakeMoved struct {
	EventHead

	From, To State
}

// RunStopped is a stop that reached the run while it was taking or running
// its actions.
type RunStopped struct {
	EventHead
}

// ActionWorkspaceAsked is an action's workspace asked for: a new one, or
// the reopened workspace Reopen.
type ActionWorkspaceAsked struct {
	EventHead

	Action ActionName
	Reopen Optional[Workspace]
}

// WorkspaceMissing is the workspace an action asked to reopen, which no
// longer exists.
type WorkspaceMissing struct {
	EventHead

	Action    ActionName
	Workspace Workspace
}

// ActionOpened is an action's workspace that is ready: the action run's
// start, recorded before its session starts.
type ActionOpened struct {
	EventHead

	Action    ActionName
	Workspace Workspace
	// Log is the repository-relative path of the session's log file; empty
	// when a stop reached the run first, as no session will write it.
	Log string
	// Resumed says whether the workspace is the reopened workspace of the
	// failed run the action resumes.
	Resumed bool
}

// ActionSessionAsked is an action's session asked to start.
type ActionSessionAsked struct {
	EventHead

	Action ActionName
}

// ActionSessionStarted is an action's session that started, in its
// workspace.
type ActionSessionStarted struct {
	EventHead

	Action    ActionName
	Workspace Workspace
	Log       string
	Resumed   bool
}

// ActionSessionStopAsked is an action's running session asked to stop.
type ActionSessionStopAsked struct {
	EventHead

	Action ActionName
}

// ActionSessionEnded is an action's session that ended, with how its
// harness says it ended and what it used.
type ActionSessionEnded struct {
	EventHead

	Action  ActionName
	Outcome Outcome
	Usage   Usage
}

// ActionLookupAsked is the lookup of the pull request an action opened,
// asked for.
type ActionLookupAsked struct {
	EventHead

	Action ActionName
}

// ActionCheckAsked is an action's next check asked to run.
type ActionCheckAsked struct {
	EventHead

	Action ActionName
	Check  CheckName
}

// ActionCheckStopAsked is an action's running check asked to stop.
type ActionCheckStopAsked struct {
	EventHead

	Action ActionName
}

// ActionCheckEnded is an action's check that ended.
type ActionCheckEnded struct {
	EventHead

	Action ActionName
	Result CheckResult
}

// ActionLookupDone is the lookup of an action's pull request that ended,
// with what it found.
type ActionLookupDone struct {
	EventHead

	Action      ActionName
	PullRequest PullRequest
}

// ActionFinishing is an action whose outcome is known while the lookup of
// its pull request is pending: it ends once the lookup does.
type ActionFinishing struct {
	EventHead

	Action ActionName
	End    ActionEnd
}

// ActionEnded is an action run that ended: the action run's end, with all
// it recorded.
type ActionEnded struct {
	EventHead

	Action ActionName
	End    ActionEnd
	// Workspace is the workspace its session worked in; none when it never
	// had one ready.
	Workspace Optional[OpenedWorkspace]
	// SessionStarted is when its session started, when one did.
	SessionStarted Optional[time.Time]
	// Usage is what its session reported it used, once the session ended.
	Usage Usage
	// PullRequest is what the lookup of its pull request found; nil when it
	// was not looked up.
	PullRequest PullRequest
}

// RunEnded is a run whose every action ended, and how it ended.
type RunEnded struct {
	EventHead

	Ending RunEnding
}

// EndingMoved is the run's ending move that landed: the issue moved from
// From to To.
type EndingMoved struct {
	EventHead

	From, To State
}

// EndingDropped is the run's ending move to To, which crew gave up.
type EndingDropped struct {
	EventHead

	To     State
	Reason string
}

// FailureReported is the run's failure report that landed.
type FailureReported struct {
	EventHead
}

// FailureReportDropped is the run's failure report, which crew gave up.
type FailureReportDropped struct {
	EventHead
}

// RunReleased is a run crew let go: its ending settled, or its take was
// given up.
type RunReleased struct {
	EventHead
}
