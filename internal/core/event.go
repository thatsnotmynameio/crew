package core

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Event is a domain event the core emits for subscribers (R16): the TUI and
// the line renderer. Events are plain values; At is the arrival time of the
// input that caused them. The set of events is closed: only this package's
// types implement Event.
type Event interface {
	// Time returns when the event happened.
	Time() time.Time
	event()
}

// IssueTaken is an issue a stage picked. Its take move, From the stage's
// label To its moves_to, is now in flight.
type IssueTaken struct {
	At    time.Time
	Issue crew.Issue
	Stage string
	From  crew.State
	To    crew.State
}

// ActionStarted is an action whose session is now running. Resumed is set
// when the session continues a failed run in that run's workspace.
type ActionStarted struct {
	At        time.Time
	IssueKey  string
	IssueRef  string
	Stage     string
	Action    string
	Workspace string
	Branch    string
	Log       string
	Resumed   bool
}

// WorkspaceMissing is a failed run's workspace that no longer exists, found
// when its action was to resume in it. The action gets a fresh workspace,
// unless crew is stopping.
type WorkspaceMissing struct {
	At        time.Time
	IssueKey  string
	IssueRef  string
	Stage     string
	Action    string
	Workspace string
}

// RunNotRecorded is a run record the engine could not write to the run
// journal. After a restart, crew may not know how that run ended.
type RunNotRecorded struct {
	At       time.Time
	IssueKey string
	IssueRef string
	Stage    string
	Action   string
	Reason   string
}

// ActionEnded is an action that ended, successfully or not. An action that
// never ran a session (its prompt did not render, its workspace or session
// failed to start, or crew stopped first) ends too, with a failed Outcome.
// Workspace and Log are empty when the action got no workspace or session.
type ActionEnded struct {
	At        time.Time
	IssueKey  string
	IssueRef  string
	Stage     string
	Action    string
	Outcome   crew.Outcome
	Workspace string
	Log       string
}

// IssueMoved is a move the tracker made, a take or a verdict.
type IssueMoved struct {
	At       time.Time
	IssueKey string
	IssueRef string
	From     crew.State
	To       crew.State
}

// FailureReported is a failure report the tracker posted.
type FailureReported struct {
	At       time.Time
	IssueKey string
	IssueRef string
}

// IssueSkipped is a listed issue found in two or more crew states. It is not
// taken (R15); a later poll takes it once it is in exactly one.
type IssueSkipped struct {
	At       time.Time
	IssueKey string
	IssueRef string
	States   []crew.State
}

// PollDone is a listing the core has acted on.
type PollDone struct {
	At time.Time
	// Listed is how many issues the listing returned.
	Listed int
	// Taken is how many of them were taken.
	Taken int
}

// PollSkipped is a tick that did not list because every slot is busy: the
// issues the core holds reach max_parallel_issues, so a listing could take
// nothing. The rest of the tick ran as usual.
type PollSkipped struct {
	At time.Time
	// Busy is how many issues the core holds.
	Busy int
	// Slots is max_parallel_issues.
	Slots int
}

// ListingFailed is a listing that failed. The next tick lists again.
type ListingFailed struct {
	At     time.Time
	Reason string
}

// CallOwed is a take move, verdict move, failure report or pull request
// report that failed transiently. The core owes it and retries it at the
// next tick (KTD8), or once at stop.
type CallOwed struct {
	At     time.Time
	Call   Call
	Reason string
}

// CallDropped is a tracker call the core gave up: the issue moved meanwhile,
// the tracker refused, or an owed call failed its final try at stop. Result says
// which; the call is never retried.
type CallDropped struct {
	At     time.Time
	Call   Call
	Result Result
	Reason string
}

// StatusFailed is a status write that failed after the previous one for the
// issue succeeded; failures in a row are reported once. Result says how it
// failed. A running status is written again at the next tick, an ended one
// is retried until it lands, unless the tracker refused it or the
// issue moved meanwhile (KTD5).
type StatusFailed struct {
	At       time.Time
	IssueKey string
	IssueRef string
	Result   Result
	Reason   string
}

// WindingDown means the run time limit has passed (R6): the core takes no
// new issue and stops once the issues it holds are judged. It is emitted
// once, unless a stop was requested first.
type WindingDown struct {
	At time.Time
	// Limit is the run time limit.
	Limit time.Duration
}

// Stopped means a stop, requested or ending a wind-down, has completed: the
// core holds no issue, no owed call, no status write in flight or owed and no
// pull request report not settled. It is emitted once.
type Stopped struct {
	At time.Time
}

// CallKind tells a Move, a ReportFailure and a ReportPullRequests apart in a
// Call.
type CallKind int

// The kinds of tracker call.
const (
	// CallMove is a Move.
	CallMove CallKind = iota
	// CallReport is a ReportFailure.
	CallReport
	// CallPullRequests is a ReportPullRequests.
	CallPullRequests
)

// String names the kind for renderers.
func (k CallKind) String() string {
	switch k {
	case CallReport:
		return "report"
	case CallPullRequests:
		return "pull requests"
	default:
		return "move"
	}
}

// Call describes a tracker call in events and in the View.
type Call struct {
	Kind     CallKind
	IssueKey string
	IssueRef string
	// From and To are the move's states; both are empty for a failure
	// report. For a pull request report, To is the state the pull requests
	// are put in and From is empty.
	From crew.State
	To   crew.State
}

// Time implements Event.
func (e IssueTaken) Time() time.Time { return e.At }

// Time implements Event.
func (e ActionStarted) Time() time.Time { return e.At }

// Time implements Event.
func (e WorkspaceMissing) Time() time.Time { return e.At }

// Time implements Event.
func (e RunNotRecorded) Time() time.Time { return e.At }

// Time implements Event.
func (e ActionEnded) Time() time.Time { return e.At }

// Time implements Event.
func (e IssueMoved) Time() time.Time { return e.At }

// Time implements Event.
func (e FailureReported) Time() time.Time { return e.At }

// Time implements Event.
func (e IssueSkipped) Time() time.Time { return e.At }

// Time implements Event.
func (e PollDone) Time() time.Time { return e.At }

// Time implements Event.
func (e PollSkipped) Time() time.Time { return e.At }

// Time implements Event.
func (e ListingFailed) Time() time.Time { return e.At }

// Time implements Event.
func (e CallOwed) Time() time.Time { return e.At }

// Time implements Event.
func (e CallDropped) Time() time.Time { return e.At }

// Time implements Event.
func (e StatusFailed) Time() time.Time { return e.At }

// Time implements Event.
func (e WindingDown) Time() time.Time { return e.At }

// Time implements Event.
func (e Stopped) Time() time.Time { return e.At }

func (IssueTaken) event()       {}
func (ActionStarted) event()    {}
func (ActionEnded) event()      {}
func (WorkspaceMissing) event() {}
func (RunNotRecorded) event()   {}
func (IssueMoved) event()       {}
func (FailureReported) event()  {}
func (IssueSkipped) event()     {}
func (PollDone) event()         {}
func (PollSkipped) event()      {}
func (ListingFailed) event()    {}
func (CallOwed) event()         {}
func (CallDropped) event()      {}
func (StatusFailed) event()     {}
func (WindingDown) event()      {}
func (Stopped) event()          {}
