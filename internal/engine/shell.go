package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// stepKey identifies the shell or function step at index step of the
// route of one rule run, which is no action of it.
type stepKey struct {
	run  crew.RuleRunID
	step int
}

// runShell returns the goroutine that runs c's shell action and posts its
// end. The script's context is made here, in the loop, so a StopShell that
// follows always finds it.
func (e *Engine) runShell(ctx context.Context, c core.RunShell) func() {
	scriptCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	e.shells[sessionKey{c.Run, c.Action}] = cancel
	return func() {
		defer cancel()
		outcome := e.runScript(scriptCtx, c.IssueID, c.Script, "the shell action "+string(c.Action), true)
		e.post(core.ShellEnded{IssueID: c.IssueID, Run: c.Run, Action: c.Action, Outcome: outcome})
	}
}

// runStepShell returns the goroutine that runs c's route step and posts
// its end, as runShell does. A step's reason is in crew's words only,
// without what the script printed (R49).
func (e *Engine) runStepShell(ctx context.Context, c core.RunStepShell) func() {
	scriptCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	e.steps[stepKey{c.Run, c.Step}] = cancel
	return func() {
		defer cancel()
		outcome := e.runScript(scriptCtx, c.IssueID, c.Script, "the route's shell step "+string(c.Script.Name), false)
		e.post(core.StepShellEnded{IssueID: c.IssueID, Run: c.Run, Step: c.Step, Outcome: outcome})
	}
}

// stopScript ends the script or function running holds under key. The
// core asks to stop only scripts and functions it started, and their end
// still arrives through their own goroutine.
func stopScript[K comparable](running map[K]context.CancelFunc, key K) {
	if cancel, ok := running[key]; ok {
		cancel()
	}
}

// runScript runs s, the script of a shell action or a route step that
// subject names, within shellTimeout, and returns how it ended (R8). Its
// output goes to the run's log, after a marker line naming it: the log of
// the run's workspace, or for a run without one the log of the workspace
// it would have, the script then running in an empty temporary directory
// the engine removes (KTD9). It acts as s's bot, and reads the latest
// session's prompt and last message, as that session left them beside the
// log (KTD22, KTD-S13). The outcome's reason says, in crew's words, how
// the script ended, followed, when withLine is set and the script exited,
// by the last line it printed (R49).
func (e *Engine) runScript(
	ctx context.Context, issue crew.IssueID, s core.Script, subject string, withLine bool,
) crew.ShellOutcome {
	notStarted := func(err error) crew.ShellOutcome {
		return crew.ShellOutcome{Reason: crew.NewShellReason(subject + " could not start: " + e.scrubAndStrip(err.Error()))}
	}
	if e.cfg.Shell == nil {
		return notStarted(errors.New("crew has no shell to run it"))
	}
	logRel := scriptLog(issue, s)
	prompt, last, err := e.session(logRel, s.Session)
	if err != nil {
		return notStarted(err)
	}
	dir, cleanup, err := scriptDir(s.Dir)
	if err != nil {
		return notStarted(err)
	}
	defer cleanup()
	log, err := e.openLog(logRel)
	if err != nil {
		return notStarted(err)
	}
	// A failed write or close cannot change how the script ended.
	defer func() { _ = log.Close() }()
	_, _ = fmt.Fprintf(log, "\ncrew: running %s: %s\n", subject, s.Command)
	var line lastLine
	ran, err := e.cfg.Shell.Run(ctx, port.Script{
		Dir: dir, Name: s.Name, Command: s.Command, Session: s.Session, Prompt: prompt, LastMessage: last,
		IssueRef: s.IssueRef, IssueID: issue, IssueURL: s.IssueURL, Branch: s.Branch,
		Output: io.MultiWriter(log, &line), Identity: e.cfg.Identities[s.Bot],
		CodeOwners: e.codeOwners, Bots: e.cfg.BotLogins,
	})
	switch {
	case err == nil:
		said := exited(subject, ran.Status)
		if withLine {
			said = e.saying(said, line.String())
		}
		return crew.ShellOutcome{Status: crew.Some(ran.Status), Reason: crew.NewShellReason(said)}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		said := fmt.Sprintf("%s ran out of time after %s", subject, shellTimeout)
		return crew.ShellOutcome{Reason: crew.NewShellReason(said)}
	case ctx.Err() != nil:
		return crew.ShellOutcome{Reason: crew.NewShellReason(subject + " was stopped")}
	}
	return notStarted(err)
}

// scriptLog returns the repository-relative path of the log s writes
// into: its run's, or for a run without a workspace the log of the
// workspace the run of s's rule on issue would have (KTD-S13).
func scriptLog(issue crew.IssueID, s core.Script) string {
	if s.Log != "" {
		return s.Log
	}
	return logPath(port.WorkspaceBase(issue, s.Rule))
}

// scriptDir returns the directory a script runs in, dir, and what removes
// it afterwards: nothing for a workspace, and for an empty dir, a run
// without a workspace, a new empty temporary directory (KTD9).
func scriptDir(dir string) (string, func(), error) {
	if dir != "" {
		return dir, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "crew-run-")
	if err != nil {
		return "", nil, fmt.Errorf("create the script's directory: %w", err)
	}
	// What a failed removal leaves is in the system's temporary directory.
	return tmp, func() { _ = os.RemoveAll(tmp) }, nil
}

// exited says, in crew's words, that the script subject names exited with
// status, -1 being a signal crew did not send.
func exited(subject string, status int) string {
	if status < 0 {
		return subject + " was killed by a signal"
	}
	return fmt.Sprintf("%s exited with status %d", subject, status)
}

// saying returns said, followed by line, the last line a script printed,
// scrubbed, stripped, scrubbed again (scrubAndStrip) and cut, when the
// script printed one.
func (e *Engine) saying(said, line string) string {
	if line == "" {
		return said
	}
	return said + ": " + lastWords(e.scrubAndStrip(line))
}

// maxLine bounds how much of a script's current line lastLine keeps: the
// end of a line is what a script says last.
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
