package core

import (
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Input is one thing the engine tells the core: a tick, a stop request, the
// end of the run time, or the result of a Command. Every input carries At,
// the time it reached the engine's inbox, so start and elapsed times stay
// pure in the core (KTD2).
// The set of inputs is closed: only this package's types implement Input.
type Input interface {
	// Stamped returns a copy of the input whose At is at. The engine stamps
	// each input with this as it takes it from its inbox, with a fresh seed
	// besides the time, so the ids of the rule runs the input takes are
	// minted outside the core and are global (KTD5). Only IssuesListed
	// keeps the seed: only a listing takes issues.
	Stamped(at time.Time, seed uuid.UUID) Input
	arrival() time.Time
}

// Tick is a poll: the core lists issues, unless a listing is outstanding or
// every slot is busy, retries its owed calls and pull request reports (KTD8)
// and reports the status of its running issues. Ticks after a stop request
// do nothing.
type Tick struct {
	At time.Time
	// Said is what the running sessions last said, for their issues'
	// statuses, with local paths already shortened.
	Said []Said
}

// Said is what the running session of Action on an issue last said.
type Said struct {
	IssueID crew.IssueID
	Action  crew.ActionName
	Text    crew.Said
}

// StopRequested asks the core to stop (R9). The core starts nothing new,
// stops the running sessions, gives each owed call one final try and judges
// every issue as its actions end. A second request changes nothing.
type StopRequested struct {
	At time.Time
}

// TimeUp says the run time limit has passed since the first poll (R2). The
// core takes no new issue from now on, lets the issues it holds run and be
// judged as usual, and once no action is left to end gives each owed call
// its final try and stops, as after StopRequested. It does nothing after a
// stop request or a first TimeUp.
type TimeUp struct {
	At time.Time
	// Limit is the run time limit, for the WindingDown event.
	Limit time.Duration
}

// IssuesListed is the result of ListIssues: the open issues in any of the
// requested states, each carrying every crew state it is in.
type IssuesListed struct {
	At     time.Time
	Issues []crew.Issue
	// Seed is the fresh seed the engine stamped, from which the rule runs
	// this listing takes get their ids.
	Seed uuid.UUID
}

// ListFailed is a ListIssues that failed. The next tick lists again.
type ListFailed struct {
	At time.Time
	// Reason says why in one line, with local paths already shortened.
	Reason string
}

// BoardListed is the result of ListBoard: the open issues that carry any of
// the board's labels, each with the board labels it carries.
type BoardListed struct {
	At     time.Time
	Issues []crew.BoardIssue
}

// BoardListFailed is a ListBoard that failed. The core keeps the last board
// it read, and the next tick reads it again.
type BoardListFailed struct {
	At time.Time
	// Reason says why in one line, with local paths already shortened.
	Reason string
}

// BotsChecked is a reading of the bots' live state, sent when it differs
// from the last one (KTD1, KTD3).
type BotsChecked struct {
	At time.Time
	// WritesLost is the warning crew wrote when its writes as the default
	// bot went back to you; empty while they go as the default bot.
	WritesLost string
	// NotRenewed holds, by bot, the warning of its last renewal, for each
	// bot whose last renewal failed.
	NotRenewed map[crew.BotName]string
}

// Result classifies how a tracker call (a Move, a ReportFailure or a
// ReportPullRequests) ended.
// The engine maps the port's errors onto it: nil is ResultDone,
// port.ErrMovedMeanwhile is ResultMovedMeanwhile, port.ErrRefused is
// ResultRefused, and any other error, a timeout included, is ResultFailed.
type Result int

// The results of a tracker call.
const (
	// ResultDone means the call succeeded.
	ResultDone Result = iota
	// ResultMovedMeanwhile means the issue was closed or left the expected
	// state. The call is dropped and reported, never retried.
	ResultMovedMeanwhile
	// ResultRefused means the tracker refused for good. The call is dropped
	// and reported, never retried.
	ResultRefused
	// ResultFailed means the call failed transiently. The call becomes owed
	// and is retried.
	ResultFailed
)

// String names the result for renderers.
func (r Result) String() string {
	switch r {
	case ResultDone:
		return "done"
	case ResultMovedMeanwhile:
		return "moved meanwhile"
	case ResultRefused:
		return "refused"
	case ResultFailed:
		return "failed"
	}
	return unknownName
}

// CallResult is how a Move or a ReportFailure command ended, correlated by
// the command's ID.
type CallResult struct {
	At time.Time
	// ID is the ID of the Move or ReportFailure this answers.
	ID     CallID
	Result Result
	// Reason says why the call did not succeed, in one line. Empty on
	// ResultDone.
	Reason string
}

// StatusResult is how a ReportStatus command ended, correlated by its
// issue's id. Its Result is classified as a CallResult's is.
type StatusResult struct {
	At      time.Time
	IssueID crew.IssueID
	Result  Result
	// Reason says why the write did not succeed, in one line. Empty on
	// ResultDone.
	Reason string
}

// PullRequestsResult is how a ReportPullRequests command ended, correlated
// by its issue's id. Its Result is classified as a CallResult's is.
type PullRequestsResult struct {
	At      time.Time
	IssueID crew.IssueID
	Result  Result
	// Reason says why the report did not succeed, in one line. Empty on
	// ResultDone.
	Reason string
}

// WorkspaceReady is a CreateWorkspace or ReopenWorkspace that succeeded.
type WorkspaceReady struct {
	At time.Time
	// IssueID and Action identify the CreateWorkspace this answers.
	IssueID crew.IssueID
	Action  crew.ActionName
	// Workspace is the workspace's unique name.
	Workspace crew.WorkspaceName
	// Dir is the workspace's absolute directory, where the session runs.
	Dir string
	// Branch is the branch the action's work goes on.
	Branch string
	// Log is the repository-relative path of the session's log file, built
	// by the engine from Workspace (KTD12).
	Log string
	// LogFromDir is the same log's path relative to Dir, so a resumed
	// session can open it from its workspace.
	LogFromDir string
	// Resumed is set when this answers a ReopenWorkspace: the workspace is
	// the failed run's, as it was left.
	Resumed bool
}

// WorkspaceGone is a ReopenWorkspace whose workspace no longer exists. The
// core creates a fresh one instead.
type WorkspaceGone struct {
	At      time.Time
	IssueID crew.IssueID
	Action  crew.ActionName
}

// RecordFailed is a RecordRun the engine could not write. Record is the
// record that was not written.
type RecordFailed struct {
	At     time.Time
	Record RunRecord
	Reason string
}

// WorkspaceFailed is a CreateWorkspace that failed. The action counts as
// failed with Reason, and its sibling actions go on.
type WorkspaceFailed struct {
	At      time.Time
	IssueID crew.IssueID
	Action  crew.ActionName
	Reason  crew.SessionText
}

// SessionStarted is a StartSession whose session is now running. Its At is
// the action's start time.
type SessionStarted struct {
	At      time.Time
	IssueID crew.IssueID
	Action  crew.ActionName
}

// SessionFailedToStart is a StartSession that started no session. The action
// counts as failed with Reason.
type SessionFailedToStart struct {
	At      time.Time
	IssueID crew.IssueID
	Action  crew.ActionName
	Reason  crew.SessionText
}

// SessionEnded is a running session that ended, with its harness's
// verdict, what the harness reported it used, and the session's last
// message, which only the action's checks read.
type SessionEnded struct {
	At          time.Time
	IssueID     crew.IssueID
	Action      crew.ActionName
	Outcome     crew.Outcome
	Usage       crew.Usage
	LastMessage string
}

// CheckEnded is a RunCheck that ended, with the check's verdict: Passed, or
// it failed, ran out of time, was stopped or could not start, as its Reason
// says.
type CheckEnded struct {
	At      time.Time
	IssueID crew.IssueID
	Action  crew.ActionName
	Passed  bool
	Reason  crew.CheckReason
}

// PullRequestFound is a FindPullRequest that ended: the pull request the
// tracker found, none, or not looked up when the lookup failed.
type PullRequestFound struct {
	At          time.Time
	IssueID     crew.IssueID
	Action      crew.ActionName
	PullRequest crew.PullRequest
}

// Stamped implements Input.
func (i Tick) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i StopRequested) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i TimeUp) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i IssuesListed) Stamped(at time.Time, seed uuid.UUID) Input {
	i.At, i.Seed = at, seed
	return i
}

