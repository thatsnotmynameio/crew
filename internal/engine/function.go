package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Function is the function the engine calls for one function use: the
// function built for the use, and how to decode the use's parameters for
// one call. Both must be set.
type Function struct {
	// Function is the function the use's parameters built.
	Function port.Function
	// Bind returns a Decode over the use's parameters with each text
	// parameter's value replaced by texts[name], its rendering for the
	// issue of one call. Every Decode it returns must be the call's own,
	// as calls of one use run at the same time for different issues.
	Bind func(texts map[string]string) port.Decode
}

// runFunction returns the goroutine that calls c's function action and
// posts its end. The call's context is made here, in the loop, so a
// StopFunction that follows always finds it.
func (e *Engine) runFunction(ctx context.Context, c core.RunFunction) func() {
	callCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	e.shells[sessionKey{c.Run, c.Action}] = cancel
	return func() {
		defer cancel()
		outcome := e.callFunction(callCtx, c.IssueID, c.Call, "the function action "+string(c.Action), true)
		e.post(core.FunctionEnded{IssueID: c.IssueID, Run: c.Run, Action: c.Action, Outcome: outcome})
	}
}

// runStepFunction returns the goroutine that calls c's route step and
// posts its end, as runFunction does. A step's reason is in crew's words
// only, without the error the function returned (R49).
func (e *Engine) runStepFunction(ctx context.Context, c core.RunStepFunction) func() {
	callCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	e.steps[stepKey{c.Run, c.Step}] = cancel
	return func() {
		defer cancel()
		outcome := e.callFunction(callCtx, c.IssueID, c.Call, "the route's function step "+string(c.Call.Name), false)
		e.post(core.StepFunctionEnded{IssueID: c.IssueID, Run: c.Run, Step: c.Step, Outcome: outcome})
	}
}

// callFunction calls c, the function of a function action or a route step
// that subject names, within shellTimeout, and returns how it ended
// (KTD-F12). It finds the function by c's use, decodes its parameters with
// c's texts, and hands it the run's log, after a marker line naming it: the
// log of the run's workspace, or for a run without one the log of the
// workspace it would have (scriptLog). It acts as c's bot. An error the
// function returns goes into the log too, and, when withError is set,
// into the outcome's reason, scrubbed and stripped (R49).
func (e *Engine) callFunction(
	ctx context.Context, issue crew.IssueID, c core.FunctionCall, subject string, withError bool,
) crew.FunctionOutcome {
	notStarted := func(err error) crew.FunctionOutcome {
		return crew.FunctionOutcome{
			Reason: crew.NewShellReason(subject + " could not start: " + e.scrubAndStrip(err.Error())),
		}
	}
	f, ok := e.cfg.Functions[c.Use]
	if !ok {
		return notStarted(fmt.Errorf("crew has no function for %s", c.Use))
	}
	log, err := e.openLog(scriptLog(issue, c.Log, c.Rule))
	if err != nil {
		return notStarted(err)
	}
	// A failed write or close cannot change how the function ended.
	defer func() { _ = log.Close() }()
	_, _ = fmt.Fprintf(log, "\ncrew: calling %s (%s)\n", subject, c.Function)
	v, err := f.Function.Run(ctx, port.FunctionCall{
		Params: f.Bind(c.Texts), IssueRef: c.IssueRef, IssueID: issue, IssueURL: c.IssueURL, Dir: c.Dir,
		Branch: c.Branch, Log: log, Identity: e.cfg.Identities[c.Bot],
	})
	if err != nil {
		_, _ = fmt.Fprintf(log, "crew: %s failed: %v\n", subject, err)
	}
	return e.functionOutcome(ctx, subject, v, err, withError)
}

// functionOutcome returns how the function subject names ended, having
// returned v or err on ctx: out of time or stopped once ctx ended, failed
// on an error, with it when withError is set, and with v otherwise.
func (e *Engine) functionOutcome(
	ctx context.Context, subject string, v crew.Verdict, err error, withError bool,
) crew.FunctionOutcome {
	var said string
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		said = fmt.Sprintf("%s ran out of time after %s", subject, shellTimeout)
	case ctx.Err() != nil:
		said = subject + " was stopped"
	case err != nil:
		said = subject + " failed"
		if withError {
			said = e.saying(said, err.Error())
		}
	default:
		said = fmt.Sprintf("%s returned %q", subject, v)
		return crew.FunctionOutcome{Verdict: crew.Some(v), Reason: crew.NewShellReason(said)}
	}
	return crew.FunctionOutcome{Reason: crew.NewShellReason(said)}
}
