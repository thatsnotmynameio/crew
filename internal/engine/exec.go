package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
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

// launch runs cmd in its own goroutine on the command context ctx (KTD7).
// Each goroutine posts its results with blocking sends, the last one final.
func (e *Engine) launch(ctx context.Context, cmd core.Command) {
	job := e.job(ctx, cmd)
	if job == nil {
		return
	}
	e.inflight++
	e.wg.Go(job)
}

// job returns the goroutine that runs cmd on the command context ctx, or nil
// when nothing is left to run once the loop has done its part.
func (e *Engine) job(ctx context.Context, cmd core.Command) func() {
	switch c := cmd.(type) {
	case core.ListIssues:
		return func() { e.list(ctx, c) }
	case core.ListBoard:
		return func() { e.listBoard(ctx, c) }
	case core.Move:
		return func() { e.move(ctx, c) }
	case core.ReportFailure:
		return func() { e.report(ctx, c) }
	case core.ReportStatus:
		return func() { e.reportStatus(ctx, c) }
	case core.ReportPullRequests:
		return func() { e.reportPullRequests(ctx, c) }
	case core.CreateWorkspace:
		return func() { e.createWorkspace(ctx, c) }
	case core.ReopenWorkspace:
		return func() { e.reopenWorkspace(ctx, c) }
	case core.StartSession:
		return func() { e.startSession(ctx, c) }
	case core.FindPullRequest:
		return func() { e.findPullRequest(ctx, c) }
	}
	return e.loopJob(ctx, cmd)
}

// loopJob does the part of cmd that the loop itself must do, as it owns the
// sessions, the checks and the order of the journal, and returns the
// goroutine that runs the rest, or nil when nothing is left.
func (e *Engine) loopJob(ctx context.Context, cmd core.Command) func() {
	switch c := cmd.(type) {
	case core.RecordRun:
		// Written here, in the loop, so records land in the order the core
		// asked for them: a run's end never before its start (KTD3).
		err := e.appendJournal(c.Record)
		if err == nil {
			return nil
		}
		failure := core.RecordFailed{Record: c.Record, Reason: e.scrub(err.Error())}
		return func() { e.post(failure) }
	case core.StopSession:
		s, ok := e.sessions[sessionKey{c.IssueKey, c.Action}]
		if !ok {
			// The core asks to stop only sessions it saw start, so the
			// session is known; nothing runs otherwise.
			return nil
		}
		return func() { e.stopSession(ctx, s) }
	case core.RunCheck:
		// The check's context is made here, in the loop, so a StopCheck
		// that follows always finds it.
		checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		e.checks[sessionKey{c.IssueKey, c.Action}] = cancel
		return func() { e.runCheck(checkCtx, cancel, c) }
	case core.StopCheck:
		// The core asks to stop only checks it started; the check's end
		// still arrives through its own goroutine, in runCheck.
		if cancel, ok := e.checks[sessionKey{c.IssueKey, c.Action}]; ok {
			cancel()
		}
		return nil
	}
	panic(fmt.Sprintf("engine: unknown core command %T", cmd))
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
	e.post(core.BoardListed{Issues: issues})
}

func (e *Engine) move(ctx context.Context, c core.Move) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.cfg.Tracker.Move(ctx, c.IssueKey, c.From, c.To)
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
	e.post(core.StatusResult{IssueKey: c.Status.IssueKey, Result: result, Reason: reason})
}

// reportPullRequests shows a report on the issue's pull requests through the
// tracker's PullRequestReporter, which the core asks for only when the
// tracker has one.
func (e *Engine) reportPullRequests(ctx context.Context, c core.ReportPullRequests) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.pullRequests.ReportPullRequests(ctx, c.Report)
	result, reason := e.classify(ctx, err)
	e.post(core.PullRequestsResult{IssueKey: c.Report.IssueKey, Result: result, Reason: reason})
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
	text := err.Error()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		text = fmt.Sprintf("timed out after %s: %s", callTimeout, text)
	}
	return e.scrub(text)
}

