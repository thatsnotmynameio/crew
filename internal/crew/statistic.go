package crew

import (
	"strconv"
	"time"
)

// Statistic is one record crew keeps in its statistics store, which
// outlives restarts and repositories: a Process, a RepositoryRecord, an
// IssueSighting, a LabelMove, a RuleRunSpan, an ActionSpan, a RouteSpan or
// a StepSpan.
//
//sumtype:decl
type Statistic interface {
	statistic()
}

// ProcessID identifies one crew process in the statistics store, so the
// work it runs can point to it. It is the store's own, apart from the run
// journal's process id.
type ProcessID string

// Process is one crew process, recorded once when it starts. It is not a
// level of the work it runs: one process works on many issues, and one
// issue's runs span many processes.
type Process struct {
	// ID identifies the process.
	ID ProcessID
	// Version is crew's version, as crew --version prints it.
	Version string
	// Folder is the root of the repository the process works in.
	Folder string
	// Start is when the process started.
	Start time.Time
}

func (Process) statistic() {}

// TrackerName names a tracker adapter, as the config's tracker.name names
// it, such as "github". A repository's id is unique only within its
// tracker, so the store keys a repository by both.
type TrackerName string

// RepositoryRecord is a repository crew works in, with the identity and
// the name its tracker gives it. The store keeps one per tracker and id,
// with the latest name.
type RepositoryRecord struct {
	// Tracker is the tracker the repository is on.
	Tracker TrackerName
	// Repository is the repository's identity and display name.
	Repository Repository
}

func (RepositoryRecord) statistic() {}

// IssueSighting is crew seeing an issue that carries a label one of its
// rules knows. The store keeps the first sighting of each issue; a later
// one tells it which label the issue is at now.
type IssueSighting struct {
	// Tracker is the tracker the issue is on.
	Tracker TrackerName
	// Issue is the issue's identity: its repository and key.
	Issue IssueID
	// Ref is how humans write the issue, such as "#42".
	Ref string
	// Kind is whether the item is an issue or a pull request.
	Kind Kind
	// Created is when the tracker says the issue was opened, when it says.
	Created Optional[time.Time]
	// Seen is when crew saw the issue.
	Seen time.Time
	// State is the one crew state the issue is in, none when it is in two
	// or more.
	State Optional[State]
}

func (IssueSighting) statistic() {}

// LabelMove is an issue moving from one crew label to another: an event
// of the issue, made by a rule run or outside crew.
type LabelMove struct {
	// Tracker is the tracker the issue is on.
	Tracker TrackerName
	// Issue is the issue that moved.
	Issue IssueID
	// From is the label the issue left.
	From State
	// To is the label the issue reached.
	To State
	// Moved is the tracker's time of the move, when the tracker reports
	// one.
	Moved Optional[time.Time]
	// Seen is when crew made the move or saw it.
	Seen time.Time
	// Run is the rule run that made the move, none when it was made
	// outside crew.
	Run Optional[RuleRunID]
}

func (LabelMove) statistic() {}

// RuleRunSpan is a rule run as a span of work under its issue: it opens
// when the rule takes the issue and ends when crew releases the run. crew
// records it twice, at the take with no end and at the release with its
// end; the store keeps one span and its first end. A span a killed crew
// left stays without an end: the run a restarted crew takes is another
// span, which names the run it continues.
type RuleRunSpan struct {
	// Tracker is the tracker the issue is on.
	Tracker TrackerName
	// Issue is the issue the run was for: the span's parent.
	Issue IssueID
	// Run is the rule run, which identifies the span.
	Run RuleRunID
	// Process is the crew process that took the run.
	Process ProcessID
	// Rule is the rule that ran.
	Rule RuleName
	// Queue is the queue the rule runs in, none for a rule of no named
	// queue.
	Queue Optional[QueueName]
	// Continues is the run of the same issue and rule this run continues,
	// when there was one.
	Continues Optional[RuleRunID]
	// Start is when the rule took the issue.
	Start time.Time
	// End is how and when the run ended, none while it runs.
	End Optional[RuleRunEnd]
}

func (RuleRunSpan) statistic() {}

// RuleRunEnd is how a rule run ended: when, its outcome, the route it
// ended through and the halt that chose that route.
type RuleRunEnd struct {
	// At is when crew released the run.
	At time.Time
	// Outcome says how the run ended.
	Outcome RunOutcome
	// Route is the route the run ended through, none when its take did not
	// land.
	Route Optional[RouteName]
	// Halt is the halt that chose the route, none when the run chose it on
	// its own.
	Halt Optional[RunHalt]
}

