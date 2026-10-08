package crew

import "time"

// RunEvent is one change of a rule run, which Decide returns and Apply
// applies. Each carries its run's id, its time, the issue and the rule
// (Head), so it stands alone in a journal. An event's fields are plain
// values.
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
	// Actions are the rule's actions, in its action order.
	Actions []ActionName
	// Start is how the run starts, decided from the run it continues: at
	// the first action, at a restart point in that run's reopened
	// worktree, or with PassedRoute alone. Nil counts as StartFresh.
	Start Start
	// Questions are the open questions the run inherits from the run it
	// continues (History.Questions), oldest first.
	Questions []Question
}

// TakeMoved is the run's take move that landed: the issue moved from From
// to To.
type TakeMoved struct {
	EventHead

	From, To State
}

// RunStopped is a stop that reached the run before it was released.
type RunStopped struct {
	EventHead
}

// RunOutOfTime is crew's run time that was up while the run was taking or
// running its actions: the action that runs finishes, and none starts
// after it.
type RunOutOfTime struct {
	EventHead
}

// WorkspaceAsked is the run's one workspace asked for, before its first
// action: a new one, or the reopened workspace Reopen.
type WorkspaceAsked struct {
	EventHead

	Reopen Optional[Workspace]
}

// WorkspaceMissing is the workspace the run asked to reopen, which no
// longer exists. A run that resumed at an action starts fresh at its first
// action, in a new worktree; the passed route alone runs without one.
type WorkspaceMissing struct {
	EventHead

	Workspace Workspace
}

// WorkspaceOpened is the run's workspace that is ready, recorded before
// its first action starts.
type WorkspaceOpened struct {
	EventHead

	Workspace Workspace
	// Log is the repository-relative path of the run's log file; empty
	// when a stop reached the run first, as no action will write it.
	Log string
	// Resumed says whether the workspace is the reopened workspace of the
	// run this one resumes.
	Resumed bool
}

// ActionSessionAsked is a session action's start: its session asked to
// start, at the run's cursor.
type ActionSessionAsked struct {
	EventHead

	Action ActionName
}

// ActionSessionStarted is an action's session that started, acting as Bot.
type ActionSessionStarted struct {
	EventHead

	Action ActionName
	// Bot is the bot the session acts as; the run's actions that are not
	// sessions act as it from then on.
	Bot Bot
	// Login is the login the session acts as; empty when unknown.
	Login string
	// Asks says whether the session may ask a question on the issue: its
	// action's on: has a Waiting entry. Applied, it adds the session to
	// the run's open questions (KTD-W7).
	Asks bool
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

// ActionShellAsked is a shell action's start: its script asked to run, at
// the run's cursor, acting as Bot.
type ActionShellAsked struct {
	EventHead

	Action ActionName
	// Bot is the bot the script acts as: the run's latest session's, or
	// the zero Bot, the tracker's identity, before any session started.
	Bot Bot
}

// ActionShellStopAsked is an action's running script asked to stop.
type ActionShellStopAsked struct {
	EventHead

	Action ActionName
}

// ActionShellEnded is an action's script that ended.
type ActionShellEnded struct {
	EventHead

	Action  ActionName
	Outcome ShellOutcome
}

// ActionFunctionAsked is a function action's start: its function asked to
// run, at the run's cursor, acting as Bot.
type ActionFunctionAsked struct {
	EventHead

	Action ActionName
	// Bot is the bot the function acts as: the run's latest session's, or
	// the zero Bot, the tracker's identity, before any session started.
	Bot Bot
}

// ActionFunctionStopAsked is an action's running function asked to stop.
type ActionFunctionStopAsked struct {
	EventHead

	Action ActionName
}

// ActionFunctionEnded is an action's function that ended.
type ActionFunctionEnded struct {
	EventHead

	Action  ActionName
	Outcome FunctionOutcome
}

// ActionReturnAsked is the answered rule's action's start: the read of the
// item's comments asked, at the run's cursor, to find where the item
// returns (KTD2).
type ActionReturnAsked struct {
	EventHead

	Action ActionName
}

// ActionEnded is an action run that ended: the action run's end, with its
// verdict, where the verdict leads, and all it recorded.
type ActionEnded struct {
	EventHead

	Action ActionName
	End    ActionEnd
	// Verdict is the action's verdict, and Target where it leads: the next
	// action, or the route the run ends through.
	Verdict Verdict
	Target  Target
	// SessionStarted is when its session started, when one did.
	SessionStarted Optional[time.Time]
	// Usage is what its session reported it used, once the session ended.
	Usage Usage
}

// RouteChosen is the run's sequence that is over: the run ends through
// Route.
type RouteChosen struct {
	EventHead

	Route RouteName
	// Action is the action at the run's cursor, whose verdict led to the
	// route: the action that did not go on to the next, or the last
	// action; empty for a rule without actions.
	Action ActionName
	// Steps are the route's steps, in the order they run.
	Steps []StepPlan
}

// RunLookupAsked is the lookup of the pull requests of the run's branch,
// asked for once the run chose its route.
type RunLookupAsked struct {
	EventHead
}

// RunLookupDone is the lookup of the run's pull requests that ended, with
// what it found.
type RunLookupDone struct {
	EventHead

	PullRequest PullRequest
}

// StepAsked is the route's step at index Step asked: a tracker call to
// deliver, a shell action's script to run or a function to call, acting as
// the run's bot.
type StepAsked struct {
	EventHead

	Step int
}

// StepShellStopAsked is the route's shell step at index Step, whose script
// runs, asked to stop.
type StepShellStopAsked struct {
	EventHead

	Step int
}

// StepFunctionStopAsked is the route's function step at index Step, whose
// function runs, asked to stop.
type StepFunctionStopAsked struct {
	EventHead

	Step int
}

// StepEnded is the route's step at index Step that settled, and how.
type StepEnded struct {
	EventHead

	Step    int
	Outcome StepOutcome
}

// RunReleased is a run crew let go: the final step of its route settled,
// or its take was given up.
type RunReleased struct {
	EventHead
}