func (e *Engine) createWorkspace(ctx context.Context, c core.CreateWorkspace) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	space, err := e.cfg.Workspace.Create(ctx, c.Issue, c.Action)
	if err != nil {
		e.post(core.WorkspaceFailed{IssueKey: c.Issue.Key, Action: c.Action, Reason: e.reason(ctx, err)})
		return
	}
	e.post(e.ready(c.Issue.Key, c.Action, space, false))
}

// reopenWorkspace reopens a failed run's workspace through the workspace's
// port.Reopener, which the core asks for only when the workspace has one.
func (e *Engine) reopenWorkspace(ctx context.Context, c core.ReopenWorkspace) {
	r, ok := e.cfg.Workspace.(port.Reopener)
	if !ok {
		e.post(core.WorkspaceGone{IssueKey: c.IssueKey, Action: c.Action})
		return
	}
	ctx, cancel := callContext(ctx)
	defer cancel()
	space, err := r.Reopen(ctx, port.Space{Name: c.Workspace, Branch: c.Branch})
	switch {
	case errors.Is(err, port.ErrWorkspaceGone):
		e.post(core.WorkspaceGone{IssueKey: c.IssueKey, Action: c.Action})
	case err != nil:
		e.post(core.WorkspaceFailed{IssueKey: c.IssueKey, Action: c.Action, Reason: e.reason(ctx, err)})
	default:
		e.post(e.ready(c.IssueKey, c.Action, space, true))
	}
}

// ready is the WorkspaceReady of space for action on the issue keyed key.
func (e *Engine) ready(key, action string, space port.Space, resumed bool) core.WorkspaceReady {
	log := logPath(space.Name)
	return core.WorkspaceReady{
		IssueKey: key, Action: action,
		Workspace: space.Name, Dir: space.Dir, Branch: space.Branch, Log: log,
		LogFromDir: e.logFromDir(space.Dir, log), Resumed: resumed,
	}
}

// startSession starts the session with its output going to its log, after
// a marker line when the session resumes a failed run (KTD8), then waits for
// it to end in the same goroutine (R19). The session acts as its action's
// bot, or as you when that bot does not act, and learns the code
// owners' and the bots' logins.
func (e *Engine) startSession(ctx context.Context, c core.StartSession) {
	log, err := e.openLog(c.Log)
	if err == nil && c.Resumed {
		if err = markResumed(log); err != nil {
			_ = log.Close() // the write error is the one to report
		}
	}
	if err != nil {
		e.post(core.SessionFailedToStart{IssueKey: c.IssueKey, Action: c.Action, Reason: e.scrub(err.Error())})
		return
	}
	s, err := e.cfg.Harness.Start(ctx, port.Run{
		Dir: c.Dir, Prompt: c.Prompt, Output: log,
		Identity: e.cfg.Identities[c.Bot], CodeOwners: e.codeOwners, Bots: e.cfg.BotLogins,
	})
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
	var usage crew.Usage
	if r, ok := s.(port.UsageReporter); ok {
		usage = r.Usage()
	}
	e.post(core.SessionEnded{IssueKey: c.IssueKey, Action: c.Action, Outcome: outcome, Usage: usage})
}

// findPullRequest looks up the pull request c's action opened, within
// lookupTimeout. A lookup that fails or times out leaves it not looked up,
// which changes nothing else (R7).
func (e *Engine) findPullRequest(ctx context.Context, c core.FindPullRequest) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	pr, err := e.finder.FindPullRequest(ctx, c.Branch, c.Since)
	if err != nil {
		pr = crew.PullRequest{}
	}
	e.post(core.PullRequestFound{IssueKey: c.IssueKey, Action: c.Action, PullRequest: pr})
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

// resumeMarker is the line a resumed session's output follows in its log. It
// is JSON, as the harness's output is, so tools reading the log as JSON
// lines keep working.
type resumeMarker struct {
	Type    string    `json:"type"`
	Subtype string    `json:"subtype"`
	Time    time.Time `json:"time"`
}

