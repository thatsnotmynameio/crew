package crew

import (
	"slices"
	"time"
)

// ActionRun is one run of an action inside a rule run: the action's name,
// its session, how its shell script or its function ended, whether it is
// the answered rule's check, and where it stands. The
// workspace, log and pull requests belong to the rule run, which all its
// actions share. It cannot be changed once built: a rule run's events make
// new ones.
type ActionRun struct {
	name     ActionName
	session  Optional[time.Time]
	usage    Usage
	shell    Optional[ShellOutcome]
	function Optional[FunctionOutcome]
	// checked says whether the action is the answered rule's check: crew
	// asked to read the item's comments for it (ActionReturnAsked).
	checked bool
	state   ActionRunState
}

// newActionRun returns the run of the action named name, which its rule run
// has not reached.
func newActionRun(name ActionName) ActionRun {
	return ActionRun{name: name, state: AwaitingTurn{}}
}

// Name returns the action's name.
func (a ActionRun) Name() ActionName { return a.name }

// SessionStarted returns when its session started, when one did.
func (a ActionRun) SessionStarted() Optional[time.Time] { return a.session }

// Usage returns what its session reported it used, once the session ended;
// the zero Usage before. The copy has its own Models and ByModel.
func (a ActionRun) Usage() Usage { return cloneUsage(a.usage) }

// Spend returns what its session used, or nothing when no session started.
func (a ActionRun) Spend() Spend {
	if _, ok := a.session.Get(); !ok {
		return Spend{}
	}
	return a.usage.Spend()
}

// Shell returns how its shell script ended, once a shell action's script
// ended.
func (a ActionRun) Shell() Optional[ShellOutcome] { return a.shell }

// Function returns how its function ended, once a function action's
// function ended.
func (a ActionRun) Function() Optional[FunctionOutcome] { return a.function }

// State returns where the action run stands.
func (a ActionRun) State() ActionRunState { return a.state }

// Ended reports whether the action run ended.
func (a ActionRun) Ended() bool {
	_, ended := a.state.(Finished)
	return ended
}

// running reports whether the action run has a session, a script, a
// function or a read of the item's comments that was asked for and has not
// ended.
func (a ActionRun) running() bool {
	switch a.state.(type) {
	case StartingSession, InSession, InShell, InFunction, InReturnCheck:
		return true
	case AwaitingTurn, DoneInEarlierRun, NotRun, Finished:
	}
	return false
}

// ActionRunState is where an action run stands: AwaitingTurn,
// DoneInEarlierRun, StartingSession, InSession, InShell, InFunction,
// InReturnCheck, Finished or NotRun.
//
//sumtype:decl
type ActionRunState interface {
	actionRunState()
}

// AwaitingTurn is an action run its rule run has not reached: its take has
// not landed, its workspace is not ready, or an action before it has not
// ended.
type AwaitingTurn struct{}

// DoneInEarlierRun is an action before the one its rule run resumes at: it
// went on to the next action in the run this one continues, so it does not
// run again.
type DoneInEarlierRun struct{}

// StartingSession is an action run whose session is being started.
type StartingSession struct{}

// InSession is an action run whose session runs.
type InSession struct{}

// InShell is an action run whose shell script runs.
type InShell struct {
	// Started is when crew asked for the script to run.
	Started time.Time
}

// InFunction is an action run whose function runs.
type InFunction struct {
	// Started is when crew asked for the function to run.
	Started time.Time
}

// InReturnCheck is the answered rule's action run while crew reads the
// item's comments to find where the item returns (KTD2).
type InReturnCheck struct {
	// Started is when crew asked for the read.
	Started time.Time
}

// Finished is an action run that ended, with its verdict and where the
// verdict leads.
type Finished struct {
	End     ActionEnd
	Verdict Verdict
	Target  Target
}

// NotRun is an action its rule run never reached: an action before it
// chose a route.
type NotRun struct{}

func (AwaitingTurn) actionRunState()     {}
func (DoneInEarlierRun) actionRunState() {}
func (StartingSession) actionRunState()  {}
func (InSession) actionRunState()        {}
func (InShell) actionRunState()          {}
func (InFunction) actionRunState()       {}
func (InReturnCheck) actionRunState()    {}
func (Finished) actionRunState()         {}
func (NotRun) actionRunState()           {}

// ActionEnd is how an action run ended: EndSucceeded or EndFailed.
//
//sumtype:decl
type ActionEnd interface {
	// Outcome returns the end as an Outcome.
	Outcome() Outcome
	actionEnd()
}

// EndSucceeded is an action run that ended well, with its reason.
type EndSucceeded struct {
	Reason SessionText
}

// EndFailed is an action run that ended in a failure, with its reason and
// what made it fail.
type EndFailed struct {
	Reason SessionText
	Cause  FailureCause
}

// Outcome implements ActionEnd.
func (e EndSucceeded) Outcome() Outcome { return Outcome{Succeeded: true, Reason: e.Reason} }

// Outcome implements ActionEnd.
func (e EndFailed) Outcome() Outcome { return Outcome{Reason: e.Reason} }

func (EndSucceeded) actionEnd() {}
func (EndFailed) actionEnd()    {}

// Lookup is how the lookup of a rule run's pull requests stands:
// LookupNotAsked, LookupPending or LookupDone.
//
//sumtype:decl
type Lookup interface {
	lookup()
}

// LookupNotAsked is a pull request crew did not ask its tracker for.
type LookupNotAsked struct{}

// LookupPending is a lookup asked for and not answered yet.
type LookupPending struct{}

// LookupDone is a lookup that was answered, with what it found.
type LookupDone struct {
	PullRequest PullRequest
}

func (LookupNotAsked) lookup() {}
func (LookupPending) lookup()  {}
func (LookupDone) lookup()     {}

// ActionRunSnapshot is an action run as plain data, inside a
// RuleRunSnapshot.
type ActionRunSnapshot struct {
	Name           ActionName
	SessionStarted Optional[time.Time]
	Usage          Usage
	Shell          Optional[ShellOutcome]
	Function       Optional[FunctionOutcome]
	Checked        bool
	State          ActionRunState
}

// snapshot returns a as plain data, sharing no memory with it.
func (a ActionRun) snapshot() ActionRunSnapshot {
	return ActionRunSnapshot{
		Name: a.name, SessionStarted: a.session, Usage: cloneUsage(a.usage), Shell: a.shell, Function: a.function,
		Checked: a.checked, State: a.state,
	}
}

// restoreAction returns the action run s describes, sharing no memory with
// it.
func restoreAction(s ActionRunSnapshot) ActionRun {
	return ActionRun{
		name: s.Name, session: s.SessionStarted, usage: cloneUsage(s.Usage), shell: s.Shell, function: s.Function,
		checked: s.Checked, state: s.State,
	}
}

// cloneUsage returns a copy of u with its own Models and ByModel.
func cloneUsage(u Usage) Usage {
	u.Models = slices.Clone(u.Models)
	u.ByModel = slices.Clone(u.ByModel)
	return u
}