// Stamped implements Input.
func (i ListFailed) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i BoardListed) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i BoardListFailed) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i BotsChecked) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i CallResult) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i StatusResult) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i PullRequestsResult) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i WorkspaceReady) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i WorkspaceGone) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i RecordFailed) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i WorkspaceFailed) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i SessionStarted) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i SessionFailedToStart) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i SessionEnded) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i CheckEnded) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i PullRequestFound) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

func (i Tick) arrival() time.Time                 { return i.At }
func (i StopRequested) arrival() time.Time        { return i.At }
func (i TimeUp) arrival() time.Time               { return i.At }
func (i IssuesListed) arrival() time.Time         { return i.At }
func (i ListFailed) arrival() time.Time           { return i.At }
func (i BoardListed) arrival() time.Time          { return i.At }
func (i BoardListFailed) arrival() time.Time      { return i.At }
func (i BotsChecked) arrival() time.Time          { return i.At }
func (i CallResult) arrival() time.Time           { return i.At }
func (i StatusResult) arrival() time.Time         { return i.At }
func (i PullRequestsResult) arrival() time.Time   { return i.At }
func (i WorkspaceReady) arrival() time.Time       { return i.At }
func (i WorkspaceFailed) arrival() time.Time      { return i.At }
func (i WorkspaceGone) arrival() time.Time        { return i.At }
func (i RecordFailed) arrival() time.Time         { return i.At }
func (i SessionStarted) arrival() time.Time       { return i.At }
func (i SessionFailedToStart) arrival() time.Time { return i.At }
func (i SessionEnded) arrival() time.Time         { return i.At }
func (i CheckEnded) arrival() time.Time           { return i.At }
func (i PullRequestFound) arrival() time.Time     { return i.At }