// RunOutcome is how a rule run ended, as the statistics store writes it.
type RunOutcome string

// The outcomes of a rule run.
const (
	// OutcomeRouted is a run whose route's final move or close landed, or
	// whose route has no steps.
	OutcomeRouted RunOutcome = "routed"
	// OutcomeRouteDropped is a run whose route's final move or close was
	// dropped, as the item was moved or closed meanwhile: the route is
	// finished, the item is where someone else put it.
	OutcomeRouteDropped RunOutcome = "route_dropped"
	// OutcomeRouteGivenUp is a run whose route's final move or close the
	// tracker refused, or whose final try failed: the route is unfinished,
	// and the rule's next run runs it again.
	OutcomeRouteGivenUp RunOutcome = "route_given_up"
	// OutcomeNotTaken is a run whose take did not land: it chose no route.
	OutcomeNotTaken RunOutcome = "not_taken"
)

// RunHalt is what halted a rule run and chose its route, as the statistics
// store writes it.
type RunHalt string

// The halts of a rule run.
const (
	// HaltStop is crew's stop, which reached the run before it chose its
	// route and sent it through FailedRoute.
	HaltStop RunHalt = "stop"
	// HaltRunTimeLimit is crew's run time limit, which kept the run's next
	// action from starting and sent it through FailedRoute.
	HaltRunTimeLimit RunHalt = "run_time_limit"
)

// SpanID identifies a span in the statistics store. A rule run's span has
// the run's id; the spans under it derive theirs from it, so a span
// recorded twice is the same span (KTD1).
type SpanID string

// WorkOutcome is how an action or a route's step ended, as the statistics
// store writes it. Its last three say a halt chose the end.
type WorkOutcome string

// The outcomes of an action or a route's step.
const (
	// WorkSucceeded is an action that ended well, or a step that ran.
	WorkSucceeded WorkOutcome = "succeeded"
	// WorkFailed is an action or a step that failed of itself.
	WorkFailed WorkOutcome = "failed"
	// WorkStopped is an action or a step that crew's stop ended.
	WorkStopped WorkOutcome = "stopped"
	// WorkStoppedBeforeStart is an action or a step that crew's stop kept
	// from starting.
	WorkStoppedBeforeStart WorkOutcome = "stopped_before_start"
	// WorkRunTimeLimit is an action that crew's run time limit kept from
	// starting.
	WorkRunTimeLimit WorkOutcome = "run_time_limit"
)

// Work is what an action or a route's step did, with its kind's detail:
// SessionWork, ShellWork, FunctionWork, QuestionWork or CheckWork. A step
// only ever does ShellWork or FunctionWork.
//
//sumtype:decl
type Work interface {
	work()
}

// SessionWork is an action that ran one coding-agent session.
type SessionWork struct {
	// Agent is the agent whose harness ran the session.
	Agent AgentName
	// Harness is the harness that ran it.
	Harness HarnessName
	// Bot is the bot the session acted as, empty for the tracker's own
	// identity.
	Bot BotName
	// PromptHash is the hash of the action's prompt template
	// (Prompt.Hash), never its text.
	PromptHash string
	// Started is when the session started, none when it never did.
	Started Optional[time.Time]
	// Turns is how many turns the session took, when its harness said.
	Turns Optional[int]
	// Tokens is the tokens its harness reported, one record per model
	// (Usage.TokenRecords): none when it reported none, which reads as not
	// reported, never as zero.
	Tokens []ModelTokens
}

// ShellWork is an action or a step that ran one shell script.
type ShellWork struct {
	// ExitStatus is the script's exit status, none when it did not exit.
	ExitStatus Optional[int]
}

// FunctionWork is an action or a step that called one of crew's
// functions.
type FunctionWork struct {
	// Function is the function's registered name.
	Function FunctionName
	// Verdict is the verdict the function returned, none when it returned
	// none.
	Verdict Optional[Verdict]
}

// QuestionWork is a rule's question action, which asks its question and
// ends at once.
type QuestionWork struct{}

// CheckWork is the answered rule's check of where the item returns.
type CheckWork struct{}

