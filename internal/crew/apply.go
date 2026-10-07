package crew

import (
	"fmt"
	"slices"
)

// Apply returns run with e applied. It refuses only an event of another
// run, with an error wrapping ErrRefused. Any other event applies as far as
// its own fields allow, so a run rebuilt from a journal with gaps never
// fails: applied to the zero run, a RunTaken event starts a run, and any
// other event starts one from its head; an action event for an action the
// run does not have adds it.
func Apply(run RuleRun, e RunEvent) (RuleRun, error) {
	h := e.Head()
	if run.id != "" && h.Run != run.id {
		return run, fmt.Errorf("%w: an event of run %q applied to run %q", ErrRefused, h.Run, run.id)
	}
	if run.id == "" {
		run = RuleRun{
			id: h.Run, issue: NewIssue(IssueData{ID: h.IssueID, Ref: h.IssueRef}), rule: h.Rule, taken: h.At,
			phase: TakingPhase{},
		}
	}
	return e.apply(run), nil
}

func (e RunTaken) apply(RuleRun) RuleRun {
	r := RuleRun{
		id: e.Run, continues: e.Continues, issue: NewIssue(e.Issue), rule: e.Rule, taken: e.At,
		phase: TakingPhase{},
	}
	for _, a := range e.Actions {
		r.actions = append(r.actions, newActionRun(a.Name, a.Resume))
	}
	return r
}

func (TakeMoved) apply(r RuleRun) RuleRun { return r.acting() }

func (RunStopped) apply(r RuleRun) RuleRun {
	r.stopping = true
	return r
}

func (e ActionWorkspaceAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.state = CreatingWorkspace{}
		if _, reopen := e.Reopen.Get(); reopen {
			a.state = ReopeningWorkspace{}
		}
		return a
	})
}

func (e WorkspaceMissing) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun { return a })
}

func (e ActionOpened) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.workspace = Some(OpenedWorkspace{Workspace: e.Workspace, Log: e.Log, Resumed: e.Resumed, Opened: e.At})
		return a
	})
}

func (e ActionSessionAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.state = StartingSession{}
		return a
	})
}

func (e ActionSessionStarted) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		if _, ok := a.workspace.Get(); !ok {
			a.workspace = Some(OpenedWorkspace{Workspace: e.Workspace, Log: e.Log, Resumed: e.Resumed})
		}
		a.state, a.session = InSession{}, Some(e.At)
		return a
	})
}

func (e ActionSessionStopAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun { return a })
}

func (e ActionSessionEnded) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.usage = cloneUsage(e.Usage)
		return a
	})
}

func (e ActionLookupAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.lookup = LookupPending{}
		return a
	})
}

func (e ActionCheckAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.state = InChecks{Check: e.Check}
		return a
	})
}

func (e ActionCheckStopAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		if checks, ok := a.state.(InChecks); ok {
			checks.StopSent = true
			a.state = checks
		}
		return a
	})
}

func (e ActionCheckEnded) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.checks = append(slices.Clip(a.checks), e.Result)
		return a
	})
}

func (e ActionLookupDone) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.lookup = LookupDone{PullRequest: e.PullRequest}
		return a
	})
}

func (e ActionFinishing) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.state = Finishing{End: e.End}
		return a
	})
}

// apply ends the action with what the event recorded, which, for an end
// Decide returned, is what the action run already holds.
func (e ActionEnded) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		if _, ok := e.Workspace.Get(); ok {
			a.workspace = e.Workspace
		}
		if _, ok := e.SessionStarted.Get(); ok {
			a.session = e.SessionStarted
		}
		if e.PullRequest != nil {
			a.lookup = LookupDone{PullRequest: e.PullRequest}
		}
		a.usage, a.state = cloneUsage(e.Usage), Finished{End: e.End}
		return a
	})
}

func (e RunEnded) apply(r RuleRun) RuleRun {
	r.phase = EndingPhase{Ending: e.Ending.clone(), Ended: e.At, ReportSettled: !e.Ending.Failed()}
	return r
}

func (e EndingMoved) apply(r RuleRun) RuleRun {
	return r.whileEnding(func(j EndingPhase) EndingPhase {
		j.Move = Some[EndingMove](EndingLanded{})
		return j
	})
}

func (e EndingDropped) apply(r RuleRun) RuleRun {
	return r.whileEnding(func(j EndingPhase) EndingPhase {
		j.Move = Some[EndingMove](EndingGivenUp{Reason: e.Reason})
		return j
	})
}

func (FailureReported) apply(r RuleRun) RuleRun { return r.reportSettled() }

func (FailureReportDropped) apply(r RuleRun) RuleRun { return r.reportSettled() }

// apply releases the run, keeping its ending once the ending's move
// settled.
func (RunReleased) apply(r RuleRun) RuleRun {
	var released ReleasedPhase
	if j, ok := r.phase.(EndingPhase); ok {
		if move, settled := j.Move.Get(); settled {
			released.Ending = Some(SettledEnding{Ending: j.Ending, Ended: j.Ended, Move: move})
		}
	}
	r.phase = released
	return r
}

// acting returns r running its actions, when it was taking.
func (r RuleRun) acting() RuleRun {
	if _, taking := r.phase.(TakingPhase); taking {
		r.phase = RunningPhase{}
	}
	return r
}

// withAction returns r with change applied to the run of the action named
// name, added when r has none, and r running its actions when it was
// taking. r's actions are left as they were.
func (r RuleRun) withAction(name ActionName, change func(ActionRun) ActionRun) RuleRun {
	r = r.acting()
	r.actions = slices.Clone(r.actions)
	i := r.actionIndex(name)
	if i < 0 {
		r.actions = append(r.actions, newActionRun(name, Optional[ResumePoint]{}))
		i = len(r.actions) - 1
	}
	r.actions[i] = change(r.actions[i])
	return r
}

// whileEnding returns r with change applied to its phase, when it is ending.
func (r RuleRun) whileEnding(change func(EndingPhase) EndingPhase) RuleRun {
	if j, ok := r.phase.(EndingPhase); ok {
		r.phase = change(j)
	}
	return r
}

// reportSettled returns r with its failure report settled, when it is
// ending.
func (r RuleRun) reportSettled() RuleRun {
	return r.whileEnding(func(j EndingPhase) EndingPhase {
		j.ReportSettled = true
		return j
	})
}
