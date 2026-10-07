package crew

import "time"

// Fact is what happened to a rule run, which Decide turns into the run's
// events: a delivery of the run settled, a stop reached it, its workspace
// or the lookup of its pull requests answered, or the action at its cursor
// started or ended.
//
//sumtype:decl
type Fact interface {
	factHead() FactHead
	// decide adds to d the events the fact produces, or returns why the
	// run refuses it.
	decide(d *decider) error
}

// FactHead is what every fact carries: the run it happened to, and when.
type FactHead struct {
	Run RuleRunID
	At  time.Time
}

func (h FactHead) factHead() FactHead { return h }

// TakeSettled is the run's take move settling: it landed, or crew gave it
// up.
type TakeSettled struct {
	FactHead

	Landed bool
}

// EndingMoveSettled is the run's ending move settling.
type EndingMoveSettled struct {
	FactHead

	Move EndingMove
}

// FailureReportSettled is the run's failure report settling: it landed, or
// crew gave it up.
type FailureReportSettled struct {
	FactHead

	Landed bool
}

// StopReached is a stop reaching the run: requested, or the stop that ends
// a wind-down.
type StopReached struct {
	FactHead
}

// WorkspaceReady is the run's workspace that is ready, new or reopened.
type WorkspaceReady struct {
	FactHead

	Workspace Workspace
	// Log is the repository-relative path of the run's log file.
	Log string
	// Resumed says whether the workspace is the reopened workspace of the
	// run this one resumes.
	Resumed bool
}

// WorkspaceGone is the workspace the run asked to reopen, which no longer
// exists.
type WorkspaceGone struct {
	FactHead
}

// WorkspaceFailed is the run's workspace that could not be made or
// reopened.
type WorkspaceFailed struct {
	FactHead

	Reason SessionText
}

// PullRequestLookedUp is the lookup of the run's pull requests that ended,
// with what it found.
type PullRequestLookedUp struct {
	FactHead

	PullRequest PullRequest
}

// decide moves the issue on once the take landed: a rule without actions
// chooses PassedRoute at once, after a stop too; after a stop, the first
// action ends without starting; otherwise the run asks for its workspace,
// the resumed run's when it inherited a resume point. A take given up
// releases the run.
func (f TakeSettled) decide(d *decider) error {
	if _, taking := d.run.phase.(TakingPhase); !taking {
		return d.refused("its take")
	}
	if !f.Landed {
		d.emit(RunReleased{EventHead: d.head()})
		return nil
	}
	labels := d.def.Rule.Labels
	d.emit(TakeMoved{EventHead: d.head(), From: labels.Ready, To: labels.Running})
	switch {
	case len(d.run.actions) == 0:
		d.choose(PassedRoute, "")
	case d.run.stopping:
		d.stopAtCursor()
	default:
		asked := WorkspaceAsked{EventHead: d.head()}
		if resume, ok := d.run.resume.Get(); ok {
			asked.Reopen = Some(resume.Workspace)
		}
		d.emit(asked)
	}
	return nil
}

// decide releases the run once its ending move settled and its failure
// report, when it posts one, settled too.
func (f EndingMoveSettled) decide(d *decider) error {
	j, ok := d.run.phase.(EndingPhase)
	if _, settled := j.Move.Get(); !ok || settled || f.Move == nil {
		return d.refused("an ending move")
	}
	switch m := f.Move.(type) {
	case EndingLanded:
		d.emit(EndingMoved{EventHead: d.head(), From: d.def.Rule.Labels.Running, To: j.Ending.To})
	case EndingGivenUp:
		d.emit(EndingDropped{EventHead: d.head(), To: j.Ending.To, Reason: m.Reason})
	}
	d.releaseOnceSettled()
	return nil
}

