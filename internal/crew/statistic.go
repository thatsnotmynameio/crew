package crew

import "time"

// Statistic is one record crew keeps in its statistics store, which
// outlives restarts and repositories: a Process, a RepositoryRecord, an
// IssueSighting or a LabelMove.
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
