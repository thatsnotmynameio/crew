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

// apply starts the run as its start says: with its cursor on its first
// action; on the restart point's action, the actions before which were
// done in an earlier run, an action the rule lost counting as the first;
// or, for the passed route alone, with every action done in an earlier run
// and its cursor on the last. A resumed run inherits the continued run's
// latest session.
func (e RunTaken) apply(RuleRun) RuleRun {
	start := e.Start
	if start == nil {
		start = StartFresh{}
	}
	r := RuleRun{
		id: e.Run, continues: e.Continues, issue: NewIssue(e.Issue), rule: e.Rule, taken: e.At,
		phase: TakingPhase{}, workspace: NoWorkspace{}, start: start, session: inherited(start),
		lookup: LookupNotAsked{},
	}
	for _, name := range e.Actions {
		r.actions = append(r.actions, newActionRun(name))
	}
	done := 0
	switch s := start.(type) {
	case StartAt:
		r.cursor = max(r.actionIndex(s.Action), 0)
		done = r.cursor
	case StartPassedRoute:
		r.cursor = max(len(r.actions)-1, 0)
		done = len(r.actions)
	case StartFresh, StartWithoutAction:
	}
	for i := range done {
		r.actions[i].state = DoneInEarlierRun{}
	}
	return r
}

func (TakeMoved) apply(r RuleRun) RuleRun { return r.acting() }

func (RunStopped) apply(r RuleRun) RuleRun {
	r.stopping = true
	return r
}

func (RunOutOfTime) apply(r RuleRun) RuleRun {
	r.timeUp = true
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

// apply drops the worktree from the run's start. A run that resumed at an
// action starts fresh: its cursor moves back to its first action, which no
// earlier run did in the new workspace, and it no longer inherits the
// continued run's latest session. The passed route alone keeps its
// actions as they were.
func (WorkspaceMissing) apply(r RuleRun) RuleRun {
	r = r.acting()
	r.workspace, r.start = NoWorkspace{}, WithoutWorktree(r.Start())
	if _, passedAlone := r.start.(StartPassedRoute); passedAlone {
		return r
	}
	r.cursor, r.session = 0, Optional[LatestSession]{}
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
	r.session = Some(LatestSession{Action: e.Action, Bot: e.Bot})
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
	r.phase = RoutingPhase{Route: e.Route, Chosen: e.At, Steps: slices.Clone(e.Steps)}
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

func (e StepAsked) apply(r RuleRun) RuleRun {
	return r.whileRouting(e.Step, func(p RoutingPhase) RoutingPhase {
		p.Asked = true
		return p
	})
}

func (StepShellStopAsked) apply(r RuleRun) RuleRun { return r }

// apply settles the step, which is no longer in flight.
func (e StepEnded) apply(r RuleRun) RuleRun {
	return r.whileRouting(e.Step, func(p RoutingPhase) RoutingPhase {
		p.Settled = append(slices.Clone(p.Settled), e.Outcome)
		p.Asked = false
		return p
	})
}

// apply releases the run, keeping its route and how its steps settled
// once it chose one.
func (RunReleased) apply(r RuleRun) RuleRun {
	var released ReleasedPhase
	if p, ok := r.phase.(RoutingPhase); ok {
		released.Route = Some(p)
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

// whileRouting returns r with change applied to its phase, when it is
// routing and its next step to settle is the one at index step, a step of
// its route.
func (r RuleRun) whileRouting(step int, change func(RoutingPhase) RoutingPhase) RuleRun {
	if p, ok := r.phase.(RoutingPhase); ok && len(p.Settled) == step && step < len(p.Steps) {
		r.phase = change(p)
	}
	return r
}
