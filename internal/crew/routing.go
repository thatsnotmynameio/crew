package crew

// StepKind is what one step of a route does.
type StepKind int

// The kinds of a route's steps, one for each Step.
const (
	// StepMove moves the item to a state (MoveStep).
	StepMove StepKind = iota
	// StepClose closes the issue (CloseStep).
	StepClose
	// StepComment posts the config's comment (CommentStep).
	StepComment
	// StepReport posts crew's report (ReportStep).
	StepReport
	// StepShell runs one of the config's shell actions (ShellStep).
	StepShell
)

// StepPlan is one step of the route a run ends through, as plain data: what
// it does, the state a move moves the item to, and the shell action a shell
// step runs.
type StepPlan struct {
	Kind StepKind
	// To is the state a StepMove moves the item to; empty for the other
	// kinds.
	To State
	// Shell is the shell action a StepShell runs; empty for the other
	// kinds.
	Shell ActionName
}

// planOf returns s as plain data.
func planOf(s Step) StepPlan {
	switch s := s.(type) {
	case MoveStep:
		return StepPlan{Kind: StepMove, To: s.To}
	case CloseStep:
		return StepPlan{Kind: StepClose}
	case CommentStep:
		return StepPlan{Kind: StepComment}
	case ReportStep:
		return StepPlan{Kind: StepReport}
	case ShellStep:
		return StepPlan{Kind: StepShell, Shell: s.Name}
	}
	return StepPlan{}
}

// plans returns the steps of route as plain data.
func plans(route Route) []StepPlan {
	out := make([]StepPlan, 0, len(route.Steps))
	for _, s := range route.Steps {
		out = append(out, planOf(s))
	}
	return out
}

// StepOutcome is how one step of a route settled: StepLanded, StepRan,
// StepFailed, StepGivenUp, StepDropped, StepSkipped or StepStopped. Whatever
// it is, the route goes on to its next step.
//
//sumtype:decl
type StepOutcome interface {
	stepOutcome()
}

// StepLanded is a tracker step that landed: the item moved or was closed,
// or the comment or report was posted.
type StepLanded struct{}

// StepRan is a shell step whose script exited 0.
type StepRan struct {
	// Reason is crew's one line on how the script ended, followed by the
	// last line it printed.
	Reason CheckReason
}

// StepFailed is a shell step whose script exited with another status or
// did not run to its end, or a comment step whose comment did not render
// for the run, so crew never posted it.
type StepFailed struct {
	// Reason is crew's one line on how the script ended, followed by the
	// last line it printed, or why the comment did not render.
	Reason CheckReason
}

// StepGivenUp is a tracker step crew gave up: the tracker refused it, or
// its final try failed. A final move or close given up leaves the route
// unfinished.
type StepGivenUp struct {
	Reason string
}

// StepDropped is a tracker step crew dropped as the item was closed or
// moved meanwhile. A final move or close dropped finishes the route: the
// item is where someone else put it.
type StepDropped struct {
	Reason string
}

// StepSkipped is a shell step that never ran, as a stop reached the run
// before its turn.
type StepSkipped struct{}

// StepStopped is a shell step crew stopped while its script ran.
type StepStopped struct {
	// Reason is crew's one line on how the script ended.
	Reason CheckReason
}

func (StepLanded) stepOutcome()  {}
func (StepRan) stepOutcome()     {}
func (StepFailed) stepOutcome()  {}
func (StepGivenUp) stepOutcome() {}
func (StepDropped) stepOutcome() {}
func (StepSkipped) stepOutcome() {}
func (StepStopped) stepOutcome() {}

// judgeStep returns the outcome of a route's shell step whose script ended
// as outcome says: stopped once a stop reached the run, which asked the
// script to stop, ran when it exited 0, and failed otherwise.
func judgeStep(outcome ShellOutcome, stopping bool) StepOutcome {
	status, exited := outcome.Status.Get()
	switch {
	case stopping:
		return StepStopped{Reason: outcome.Reason}
	case exited && status == 0:
		return StepRan{Reason: outcome.Reason}
	}
	return StepFailed{Reason: outcome.Reason}
}

// CommentData returns what a comment step of the run's route renders: the
// run's issue and rule, the action at its cursor and that action's verdict,
// the route it chose and its log. A run without actions has no action or
// verdict, and a run without a log, none.
func (r RuleRun) CommentData() CommentData {
	d := CommentData{Issue: r.issue, Rule: r.rule}
	if a, ok := r.Cursor(); ok {
		d.Action = a.name
		if f, ended := a.state.(Finished); ended {
			d.Verdict = f.Verdict
		}
	}
	if route, ok := r.route(); ok {
		d.Route = route.Route
	}
	if w, ok := r.Workspace().Get(); ok {
		d.Log = w.Log
	}
	return d
}
