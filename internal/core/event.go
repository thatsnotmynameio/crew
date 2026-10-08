package core

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Published is an event the core publishes for subscribers (R16, R13): the
// TUI and the line renderer. It is either a crew.RunEvent, one of a rule
// run's events that the views word (the take, its landed move, a missing
// or ready workspace, a started session or script, an ended action, the
// route chosen), or an Event of the core's own, a step of the route that
// settled among them. The rule runs' other events are not published.
type Published interface {
	// Time returns when the event happened.
	Time() time.Time
}

// Event is an event of the core's own, about what no rule run owns: bots,
// listings, polls, tracker calls and status writes, the journal, the run
// time limit and the stop. Events are plain values; At is the arrival time
// of the input that caused them.
//
//sumtype:decl
type Event interface {
	Published
	event()
}

// RunNotRecorded is a run event a resume depends on that the engine could
// not append to the run journal (KTD18): the run's worktree, an action's
// start or end, its session's start, the route it chose, a step's outcome
// or its release. After a restart, crew may not know where that run
// stopped.
type RunNotRecorded struct {
	At       time.Time
	IssueID  crew.IssueID
	IssueRef string
	Rule     crew.RuleName
	// Action is the action the event is about; empty for an event about
	// the run as a whole.
	Action crew.ActionName
	// What says which event was not recorded, in crew's words, such as
	// "the start of lfg" or "the route failed it chose".
	What   string
	Reason string
}

// RouteStepEnded is a step of the route a rule run ends through that
// settled (crew.StepEnded), with what the step does, for the views to word
// it (R16, KTD23).
type RouteStepEnded struct {
	At       time.Time
	IssueID  crew.IssueID
	IssueRef string
	Rule     crew.RuleName
	// Route is the route the run ends through.
	Route crew.RouteName
	// Step is the step's index in the route, and Plan what it does.
	Step int
	Plan crew.StepPlan
	// From is the state a move or close took the issue from, the rule's
	// running label; empty for the other steps.
	From    crew.State
	Outcome crew.StepOutcome
}

// IssueSkipped is a listed issue found in two or more crew states. It is not
// taken (R15); a later poll takes it once it is in exactly one.
type IssueSkipped struct {
	At       time.Time
	IssueID  crew.IssueID
	IssueRef string
	States   []crew.State
}

// IssueOfOtherKind is a listed item in one crew state, the Label of a rule
// that Takes the other kind of item. It is not taken, moved or commented on
// (#92). It is emitted once while the item stays in that state, and again
// once a listing found it in no such state.
type IssueOfOtherKind struct {
	At       time.Time
	IssueID  crew.IssueID
	IssueRef string
	// Kind is the item's kind.
	Kind crew.Kind
	// Label is the crew state the item is in, Rule's label.
	Label crew.State
	Rule  crew.RuleName
	// Takes is the kind Rule takes.
	Takes crew.Kind
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
// issues the core holds reach max_parallel_issues, or every queue some rule
// runs in is full, so a listing could take nothing. The rest of the tick ran
// as usual.
type PollSkipped struct {
	At time.Time
	// Busy is how many issues the core holds.
	Busy int
	// Slots is how many the rules can use: the slots of the queues they
	// run in, summed, at most max_parallel_issues (R9).
	Slots int
}

// ListingFailed is a listing that failed. The next tick lists again.
type ListingFailed struct {
	At     time.Time
	Reason string
}

// CallOwed is a take move, a route's move, close, comment or report, or a
// pull request report that failed transiently. The core owes it and retries it at the
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
	IssueID  crew.IssueID
	IssueRef string
	Result   Result
	Reason   string
}

// WindingDown means the run time limit has passed (R6): the core takes no
// new issue and stops once the runs of the issues it holds ended. It is emitted
// once, unless a stop was requested first.
type WindingDown struct {
	At time.Time
	// Limit is the run time limit.
	Limit time.Duration
}

// Paused means crew paused the taking of new issues (R7 of #282): it lists
// and takes nothing until it resumes, while the issues it holds run on.
type Paused struct {
	At time.Time
}

// Resumed means crew takes new issues again after a pause (R7 of #282).
type Resumed struct {
	At time.Time
}

// Stopped means a stop, requested or ending a wind-down, has completed: the
// core holds no issue, no owed call, no status write in flight or owed and no
// pull request report not settled. It is emitted once.
type Stopped struct {
	At time.Time
}

// BotStopped is a bot that stopped acting during the run: crew's writes as
// the default bot went back to you, or the bot's token was not renewed (R9,
// R11). It is emitted once per problem, when the bot gains it.
type BotStopped struct {
	At  time.Time
	Bot crew.BotName
	// Reason is the short reason: "writes as you" or "token not renewed".
	Reason string
	// Warning is the full reason and its fix.
	Warning string
}

// BotActsAgain is a bot whose state returned to acting during the run:
// its token was renewed after a failure (R10).
type BotActsAgain struct {
	At  time.Time
	Bot crew.BotName
}

// CallKind tells a Move, a ReportFailure, a ReportPullRequests, a Comment
// and a Close apart in a Call.
type CallKind int

// The kinds of tracker call.
const (
	// CallMove is a Move.
	CallMove CallKind = iota
	// CallReport is a ReportFailure.
	CallReport
	// CallPullRequests is a ReportPullRequests.
	CallPullRequests
	// CallComment is a Comment.
	CallComment
	// CallClose is a Close.
	CallClose
)

// String names the kind for renderers.
func (k CallKind) String() string {
	switch k {
	case CallReport:
		return "report"
	case CallPullRequests:
		return "pull requests"
	case CallComment:
		return "comment"
	case CallClose:
		return "close"
	default:
		return "move"
	}
}

// Call describes a tracker call in events and in the View.
type Call struct {
	Kind     CallKind
	IssueID  crew.IssueID
	IssueRef string
	// From and To are the move's states; both are empty for a failure
	// report and a comment. For a pull request report, To is the state the
	// pull requests are put in and From is empty; for a close, From is the
	// state the issue is closed from and To is empty.
	From crew.State
	To   crew.State
}

// Time implements Event.
func (e RunNotRecorded) Time() time.Time { return e.At }

// Time implements Event.
func (e RouteStepEnded) Time() time.Time { return e.At }

// Time implements Event.
func (e IssueSkipped) Time() time.Time { return e.At }

// Time implements Event.
func (e IssueOfOtherKind) Time() time.Time { return e.At }

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
func (e Paused) Time() time.Time { return e.At }

// Time implements Event.
func (e Resumed) Time() time.Time { return e.At }

// Time implements Event.
func (e Stopped) Time() time.Time { return e.At }

// Time implements Event.
func (e BotStopped) Time() time.Time { return e.At }

// Time implements Event.
func (e BotActsAgain) Time() time.Time { return e.At }

func (RunNotRecorded) event()   {}
func (RouteStepEnded) event()   {}
func (IssueSkipped) event()     {}
func (IssueOfOtherKind) event() {}
func (PollDone) event()         {}
func (PollSkipped) event()      {}
func (ListingFailed) event()    {}
func (CallOwed) event()         {}
func (CallDropped) event()      {}
func (StatusFailed) event()     {}
func (WindingDown) event()      {}
func (Paused) event()           {}
func (Resumed) event()          {}
func (Stopped) event()          {}
func (BotStopped) event()       {}
func (BotActsAgain) event()     {}
