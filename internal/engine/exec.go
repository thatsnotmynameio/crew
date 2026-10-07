package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fileline"
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

// sessionKey identifies the session, or the script, of one action of a rule
// run (KTD7).
type sessionKey struct {
	run    crew.RuleRunID
	action crew.ActionName
}

// liveSession is a running session, with the issue its rule run works on, by
// which said orders it.
type liveSession struct {
	issue   crew.IssueID
	session port.Session
}

// launch runs cmd in its own goroutine on the command context ctx (KTD7).
// Each goroutine posts its results with blocking sends, the last one final.
func (e *Engine) launch(ctx context.Context, cmd core.Command) {
	var job func()
	switch c := cmd.(type) {
	case core.TrackerCommand:
		job = e.trackerJob(ctx, c)
	case core.RunCommand:
		job = e.runJob(ctx, c)
	}
	if job == nil {
		return
	}
	e.inflight++
	e.wg.Go(job)
}

// trackerJob returns the goroutine that runs cmd, a command to the tracker,
// on the command context ctx.
func (e *Engine) trackerJob(ctx context.Context, cmd core.TrackerCommand) func() {
	return func() {
		switch c := cmd.(type) {
		case core.ListIssues:
			e.list(ctx, c)
		case core.ListBoard:
			e.listBoard(ctx, c)
		case core.Move:
			e.move(ctx, c)
		case core.Comment:
			e.comment(ctx, c)
		case core.Close:
			e.close(ctx, c)
		case core.ReportFailure:
			e.report(ctx, c)
		case core.ReportStatus:
			e.reportStatus(ctx, c)
		case core.ReportPullRequests:
			e.reportPullRequests(ctx, c)
		}
	}
}

// runJob returns the goroutine that runs cmd, a command about one rule run,
// on the command context ctx, or nil when nothing is left to run once the
// loop has done its part. The loop itself records run events, starts and stops
// scripts and stops sessions, as it owns the order of the journal, the
// scripts and the sessions.
func (e *Engine) runJob(ctx context.Context, cmd core.RunCommand) func() {
	switch c := cmd.(type) {
	case core.CreateWorkspace:
		return func() { e.createWorkspace(ctx, c) }
	case core.ReopenWorkspace:
		return func() { e.reopenWorkspace(ctx, c) }
	case core.StartSession:
		return func() { e.startSession(ctx, c) }
	case core.FindPullRequest:
		return func() { e.findPullRequest(ctx, c) }
	case core.Record:
		// Appended here, in the loop, so events land in the order the core
		// asked for them: an action's start before its session starts and
		// its end never before its start (KTD3).
		err := e.cfg.Journal.Append(c.Event)
		if err == nil {
			return nil
		}
		failure := core.RecordFailed{Event: c.Event, Reason: e.scrub(err.Error())}
		return func() { e.post(failure) }
	case core.StopSession:
		s, ok := e.sessions[sessionKey{c.Run, c.Action}]
		if !ok {
			// The core asks to stop only sessions it saw start, so the
			// session is known; nothing runs otherwise.
			return nil
		}
		return func() { e.stopSession(ctx, s.session) }
	case core.RunShell:
		return e.runShell(ctx, c)
	case core.RunStepShell:
		return e.runStepShell(ctx, c)
	case core.StopShell:
		stopScript(e.shells, sessionKey{c.Run, c.Action})
	case core.StopStepShell:
		stopScript(e.steps, stepKey{c.Run, c.Step})
	}
	return nil
}

// post sends in as the goroutine's final message.
func (e *Engine) post(in core.Input) {
	e.inbox <- message{input: in, final: true}
}

// callContext bounds one tracker or workspace call on the command context
// ctx (KTD7).
func callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, callTimeout)
}

func (e *Engine) list(ctx context.Context, c core.ListIssues) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	issues, err := e.cfg.Tracker.List(ctx, c.States)
	if err != nil {
		e.post(core.ListFailed{Reason: e.reason(ctx, err)})
		return
	}
	for i := range issues {
		issues[i] = issues[i].WithRepository(e.repository.ID)
	}
	e.post(core.IssuesListed{Issues: issues})
}

// listBoard reads the board's issues; the core asks only when the engine
// gave it ListingBoard, so the tracker is a port.BoardLister.
func (e *Engine) listBoard(ctx context.Context, c core.ListBoard) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	issues, err := e.board.ListBoard(ctx, c.Labels)
	if err != nil {
		e.post(core.BoardListFailed{Reason: e.reason(ctx, err)})
		return
	}
	for i := range issues {
		issues[i] = crew.NewBoardIssue(issues[i].Issue().WithRepository(e.repository.ID), issues[i].Labels())
	}
	e.post(core.BoardListed{Issues: issues})
}

