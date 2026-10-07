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
// session or checks own: a listing, a board read, or a write the outbox
// delivers (KTD8). Its result finds what asked by its CallID or its issue.
//
//sumtype:decl
type TrackerCommand interface {
	Command
	trackerCommand()
}

// RunCommand is a command about one rule run: its actions' workspaces,
// sessions, checks and pull request lookups, which carry the run's id so
// their results reach that run only (KTD7), and the record of its runs in
// the journal.
//
//sumtype:decl
type RunCommand interface {
	Command
	runCommand()
}

// CallID identifies one tracker call, a Move or a ReportFailure, so its
// CallResult finds it. A retried call keeps its ID; the core never has two
// attempts of one call in flight.
type CallID uint64

// ListIssues asks the tracker for the open issues in any of States. Its
// result is IssuesListed or ListFailed. The core keeps at most one listing
// outstanding. It asks at a tick with a free slot, and at once when an issue
// it releases frees a slot after a tick skipped its listing.
type ListIssues struct {
	// States are the rules' ready and running states, rule by rule in
	// config order, each once (KTD10).
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

// CreateWorkspace asks for a new workspace for Action of the rule run Run
// on Issue. Its result is WorkspaceReady or WorkspaceFailed, carrying
// Issue.ID(), Run and Action.
type CreateWorkspace struct {
	Issue  crew.Issue
	Run    crew.RuleRunID
	Action crew.ActionName
}

// ReopenWorkspace asks to reopen, for Action of the rule run Run, the
// workspace a failed run of Action on the issue left, named Workspace, on
// Branch, as recorded. Its result, carrying Run and Action, is
// WorkspaceReady with Resumed set, WorkspaceGone when the workspace no
// longer exists, or WorkspaceFailed. The core asks only when the workspace
// can reopen (Reopening).
type ReopenWorkspace struct {
	IssueID   crew.IssueID
	Run       crew.RuleRunID
	Action    crew.ActionName
	Workspace crew.WorkspaceName
	Branch    string
}

// Record asks the engine to append Event, a run event, to the run journal
// (KTD12). It comes before the commands Event calls for, so an action's
// start is in the journal before its session starts. The engine appends
// events in the order the core asks for them; an append that fails comes
// back as RecordFailed. The core asks only when it journals (Journaling).
type Record struct {
	Event crew.RunEvent
}

// StartSession asks the harness to start the session of Action of the rule
// run Run in Dir with Prompt, its output going to the log file at Log
// (repository-relative, as received in WorkspaceReady). Its result is
// SessionStarted or SessionFailedToStart, then SessionEnded once a started
// session ends, each carrying Run and Action. Resumed is set when the
// session continues a failed run in its reopened workspace, so the engine
// marks in the log where the new session starts. Agent is the action's
// agent, whose harness runs the session. Bot is the action's bot, whom the
// session acts as on the tracker; empty means you.
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

// RunCheck asks the engine to run Command, the script of the action's
// check called Name, in Dir once its session succeeded, its output going to
// the log at Log after the session's. The issue's ref, key and URL, the
// action's Branch, the Prompt its session started with and the session's
// LastMessage reach the command as environment variables and files, never
// as part of it. Bot is the action's bot, whom the check acts as on the
// tracker; empty means you. Its result is CheckEnded, carrying Run, the
// action's rule run, and Action.
type RunCheck struct {
	IssueID     crew.IssueID
	Run         crew.RuleRunID
	Action      crew.ActionName
	Dir         string
	Name        crew.CheckName
	Command     string
	Log         string
	IssueRef    string
	IssueURL    string
	Branch      string
	Bot         crew.BotName
	Prompt      string
	LastMessage string
}

// FindPullRequest asks the tracker for the pull request opened from Branch
// once Action's session on the issue ended: an open one, or a closed or
// merged one created at or after Since, which is zero for a resumed
// workspace (KTD6). Its result is PullRequestFound, carrying Run, the
// action's rule run, and Action. The core asks only when the tracker can
// find pull requests (FindingPullRequests).
type FindPullRequest struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
	Branch  string
	Since   time.Time
}

// StopCheck asks the engine to stop the running check of Action of the
// rule run Run on the issue. The check's end still arrives as CheckEnded.
type StopCheck struct {
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
func (ReportFailure) command()      {}
func (ReportStatus) command()       {}
func (ReportPullRequests) command() {}
func (CreateWorkspace) command()    {}
func (ReopenWorkspace) command()    {}
func (Record) command()             {}
func (StartSession) command()       {}
func (StopSession) command()        {}
func (RunCheck) command()           {}
func (FindPullRequest) command()    {}
func (StopCheck) command()          {}

func (ListIssues) trackerCommand()         {}
func (ListBoard) trackerCommand()          {}
func (Move) trackerCommand()               {}
func (ReportFailure) trackerCommand()      {}
func (ReportStatus) trackerCommand()       {}
func (ReportPullRequests) trackerCommand() {}

func (CreateWorkspace) runCommand() {}
func (ReopenWorkspace) runCommand() {}
func (Record) runCommand()          {}
func (StartSession) runCommand()    {}
func (StopSession) runCommand()     {}
func (RunCheck) runCommand()        {}
func (FindPullRequest) runCommand() {}
func (StopCheck) runCommand()       {}
