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

// decide records how the tracker step settled and goes on with the route.
func (f StepSettled) decide(d *decider) error {
	if err := d.awaitsStep(f.Step, false); err != nil {
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
	if err := d.awaitsStep(f.Step, true); err != nil {
		return err
	}
	d.emit(StepEnded{EventHead: d.head(), Step: f.Step, Outcome: judgeStep(f.Outcome, d.run.stopping)})
	d.nextStep()
	return nil
}

// awaitsStep returns the refusal of a fact of the route's step at index
// step, unless that step is in flight and is a shell step exactly when
// shell is set.
func (d *decider) awaitsStep(step int, shell bool) error {
	p, routing := d.run.phase.(RoutingPhase)
	if i, asked := p.InFlight(); !routing || !asked || i != step || (p.Steps[i].Kind == StepShell) != shell {
		return d.refused(fmt.Sprintf("this fact of step %d of its route", step))
	}
	return nil
}