func (e *Engine) move(ctx context.Context, c core.Move) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.cfg.Tracker.Move(ctx, c.IssueID, c.From, c.To)
	e.post(e.callResult(ctx, c.ID, err))
}

func (e *Engine) report(ctx context.Context, c core.ReportFailure) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.cfg.Tracker.ReportFailure(ctx, c.Report)
	e.post(e.callResult(ctx, c.ID, err))
}

// reportStatus writes a status through the tracker's StatusReporter, which
// the core asks for only when the tracker has one.
func (e *Engine) reportStatus(ctx context.Context, c core.ReportStatus) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.reporter.ReportStatus(ctx, c.Status)
	result, reason := e.classify(ctx, err)
	e.post(core.StatusResult{IssueID: c.Status.IssueID(), Result: result, Reason: reason})
}

// reportPullRequests shows a report on the issue's pull requests through the
// tracker's PullRequestReporter, which the core asks for only when the
// tracker has one.
func (e *Engine) reportPullRequests(ctx context.Context, c core.ReportPullRequests) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.pullRequests.ReportPullRequests(ctx, c.Report)
	result, reason := e.classify(ctx, err)
	e.post(core.PullRequestsResult{IssueID: c.Report.IssueID(), Result: result, Reason: reason})
}

// callResult maps a tracker call's error onto the core's result classes.
func (e *Engine) callResult(ctx context.Context, id core.CallID, err error) core.CallResult {
	result, reason := e.classify(ctx, err)
	return core.CallResult{ID: id, Result: result, Reason: reason}
}

// classify maps a tracker call's error onto the core's result classes, with
// its reason; nil is ResultDone, with no reason.
func (e *Engine) classify(ctx context.Context, err error) (core.Result, string) {
	switch {
	case err == nil:
		return core.ResultDone, ""
	case errors.Is(err, port.ErrMovedMeanwhile):
		return core.ResultMovedMeanwhile, e.reason(ctx, err)
	case errors.Is(err, port.ErrRefused):
		return core.ResultRefused, e.reason(ctx, err)
	}
	return core.ResultFailed, e.reason(ctx, err)
}

// reason is err as a reason for the core: local paths shortened, and a
// timeout said as such, as a killed tool's own error rarely does.
func (e *Engine) reason(ctx context.Context, err error) string {
	return e.scrub(callError(ctx, err))
}

// callError is err's text, with a timeout said as such.
func callError(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s: %s", callTimeout, err)
	}
	return err.Error()
}

// sessionText is text, from outside crew, as the reason of an action's
// outcome: scrubbed, stripped and scrubbed again (scrubAndStrip).
func (e *Engine) sessionText(text string) crew.SessionText {
	return crew.NewSessionText(e.scrubAndStrip(text))
}

func (e *Engine) createWorkspace(ctx context.Context, c core.CreateWorkspace) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	space, err := e.cfg.Workspace.Create(ctx, c.Issue, c.Rule)
	if err != nil {
		e.post(core.WorkspaceFailed{IssueID: c.Issue.ID(), Run: c.Run, Reason: e.sessionText(callError(ctx, err))})
		return
	}
	// A new workspace may reuse the name of one that is gone, and with it
	// the log's name: the files an earlier run's session left beside that
	// log are not this run's (KTD22).
	e.clearSession(logPath(space.Workspace.Name))
	e.post(e.ready(c.Issue.ID(), c.Run, space, false))
}

// reopenWorkspace reopens the workspace of the run c's run continues
// through the workspace's port.Reopener, which the core asks for only when
// the workspace has one.
func (e *Engine) reopenWorkspace(ctx context.Context, c core.ReopenWorkspace) {
	r, ok := e.cfg.Workspace.(port.Reopener)
	if !ok {
		e.post(core.WorkspaceGone{IssueID: c.IssueID, Run: c.Run})
		return
	}
	ctx, cancel := callContext(ctx)
	defer cancel()
	space, err := r.Reopen(ctx, crew.Workspace{Name: c.Workspace, Branch: c.Branch})
	switch {
	case errors.Is(err, port.ErrWorkspaceGone):
		e.post(core.WorkspaceGone{IssueID: c.IssueID, Run: c.Run})
	case err != nil:
		e.post(core.WorkspaceFailed{IssueID: c.IssueID, Run: c.Run, Reason: e.sessionText(callError(ctx, err))})
	default:
		e.post(e.ready(c.IssueID, c.Run, space, true))
	}
}

// ready is the WorkspaceReady of space on the issue id, which answers the
// rule run identified by run.
func (e *Engine) ready(id crew.IssueID, run crew.RuleRunID, space port.Space, resumed bool) core.WorkspaceReady {
	log := logPath(space.Workspace.Name)
	return core.WorkspaceReady{
		IssueID: id, Run: run,
		Workspace: space.Workspace.Name, Dir: space.Dir, Branch: space.Workspace.Branch, Log: log,
		LogFromDir: e.logFromDir(space.Dir, log), Resumed: resumed,
	}
}

