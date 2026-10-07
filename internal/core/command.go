package core

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Command is a side effect the core asks the engine to run through a port.
// The engine runs each command and feeds its result back as an Input. The
// set of commands is closed: only this package's types implement Command,
// each either a TrackerCommand or a RunCommand.
//
//sumtype:decl
type Command interface {
	command()
}

// TrackerCommand is a command to the tracker that no one rule run's
// session or scripts own: a listing, a board read, or a write the outbox
// delivers (KTD8). Its result finds what asked by its CallID or its issue.
//
//sumtype:decl
type TrackerCommand interface {
	Command
	trackerCommand()
}

// RunCommand is a command about one rule run: its workspace, its actions'
// sessions and scripts, the read of the answers its sessions start with,
// its route's shell steps and the lookup of its pull requests, which carry
// the run's id so their results reach that run only (KTD7), and the record
// of its runs in the journal.
//
//sumtype:decl
type RunCommand interface {
	Command
	runCommand()
}

// CallID identifies one tracker call, a Move, a Close, a Comment or a
// ReportFailure, so its CallResult finds it. A retried call keeps its ID; the core never has two
// attempts of one call in flight.
type CallID uint64

// ListIssues asks the tracker for the open issues in any of States. Its
// result is IssuesListed or ListFailed. The core keeps at most one listing
// outstanding. It asks at a tick with a free slot, and at once when an issue
// it releases frees a slot after a tick skipped its listing.
type ListIssues struct {
	// States are the rules' ready and running states and, for a board
	// filled from the listings, the states their waiting routes move to,
	// rule by rule in config order, each once (KTD10, KTD17).
	States []crew.State
}

// ListBoard asks the tracker for the open issues that carry any of Labels,
// never a pull request. Its result is BoardListed or BoardListFailed. The
// core keeps at most one board read outstanding, and asks at each tick, busy
// or not, until a stop starts (KTD4). It asks only when it reads a board
// (ListingBoard).
type ListBoard struct {
	// Labels are the board's labels, in board order.
	Labels []crew.State
}

// Move asks the tracker to move an issue from one state to another. Its
// result is a CallResult carrying ID.
type Move struct {
	ID      CallID
	IssueID crew.IssueID
	From    crew.State
	To      crew.State
}

// ReportFailure asks the tracker to post Report on its issue (R7). Its
// result is a CallResult carrying ID.
type ReportFailure struct {
	ID     CallID
	Report crew.FailureReport
}

// Comment asks the tracker to post Body, a route's comment rendered for
// its run, on the issue. Its result is a CallResult carrying ID.
type Comment struct {
	ID      CallID
	IssueID crew.IssueID
	Body    string
}

// Close asks the tracker to close the issue, which must be in From, and to
// take crew's states off it and its pull requests. Its result is a
// CallResult carrying ID.
type Close struct {
	ID      CallID
	IssueID crew.IssueID
	From    crew.State
}

// CreateWorkspace asks for the one new workspace of the rule run Run of
// Rule on Issue, which its actions share. Its result is WorkspaceReady or
// WorkspaceFailed, carrying Issue.ID() and Run.
type CreateWorkspace struct {
	Issue crew.Issue
	Run   crew.RuleRunID
	Rule  crew.RuleName
}

// ReopenWorkspace asks to reopen, for the rule run Run, the workspace the
// run it continues left, named Workspace, on Branch, as recorded. Its
// result, carrying Run, is WorkspaceReady with Resumed set, WorkspaceGone
// when the workspace no longer exists, or WorkspaceFailed. The core asks
// only when the workspace can reopen (Reopening).
type ReopenWorkspace struct {
	IssueID   crew.IssueID
	Run       crew.RuleRunID
	Workspace crew.WorkspaceName
	Branch    string
}

// Record asks the engine to append Event, a run event, to the run journal
// (KTD12). It comes before the commands Event calls for, so an action's
// start is in the journal before its session or script starts. The engine
// appends events in the order the core asks for them; an append that fails
// comes back as RecordFailed. The core asks only when it journals
// (Journaling).
type Record struct {
	Event crew.RunEvent
}

// StartSession asks the harness to start the session of Action of the rule
// run Run in Dir with Prompt, its output going to the log file at Log
// (repository-relative, as received in WorkspaceReady). Its result is
// SessionStarted or SessionFailedToStart, then SessionEnded once a started
// session ends, each carrying Run and Action. Resumed is set when the
// session continues the work of the run this one resumes, in its reopened
// workspace, so the engine marks in the log where the new session starts.
// Agent is the action's agent, whose harness runs the session. Bot is the
// session's bot, whom it acts as on the tracker; empty means you.
type StartSession struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
	Dir     string
	Prompt  string
	Log     string
	Resumed bool
	Agent   crew.AgentName
	Bot     crew.BotName
}

