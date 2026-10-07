package crew

import (
	"errors"
	"fmt"
	"time"
)

// ErrRefused is the error Decide wraps for a fact the run does not wait
// for, and Apply for an event of another run.
var ErrRefused = errors.New("refused")

// stoppedReason is the reason of an action crew stopped before it could end
// on its own, or before it started.
const stoppedReason = "crew stopped"

// timeUpReason is the reason of an action that did not start as crew's run
// time was up.
const timeUpReason = "crew's run time was up"

// RunDefinition is what a rule run decides by: its rule, and what crew can
// do for it.
type RunDefinition struct {
	Rule Rule
	// FindsPullRequests says whether crew can look up the pull requests of
	// a run's branch. A run of a rule with a session asks for them once it
	// chose its route.
	FindsPullRequests bool
}

// Decide returns the events fact produces on run, in the order to apply
// them, or none when the fact changes nothing, such as crew's run time
// being up for a run that is routing. It refuses, with an error wrapping ErrRefused, a
// fact of another run, any fact for a released run, and a fact for an
// action or a delivery that does not wait for it. It never changes run:
// the caller applies the events.
//
// It renders the prompt of each session action it starts, from def, only
// to decide whether the action fails with CausePrompt, the text parameters
// of each function action it starts, only to decide whether the action
// fails with CauseFunction, and the comment of each comment step and the
// text parameters of each function step it asks, only to decide whether
// the step fails.
func Decide(run RuleRun, def RunDefinition, fact Fact) ([]RunEvent, error) {
	h := fact.factHead()
	if run.id == "" || h.Run != run.id {
		return nil, fmt.Errorf("%w: a fact of run %q for run %q", ErrRefused, h.Run, run.id)
	}
	if _, released := run.phase.(ReleasedPhase); released {
		return nil, fmt.Errorf("%w: run %q was released", ErrRefused, run.id)
	}
	d := &decider{run: run, def: def, at: h.At}
	if err := fact.decide(d); err != nil {
		return nil, err
	}
	return d.events, nil
}

// decider is one Decide in progress: the run with the events decided so
// far applied, and those events.
type decider struct {
	run    RuleRun
	def    RunDefinition
	at     time.Time
	events []RunEvent
}

// head returns the head of an event of the run, at the fact's time.
func (d *decider) head() EventHead {
	return EventHead{Run: d.run.id, At: d.at, IssueID: d.run.issue.ID(), IssueRef: d.run.issue.Ref(), Rule: d.run.rule}
}

// emit decides e and applies it to the run.
func (d *decider) emit(e RunEvent) {
	d.events = append(d.events, e)
	d.run = e.apply(d.run)
}

// refused returns the refusal of a fact the run does not wait for.
func (d *decider) refused(what string) error {
	return fmt.Errorf("%w: run %q does not wait for %s", ErrRefused, d.run.id, what)
}

// awaits returns the refusal of a fact of the action named name, unless
// that action is at the run's cursor in a state waits accepts.
func (d *decider) awaits(name ActionName, waits func(ActionRunState) bool) error {
	if a, ok := d.run.Cursor(); !ok || a.name != name || !waits(a.state) {
		return d.refused(fmt.Sprintf("this fact of action %q", name))
	}
	return nil
}

// is reports whether s is a T.
func is[T ActionRunState](s ActionRunState) bool {
	_, ok := s.(T)
	return ok
}

// start starts the action named name: asks for its session, after
// rendering its prompt, or for its script or its function, after rendering
// its text parameters, acting as the run's bot. A session whose prompt
// does not render, and a function whose text parameters do not, end at
// once, and once a stop or time-up reached the run the action ends without
// starting.
func (d *decider) start(name ActionName) {
	if j, halted := d.halted(); halted {
		d.end(name, j, ToRoute{Route: FailedRoute})
		return
	}
	switch k := d.def.Rule.Action(name).Kind.(type) {
	case SessionSpec:
		if _, err := k.Prompt.Render(d.run.issue); err != nil {
			d.finish(name, failedBy(NewSessionText(err.Error()), CausePrompt))
			return
		}
		d.emit(ActionSessionAsked{EventHead: d.head(), Action: name})
	case ShellSpec:
		d.emit(ActionShellAsked{EventHead: d.head(), Action: name, Bot: d.run.Bot()})
	case FunctionSpec:
		if _, err := k.RenderTexts(d.run.issue); err != nil {
			d.finish(name, failedBy(NewSessionText(err.Error()), CauseFunction))
			return
		}
		d.emit(ActionFunctionAsked{EventHead: d.head(), Action: name, Bot: d.run.Bot()})
	}
}

