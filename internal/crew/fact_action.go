package crew

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

// decide records the session's start, as its action's bot, and asks it to
// stop at once when a stop reached the run while it was starting.
func (f SessionStarted) decide(d *decider) error {
	if err := d.awaits(f.Action, is[StartingSession]); err != nil {
		return err
	}
	spec, _ := d.def.Rule.Action(f.Action).Kind.(SessionSpec)
	d.emit(ActionSessionStarted{EventHead: d.head(), Action: f.Action, Bot: spec.Bot})
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
