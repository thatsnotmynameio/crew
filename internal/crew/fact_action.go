package crew

// SessionStarted is an action's session that started.
type SessionStarted struct {
	FactHead

	Action ActionName
	// Login is the login the session acts as; empty when unknown.
	Login string
}

// SessionFailedToStart is an action's session that could not start.
type SessionFailedToStart struct {
	FactHead

	Action ActionName
	Reason SessionText
}

// SessionEnded is an action's session that ended, with how its harness
// says it ended, the verdict it reported and what it reported the session
// used.
type SessionEnded struct {
	FactHead

	Action  ActionName
	Outcome Outcome
	// Report is the verdict the session reported; nil counts as
	// NoVerdictReported.
	Report VerdictReport
	Usage  Usage
}

// ShellEnded is a shell action's script that ended.
type ShellEnded struct {
	FactHead

	Action  ActionName
	Outcome ShellOutcome
}

// FunctionEnded is a function action's function that ended.
type FunctionEnded struct {
	FactHead

	Action  ActionName
	Outcome FunctionOutcome
}

// ReturnChecked is the answered rule's check that ended: what CheckReturn
// found in the item's comments, never the comments themselves (KTD2).
type ReturnChecked struct {
	FactHead

	Action ActionName
	Check  ReturnCheck
}

// decide records the session's start, as its action's bot and the login
// the fact names, and whether it may ask a question, and asks it to stop
// at once when a stop reached the run while it was starting.
func (f SessionStarted) decide(d *decider) error {
	if err := d.awaits(f.Action, is[StartingSession]); err != nil {
		return err
	}
	action := d.def.Rule.Action(f.Action)
	spec, _ := action.Kind.(SessionSpec)
	d.emit(ActionSessionStarted{
		EventHead: d.head(), Action: f.Action, Bot: spec.Bot, Login: f.Login, Asks: action.MayWait(),
	})
	if d.run.stopping {
		d.emit(ActionSessionStopAsked{EventHead: d.head(), Action: f.Action})
	}
	return nil
}

// decide fails the action by its session's start.
func (f SessionFailedToStart) decide(d *decider) error {
	if err := d.awaits(f.Action, is[StartingSession]); err != nil {
		return err
	}
	d.finish(f.Action, failedBy(f.Reason, CauseStart))
	return nil
}

// decide keeps what the session used and ends the action with the verdict
// judgeSession gives.
func (f SessionEnded) decide(d *decider) error {
	if err := d.awaits(f.Action, func(s ActionRunState) bool {
		return is[StartingSession](s) || is[InSession](s)
	}); err != nil {
		return err
	}
	d.emit(ActionSessionEnded{EventHead: d.head(), Action: f.Action, Outcome: f.Outcome, Usage: cloneUsage(f.Usage)})
	d.finish(f.Action, judgeSession(d.def.Rule.Action(f.Action).On, f.Outcome, f.Report, d.run.stopping))
	return nil
}

// decide keeps how the script ended and ends the action with the verdict
// judgeShell gives.
func (f ShellEnded) decide(d *decider) error {
	if err := d.awaits(f.Action, is[InShell]); err != nil {
		return err
	}
	d.emit(ActionShellEnded{EventHead: d.head(), Action: f.Action, Outcome: f.Outcome})
	action := d.def.Rule.Action(f.Action)
	spec, _ := action.Kind.(ShellSpec)
	d.finish(f.Action, judgeShell(spec, action.On, f.Outcome, d.run.stopping))
	return nil
}

// decide keeps how the function ended and ends the action with the
// verdict judgeFunction gives.
func (f FunctionEnded) decide(d *decider) error {
	if err := d.awaits(f.Action, is[InFunction]); err != nil {
		return err
	}
	d.emit(ActionFunctionEnded{EventHead: d.head(), Action: f.Action, Outcome: f.Outcome})
	action := d.def.Rule.Action(f.Action)
	spec, _ := action.Kind.(FunctionSpec)
	d.finish(f.Action, judgeFunction(spec, action.On, f.Outcome, d.run.stopping))
	return nil
}

// decide ends the action with the check's verdict, where its on sends it,
// once a stop reached the run too: the read finishes within the lookup
// timeout, so its verdict stands (KTD6). Passed goes on to the passed
// route, whose return step moves the item to the label the check found.
func (f ReturnChecked) decide(d *decider) error {
	if err := d.awaits(f.Action, is[InReturnCheck]); err != nil {
		return err
	}
	j := judgeReturn(f.Check)
	d.returnTo = f.Check.To
	d.end(f.Action, j, d.def.Rule.Action(f.Action).On.Target(j.Verdict))
	return nil
}
