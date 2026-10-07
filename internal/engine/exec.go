package engine

import (
	"cmp"
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
	issue  crew.IssueID
	action crew.ActionName
}

// compare orders session keys by repository, issue key and action.
func (k sessionKey) compare(o sessionKey) int {
	return cmp.Or(k.issue.Compare(o.issue), cmp.Compare(k.action, o.action))
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
		s, ok := e.sessions[sessionKey{c.IssueID, c.Action}]
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
		e.checks[sessionKey{c.IssueID, c.Action}] = cancel
		return func() { e.runCheck(checkCtx, cancel, c) }
	case core.StopCheck:
		// The core asks to stop only checks it started; the check's end
		// still arrives through its own goroutine, in runCheck.
		if cancel, ok := e.checks[sessionKey{c.IssueID, c.Action}]; ok {
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
	for i := range issues {
		issues[i].ID.Repository = e.repository.ID
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
		issues[i].Issue.ID.Repository = e.repository.ID
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
	e.post(core.StatusResult{IssueID: c.Status.IssueID, Result: result, Reason: reason})
}

// reportPullRequests shows a report on the issue's pull requests through the
// tracker's PullRequestReporter, which the core asks for only when the
// tracker has one.
func (e *Engine) reportPullRequests(ctx context.Context, c core.ReportPullRequests) {
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.pullRequests.ReportPullRequests(ctx, c.Report)
	result, reason := e.classify(ctx, err)
	e.post(core.PullRequestsResult{IssueID: c.Report.IssueID, Result: result, Reason: reason})
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
	space, err := e.cfg.Workspace.Create(ctx, c.Issue, c.Action)
	if err != nil {
		e.post(core.WorkspaceFailed{IssueID: c.Issue.ID, Action: c.Action, Reason: e.sessionText(callError(ctx, err))})
		return
	}
	e.post(e.ready(c.Issue.ID, c.Action, space, false))
}

// reopenWorkspace reopens a failed run's workspace through the workspace's
// port.Reopener, which the core asks for only when the workspace has one.
func (e *Engine) reopenWorkspace(ctx context.Context, c core.ReopenWorkspace) {
	r, ok := e.cfg.Workspace.(port.Reopener)
	if !ok {
		e.post(core.WorkspaceGone{IssueID: c.IssueID, Action: c.Action})
		return
	}
	ctx, cancel := callContext(ctx)
	defer cancel()
	space, err := r.Reopen(ctx, port.Space{Name: c.Workspace, Branch: c.Branch})
	switch {
	case errors.Is(err, port.ErrWorkspaceGone):
		e.post(core.WorkspaceGone{IssueID: c.IssueID, Action: c.Action})
	case err != nil:
		e.post(core.WorkspaceFailed{IssueID: c.IssueID, Action: c.Action, Reason: e.sessionText(callError(ctx, err))})
	default:
		e.post(e.ready(c.IssueID, c.Action, space, true))
	}
}

// ready is the WorkspaceReady of space for action on the issue id.
func (e *Engine) ready(id crew.IssueID, action crew.ActionName, space port.Space, resumed bool) core.WorkspaceReady {
	log := logPath(space.Name)
	return core.WorkspaceReady{
		IssueID: id, Action: action,
		Workspace: space.Name, Dir: space.Dir, Branch: space.Branch, Log: log,
		LogFromDir: e.logFromDir(space.Dir, log), Resumed: resumed,
	}
}

// startSession starts the session with its output going to its log, after
// a marker line when the session resumes a failed run (KTD8), then waits for
// it to end in the same goroutine (R19). The session acts as its action's
// bot, or as you when that bot does not act, and learns the code owners' and
// the bots' logins.
func (e *Engine) startSession(ctx context.Context, c core.StartSession) {
	log, err := e.openLog(c.Log)
	if err == nil && c.Resumed {
		if err = markResumed(log); err != nil {
			_ = log.Close() // the write error is the one to report
		}
	}
	if err != nil {
		e.post(core.SessionFailedToStart{IssueID: c.IssueID, Action: c.Action, Reason: e.sessionText(err.Error())})
		return
	}
	s, err := e.harnesses[c.Agent].Start(ctx, port.Run{
		Dir: c.Dir, Prompt: c.Prompt, Output: log,
		Identity: e.cfg.Identities[c.Bot], CodeOwners: e.codeOwners, Bots: e.cfg.BotLogins,
	})
	if err != nil {
		_ = log.Close() // nothing was written to it worth keeping
		e.post(core.SessionFailedToStart{IssueID: c.IssueID, Action: c.Action, Reason: e.sessionText(err.Error())})
		return
	}
	e.inbox <- message{input: core.SessionStarted{IssueID: c.IssueID, Action: c.Action}, session: s}
	verdict := s.Wait()
	// The harness stops writing once Wait returns. A failed close cannot
	// change the session's verdict, which is what the core needs.
	_ = log.Close()
	outcome := crew.Outcome{Succeeded: verdict.Succeeded, Reason: e.sessionText(verdict.Reason)}
	var usage crew.Usage
	if r, ok := s.(port.UsageReporter); ok {
		usage = r.Usage()
	}
	var last string
	if r, ok := s.(port.LastMessageReporter); ok {
		last = r.LastMessage()
	}
	e.post(core.SessionEnded{IssueID: c.IssueID, Action: c.Action, Outcome: outcome, Usage: usage, LastMessage: last})
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
	e.post(core.PullRequestFound{IssueID: c.IssueID, Action: c.Action, PullRequest: pr})
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

// runCheck runs one of the action's checks within checkTimeout, its output
// going to the action's log after the session's, and posts its verdict. Its
// reason says, in crew's words and naming the check, how the check ended,
// followed, for a check that passed or failed, by the last line it printed
// (R5, R6).
func (e *Engine) runCheck(ctx context.Context, cancel context.CancelFunc, c core.RunCheck) {
	defer cancel()
	passed, reason := e.check(ctx, c)
	e.post(core.CheckEnded{IssueID: c.IssueID, Action: c.Action, Passed: passed, Reason: reason})
}

// check runs c, as its action's bot like its session, and returns whether
// it passed and its reason. The session's last message reaches the check as
// the session wrote it (KTD10): crew does not show it.
func (e *Engine) check(ctx context.Context, c core.RunCheck) (bool, crew.CheckReason) {
	subject := "the check " + string(c.Name)
	if e.cfg.Checker == nil {
		return false, crew.NewCheckReason(subject + " could not start: crew has no check runner")
	}
	log, err := e.openLog(c.Log)
	if err != nil {
		return false, crew.NewCheckReason(subject + " could not start: " + e.scrubAndStrip(err.Error()))
	}
	// A failed write or close cannot change the check's verdict.
	defer func() { _ = log.Close() }()
	_, _ = fmt.Fprintf(log, "\ncrew: running %s: %s\n", subject, c.Command)
	var last lastLine
	err = e.cfg.Checker.Check(ctx, port.Check{
		Dir: c.Dir, Name: c.Name, Command: c.Command, Action: c.Action, Prompt: c.Prompt, LastMessage: c.LastMessage,
		IssueRef: c.IssueRef, IssueID: c.IssueID, IssueURL: c.IssueURL,
		Branch: c.Branch, Output: io.MultiWriter(log, &last),
		Identity: e.cfg.Identities[c.Bot], CodeOwners: e.codeOwners, Bots: e.cfg.BotLogins,
	})
	switch {
	case err == nil:
		return true, e.saying(subject+" passed", last.String())
	case errors.Is(err, port.ErrCheckFailed):
		line := last.String()
		if line == "" {
			return false, crew.NewCheckReason(subject + " failed and printed nothing")
		}
		return false, e.saying(subject+" failed", line)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return false, crew.NewCheckReason(fmt.Sprintf("%s ran out of time after %s", subject, checkTimeout))
	case ctx.Err() != nil:
		return false, crew.NewCheckReason(subject + " was stopped")
	}
	return false, crew.NewCheckReason(subject + " could not start: " + e.scrubAndStrip(err.Error()))
}

// saying returns verdict, followed by line, the last line a check printed,
// scrubbed, stripped, scrubbed again (scrubAndStrip) and cut, when the check
// printed one.
func (e *Engine) saying(verdict, line string) crew.CheckReason {
	if line == "" {
		return crew.NewCheckReason(verdict)
	}
	return crew.NewCheckReason(verdict + ": " + lastWords(e.scrubAndStrip(line)))
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
