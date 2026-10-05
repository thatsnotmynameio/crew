// Package codex is the harness adapter for OpenAI's Codex CLI. It runs
// `codex exec --json` headless in an action's workspace, under Codex's
// automatic approval review in its workspace-write sandbox, and judges the
// session by the end of its turn and its exit code.
package codex

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Compile-time guards. The adapter reports no usage and no last words yet,
// and checks nothing at startup: it has none of the optional capabilities.
var (
	_ port.Harness = (*harness)(nil)
	_ port.Session = (*session)(nil)
)

// settings is the codex adapter's config section: the keys of an agent's
// harness but its name, which are model alone. Without a model, Codex runs
// the one its own config or its default names.
type settings struct {
	Model string `yaml:"model"`
}

// process is what a session needs of a started child; *proc.Process is one.
type process interface {
	Wait() error
	Stop(ctx context.Context) error
}

// spawner starts c with its stdout and stderr copied to the given writers.
// Tests replace it to script codex.
type spawner func(c proc.Command, stdout, stderr io.Writer) (process, error)

// Factory returns the codex harness factory. Its section is an agent's
// harness without its name: model, which is optional. Each session runs
// through group, in its own process group, so a forced exit kills it, and
// so does the git call that finds its git dirs.
func Factory(group *proc.Group) port.HarnessFactory {
	return func(decode port.Decode) (port.Harness, error) {
		var s settings
		if err := decode(&s); err != nil {
			return nil, err
		}
		return &harness{model: s.Model, spawn: groupSpawner(group), git: group.Run}, nil
	}
}

// groupSpawner starts processes through group.
func groupSpawner(group *proc.Group) spawner {
	return func(c proc.Command, stdout, stderr io.Writer) (process, error) {
		started, err := group.Start(c, stdout, stderr)
		if err != nil {
			return nil, err // never a nil *proc.Process inside a non-nil interface
		}
		return started, nil
	}
}

// harness starts codex sessions with one model, or Codex's own when empty.
type harness struct {
	model string
	spawn spawner
	git   proc.Runner
}

// Start implements port.Harness. It finds the git dirs of run.Dir, then runs
// codex there with the harness's model, acting as run.Identity. Everything
// codex prints goes to run.Output, and also to a recorder as it is printed,
// so the verdict never re-reads the log.
func (h *harness) Start(ctx context.Context, run port.Run) (port.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binary, err)
	}
	dirs, err := gitDirs(ctx, h.git, run.Dir)
	if err != nil {
		return nil, err
	}
	log := &logWriter{w: run.Output}
	rec := newRecorder()
	p, err := h.spawn(command(run, h.model, dirs), io.MultiWriter(log, rec.stdout()), io.MultiWriter(log, rec.stderr()))
	if err != nil {
		return nil, err
	}
	s := &session{process: p, done: make(chan struct{})}
	s.verdict = sync.OnceValue(func() crew.Outcome {
		defer close(s.done)
		exit := p.Wait() // codex's output is fully copied once it returns
		rec.end()
		return rec.judge(exit, s.stopped.Load())
	})
	go s.verdict() // judged as soon as codex ends, so a later Stop cannot change it
	return s, nil
}

// session is a running codex process.
type session struct {
	process process
	stopped atomic.Bool
	verdict func() crew.Outcome // waits for the process once, then judges it
	done    chan struct{}       // closed once the verdict is settled
}

// Wait implements port.Session.
func (s *session) Wait() crew.Outcome { return s.verdict() }

// Stop implements port.Session. proc sends the terminate signal to the
// session's process group, and the kill signal once ctx is done. The
// session's outcome is then a failure saying crew stopped it. Stopping a
// session that already ended does nothing.
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
	return nil
}

// logWriter hands codex's stdout and stderr, copied from two goroutines, to
// the session's log one write at a time, as port.Run.Output requires. It
// never fails: once the log fails, such as on a full disk, it drops the rest,
// since a failed write would end proc's copy of that pipe and leave codex
// blocked on it.
type logWriter struct {
	mu     sync.Mutex
	w      io.Writer
	broken bool
}

// Write implements io.Writer.
func (l *logWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.broken {
		if _, err := l.w.Write(p); err != nil {
			l.broken = true
		}
	}
	return len(p), nil
}