// decide settles the failure report, and releases the run once its
// ending move settled too.
func (f FailureReportSettled) decide(d *decider) error {
	if j, ok := d.run.phase.(EndingPhase); !ok || j.ReportSettled {
		return d.refused("a failure report")
	}
	if f.Landed {
		d.emit(FailureReported{EventHead: d.head()})
	} else {
		d.emit(FailureReportDropped{EventHead: d.head()})
	}
	d.releaseOnceSettled()
	return nil
}

// releaseOnceSettled releases the ending run once its ending move and its
// failure report settled.
func (d *decider) releaseOnceSettled() {
	j, _ := d.run.phase.(EndingPhase)
	if _, moved := j.Move.Get(); moved && j.ReportSettled {
		d.emit(RunReleased{EventHead: d.head()})
	}
}

// decide marks a run taking or running its actions as stopping, once, and
// asks the session or script at its cursor to stop. A run whose sequence is
// over goes on.
func (StopReached) decide(d *decider) error {
	if d.run.ActionsEnded() || d.run.stopping {
		return nil
	}
	d.emit(RunStopped{EventHead: d.head()})
	a, _ := d.run.Cursor()
	switch a.state.(type) {
	case InSession:
		d.emit(ActionSessionStopAsked{EventHead: d.head(), Action: a.name})
	case InShell:
		d.emit(ActionShellStopAsked{EventHead: d.head(), Action: a.name})
	case AwaitingTurn, DoneInEarlierRun, StartingSession, Finished, NotRun:
		// No session or script runs: the run's next fact sees the stop.
	}
	return nil
}

// awaitsWorkspace returns the refusal of a workspace fact when the run does
// not wait for its workspace, or, with reopened, for its reopened one.
func (d *decider) awaitsWorkspace(reopened bool) error {
	switch d.run.workspace.(type) {
	case ReopeningWorkspace:
		return nil
	case CreatingWorkspace:
		if !reopened {
			return nil
		}
	case NoWorkspace, InWorkspace:
	}
	return d.refused("its workspace")
}

// decide records the run's workspace and starts the action at its cursor,
// or, after a stop, ends that action without starting it: no action writes
// the log, so the workspace names none.
func (f WorkspaceReady) decide(d *decider) error {
	if err := d.awaitsWorkspace(false); err != nil {
		return err
	}
	opened := WorkspaceOpened{EventHead: d.head(), Workspace: f.Workspace}
	if d.run.stopping {
		d.emit(opened)
		d.stopAtCursor()
		return nil
	}
	_, resumes := d.run.resume.Get()
	opened.Log, opened.Resumed = f.Log, f.Resumed && resumes
	d.emit(opened)
	a, _ := d.run.Cursor()
	d.start(a.name)
	return nil
}

// decide asks for a new workspace in place of the reopened one that is
// gone, for a run that starts again at its first action, or, after a stop,
// ends that action without a workspace.
func (f WorkspaceGone) decide(d *decider) error {
	if err := d.awaitsWorkspace(true); err != nil {
		return err
	}
	reopening, _ := d.run.workspace.(ReopeningWorkspace)
	d.emit(WorkspaceMissing{EventHead: d.head(), Workspace: reopening.Workspace})
	if d.run.stopping {
		d.stopAtCursor()
		return nil
	}
	d.emit(WorkspaceAsked{EventHead: d.head()})
	return nil
}

// decide fails the action at the run's cursor by the workspace, after a
// stop too.
func (f WorkspaceFailed) decide(d *decider) error {
	if err := d.awaitsWorkspace(false); err != nil {
		return err
	}
	a, _ := d.run.Cursor()
	d.finish(a.name, failedBy(f.Reason, CauseWorkspace))
	return nil
}

// decide keeps the pull requests the lookup found.
func (f PullRequestLookedUp) decide(d *decider) error {
	if _, pending := d.run.lookup.(LookupPending); !pending {
		return d.refused("the lookup of its pull requests")
	}
	d.emit(RunLookupDone{EventHead: d.head(), PullRequest: f.PullRequest})
	return nil
}
