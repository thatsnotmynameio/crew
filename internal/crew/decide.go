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
// on its own.
const stoppedReason = "crew stopped"

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
// them, or none when the fact changes nothing, such as a stop reaching a
// run that is ending. It refuses, with an error wrapping ErrRefused, a
// fact of another run, any fact for a released run, and a fact for an
// action or a delivery that does not wait for it. It never changes run:
// the caller applies the events.
//
// It renders the prompt of each session action it starts, from def, only
// to decide whether the action fails with CausePrompt.
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
// rendering its prompt, or for its script, acting as the run's bot. A
// session whose prompt does not render ends at once.
func (d *decider) start(name ActionName) {
	switch k := d.def.Rule.Action(name).Kind.(type) {
	case SessionSpec:
		if _, err := k.Prompt.Render(d.run.issue); err != nil {
			d.finish(name, failedBy(NewSessionText(err.Error()), CausePrompt))
			return
		}
		d.emit(ActionSessionAsked{EventHead: d.head(), Action: name})
	case ShellSpec:
		d.emit(ActionShellAsked{EventHead: d.head(), Action: name, Bot: d.run.bot})
	}
}

// finish ends the action named name with j, then starts the next action
// when its verdict leads there, or chooses the route it leads to: its own,
// or PassedRoute after the last action.
func (d *decider) finish(name ActionName, j Judged) {
	a, _ := d.run.Action(name)
	target := d.target(name, j.Verdict)
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

// stopAtCursor ends the action at the cursor, which did not start, as
// stopped: the run ends through FailedRoute.
func (d *decider) stopAtCursor() {
	a, _ := d.run.Cursor()
	d.finish(a.name, failedBy(NewSessionText(stoppedReason), CauseStopped))
}

// target returns where verdict v of the action named name leads: where its
// on sends v, or FailedRoute once a stop reached the run.
func (d *decider) target(name ActionName, v Verdict) Target {
	if d.run.stopping {
		return ToRoute{Route: FailedRoute}
	}
	return d.def.Rule.Action(name).On.Target(v)
}

// choose ends the run's sequence through route, with the action named
// action at its cursor, and asks for the lookup of the run's pull requests
// when the rule has a session that could have opened one in the run's
// workspace.
func (d *decider) choose(route RouteName, action ActionName) {
	d.emit(RouteChosen{EventHead: d.head(), Route: route, Action: action})
	if _, ok := d.run.Workspace().Get(); ok && d.def.FindsPullRequests && d.def.Rule.hasSession() {
		d.emit(RunLookupAsked{EventHead: d.head()})
	}
}