// StopSession asks the engine to stop the running session of Action of the
// rule run Run on the issue. The session's end still arrives as
// SessionEnded.
type StopSession struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
}

// Script is the shell script of a RunShell or a RunStepShell: Command, the
// script of the config's shell action called Name, to run in Dir, its
// output going to the log at Log after what the run wrote there. Dir and
// Log are empty for a run without a workspace, which the engine runs in a
// temporary directory and logs under the name its workspace would have, of
// the issue and Rule. The issue's ref, key and URL, the run's Branch and
// Session, the name of the run's latest session, reach the command as
// environment variables, never as part of it; Session is empty before any
// session. Bot is the run's bot, the latest session's, whom the script acts
// as on the tracker; empty means you.
type Script struct {
	Dir      string
	Name     crew.ActionName
	Command  string
	Log      string
	Rule     crew.RuleName
	IssueRef string
	IssueURL string
	Branch   string
	Session  crew.ActionName
	Bot      crew.BotName
}

// RunShell asks the engine to run the script of Action, the shell action
// at the cursor of the rule run Run. Its result is ShellEnded, carrying
// Run and Action.
type RunShell struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
	Script  Script
}

// StopShell asks the engine to stop the running script of Action of the
// rule run Run. The script's end still arrives as ShellEnded.
type StopShell struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
}

// RunStepShell asks the engine to run the script of the shell step at
// index Step of the route of the rule run Run. Its result is
// StepShellEnded, carrying Run and Step.
type RunStepShell struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Step    int
	Script  Script
}

// StopStepShell asks the engine to stop the running script of the shell
// step at index Step of the route of the rule run Run. The script's end
// still arrives as StepShellEnded.
type StopStepShell struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Step    int
}

// FindPullRequest asks the tracker for the pull request opened from Branch,
// the branch of the rule run Run, once the run chose its route: an open
// one, or a closed or merged one created at or after Since, which is zero
// for a resumed workspace (KTD6). Its result is PullRequestFound, carrying
// Run. The core asks only when the tracker can find pull requests
// (FindingPullRequests).
type FindPullRequest struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Branch  string
	Since   time.Time
}

// ReadAnswers asks the tracker for every comment on the issue of the rule
// run Run before the session of its action Action starts, when the run has
// open questions at Action (KTD-W6). Its result is AnswersRead, carrying
// Run and Action.
type ReadAnswers struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
}

// ReportStatus asks the tracker to show Status on its issue's status
// comment (KTD3). Its result is a StatusResult carrying Status.IssueID. The
// core never has two status writes of one issue in flight.
type ReportStatus struct {
	Status crew.Status
}

// ReportPullRequests asks the tracker to show Report on the open pull
// requests that close its issue (KTD1). Its result is a PullRequestsResult
// carrying Report.IssueID. The core never has two reports of one issue in
// flight, and a retried report keeps its ID.
type ReportPullRequests struct {
	Report crew.PullRequestReport
}

func (ListIssues) command()         {}
func (ListBoard) command()          {}
func (Move) command()               {}
func (Comment) command()            {}
func (Close) command()              {}
func (ReportFailure) command()      {}
func (ReportStatus) command()       {}
func (ReportPullRequests) command() {}
func (CreateWorkspace) command()    {}
func (ReopenWorkspace) command()    {}
func (Record) command()             {}
func (StartSession) command()       {}
func (StopSession) command()        {}
func (RunShell) command()           {}
func (StopShell) command()          {}
func (RunStepShell) command()       {}
func (StopStepShell) command()      {}
func (FindPullRequest) command()    {}
func (ReadAnswers) command()        {}

func (ListIssues) trackerCommand()         {}
func (ListBoard) trackerCommand()          {}
func (Move) trackerCommand()               {}
func (Comment) trackerCommand()            {}
func (Close) trackerCommand()              {}
func (ReportFailure) trackerCommand()      {}
func (ReportStatus) trackerCommand()       {}
func (ReportPullRequests) trackerCommand() {}

func (CreateWorkspace) runCommand() {}
func (ReopenWorkspace) runCommand() {}
func (Record) runCommand()          {}
func (StartSession) runCommand()    {}
func (StopSession) runCommand()     {}
func (RunShell) runCommand()        {}
func (StopShell) runCommand()       {}
func (RunStepShell) runCommand()    {}
func (StopStepShell) runCommand()   {}
func (FindPullRequest) runCommand() {}
func (ReadAnswers) runCommand()     {}
