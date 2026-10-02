package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// message is what a command's goroutine posts to the loop's inbox.
type message struct {
	// input is the result to feed the core; nil when the goroutine only
	// reports that it is done.
	input core.Input
	// session is the session a SessionStarted input started, for StopSession.
	session port.Session
	// final is set on the goroutine's last message: after it, the goroutine
	// posts nothing more and the loop no longer counts it as in flight.
	final bool
}

// sessionKey identifies the session of one action of an issue.
type sessionKey struct {
	issue, action string
}

// launch runs cmd in its own goroutine on the command context (KTD7). Each
// goroutine posts its results with blocking sends, the last one final.
func (e *Engine) launch(cmd core.Command) {
	var job func()
	switch c := cmd.(type) {
	case core.ListIssues:
		job = func() { e.list(c) }
	case core.Move:
		job = func() { e.move(c) }
	case core.ReportFailure:
		job = func() { e.report(c) }
	case core.CreateWorkspace:
		job = func() { e.createWorkspace(c) }
	case core.StartSession:
		job = func() { e.startSession(c) }
	case core.StopSession:
		s, ok := e.sessions[sessionKey{c.IssueKey, c.Action}]
		if !ok {
			// The core asks to stop only sessions it saw start, so the
			// session is known; nothing runs otherwise.
			return
		}
		job = func() { e.stopSession(s) }
	default:
		panic(fmt.Sprintf("engine: unknown core command %T", cmd))
	}
	e.inflight++
	e.wg.Go(job)
}

// post sends in as the goroutine's final message.
func (e *Engine) post(in core.Input) {
	e.inbox <- message{input: in, final: true}
}

// callContext bounds one tracker or workspace call (KTD7).
func (e *Engine) callContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(e.cmdCtx, callTimeout)
}

func (e *Engine) list(c core.ListIssues) {
	ctx, cancel := e.callContext()
	defer cancel()
	issues, err := e.cfg.Tracker.List(ctx, c.States)
	if err != nil {
		e.post(core.ListFailed{Reason: e.reason(ctx, err)})
		return
	}
	e.post(core.IssuesListed{Issues: issues})
}

func (e *Engine) move(c core.Move) {
	ctx, cancel := e.callContext()
	defer cancel()
	err := e.cfg.Tracker.Move(ctx, c.IssueKey, c.From, c.To)
	e.post(e.callResult(ctx, c.ID, err))
}

func (e *Engine) report(c core.ReportFailure) {
	ctx, cancel := e.callContext()
	defer cancel()
	err := e.cfg.Tracker.ReportFailure(ctx, c.Report)
	e.post(e.callResult(ctx, c.ID, err))
}

// callResult maps a tracker call's error onto the core's result classes.
func (e *Engine) callResult(ctx context.Context, id core.CallID, err error) core.CallResult {
	r := core.CallResult{ID: id, Result: core.ResultDone}
	if err == nil {
		return r
	}
	switch {
	case errors.Is(err, port.ErrMovedMeanwhile):
		r.Result = core.ResultMovedMeanwhile
	case errors.Is(err, port.ErrRefused):
		r.Result = core.ResultRefused
	default:
		r.Result = core.ResultFailed
	}
	r.Reason = e.reason(ctx, err)
	return r
}

// reason is err as a reason for the core: local paths shortened, and a
// timeout said as such, as a killed tool's own error rarely does.
func (e *Engine) reason(ctx context.Context, err error) string {
	text := err.Error()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		text = fmt.Sprintf("timed out after %s: %s", callTimeout, text)
	}
	return e.scrub(text)
}

func (e *Engine) createWorkspace(c core.CreateWorkspace) {
	ctx, cancel := e.callContext()
	defer cancel()
	space, err := e.cfg.Workspace.Create(ctx, c.Issue, c.Action)
	if err != nil {
		e.post(core.WorkspaceFailed{IssueKey: c.Issue.Key, Action: c.Action, Reason: e.reason(ctx, err)})
		return
	}
	e.post(core.WorkspaceReady{
		IssueKey: c.Issue.Key, Action: c.Action,
		Workspace: space.Name, Dir: space.Dir, Branch: space.Branch, Log: logPath(space.Name),
	})
}

// startSession starts the session with its output going to its log, then
// waits for it to end in the same goroutine (R19).
func (e *Engine) startSession(c core.StartSession) {
	log, err := e.openLog(c.Log)
	if err != nil {
		e.post(core.SessionFailedToStart{IssueKey: c.IssueKey, Action: c.Action, Reason: e.scrub(err.Error())})
		return
	}
	s, err := e.cfg.Harness.Start(e.cmdCtx, port.Run{Dir: c.Dir, Prompt: c.Prompt, Output: log})
	if err != nil {
		_ = log.Close() // nothing was written to it worth keeping
		e.post(core.SessionFailedToStart{IssueKey: c.IssueKey, Action: c.Action, Reason: e.scrub(err.Error())})
		return
	}
	e.inbox <- message{input: core.SessionStarted{IssueKey: c.IssueKey, Action: c.Action}, session: s}
	outcome := s.Wait()
	// The harness stops writing once Wait returns. A failed close cannot
	// change the session's verdict, which is what the core needs.
	_ = log.Close()
	outcome.Reason = e.scrub(outcome.Reason)
	e.post(core.SessionEnded{IssueKey: c.IssueKey, Action: c.Action, Outcome: outcome})
}

// stopSession stops s within the stop deadline (KTD7). Its end reaches the
// core through its own Wait, in startSession.
func (e *Engine) stopSession(s port.Session) {
	ctx, cancel := context.WithTimeout(e.cmdCtx, stopTimeout)
	defer cancel()
	// The adapter kills the session at the deadline, so Stop's error adds
	// nothing the session's outcome will not say.
	_ = s.Stop(ctx)
	e.inbox <- message{final: true}
}
