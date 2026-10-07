package crew

import (
	"fmt"
	"time"
)

// Fact is what happened to a rule run, which Decide turns into the run's
// events: a delivery of the run settled, a stop reached it, or one of its
// actions' workspace, session, check or pull request lookup answered.
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

// VerdictSettled is the run's verdict move settling.
type VerdictSettled struct {
	FactHead

	Move VerdictMove
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

// WorkspaceReady is an action's workspace that is ready, new or reopened.
type WorkspaceReady struct {
	FactHead

	Action    ActionName
	Workspace Workspace
	// Log is the repository-relative path of the session's log file.
	Log string
	// Resumed says whether the workspace is the reopened workspace of a failed
	// run.
	Resumed bool
}

// WorkspaceGone is the workspace an action asked to reopen, which no longer
// exists.
type WorkspaceGone struct {
	FactHead

	Action ActionName
}

// WorkspaceFailed is an action's workspace that could not be made or
// reopened.
type WorkspaceFailed struct {
	FactHead

	Action ActionName
	Reason SessionText
}

// SessionStarted is an action's session that started.
type SessionStarted struct {
	FactHead

	Action ActionName
}

// SessionFailedToStart is an action's session that could not start.
type SessionFailedToStart struct {
	FactHead

	Action ActionName
	Reason SessionText
}

// SessionEnded is an action's session that ended, with its harness's
// verdict and what it reported the session used.
type SessionEnded struct {
	FactHead

	Action  ActionName
	Outcome Outcome
	Usage   Usage
}

// CheckEnded is an action's running check that ended.
type CheckEnded struct {
	FactHead

	Action ActionName
	Passed bool
	Reason CheckReason
}

// PullRequestLookedUp is the lookup of an action's pull request that
// ended, with what it found.
type PullRequestLookedUp struct {
	FactHead

	Action      ActionName
	PullRequest PullRequest
}

// decide moves the issue on once the take landed: a rule without actions
// is judged at once, after a stop too; after a stop, every action ends
// stopped; otherwise every action starts. A take given up releases the run.
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
		d.judge()
	case d.run.stopping:
		for _, a := range d.run.actions {
			d.end(a.name, stoppedEnd())
		}
	default:
		d.start()
	}
	return nil
}

// start starts every action, in a new workspace or in its failed run's
// reopened one when it inherited a resume point. An action whose prompt
// does not render for the issue ends at once.
func (d *decider) start() {
	for _, a := range d.run.actions {
		if _, err := d.definition(a.name).Prompt.Render(d.run.issue); err != nil {
			d.end(a.name, EndFailed{Reason: NewSessionText(err.Error()), Cause: CausePrompt})
			continue
		}
		asked := ActionWorkspaceAsked{EventHead: d.head(), Action: a.name}
		if resume, ok := a.resume.Get(); ok {
			asked.Reopen = Some(resume.Workspace)
		}
		d.emit(asked)
	}
}

// decide releases the run once its verdict move settled and its failure
// report, when it posts one, settled too.
func (f VerdictSettled) decide(d *decider) error {
	j, ok := d.run.phase.(JudgingPhase)
	if _, settled := j.Move.Get(); !ok || settled || f.Move == nil {
		return d.refused("a verdict")
	}
	switch m := f.Move.(type) {
	case VerdictLanded:
		d.emit(VerdictMoved{EventHead: d.head(), From: d.def.Rule.Labels.Running, To: j.Verdict.To})
	case VerdictGivenUp:
		d.emit(VerdictDropped{EventHead: d.head(), To: j.Verdict.To, Reason: m.Reason})
	}
	d.releaseOnceSettled()
	return nil
}

