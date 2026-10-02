package core

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Input is one thing the engine tells the core: a tick, a stop request, the
// end of the run time, or the result of a Command. Every input carries At, the time it reached the
// engine's inbox, so start and elapsed times stay pure in the core (KTD2).
// The set of inputs is closed: only this package's types implement Input.
type Input interface {
	// Stamped returns a copy of the input whose At is at. The engine stamps
	// each input with this as it takes it from its inbox.
	Stamped(at time.Time) Input
	arrival() time.Time
}

// Tick is a poll: the core lists issues, unless a listing is outstanding,
// and retries its owed calls (KTD8). Ticks after a stop request do nothing.
type Tick struct {
	At time.Time
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
}

// ListFailed is a ListIssues that failed. The next tick lists again.
type ListFailed struct {
	At time.Time
	// Reason says why in one line, with local paths already shortened.
	Reason string
}

// Result classifies how a tracker call (a Move or a ReportFailure) ended.
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
	// ResultFailed means the call failed transiently. A verdict call becomes
	// owed and is retried; a take is abandoned and the next poll may take
	// the issue again.
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
	return "unknown"
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

// WorkspaceReady is a CreateWorkspace that succeeded.
type WorkspaceReady struct {
	At time.Time
	// IssueKey and Action identify the CreateWorkspace this answers.
	IssueKey string
	Action   string
	// Workspace is the workspace's unique name.
	Workspace string
	// Dir is the workspace's absolute directory, where the session runs.
	Dir string
	// Branch is the branch the action's work goes on.
	Branch string
	// Log is the repository-relative path of the session's log file, built
	// by the engine from Workspace (KTD12).
	Log string
}

// WorkspaceFailed is a CreateWorkspace that failed. The action counts as
// failed with Reason, and its sibling actions go on.
type WorkspaceFailed struct {
	At       time.Time
	IssueKey string
	Action   string
	Reason   string
}

// SessionStarted is a StartSession whose session is now running. Its At is
// the action's start time.
type SessionStarted struct {
	At       time.Time
	IssueKey string
	Action   string
}

// SessionFailedToStart is a StartSession that started no session. The action
// counts as failed with Reason.
type SessionFailedToStart struct {
	At       time.Time
	IssueKey string
	Action   string
	Reason   string
}

// SessionEnded is a running session that ended, with its harness's verdict.
type SessionEnded struct {
	At       time.Time
	IssueKey string
	Action   string
	Outcome  crew.Outcome
}

// Stamped implements Input.
func (i Tick) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i StopRequested) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i TimeUp) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i IssuesListed) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i ListFailed) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i CallResult) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i WorkspaceReady) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i WorkspaceFailed) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i SessionStarted) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i SessionFailedToStart) Stamped(at time.Time) Input { i.At = at; return i }

// Stamped implements Input.
func (i SessionEnded) Stamped(at time.Time) Input { i.At = at; return i }

func (i Tick) arrival() time.Time                 { return i.At }
func (i StopRequested) arrival() time.Time        { return i.At }
func (i TimeUp) arrival() time.Time               { return i.At }
func (i IssuesListed) arrival() time.Time         { return i.At }
func (i ListFailed) arrival() time.Time           { return i.At }
func (i CallResult) arrival() time.Time           { return i.At }
func (i WorkspaceReady) arrival() time.Time       { return i.At }
func (i WorkspaceFailed) arrival() time.Time      { return i.At }
func (i SessionStarted) arrival() time.Time       { return i.At }
func (i SessionFailedToStart) arrival() time.Time { return i.At }
func (i SessionEnded) arrival() time.Time         { return i.At }
