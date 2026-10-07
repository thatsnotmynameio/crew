// Package claude is the harness adapter for Claude Code. It runs `claude -p`
// headless in an action's workspace and tells how the session ended from the
// stream-json events it prints: the session succeeded when its last result
// event is not an error and the process exited 0.
package claude

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// stoppedReason is the SessionEnd.Reason of a session ended by Stop.
const stoppedReason = "stopped by crew before the session ended"

// Compile-time guards: the engine finds Preparer, Narrator, UsageReporter
// and LastMessageReporter by type assertion.
var (
	_ port.Harness             = (*harness)(nil)
	_ port.Preparer            = (*harness)(nil)
	_ port.Session             = (*session)(nil)
	_ port.Narrator            = (*session)(nil)
	_ port.UsageReporter       = (*session)(nil)
	_ port.LastMessageReporter = (*session)(nil)
)

// settings is the claude adapter's config section: the keys of an agent's
// harness but its name, which are model alone. Without a model, Claude Code
// picks it: the one set in the user's own settings, or its default.
type settings struct {
	Model string `yaml:"model"`
}

// process is what a session needs of a started child; *proc.Process is one.
type process interface {
	Wait() error
	Stop(ctx context.Context) error
}

// spawner starts c with its stdout and stderr copied to the given writers,
// as (*proc.Group).Start does. Tests replace it to script claude.
type spawner func(c proc.Command, stdout, stderr io.Writer) (process, error)

// Factory returns the claude harness factory. Its section is an agent's
// harness without its name: model, which is optional.
// The harness it builds is a port.Preparer that checks claude is on PATH.
// Every session runs through group, in its own process group, so a forced
// exit kills it.
func Factory(group *proc.Group) port.HarnessFactory {
	spawn := func(c proc.Command, stdout, stderr io.Writer) (process, error) {
		p, err := group.Start(c, stdout, stderr)
		if err != nil {
			return nil, err // never a nil *proc.Process inside a non-nil interface
		}
		return p, nil
	}
	return func(decode port.Decode) (port.Harness, error) {
		var s settings
		if err := decode(&s); err != nil {
			return nil, err
		}
		return &harness{model: s.Model, spawn: spawn}, nil
	}
}

// harness starts claude sessions with one model, or Claude Code's own when
// empty.
type harness struct {
	model string
	spawn spawner
}

// Prepare implements port.Preparer: it checks that claude is on PATH, and
// reports that step on ctx.
func (h *harness) Prepare(ctx context.Context, _ []crew.State) error {
	port.Step(ctx, "looking for claude on PATH")
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("the claude harness runs the %s CLI, which is not on PATH: %w", binary, err)
	}
	return nil
}

// Start implements port.Harness. It runs claude in run.Dir with the
// harness's model, acting as run.Identity. Everything claude prints, stdout
// and stderr, goes to run.Output, and stdout also goes through the stream
// parser as it is printed, so the session's end never re-reads the log.
func (h *harness) Start(ctx context.Context, run port.Run) (port.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binary, err)
	}
	out := &serialWriter{w: run.Output}
	events := &stream{}
	p, err := h.spawn(command(run, h.model), io.MultiWriter(out, events), out)
	if err != nil {
		return nil, err
	}
	s := &session{process: p, events: events, done: make(chan struct{})}
	go s.reap()
	return s, nil
}

// session is a running claude process.
type session struct {
	process process
	events  *stream // claude's stdout, parsed as it is printed
	stopped atomic.Bool
	end     port.SessionEnd // set before done is closed
	usage   crew.Usage      // set before done is closed
	last    string          // the last result's text; set before done is closed
	done    chan struct{}   // closed once the process is reaped and its end settled
}

// Wait implements port.Session.
func (s *session) Wait() port.SessionEnd {
	<-s.done
	return s.end
}

// Said implements port.Narrator: it returns the last text block of the last
// top-level assistant event so far, so what a subagent says never counts.
func (s *session) Said() string {
	return s.events.said()
}

// Usage implements port.UsageReporter: the cost, tokens, turns and models
// the session's result events report, or nothing when crew stopped it, a
// signal ended it, or it printed no result.
func (s *session) Usage() crew.Usage {
	<-s.done
	return s.usage
}

// LastMessage implements port.LastMessageReporter: the text of the last
// top-level result event, as claude wrote it, or "" when there was none.
func (s *session) LastMessage() string {
	<-s.done
	return s.last
}

// Stop implements port.Session. proc sends the terminate signal to the
// session's process group, and the kill signal once ctx is done. The
// session's end is then a failure saying it was stopped.
func (s *session) Stop(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	default:
	}
	s.stopped.Store(true)
	if err := s.process.Stop(ctx); err != nil {
		return fmt.Errorf("stop %s: %w", binary, err)
	}
	<-s.done
	return nil
}

// reap waits for the process, whose output is fully copied once Wait
// returns, settles how it ended, keeps its last message and reads its usage.
// A session crew stopped, or one a signal ended, reports no usage.
func (s *session) reap() {
	err := s.process.Wait()
	last := s.events.end()
	s.end = sessionEnd(last, err)
	if last != nil {
		s.last = last.Result
	}
	switch {
	case s.stopped.Load():
		s.end = port.SessionEnd{Reason: stoppedReason}
	case !signaled(err):
		s.usage = s.events.usage()
	}
	close(s.done)
}

// serialWriter passes writes to w one at a time, because stdout and stderr
// are copied from two goroutines and port.Run.Output takes one at a time.
//
// It never fails: once w fails, such as on a full disk, it drops the rest.
// A failed write would end proc's copy of that pipe, and claude, blocked
// writing to a pipe nobody reads, would hang until stopped.
type serialWriter struct {
	mu  sync.Mutex
	w   io.Writer
	err error // w's first error; nothing is written after it
}

// Write implements io.Writer.
func (s *serialWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		_, s.err = s.w.Write(p)
	}
	return len(p), nil
}
