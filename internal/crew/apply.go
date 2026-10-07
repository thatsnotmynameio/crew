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
			phase: TakingPhase{}, workspace: NoWorkspace{}, lookup: LookupNotAsked{},
		}
	}
	return e.apply(run), nil
}

// apply starts the run with its cursor on its first action, or, when it
// resumes, on the resume point's action, the actions before which were
// done in an earlier run.
func (e RunTaken) apply(RuleRun) RuleRun {
	r := RuleRun{
		id: e.Run, continues: e.Continues, issue: NewIssue(e.Issue), rule: e.Rule, taken: e.At,
		phase: TakingPhase{}, workspace: NoWorkspace{}, resume: e.Resume, lookup: LookupNotAsked{},
	}
	for _, name := range e.Actions {
		r.actions = append(r.actions, newActionRun(name))
	}
	if resume, ok := e.Resume.Get(); ok {
		r.cursor = max(r.actionIndex(resume.Action), 0)
	}
	for i := range r.cursor {
		r.actions[i].state = DoneInEarlierRun{}
	}
	return r
}

func (TakeMoved) apply(r RuleRun) RuleRun { return r.acting() }

func (RunStopped) apply(r RuleRun) RuleRun {
	r.stopping = true
	return r
}

func (e WorkspaceAsked) apply(r RuleRun) RuleRun {
	r = r.acting()
	r.workspace = CreatingWorkspace{}
	if reopen, ok := e.Reopen.Get(); ok {
		r.workspace = ReopeningWorkspace{Workspace: reopen}
	}
	return r
}

// apply moves the cursor back to the run's first action, which no earlier
// run did in the new workspace.
func (WorkspaceMissing) apply(r RuleRun) RuleRun {
	r = r.acting()
	r.workspace, r.cursor = NoWorkspace{}, 0
	r.actions = slices.Clone(r.actions)
	for i, a := range r.actions {
		if is[DoneInEarlierRun](a.state) {
			r.actions[i].state = AwaitingTurn{}
		}
	}
	return r
}

func (e WorkspaceOpened) apply(r RuleRun) RuleRun {
	r = r.acting()
	opened := OpenedWorkspace{Workspace: e.Workspace, Log: e.Log, Resumed: e.Resumed, Opened: e.At}
	r.workspace = InWorkspace{Opened: opened}
	return r
}

func (e ActionSessionAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.state = StartingSession{}
		return a
	})
}

func (e ActionSessionStarted) apply(r RuleRun) RuleRun {
	r.bot = e.Bot
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
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

func (e ActionShellAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.state = InShell{Started: e.At}
		return a
	})
}

func (e ActionShellStopAsked) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun { return a })
}

func (e ActionShellEnded) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		a.shell = Some(e.Outcome)
		return a
	})
}

// apply ends the action with what the event recorded, which, for an end
// Decide returned, is what the action run already holds.
func (e ActionEnded) apply(r RuleRun) RuleRun {
	return r.withAction(e.Action, func(a ActionRun) ActionRun {
		if _, ok := e.SessionStarted.Get(); ok {
			a.session = e.SessionStarted
		}
		a.usage = cloneUsage(e.Usage)
		a.state = Finished{End: e.End, Verdict: e.Verdict, Target: e.Target}
		return a
	})
}

// apply ends the run's sequence: its cursor stays on the event's action,
// and every action it did not reach did not run.
func (e RouteChosen) apply(r RuleRun) RuleRun {
	if e.Action != "" {
		r = r.withAction(e.Action, func(a ActionRun) ActionRun { return a })
	}
	r.actions = slices.Clone(r.actions)
	for i, a := range r.actions {
		if is[AwaitingTurn](a.state) {
			r.actions[i].state = NotRun{}
		}
	}
	r.phase = RoutingPhase{Route: e.Route, Chosen: e.At}
	return r
}

func (RunLookupAsked) apply(r RuleRun) RuleRun {
	r.lookup = LookupPending{}
	return r
}

func (e RunLookupDone) apply(r RuleRun) RuleRun {
	r.lookup = LookupDone{PullRequest: e.PullRequest}
	return r
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
// name, added when r has none, its cursor on that action, and r running its
// actions when it was taking. r's actions are left as they were.
func (r RuleRun) withAction(name ActionName, change func(ActionRun) ActionRun) RuleRun {
	r = r.acting()
	r.actions = slices.Clone(r.actions)
	i := r.actionIndex(name)
	if i < 0 {
		r.actions = append(r.actions, newActionRun(name))
		i = len(r.actions) - 1
	}
	r.actions[i], r.cursor = change(r.actions[i]), i
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
