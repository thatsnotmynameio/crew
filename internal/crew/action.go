package crew

import (
	"slices"
	"time"
)

// ActionRun is one run of an action inside a rule run: the action's name,
// the resume point it inherited, its workspace once ready, its session,
// its checks, the lookup of its pull request and where it stands. It
// cannot be changed once built: a rule run's events make new ones.
type ActionRun struct {
	name      ActionName
	resume    Optional[ResumePoint]
	workspace Optional[OpenedWorkspace]
	session   Optional[time.Time]
	usage     Usage
	checks    []CheckResult
	lookup    Lookup
	state     ActionRunState
}

// newActionRun returns the run of the action named name, waiting for the
// take, with what it inherited.
func newActionRun(name ActionName, resume Optional[ResumePoint]) ActionRun {
	return ActionRun{name: name, resume: resume, lookup: LookupNotAsked{}, state: AwaitingTake{}}
}

// Name returns the action's name.
func (a ActionRun) Name() ActionName { return a.name }

// Resume returns where the action resumes a failed run's work, when the run
// inherited a resume point.
func (a ActionRun) Resume() Optional[ResumePoint] { return a.resume }

// Workspace returns the workspace its session works in, once it was ready.
func (a ActionRun) Workspace() Optional[OpenedWorkspace] { return a.workspace }

// SessionStarted returns when its session started, when one did.
func (a ActionRun) SessionStarted() Optional[time.Time] { return a.session }

// Usage returns what its session reported it used, once the session ended;
// the zero Usage before. The copy has its own Models.
func (a ActionRun) Usage() Usage { return cloneUsage(a.usage) }

// Spend returns what its session used, or nothing when no session started.
func (a ActionRun) Spend() Spend {
	if _, ok := a.session.Get(); !ok {
		return Spend{}
	}
	return a.usage.Spend()
}

// Checks returns a copy of how its checks that ended so far ended, in the
// order they ran.
func (a ActionRun) Checks() []CheckResult { return slices.Clone(a.checks) }

// Lookup returns how the lookup of its pull request stands.
func (a ActionRun) Lookup() Lookup { return a.lookup }

// PullRequest returns what the lookup of its pull request found, or nil
// while it was not asked or is pending.
func (a ActionRun) PullRequest() PullRequest {
	if done, ok := a.lookup.(LookupDone); ok {
		return done.PullRequest
	}
	return nil
}

// State returns where the action run stands.
func (a ActionRun) State() ActionRunState { return a.state }

// Outcome returns the action's outcome once it is known: while it finishes
// and once it ended. It is the zero Outcome before.
func (a ActionRun) Outcome() Outcome {
	switch s := a.state.(type) {
	case Finishing:
		return s.End.Outcome()
	case Finished:
		return s.End.Outcome()
	case AwaitingTake, CreatingWorkspace, ReopeningWorkspace, StartingSession, InSession, InChecks:
	}
	return Outcome{}
}

// Ended reports whether the action run ended.
func (a ActionRun) Ended() bool {
	_, ended := a.state.(Finished)
	return ended
}

// OpenedWorkspace is the workspace an action run's session works in, once
// it is ready.
type OpenedWorkspace struct {
	Workspace Workspace
	// Log is the repository-relative path of the session's log file; empty
	// when crew stopped before the session could start, as such a session
	// writes no log.
	Log string
	// Resumed says whether the workspace is the reopened workspace of the
	// failed run the action resumes.
	Resumed bool
	// Opened is when the workspace was ready.
	Opened time.Time
}

// Since returns when the action's new workspace was made, from which its
// pull request is looked up: Opened, or the zero time for a reopened one.
func (w OpenedWorkspace) Since() time.Time {
	if w.Resumed {
		return time.Time{}
	}
	return w.Opened
}

// ActionRunState is where an action run stands: AwaitingTake,
// CreatingWorkspace, ReopeningWorkspace, StartingSession, InSession,
// InChecks, Finishing or Finished.
//
//sumtype:decl
type ActionRunState interface {
	actionRunState()
}

// AwaitingTake is an action run whose rule run's take move has not landed.
type AwaitingTake struct{}

// CreatingWorkspace is an action run whose new workspace is being made.
type CreatingWorkspace struct{}

// ReopeningWorkspace is an action run whose failed run's workspace is being
// reopened.
type ReopeningWorkspace struct{}

// StartingSession is an action run whose session is being started.
type StartingSession struct{}

// InSession is an action run whose session runs.
type InSession struct{}

// InChecks is an action run whose session succeeded and one of whose checks
// runs. It is still running for you.
type InChecks struct {
	// Check is the check that runs.
	Check CheckName
	// StopSent is set once crew asked the check to stop.
	StopSent bool
}

// Finishing is an action run whose outcome is known and which waits for the
// lookup of its pull request. It is still running for you.
type Finishing struct {
	End ActionEnd
}

// Finished is an action run that ended.
type Finished struct {
	End ActionEnd
}

func (AwaitingTake) actionRunState()       {}
func (CreatingWorkspace) actionRunState()  {}
func (ReopeningWorkspace) actionRunState() {}
func (StartingSession) actionRunState()    {}
func (InSession) actionRunState()          {}
func (InChecks) actionRunState()           {}
func (Finishing) actionRunState()          {}
func (Finished) actionRunState()           {}

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

// endOf returns outcome as an end, failed by cause when it did not succeed.
func endOf(outcome Outcome, cause FailureCause) ActionEnd {
	if outcome.Succeeded {
		return EndSucceeded{Reason: outcome.Reason}
	}
	return EndFailed{Reason: outcome.Reason, Cause: cause}
}

// Lookup is how the lookup of an action run's pull request stands:
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
	Resume         Optional[ResumePoint]
	Workspace      Optional[OpenedWorkspace]
	SessionStarted Optional[time.Time]
	Usage          Usage
	Checks         []CheckResult
	Lookup         Lookup
	State          ActionRunState
}

// snapshot returns a as plain data, sharing no memory with it.
func (a ActionRun) snapshot() ActionRunSnapshot {
	return ActionRunSnapshot{
		Name: a.name, Resume: a.resume, Workspace: a.workspace, SessionStarted: a.session,
		Usage: cloneUsage(a.usage), Checks: slices.Clone(a.checks), Lookup: a.lookup, State: a.state,
	}
}

// restoreAction returns the action run s describes, sharing no memory with
// it.
func restoreAction(s ActionRunSnapshot) ActionRun {
	return ActionRun{
		name: s.Name, resume: s.Resume, workspace: s.Workspace, session: s.SessionStarted,
		usage: cloneUsage(s.Usage), checks: slices.Clone(s.Checks), lookup: s.Lookup, state: s.State,
	}
}

// cloneUsage returns a copy of u with its own Models.
func cloneUsage(u Usage) Usage {
	u.Models = slices.Clone(u.Models)
	return u
}
