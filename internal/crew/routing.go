package crew

// StepKind is what one step of a route does.
type StepKind int

// The kinds of a route's steps, one for each Step.
const (
	// StepMove moves the item to a state (MoveStep), or to the label the
	// answered rule's check found (ReturnStep).
	StepMove StepKind = iota
	// StepClose closes the issue (CloseStep).
	StepClose
	// StepComment posts the config's comment (CommentStep).
	StepComment
	// StepReport posts crew's report (ReportStep).
	StepReport
	// StepShell runs one of the config's shell actions (ShellStep).
	StepShell
	// StepFunction calls one of crew's functions (FunctionStep).
	StepFunction
	// StepQuestion posts a rule's question (QuestionStep).
	StepQuestion
	// StepDelegate posts the delegation of the item's open question
	// (DelegateStep).
	StepDelegate
)

// StepPlan is one step of the route a run ends through, as plain data: what
// it does, the state a move moves the item to, the shell action a shell
// step runs, the name of a function step and the id of the question a
// question step posts.
type StepPlan struct {
	Kind StepKind
	// To is the state a StepMove moves the item to: a return step's is the
	// label the check found, written when the run chose its route; empty
	// for the other kinds.
	To State
	// Shell is the shell action a StepShell runs; empty for the other
	// kinds.
	Shell ActionName
	// Function is the name of a StepFunction; empty for the other kinds.
	Function ActionName
	// Question is the id of the question a StepQuestion posts; empty for
	// the other kinds, and in a plan a journal recorded before crew kept
	// it.
	Question QuestionID
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
	case FunctionStep:
		return StepPlan{Kind: StepFunction, Function: s.Name}
	case QuestionStep:
		return StepPlan{Kind: StepQuestion, Question: s.Question.ID}
	case DelegateStep:
		return StepPlan{Kind: StepDelegate}
	case ReturnStep:
		// The run writes the label the check found when it chooses the
		// route.
		return StepPlan{Kind: StepMove}
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
// or the comment, report, question or delegation was posted.
type StepLanded struct{}

// StepRan is a shell step whose script exited 0, or a function step whose
// function returned Passed.
type StepRan struct {
	// Reason is crew's one line on how the script ended, in crew's words
	// only: a route's step never carries what its script printed (R49).
	Reason ShellReason
}

// StepFailed is a shell step whose script exited with another status or
// did not run to its end, a function step whose function returned another
// verdict or none, or a comment step whose comment did not render for the
// run, so crew never posted it.
type StepFailed struct {
	// Reason is crew's one line on how the script ended, in crew's words
	// only, or why the comment did not render: a route's step never
	// carries what its script printed (R49).
	Reason ShellReason
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

// StepStopped is a shell step crew stopped while its script ran, or a
// function step crew stopped while its function ran.
type StepStopped struct {
	// Reason is crew's one line on how the script ended.
	Reason ShellReason
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

// judgeFunctionStep returns the outcome of a route's function step whose
// function ended as outcome says: stopped once a stop reached the run, ran
// when it returned Passed, and failed otherwise (R16).
func judgeFunctionStep(outcome FunctionOutcome, stopping bool) StepOutcome {
	v, returned := outcome.Verdict.Get()
	switch {
	case stopping:
		return StepStopped{Reason: outcome.Reason}
	case returned && v == Passed:
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