// startSession starts the session with its output going to the run's log,
// after a marker line naming its action (startMarker), with a verdict file
// of its own (KTD8), then waits for it to end in the same goroutine (R19).
// Once it ended, it reads the session's verdict, keeps its prompt and last
// message beside the log for the shell actions after it (KTD22), and posts
// its end. The session acts as its bot, or as you when that bot does not
// act, and learns the code owners' and the bots' logins.
func (e *Engine) startSession(ctx context.Context, c core.StartSession) {
	failed := func(err error) {
		e.post(core.SessionFailedToStart{
			IssueID: c.IssueID, Run: c.Run, Action: c.Action, Reason: e.sessionText(err.Error()),
		})
	}
	log, err := e.openSessionLog(c)
	if err != nil {
		failed(err)
		return
	}
	verdict, err := newVerdictFile()
	if err != nil {
		_ = log.Close() // nothing was written to it worth keeping
		failed(err)
		return
	}
	s, err := e.harnesses[c.Agent].Start(ctx, port.Run{
		Dir: c.Dir, Prompt: c.Prompt, Output: log,
		Identity: e.cfg.Identities[c.Bot], CodeOwners: e.codeOwners, Bots: e.cfg.BotLogins,
		VerdictFile: verdict.file, VerdictDir: verdict.dir,
	})
	if err != nil {
		_ = log.Close()
		verdict.remove()
		failed(err)
		return
	}
	e.inbox <- message{input: core.SessionStarted{IssueID: c.IssueID, Run: c.Run, Action: c.Action}, session: s}
	end := s.Wait()
	// The harness stops writing once Wait returns. A failed close cannot
	// change how the session ended, which is what the core needs.
	_ = log.Close()
	report := verdict.read()
	var usage crew.Usage
	if r, ok := s.(port.UsageReporter); ok {
		usage = r.Usage()
	}
	var last string
	if r, ok := s.(port.LastMessageReporter); ok {
		last = r.LastMessage()
	}
	e.keepSession(c.Log, c.Prompt, last)
	e.post(core.SessionEnded{
		IssueID: c.IssueID, Run: c.Run, Action: c.Action, Report: report, Usage: usage,
		Outcome: crew.Outcome{Succeeded: end.Succeeded, Reason: e.sessionText(end.Reason)},
	})
}

// findPullRequest looks up the pull request c's run opened, within
// lookupTimeout. A lookup that fails or times out leaves it not looked up,
// which changes nothing else (R7).
func (e *Engine) findPullRequest(ctx context.Context, c core.FindPullRequest) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	pr, err := e.finder.FindPullRequest(ctx, c.Branch, c.Since)
	if err != nil {
		pr = crew.PullRequestNotLookedUp{}
	}
	e.post(core.PullRequestFound{IssueID: c.IssueID, Run: c.Run, PullRequest: pr})
}

// stopSession stops s within the stop deadline (KTD7). Its end reaches the
// core through its own Wait, in startSession.
func (e *Engine) stopSession(ctx context.Context, s port.Session) {
	ctx, cancel := context.WithTimeout(ctx, stopTimeout)
	defer cancel()
	// The adapter kills the session at the deadline, so Stop's error adds
	// nothing the session's outcome will not say.
	_ = s.Stop(ctx)
	e.inbox <- message{final: true}
}

// startMarker is the line a session's output follows in the run's log. It
// is JSON, as the harness's output is, so tools reading the log as JSON
// lines keep working.
type startMarker struct {
	Type string `json:"type"`
	// Subtype is "resumed" for a session that resumes the work of an
	// earlier run, and "started" otherwise.
	Subtype string    `json:"subtype"`
	Action  string    `json:"action"`
	Time    time.Time `json:"time"`
}

// openSessionLog opens the log of c's run and writes the marker line naming
// c's action to it, on a line of its own, so the session and you can tell
// where its output starts, and a resumed one where the earlier run's output
// ends.
func (e *Engine) openSessionLog(c core.StartSession) (*os.File, error) {
	log, err := e.openLog(c.Log)
	if err != nil {
		return nil, err
	}
	subtype := "started"
	if c.Resumed {
		subtype = "resumed"
	}
	marker := startMarker{Type: "crew", Subtype: subtype, Action: string(c.Action), Time: time.Now().UTC()}
	data, err := json.Marshal(marker)
	if err == nil {
		err = fileline.Append(log, data)
	}
	if err != nil {
		_ = log.Close() // the write error is the one to report
		return nil, fmt.Errorf("write the session's marker: %w", err)
	}
	return log, nil
}
