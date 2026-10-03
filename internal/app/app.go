// Package app wires crew together: it loads the repository's config, builds
// the adapters through the registry, checks the environment, and runs the
// engine with the renderer the output calls for, handling the stop signals
// (KTD4, KTD7). cmd/crew only gathers the process's inputs and calls Run, so
// tests run crew whole with the fakes.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
	"github.com/thatsnotmynameio/crew/internal/registry"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
	"github.com/thatsnotmynameio/crew/internal/ui/tui"
)

// The exit codes of Run.
const (
	// ExitClean is a clean stop.
	ExitClean = 0
	// ExitFailure is a runtime failure, or an exit forced by a second stop
	// request.
	ExitFailure = 1
	// ExitConfig is a config or environment error, reported before any
	// polling and before the TUI starts.
	ExitConfig = 2
)

const (
	// lineQueue is the line renderer's queue capacity, far above the few
	// events crew publishes per poll (KTD6).
	lineQueue = 1024
	// prepareTimeout bounds the environment checks, as the engine bounds
	// every tracker and workspace call, so a hung gh or git cannot hold crew
	// before its first poll.
	prepareTimeout = 10 * time.Minute
)

// Options are what Run needs from the process it runs in.
type Options struct {
	// Registry resolves the config's tracker and harness names.
	Registry registry.Registry
	// Workspace returns the workspace adapter for the repository at root.
	Workspace func(root string) port.Workspace
	// Checker runs the actions' checks; nil fails every action that has a
	// check.
	Checker port.Checker
	// Root is the repository's absolute root, where .crew/ lives.
	Root string
	// Home is the user's home directory, shortened to ~ in failure reports;
	// empty shortens nothing.
	Home string
	// Stdin is where the TUI reads keys; nil means the standard input, or
	// the terminal when the standard input is not one.
	Stdin io.Reader
	// Stdout receives the TUI or the event lines; Stderr receives errors.
	Stdout, Stderr io.Writer
	// Terminal tells whether Stdout is a terminal. The TUI runs only on a
	// terminal and without Plain; otherwise crew prints event lines (KTD7).
	Terminal bool
	// Plain is the --plain flag: event lines even on a terminal.
	Plain bool
	// Group is the process group every adapter starts its processes in, so
	// a forced exit can kill them all (KTD16).
	Group *proc.Group
	// Signals delivers the stop signals, SIGINT, SIGTERM and SIGHUP. The
	// first ends the environment checks, or asks the engine to stop; the
	// second forces the exit.
	Signals <-chan os.Signal
}

// Run runs crew until it stops and returns its exit code. A config or
// environment error is printed to Stderr, and Run returns ExitConfig before
// anything polls or renders. So does a signal during the environment checks,
// or the checks taking over prepareTimeout; as a check's process may still
// run then, Run kills every process in Group first. Otherwise Run returns once the engine has
// stopped, with ExitClean, or ExitFailure when something failed meanwhile.
// A second stop request (a second signal, or a second Ctrl-C or q in the TUI)
// kills every process in Group and returns ExitFailure at once. Any other
// way out that skips the engine's stop sequence, a panic included, also
// kills them first.
func Run(ctx context.Context, o Options) (code int) { //nolint:nonamedreturns // the deferred recover sets the exit code
	defer func() {
		if r := recover(); r != nil {
			o.Group.KillAll()
			o.errorf("internal error: %v\n%s", r, debug.Stack())
			code = ExitFailure
		}
	}()
	eng, err := build(o)
	if err != nil {
		o.errorf("%v", err)
		return ExitConfig
	}
	signalled, err := prepare(ctx, eng, o)
	if err != nil {
		o.errorf("%v", err)
		return ExitConfig
	}
	return run(ctx, eng, o, signalled)
}

// prepare runs the environment checks within prepareTimeout, and a first
// signal meanwhile ends them. It reports whether a signal came, so one that
// came just as the checks succeeded still stops the run. When the checks
// end early, it kills every process in Group, since a check's process may
// outlive its context.
func prepare(ctx context.Context, eng *engine.Engine, o Options) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, prepareTimeout)
	defer cancel()
	got := make(chan bool, 1)
	checked := make(chan struct{})
	go func() {
		select {
		case <-o.Signals:
			cancel()
			got <- true
		case <-checked:
			got <- false
		}
	}()
	err := eng.Prepare(ctx)
	close(checked)
	signalled := <-got
	if err == nil || ctx.Err() == nil {
		return signalled, err
	}
	o.Group.KillAll()
	switch {
	case signalled:
		return true, errors.New("stopped during the environment checks")
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return false, fmt.Errorf("the environment checks timed out after %v: %w", prepareTimeout, err)
	}
	return false, err
}

