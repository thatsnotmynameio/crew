package crew

// crashedReason is the reason of a run that recorded no end for the action
// that ended it: crew did not live to see it end.
const crashedReason = "crew stopped before the run ended: it crashed or was killed"

// History is the past of the rule runs, folded from their events in the
// order they happened, replayed from a journal or live: for each issue and
// rule its last run, rebuilt with Apply. Start reads a new run's start from
// it. It retires nothing by itself: the core has it Retire the runs whose
// worktree's name another run opens.
//
// The zero History holds no past. Fold changes it, so the one that holds it
// is the one that folds every event; its accessors return copies.
type History struct {
	runs map[ruleKey]pastRun
}

// ruleKey identifies the runs of a rule on an issue.
type ruleKey struct {
	issue IssueID
	rule  RuleName
}

// pastRun is the last run of a rule on an issue, and whether its worktree
// was retired: another run opened a worktree of its name, so it is gone.
type pastRun struct {
	run     RuleRun
	retired bool
}

// Fold adds e to the past. An event of another run than its issue and
// rule's last one starts that rule's last run again, from e alone when e
// is not its take.
func (h *History) Fold(e RunEvent) {
	if h.runs == nil {
		h.runs = map[ruleKey]pastRun{}
	}
	head := e.Head()
	k := ruleKey{issue: head.IssueID, rule: head.Rule}
	past := h.runs[k]
	if past.run.ID() != head.Run {
		past = pastRun{}
	}
	if next, err := Apply(past.run, e); err == nil {
		past.run = next
		h.runs[k] = past
	}
}

// LastRun returns the last run of rule on issue, as its events rebuilt it,
// and whether there was one. A new run of the rule on the issue continues
// it.
func (h *History) LastRun(issue IssueID, rule RuleName) (RuleRun, bool) {
	past, ok := h.runs[ruleKey{issue: issue, rule: rule}]
	return past.run, ok
}

// Start returns how a new run of rule on issue starts, from the last run
// of rule on issue: fresh without one, and as startAfter says otherwise. A
// retired run's worktree is gone. Whether the worktree to reopen still
// exists is the take's to find out (WorkspaceMissing).
func (h *History) Start(issue IssueID, rule Rule) Start {
	past, ok := h.runs[ruleKey{issue: issue, rule: rule.Name}]
	if !ok {
		return StartFresh{}
	}
	start := startAfter(past.run, rule)
	if past.retired {
		return withoutWorktree(start)
	}
	return start
}

// Retire marks as gone the worktree of every last run, of any rule on any
// issue other than rule on issue, whose next start would reopen the
// worktree named w: a run of rule on issue opens w, and names repeat only
// once a worktree is gone. The retired run is still the one a new run of
// its rule continues.
func (h *History) Retire(w WorkspaceName, issue IssueID, rule RuleName) {
	own := ruleKey{issue: issue, rule: rule}
	for k, past := range h.runs {
		if held, _, ok := worktreeOf(past.run); ok && k != own && held.Name == w {
			past.retired = true
			h.runs[k] = past
		}
	}
}

// startAfter returns how a run of rule starts after last, the last run of
// rule on its issue:
//   - after a run that chose PassedRoute, fresh once the route finished, or
//     with the passed route alone, in last's worktree, when it did not;
//   - after a run that started no action and opened no worktree of its
//     own, last's own start, passed on;
//   - otherwise at the restart point, in last's worktree;
//   - and fresh, naming the action, when rule no longer has the action
//     to start at.
func startAfter(last RuleRun, rule Rule) Start {
	route, chosen := chosenRoute(last)
	if chosen && route == PassedRoute {
		return passedAfter(last)
	}
	if passesOn(last) {
		return revalidate(last.Start(), rule)
	}
	w, log, ok := worktreeOf(last)
	restart, found := restartPoint(last, rule)
	switch {
	case !found:
		return StartWithoutAction{Action: restart}
	case !ok:
		return StartFresh{}
	}
	reason := NewSessionText(crashedReason)
	if f, ended := cursorEnd(last); ended && chosen {
		reason = f.End.Outcome().Reason
	}
	return StartAt{Workspace: w, Log: log, Action: restart, Route: route, Reason: reason, Session: last.session}
}

// passedAfter returns how a run starts after last, which chose
// PassedRoute: fresh once the route finished, as its final move or close
// landed or was dropped since the item moved meanwhile, and with the
// passed route alone, in last's worktree, when it was given up or never
// settled.
func passedAfter(last RuleRun) Start {
	if p, ok := last.route(); ok {
		switch final, _ := p.Final(); final.(type) {
		case StepLanded, StepDropped:
			return StartFresh{}
		case StepGivenUp, StepRan, StepFailed, StepSkipped, StepStopped, nil:
		}
	}
	s := StartPassedRoute{Session: last.session}
	if w, log, ok := worktreeOf(last); ok {
		s.Workspace, s.Log = Some(w), log
	}
	return s
}