func (SessionWork) work()  {}
func (ShellWork) work()    {}
func (FunctionWork) work() {}
func (QuestionWork) work() {}
func (CheckWork) work()    {}

// ActionSpan is an action of a rule run that ended, as a span of work
// under the run's span. crew records it once, when the action ends, with
// its start and its end.
type ActionSpan struct {
	// Issue and Rule are the run's issue and rule, for crew's words only:
	// the store finds them through the run's span.
	Issue IssueID
	Rule  RuleName
	// Run is the rule run the action ran in: the span's parent.
	Run RuleRunID
	// Action is the action's name.
	Action ActionName
	// Process is the crew process that ran it.
	Process ProcessID
	// Start and End are when the action started and ended.
	Start time.Time
	End   time.Time
	// Outcome says how it ended.
	Outcome WorkOutcome
	// Verdict is the action's verdict.
	Verdict Verdict
	// Work is what the action did.
	Work Work
}

// ID returns the span's id: the run's, then action/ and the action's name.
func (s ActionSpan) ID() SpanID { return actionSpanID(s.Run, s.Action) }

// Parent returns the id of the span it hangs from: its rule run's.
func (s ActionSpan) Parent() SpanID { return SpanID(s.Run) }

// RouteSpan is the route a rule run ended through, as a span of work
// under the action whose verdict chose it. crew records it twice, when the
// run chooses it with no end and when crew releases the run with its end;
// the store keeps one span and its first end.
type RouteSpan struct {
	// Issue and Rule are the run's issue and rule, for crew's words only.
	Issue IssueID
	Rule  RuleName
	// Run is the rule run that chose the route.
	Run RuleRunID
	// Process is the crew process that chose it.
	Process ProcessID
	// Route is the route's name.
	Route RouteName
	// ChosenBy is the action of this run whose verdict chose the route,
	// none when no action of this run did: the route's parent is then the
	// rule run's span (KTD9).
	ChosenBy Optional[RouteChoice]
	// Start is when the run chose the route.
	Start time.Time
	// End is how and when the route ended, none while it runs.
	End Optional[RouteEnd]
}

func (ActionSpan) statistic() {}

// ID returns the span's id: the run's, then /route.
func (s RouteSpan) ID() SpanID { return routeSpanID(s.Run) }

// Parent returns the id of the span it hangs from: the action that chose
// it, else its rule run.
func (s RouteSpan) Parent() SpanID {
	if chosen, ok := s.ChosenBy.Get(); ok {
		return actionSpanID(s.Run, chosen.Action)
	}
	return SpanID(s.Run)
}

func (RouteSpan) statistic() {}

// RouteChoice is the action whose verdict chose a route, and that verdict.
type RouteChoice struct {
	Action  ActionName
	Verdict Verdict
}

// RouteEnd is how a route ended: when, and the rule run's outcome.
type RouteEnd struct {
	// At is when crew released the run.
	At time.Time
	// Outcome is the rule run's outcome.
	Outcome RunOutcome
}

// StepSpan is a shell or function step of a route, as a span of work
// under the route's span. crew records it once, when the step ends.
type StepSpan struct {
	// Issue and Rule are the run's issue and rule, for crew's words only.
	Issue IssueID
	Rule  RuleName
	// Run is the rule run whose route the step is in.
	Run RuleRunID
	// Index is the step's place in the route, from 0.
	Index int
	// Name is the shell action or function the step runs.
	Name ActionName
	// Process is the crew process that ran it.
	Process ProcessID
	// Start and End are when the step started and ended.
	Start time.Time
	End   time.Time
	// Outcome says how it ended.
	Outcome WorkOutcome
	// Work is what the step did: ShellWork or FunctionWork.
	Work Work
}

// ID returns the span's id: its route's, then / and its index.
func (s StepSpan) ID() SpanID { return routeSpanID(s.Run) + "/" + SpanID(strconv.Itoa(s.Index)) }

// Parent returns the id of the span it hangs from: its route's.
func (s StepSpan) Parent() SpanID { return routeSpanID(s.Run) }

// actionSpanID is the id of the span of action in run.
func actionSpanID(run RuleRunID, action ActionName) SpanID {
	return SpanID(string(run) + "/action/" + string(action))
}

func (StepSpan) statistic() {}

// routeSpanID is the id of the span of run's route.
func routeSpanID(run RuleRunID) SpanID { return SpanID(string(run) + "/route") }