// build loads the config and builds the engine and its adapters.
func build(o Options) (*engine.Engine, error) {
	cfg, err := config.Load(o.Root)
	if err != nil {
		return nil, err
	}
	states := crew.WorkflowStates(cfg.Workflow)
	tracker, trackerErr := o.Registry.Tracker(cfg.Tracker, cfg.TrackerSection, states, cfg.Extras)
	harness, harnessErr := o.Registry.Harness(cfg.Harness, cfg.HarnessSection)
	if err := errors.Join(trackerErr, harnessErr); err != nil {
		return nil, err
	}
	return engine.New(engine.Config{
		Workflow:          cfg.Workflow,
		MaxParallelIssues: cfg.MaxParallelIssues,
		PollInterval:      cfg.PollInterval,
		RunTimeLimit:      cfg.RunTimeLimit,
		Tracker:           tracker,
		Harness:           harness,
		Workspace:         o.Workspace(o.Root),
		Checker:           o.Checker,
		Root:              o.Root,
		Home:              o.Home,
	}), nil
}

// run runs the engine and the renderer until both have returned, and handles
// the stop signals meanwhile. stopping tells that a first signal came
// already, so the engine stops at once and the next signal forces the exit.
func run(ctx context.Context, eng *engine.Engine, o Options, stopping bool) int {
	r := &runner{eng: eng, o: o, code: ExitClean}
	render := r.renderer()
	if stopping {
		r.stop()
	}
	engineDone := goSafely(func() error { return eng.Run(ctx) })
	rendered := goSafely(render)

	for engineDone != nil || rendered != nil {
		select {
		case <-o.Signals:
			if !r.stopping {
				r.stop()
				continue
			}
			return r.forceExit(rendered)
		case err := <-engineDone:
			engineDone = nil
			r.engineEnded(err)
		case err := <-rendered:
			rendered = nil
			if r.forced.Load() {
				return o.forcedExit()
			}
			r.renderEnded(err)
		}
	}
	return r.code
}

// runner is the state of one run: whether the engine was asked to stop,
// whether the exit was forced, and the exit code so far.
type runner struct {
	eng      *engine.Engine
	o        Options
	forced   atomic.Bool
	stopping bool
	// quit, when set, ends the renderer early; the line renderer needs no
	// such end, as it holds no terminal state.
	quit func()
	code int
}

// renderer returns the TUI on a terminal without Plain, and the event lines
// otherwise.
func (r *runner) renderer() func() error {
	if r.o.Terminal && !r.o.Plain {
		model := tui.New(r.eng.SubscribeLatest(), r.eng.Stop, r.force, time.Now, time.Local)
		program := tui.NewProgram(model, r.o.Stdin, r.o.Stdout)
		r.quit = program.Quit
		return program.Run
	}
	queue := r.eng.SubscribeQueue(lineQueue)
	return func() error { return lines.Run(queue, r.o.Stdout, time.Local, time.Now) }
}

// stop asks the engine to stop.
func (r *runner) stop() {
	r.stopping = true
	r.eng.Stop()
}

// force kills every process in Group and marks the exit as forced.
func (r *runner) force() {
	r.forced.Store(true)
	r.o.Group.KillAll()
}

// forceExit forces the exit. When the TUI still runs, it waits for it to
// end, so the terminal is restored first.
func (r *runner) forceExit(rendered <-chan error) int {
	r.force()
	if r.quit != nil && rendered != nil {
		r.quit()
		<-rendered // the TUI has restored the terminal
	}
	return r.o.forcedExit()
}

// engineEnded handles the engine's return.
func (r *runner) engineEnded(err error) {
	if err == nil {
		return
	}
	// The engine did not finish its stop sequence, so its sessions may
	// still run.
	r.o.Group.KillAll()
	r.o.errorf("the engine failed: %v", err)
	r.code = ExitFailure
}

// renderEnded handles the renderer's return before a forced exit: a
// renderer that failed stops the engine.
func (r *runner) renderEnded(err error) {
	if err == nil {
		return
	}
	r.o.errorf("%v; stopping", err)
	r.code = ExitFailure
	r.stop()
}

func (o Options) forcedExit() int {
	o.errorf("forced exit; every process crew started was killed")
	return ExitFailure
}

// errorf prints an error line to Stderr. A failing Stderr leaves crew no
// better place to say so, so its error is dropped.
func (o Options) errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(o.Stderr, "crew: "+format+"\n", args...)
}

// goSafely runs f in a goroutine and delivers its result, or its panic as an
// error, on the returned channel. The channel is buffered, so the goroutine
// ends even when nobody receives.
func goSafely(f func() error) <-chan error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		done <- f()
	}()
	return done
}