// startAtCursor starts the action at the run's cursor.
func (d *decider) startAtCursor() {
	a, _ := d.run.Cursor()
	d.start(a.name)
}

// halted returns the end of an action that a stop, or crew's run time
// being up, keeps from starting, and whether one does: the stop wins.
func (d *decider) halted() (Judged, bool) {
	switch {
	case d.run.stopping:
		return failedBy(NewSessionText(stoppedReason), CauseStoppedBeforeStart), true
	case d.run.timeUp:
		return failedBy(NewSessionText(timeUpReason), CauseTimeUp), true
	}
	return Judged{}, false
}

// finish ends the action named name with j, which leads where its
// action's on sends j's verdict, or to FailedRoute once a stop reached the
// run.
func (d *decider) finish(name ActionName, j Judged) {
	target := d.def.Rule.Action(name).On.Target(j.Verdict)
	if d.run.stopping {
		target = ToRoute{Route: FailedRoute}
	}
	d.end(name, j, target)
}

// end ends the action named name with j and target, then starts the next
// action when target leads there, or chooses the route it leads to: its
// own, or PassedRoute after the last action.
func (d *decider) end(name ActionName, j Judged, target Target) {
	a, _ := d.run.Action(name)
	d.emit(ActionEnded{
		EventHead: d.head(), Action: name, End: j.End, Verdict: j.Verdict, Target: target,
		SessionStarted: a.session, Usage: cloneUsage(a.usage),
	})
	switch t := target.(type) {
	case ToRoute:
		d.choose(t.Route, name)
	case Next:
		if i := d.run.actionIndex(name) + 1; i < len(d.run.actions) {
			d.start(d.run.actions[i].name)
			return
		}
		d.choose(PassedRoute, name)
	}
}

// choose ends the run's sequence through route, with the action named
// action at its cursor. It asks for the lookup of the run's pull requests
// when the rule has a session that could have opened one in the run's
// workspace, and the route's first step once the lookup answered, or at
// once without one.
func (d *decider) choose(route RouteName, action ActionName) {
	r, _ := d.def.Rule.Route(route)
	d.emit(RouteChosen{EventHead: d.head(), Route: route, Action: action, Steps: plans(r)})
	if _, ok := d.run.Workspace().Get(); ok && d.def.FindsPullRequests && d.def.Rule.hasSession() {
		d.emit(RunLookupAsked{EventHead: d.head()})
		return
	}
	d.nextStep()
}

// passedAlone reports whether the run runs only PassedRoute, which the run
// it continues chose and never finished.
func (d *decider) passedAlone() bool {
	_, ok := d.run.Start().(StartPassedRoute)
	return ok
}

// choosePassed ends the run's sequence through PassedRoute, with the
// action at its cursor, or none for a rule without actions.
func (d *decider) choosePassed() {
	a, _ := d.run.Cursor()
	d.choose(PassedRoute, a.name)
}

// nextStep asks the next step of the run's route, once it is routing, or
// releases the run once the route's final step settled. A step that
// settles without being asked (unasked) settles at once, and the route
// goes on.
func (d *decider) nextStep() {
	for {
		p, routing := d.run.phase.(RoutingPhase)
		if !routing {
			return
		}
		i := len(p.Settled)
		if i >= len(p.Steps) {
			d.emit(RunReleased{EventHead: d.head()})
			return
		}
		outcome, settled := d.unasked(p.Route, i)
		if !settled {
			d.emit(StepAsked{EventHead: d.head(), Step: i})
			return
		}
		d.emit(StepEnded{EventHead: d.head(), Step: i, Outcome: outcome})
	}
}

// unasked returns the outcome of the step at index i of route when it
// settles without being asked, and whether it does: a shell or function
// step once a stop reached the run is skipped, and a function step whose
// text parameters, or a comment whose template, do not render for the run
// fails.
func (d *decider) unasked(route RouteName, i int) (StepOutcome, bool) {
	r, _ := d.def.Rule.Route(route)
	switch s := r.Steps[i].(type) {
	case ShellStep:
		if d.run.stopping {
			return StepSkipped{}, true
		}
	case FunctionStep:
		if d.run.stopping {
			return StepSkipped{}, true
		}
		if _, err := s.Function.RenderTexts(d.run.issue); err != nil {
			return StepFailed{Reason: NewShellReason(err.Error())}, true
		}
	case CommentStep:
		if _, err := s.Template.Render(d.run.CommentData()); err != nil {
			return StepFailed{Reason: NewShellReason(err.Error())}, true
		}
	case MoveStep, CloseStep, ReportStep:
	}
	return nil, false
}
