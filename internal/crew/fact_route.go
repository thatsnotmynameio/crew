package crew

import "fmt"

// StepSettled is the delivery of the route's tracker step at index Step
// settling: StepLanded, StepGivenUp, or StepDropped as the item was closed
// or moved meanwhile.
type StepSettled struct {
	FactHead

	Step    int
	Outcome StepOutcome
}

// StepShellEnded is the script of the route's shell step at index Step that
// ended.
type StepShellEnded struct {
	FactHead

	Step    int
	Outcome ShellOutcome
}

// StepFunctionEnded is the function of the route's function step at index
// Step that ended.
type StepFunctionEnded struct {
	FactHead

	Step    int
	Outcome FunctionOutcome
}

// decide records how the tracker step settled and goes on with the route.
func (f StepSettled) decide(d *decider) error {
	if err := d.awaitsStep(f.Step, StepKind.Delivered); err != nil {
		return err
	}
	switch f.Outcome.(type) {
	case StepLanded, StepGivenUp, StepDropped:
	case StepRan, StepFailed, StepSkipped, StepStopped, nil:
		return d.refused(fmt.Sprintf("the outcome %T of a tracker step", f.Outcome))
	}
	d.emit(StepEnded{EventHead: d.head(), Step: f.Step, Outcome: f.Outcome})
	d.nextStep()
	return nil
}

// decide records the outcome judgeStep gives the shell step and goes on
// with the route, whatever it is.
func (f StepShellEnded) decide(d *decider) error {
	if err := d.awaitsStep(f.Step, kindIs(StepShell)); err != nil {
		return err
	}
	d.emit(StepEnded{EventHead: d.head(), Step: f.Step, Outcome: judgeStep(f.Outcome, d.run.stopping)})
	d.nextStep()
	return nil
}

// decide records the outcome judgeFunctionStep gives the function step and
// goes on with the route, whatever it is.
func (f StepFunctionEnded) decide(d *decider) error {
	if err := d.awaitsStep(f.Step, kindIs(StepFunction)); err != nil {
		return err
	}
	d.emit(StepEnded{EventHead: d.head(), Step: f.Step, Outcome: judgeFunctionStep(f.Outcome, d.run.stopping)})
	d.nextStep()
	return nil
}

// awaitsStep returns the refusal of a fact of the route's step at index
// step, unless that step is in flight and its kind is one kind accepts.
func (d *decider) awaitsStep(step int, kind func(StepKind) bool) error {
	p, routing := d.run.phase.(RoutingPhase)
	if i, asked := p.InFlight(); !routing || !asked || i != step || !kind(p.Steps[i].Kind) {
		return d.refused(fmt.Sprintf("this fact of step %d of its route", step))
	}
	return nil
}

// kindIs returns the test that a step's kind is want.
func kindIs(want StepKind) func(StepKind) bool {
	return func(k StepKind) bool { return k == want }
}

// Delivered reports whether a step of kind k is a tracker step: one crew
// delivers through its tracker, rather than a shell or a function step it
// runs itself.
func (k StepKind) Delivered() bool {
	switch k {
	case StepShell, StepFunction:
		return false
	case StepMove, StepClose, StepComment, StepReport, StepQuestion, StepDelegate:
	}
	return true
}