// decide settles the failure report, and releases the run once its
// verdict move settled too.
func (f FailureReportSettled) decide(d *decider) error {
	if j, ok := d.run.phase.(JudgingPhase); !ok || j.ReportSettled {
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

// releaseOnceSettled releases the judging run once its verdict move and its
// failure report settled.
func (d *decider) releaseOnceSettled() {
	j, _ := d.run.phase.(JudgingPhase)
	if _, moved := j.Move.Get(); moved && j.ReportSettled {
		d.emit(RunReleased{EventHead: d.head()})
	}
}

// decide marks a run taking or running its actions as stopping, once, and
// asks its running sessions and checks to stop. A judging run's verdict
// goes on.
func (StopReached) decide(d *decider) error {
	if _, judging := d.run.phase.(JudgingPhase); judging || d.run.stopping {
		return nil
	}
	d.emit(RunStopped{EventHead: d.head()})
	for _, a := range d.run.actions {
		switch a.state.(type) {
		case InSession:
			d.emit(ActionSessionStopAsked{EventHead: d.head(), Action: a.name})
		case InChecks:
			d.emit(ActionCheckStopAsked{EventHead: d.head(), Action: a.name})
		case AwaitingTake, CreatingWorkspace, ReopeningWorkspace, StartingSession, Finishing, Finished:
			// No session or check runs: its next fact sees the stop.
		}
	}
	return nil
}

// decide records the action run's start in its workspace and asks for its
// session, or, after a stop, ends the action without starting it: a
// session that never starts writes no log, so the action names none.
func (f WorkspaceReady) decide(d *decider) error {
	a, err := d.action(f.Action, awaitsWorkspace)
	if err != nil {
		return err
	}
	opened := ActionOpened{EventHead: d.head(), Action: f.Action, Workspace: f.Workspace}
	if d.run.stopping {
		d.emit(opened)
		d.end(f.Action, stoppedEnd())
		return nil
	}
	_, resumes := a.resume.Get()
	opened.Log, opened.Resumed = f.Log, f.Resumed && resumes
	d.emit(opened)
	d.emit(ActionSessionAsked{EventHead: d.head(), Action: f.Action})
	return nil
}

// decide asks for a new workspace in place of the reopened one that is
// gone, or, after a stop, ends the action without a workspace.
func (f WorkspaceGone) decide(d *decider) error {
	a, err := d.action(f.Action, is[ReopeningWorkspace])
	if err != nil {
		return err
	}
	resume, _ := a.resume.Get()
	d.emit(WorkspaceMissing{EventHead: d.head(), Action: f.Action, Workspace: resume.Workspace})
	if d.run.stopping {
		d.end(f.Action, stoppedEnd())
		return nil
	}
	d.emit(ActionWorkspaceAsked{EventHead: d.head(), Action: f.Action})
	return nil
}

// decide fails the action by its workspace, after a stop too.
func (f WorkspaceFailed) decide(d *decider) error {
	if _, err := d.action(f.Action, awaitsWorkspace); err != nil {
		return err
	}
	d.end(f.Action, EndFailed{Reason: f.Reason, Cause: CauseWorkspace})
	return nil
}

// decide records the session's start, and asks it to stop at once when a
// stop reached the run while it was starting.
func (f SessionStarted) decide(d *decider) error {
	a, err := d.action(f.Action, is[StartingSession])
	if err != nil {
		return err
	}
	w, _ := a.workspace.Get()
	d.emit(ActionSessionStarted{
		EventHead: d.head(), Action: f.Action, Workspace: w.Workspace, Log: w.Log, Resumed: w.Resumed,
	})
	if d.run.stopping {
		d.emit(ActionSessionStopAsked{EventHead: d.head(), Action: f.Action})
	}
	return nil
}

// decide fails the action by its session's start.
func (f SessionFailedToStart) decide(d *decider) error {
	if _, err := d.action(f.Action, is[StartingSession]); err != nil {
		return err
	}
	d.end(f.Action, EndFailed{Reason: f.Reason, Cause: CauseStart})
	return nil
}

// decide keeps what the session used, asks for the lookup of its pull
// request whatever its outcome, and ends the action, or runs its first
// check when the session succeeded and the action has checks. After a stop
// no check starts, and a failed session counts as stopped.
func (f SessionEnded) decide(d *decider) error {
	if _, err := d.action(f.Action, func(s ActionRunState) bool {
		return is[StartingSession](s) || is[InSession](s)
	}); err != nil {
		return err
	}
	d.emit(ActionSessionEnded{EventHead: d.head(), Action: f.Action, Outcome: f.Outcome, Usage: cloneUsage(f.Usage)})
	if d.def.FindsPullRequests {
		d.emit(ActionLookupAsked{EventHead: d.head(), Action: f.Action})
	}
	cause := CauseSession
	if d.run.stopping {
		cause = CauseStopped
	}
	checks := d.definition(f.Action).Checks
	switch {
	case !f.Outcome.Succeeded || len(checks) == 0:
		d.end(f.Action, endOf(f.Outcome, cause))
	case d.run.stopping:
		d.end(f.Action, stoppedEnd())
	default:
		d.emit(ActionCheckAsked{EventHead: d.head(), Action: f.Action, Check: checks[0].Name})
	}
	return nil
}

// decide keeps how the check ended, and runs the next check after a
// passing one; the last passing check succeeds the action and a failing
// one fails it, by the check's reason either way. A check that ends after
// its stop was sent ends the action stopped.
func (f CheckEnded) decide(d *decider) error {
	a, err := d.action(f.Action, is[InChecks])
	if err != nil {
		return err
	}
	running, _ := a.state.(InChecks)
	d.emit(ActionCheckEnded{EventHead: d.head(), Action: f.Action, Result: CheckResult{
		Name: running.Check, Passed: f.Passed, Reason: f.Reason,
	}})
	checks, ran := d.definition(f.Action).Checks, len(a.checks)+1
	switch {
	case running.StopSent:
		d.end(f.Action, stoppedEnd())
	case !f.Passed || ran >= len(checks):
		d.end(f.Action, endOf(Outcome{Succeeded: f.Passed, Reason: NewSessionText(f.Reason.String())}, CauseCheck))
	default:
		d.emit(ActionCheckAsked{EventHead: d.head(), Action: f.Action, Check: checks[ran].Name})
	}
	return nil
}

// decide keeps the pull request the lookup found, and ends the action when
// its outcome waited for it.
func (f PullRequestLookedUp) decide(d *decider) error {
	a, err := d.action(f.Action, func(s ActionRunState) bool { return is[InChecks](s) || is[Finishing](s) })
	if err != nil {
		return err
	}
	if _, pending := a.lookup.(LookupPending); !pending {
		return d.refused(fmt.Sprintf("the pull request of action %q", f.Action))
	}
	d.emit(ActionLookupDone{EventHead: d.head(), Action: f.Action, PullRequest: f.PullRequest})
	if finishing, ok := a.state.(Finishing); ok {
		d.end(f.Action, finishing.End)
	}
	return nil
}