// chosenRoute returns the route last chose, and whether it chose one: the
// route of its phase, or, when crew did not live to record it, the route
// the action at its cursor led to, and PassedRoute when its last action
// went on to the next.
func chosenRoute(last RuleRun) (RouteName, bool) {
	if p, ok := last.route(); ok {
		return p.Route, true
	}
	f, ended := cursorEnd(last)
	if !ended {
		return "", false
	}
	switch t := f.Target.(type) {
	case ToRoute:
		return t.Route, true
	case Next:
		if last.cursor == len(last.actions)-1 {
			return PassedRoute, true
		}
	}
	return "", false
}

// cursorEnd returns the end of the action at last's cursor, once it
// ended.
func cursorEnd(last RuleRun) (Finished, bool) {
	a, _ := last.Cursor()
	f, ok := a.state.(Finished)
	return f, ok
}

// passesOn reports whether last started no action and opened no worktree
// of its own, so a new run inherits its start: it stopped at its take, its
// take was given up or landed after crew's run time was up, or crew
// crashed before it started an action.
func passesOn(last RuleRun) bool {
	for _, a := range last.actions {
		if a.started() {
			return false
		}
	}
	opened, ok := last.Workspace().Get()
	if !ok {
		return true
	}
	w, _, reopen := reopens(last.Start())
	return reopen && w.Name == opened.Workspace.Name
}

// started reports whether the action run started: its session or script
// was asked for. An action that ended without starting failed before it
// could: a stop or time-up kept it from starting, its workspace could not
// be made, or its prompt did not render.
func (a ActionRun) started() bool {
	switch s := a.state.(type) {
	case AwaitingTurn, DoneInEarlierRun, NotRun:
		return false
	case Finished:
		if failed, ok := s.End.(EndFailed); ok {
			switch failed.Cause {
			case CauseStoppedBeforeStart, CauseTimeUp, CauseWorkspace, CausePrompt:
				return false
			default:
			}
		}
	case StartingSession, InSession, InShell:
	}
	return true
}

// revalidate returns s, a start passed on from an earlier run, for rule:
// a resume at an action rule no longer has starts fresh, naming it, and a
// start that already named one starts fresh.
func revalidate(s Start, rule Rule) Start {
	switch s := s.(type) {
	case StartAt:
		if !rule.has(s.Action) {
			return StartWithoutAction{Action: s.Action}
		}
	case StartWithoutAction:
		return StartFresh{}
	case StartFresh, StartPassedRoute:
	}
	return s
}

// worktreeOf returns the worktree a run after last reopens, and the log it
// names: last's own once it was ready, or the one its start reopens.
func worktreeOf(last RuleRun) (Workspace, string, bool) {
	if w, ok := last.Workspace().Get(); ok {
		return w.Workspace, w.Log, true
	}
	return reopens(last.Start())
}

// restartPoint returns the action a run after last, which chose a route
// other than PassedRoute or never chose one, restarts at, and whether rule
// still has it; the action rule lost otherwise. It is the action at last's
// cursor, or the action after it when that action went on to the next and
// crew did not live to start it. A shell action that ran and gave a
// verdict of its own after a session restarts at the latest session before
// it, unless its definition resumes at itself.
func restartPoint(last RuleRun, rule Rule) (ActionName, bool) {
	a, _ := last.Cursor()
	if f, ok := a.state.(Finished); ok && last.cursor+1 < len(last.actions) {
		if _, next := f.Target.(Next); next {
			name := last.actions[last.cursor+1].name
			return name, rule.has(name)
		}
	}
	if !rule.has(a.name) {
		return a.name, false
	}
	spec, shell := rule.Action(a.name).Kind.(ShellSpec)
	if !shell || spec.ResumeSelf || !a.judgedItself() {
		return a.name, true
	}
	return rule.sessionBefore(a.name), true
}

// judgedItself reports whether the shell action run ran its script to its
// end and ended with the verdict its exit status gave: not one that never
// ran, could not finish, or that crew stopped.
func (a ActionRun) judgedItself() bool {
	f, ok := a.state.(Finished)
	if !ok {
		return false
	}
	shell, ran := a.shell.Get()
	if _, exited := shell.Status.Get(); !ran || !exited {
		return false
	}
	failed, isFailed := f.End.(EndFailed)
	return !isFailed || failed.Cause != CauseStopped
}

// has reports whether r has an action named name.
func (r Rule) has(name ActionName) bool {
	return r.Action(name).Name == name && name != ""
}

// sessionBefore returns the latest session action before the action named
// name in r's order, or name itself when no session comes before it.
func (r Rule) sessionBefore(name ActionName) ActionName {
	before := name
	for _, a := range r.Actions {
		if a.Name == name {
			break
		}
		if _, ok := a.Kind.(SessionSpec); ok {
			before = a.Name
		}
	}
	return before
}
