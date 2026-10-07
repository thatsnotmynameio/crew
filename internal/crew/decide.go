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
	// FindsPullRequests says whether crew looks up the pull request each
	// action opened, once its session ended.
	FindsPullRequests bool
}

// Decide returns the events fact produces on run, in the order to apply
// them, or none when the fact changes nothing, such as a stop reaching a
// run that is judging. It refuses, with an error wrapping ErrRefused, a
// fact of another run, any fact for a released run, and a fact for an
// action or a delivery that does not wait for it. It never changes run:
// the caller applies the events.
//
// It renders the prompt of each action the take starts, from def, only to
// decide whether the action fails with CausePrompt.
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

// action returns the run of the action named name, when its state is one
// waits accepts.
func (d *decider) action(name ActionName, waits func(ActionRunState) bool) (ActionRun, error) {
	a, ok := d.run.Action(name)
	if !ok || !waits(a.state) {
		return ActionRun{}, d.refused(fmt.Sprintf("this fact of action %q", name))
	}
	return a, nil
}

// is reports whether s is a T.
func is[T ActionRunState](s ActionRunState) bool {
	_, ok := s.(T)
	return ok
}

// awaitsWorkspace reports whether s waits for its workspace.
func awaitsWorkspace(s ActionRunState) bool {
	return is[CreatingWorkspace](s) || is[ReopeningWorkspace](s)
}

// end ends the action named name with end and judges the run once every
// action ended. While its pull request is looked up, the action finishes
// instead, and the lookup's answer ends it.
func (d *decider) end(name ActionName, end ActionEnd) {
	a, _ := d.run.Action(name)
	if _, pending := a.lookup.(LookupPending); pending {
		d.emit(ActionFinishing{EventHead: d.head(), Action: name, End: end})
		return
	}
	d.emit(ActionEnded{
		EventHead: d.head(), Action: name, End: end, Workspace: a.workspace, SessionStarted: a.session,
		Usage: cloneUsage(a.usage), PullRequest: a.PullRequest(),
	})
	if d.run.ActionsEnded() {
		d.judge()
	}
}

// stoppedEnd returns the end of an action crew stopped.
func stoppedEnd() ActionEnd {
	return EndFailed{Reason: NewSessionText(stoppedReason), Cause: CauseStopped}
}

// judge decides the run's verdict: the rule's success state when every
// action succeeded, and otherwise its failure state with each failed
// action, in action order, and where to read why it failed.
func (d *decider) judge() {
	verdict := Verdict{To: d.def.Rule.Labels.Success}
	for _, a := range d.run.actions {
		if a.Outcome().Succeeded {
			continue
		}
		f := ActionFailure{Action: a.name}
		if w, ok := a.workspace.Get(); ok {
			f.Workspace, f.Log = w.Workspace.Name, w.Log
		}
		verdict.Failures = append(verdict.Failures, f)
	}
	if verdict.Failed() {
		verdict.To = d.def.Rule.Labels.Failure
	}
	d.emit(RunJudged{EventHead: d.head(), Verdict: verdict})
}
