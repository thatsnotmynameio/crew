package crew

import "time"

// Statistic is one record crew keeps in its statistics store, which
// outlives restarts and repositories: a Process, a RepositoryRecord, an
// IssueSighting, a LabelMove or a RuleRunSpan.
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