// markResumed writes the resume marker to log, on a line of its own, so the
// resumed session and you can tell where the failed run's output ends.
func markResumed(log *os.File) error {
	data, err := json.Marshal(resumeMarker{Type: "crew", Subtype: "resumed", Time: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("encode the resume marker: %w", err)
	}
	if err := appendLine(log, data); err != nil {
		return fmt.Errorf("write the resume marker: %w", err)
	}
	return nil
}

// runCheck runs the action's check within checkTimeout, its output going to
// the action's log after the session's, and posts its verdict. Its reason
// says how the check ended in crew's words, followed, for a check that
// failed, by the last line it printed (R5).
func (e *Engine) runCheck(ctx context.Context, cancel context.CancelFunc, c core.RunCheck) {
	defer cancel()
	e.post(core.CheckEnded{IssueKey: c.IssueKey, Action: c.Action, Outcome: e.check(ctx, c)})
}

// check runs c, as its action's bot like its session, and returns its
// verdict.
func (e *Engine) check(ctx context.Context, c core.RunCheck) crew.Outcome {
	if e.cfg.Checker == nil {
		return crew.Outcome{Reason: "the check could not start: crew has no check runner"}
	}
	log, err := e.openLog(c.Log)
	if err != nil {
		return crew.Outcome{Reason: "the check could not start: " + e.scrub(err.Error())}
	}
	// A failed write or close cannot change the check's verdict.
	defer func() { _ = log.Close() }()
	_, _ = fmt.Fprintf(log, "\ncrew: running the check: %s\n", c.Command)
	var last lastLine
	err = e.cfg.Checker.Check(ctx, port.Check{
		Dir: c.Dir, Command: c.Command, IssueRef: c.IssueRef, IssueKey: c.IssueKey, IssueURL: c.IssueURL,
		Branch: c.Branch, Output: io.MultiWriter(log, &last),
		Identity: e.cfg.Identities[c.Bot], CodeOwners: e.codeOwners, Bots: e.cfg.BotLogins,
	})
	switch {
	case err == nil:
		return crew.Outcome{Succeeded: true, Reason: "the check passed"}
	case errors.Is(err, port.ErrCheckFailed):
		line := last.String()
		if line == "" {
			return crew.Outcome{Reason: "the check failed and printed nothing"}
		}
		return crew.Outcome{Reason: "the check failed: " + lastWords(e.scrub(line))}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return crew.Outcome{Reason: fmt.Sprintf("the check ran out of time after %s", checkTimeout)}
	case ctx.Err() != nil:
		return crew.Outcome{Reason: "the check was stopped"}
	}
	return crew.Outcome{Reason: "the check could not start: " + e.scrub(err.Error())}
}

// maxLine bounds how much of a check's current line lastLine keeps: the end
// of a line is what a check says last.
const maxLine = 4096

// lastLine is a writer that keeps the last non-empty line written to it,
// trimmed, in bounded memory. One goroutine writes to it at a time.
type lastLine struct {
	cur  []byte // the line being written, cut to its last maxLine bytes
	last string // the last complete non-empty line
}

func (l *lastLine) Write(p []byte) (int, error) {
	for _, b := range p {
		// A carriage return ends a line too, as progress output uses it.
		if b == '\n' || b == '\r' {
			l.end()
			continue
		}
		l.cur = append(l.cur, b)
		if len(l.cur) > 2*maxLine {
			l.cur = append(l.cur[:0], l.cur[len(l.cur)-maxLine:]...)
		}
	}
	return len(p), nil
}

// String returns the last non-empty line, counting an unended last line.
func (l *lastLine) String() string {
	l.end()
	return l.last
}

// end ends the line being written, keeping it when it is not blank. Control
// characters other than tab are dropped: the line becomes a reason that goes
// into a gh argument, which cannot hold a NUL, and into a comment.
func (l *lastLine) end() {
	line := strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(l.cur), ""))
	if line = strings.TrimSpace(line); line != "" {
		l.last = line
	}
	l.cur = l.cur[:0]
}
