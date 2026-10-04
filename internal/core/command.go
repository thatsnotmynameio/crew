package core

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Command is a side effect the core asks the engine to run through a port.
// The engine runs each command and feeds its result back as an Input. The
// set of commands is closed: only this package's types implement Command.
type Command interface {
	command()
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
	// States are the stages' trigger states, in config order.
	States []crew.State
}

// ListBoard asks the tracker for the open issues that carry any of Labels,
// never a pull request. Its result is BoardListed or BoardListFailed. The
// core keeps at most one board read outstanding, and asks at each tick, busy
// or not, until a stop starts (KTD4). It asks only when it reads a board
// (ListingBoard).
type ListBoard struct {
	// Labels are the board's labels, in board order.
	Labels []string
}

// Move asks the tracker to move an issue from one state to another. Its
// result is a CallResult carrying ID.
type Move struct {
	ID       CallID
	IssueKey string
	From     crew.State
	To       crew.State
}

// ReportFailure asks the tracker to post Report on its issue (R7). Its
// result is a CallResult carrying ID.
type ReportFailure struct {
	ID     CallID
	Report crew.FailureReport
}

// CreateWorkspace asks for a new workspace for Action on Issue. Its result
// is WorkspaceReady or WorkspaceFailed, carrying Issue.Key and Action.
type CreateWorkspace struct {
	Issue  crew.Issue
	Action string
}

// ReopenWorkspace asks to reopen the workspace a failed run of Action on
// the issue left, named Workspace, on Branch, as recorded. Its result is
// WorkspaceReady with Resumed set, WorkspaceGone when the workspace no
// longer exists, or WorkspaceFailed. The core asks only when the workspace
// can reopen (Reopening).
type ReopenWorkspace struct {
	IssueKey  string
	Action    string
	Workspace string
	Branch    string
}

// RecordRun asks the engine to append Record to the run journal (KTD1). The
// engine writes records in the order the core asks for them; a write that
// fails comes back as RecordFailed. The core asks only when it records runs
// (RecordingRuns).
type RecordRun struct {
	Record RunRecord
}

// StartSession asks the harness to start a session in Dir with Prompt, its
// output going to the log file at Log (repository-relative, as received in
// WorkspaceReady). Its result is SessionStarted or SessionFailedToStart,
// then SessionEnded once a started session ends. Resumed is set when the
// session continues a failed run in its reopened workspace, so the engine
// marks in the log where the new session starts. Mate is the action's mate,
// whom the session acts as on the tracker; empty means the boss.
type StartSession struct {
	IssueKey string
	Action   string
	Dir      string
	Prompt   string
	Log      string
	Resumed  bool
	Mate     string
}

// StopSession asks the engine to stop the running session of Action on the
// issue. The session's end still arrives as SessionEnded.
type StopSession struct {
	IssueKey string
	Action   string
}

// RunCheck asks the engine to run Command, the action's check, in Dir once
// its session succeeded, its output going to the log at Log after the
// session's. The issue's ref, key and URL and the action's Branch reach the
// command as environment variables, never as part of it. Mate is the
// action's mate, whom the check acts as on the tracker; empty means the boss.
// Its result is CheckEnded.
type RunCheck struct {
	IssueKey string
	Action   string
	Dir      string
	Command  string
	Log      string
	IssueRef string
	IssueURL string
	Branch   string
	Mate     string
}

// FindPullRequest asks the tracker for the pull request opened from Branch
// once Action's session on the issue ended: an open one, or a closed or
// merged one created at or after Since, which is zero for a resumed
// workspace (KTD6). Its result is PullRequestFound. The core asks only when
// the tracker can find pull requests (FindingPullRequests).
type FindPullRequest struct {
	IssueKey string
	Action   string
	Branch   string
	Since    time.Time
}

// StopCheck asks the engine to stop the running check of Action on the
// issue. The check's end still arrives as CheckEnded.
type StopCheck struct {
	IssueKey string
	Action   string
}

// ReportStatus asks the tracker to show Status on its issue's status
// comment (KTD3). Its result is a StatusResult carrying Status.IssueKey. The
// core never has two status writes of one issue in flight.
type ReportStatus struct {
	Status crew.Status
}

// ReportPullRequests asks the tracker to show Report on the open pull
// requests that close its issue (KTD1). Its result is a PullRequestsResult
// carrying Report.IssueKey. The core never has two reports of one issue in
// flight, and a retried report keeps its ID.
type ReportPullRequests struct {
	Report crew.PullRequestReport
}

func (ListIssues) command()         {}
func (ListBoard) command()          {}
func (Move) command()               {}
func (ReportFailure) command()      {}
func (CreateWorkspace) command()    {}
func (ReopenWorkspace) command()    {}
func (RecordRun) command()          {}
func (StartSession) command()       {}
func (StopSession) command()        {}
func (RunCheck) command()           {}
func (FindPullRequest) command()    {}
func (StopCheck) command()          {}
func (ReportStatus) command()       {}
func (ReportPullRequests) command() {}
