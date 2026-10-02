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

// lineQueue is the line renderer's queue capacity, far above the few events
// crew publishes per poll (KTD6).
const lineQueue = 1024

// Options are what Run needs from the process it runs in.
type Options struct {
	// Registry resolves the config's tracker and harness names.
	Registry registry.Registry
	// Workspace returns the workspace adapter for the repository at root.
	Workspace func(root string) port.Workspace
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
	// Signals delivers the stop signals, SIGINT and SIGTERM. The first asks
	// the engine to stop; the second forces the exit.
	Signals <-chan os.Signal
}

// Run runs crew until it stops and returns its exit code. A config or
// environment error is printed to Stderr, and Run returns ExitConfig before
// anything polls or renders. Otherwise Run returns once the engine has
// stopped, with ExitClean, or ExitFailure when something failed meanwhile.
// A second stop request (a second signal, or a second Ctrl-C or q in the TUI)
// kills every process in Group and returns ExitFailure at once. Any other
// way out that skips the engine's stop sequence, a panic included, also
// kills them first.
func Run(ctx context.Context, o Options) (code int) {
	defer func() {
		if r := recover(); r != nil {
			o.Group.KillAll()
			o.errorf("internal error: %v\n%s", r, debug.Stack())
			code = ExitFailure
		}
	}()
	eng, err := build(o)
	if err == nil {
		err = eng.Prepare(ctx)
	}
	if err != nil {
		o.errorf("%v", err)
		return ExitConfig
	}
	return run(ctx, eng, o)
}

// build loads the config and builds the engine and its adapters.
func build(o Options) (*engine.Engine, error) {
	cfg, err := config.Load(o.Root)
	if err != nil {
		return nil, err
	}
	tracker, trackerErr := o.Registry.Tracker(cfg.Tracker, cfg.TrackerSection)
	harness, harnessErr := o.Registry.Harness(cfg.Harness, cfg.HarnessSection)
	if err := errors.Join(trackerErr, harnessErr); err != nil {
		return nil, err
	}
	return engine.New(engine.Config{
		Workflow:          cfg.Workflow,
		MaxParallelIssues: cfg.MaxParallelIssues,
		PollInterval:      cfg.PollInterval,
		Tracker:           tracker,
		Harness:           harness,
		Workspace:         o.Workspace(o.Root),
		Root:              o.Root,
		Home:              o.Home,
	}), nil
}

// run runs the engine and the renderer until both have returned, and handles
// the stop signals meanwhile.
func run(ctx context.Context, eng *engine.Engine, o Options) int {
	var forced atomic.Bool
	force := func() {
		forced.Store(true)
		o.Group.KillAll()
	}

	// quit, when set, ends the renderer early; the line renderer needs no
	// such end, as it holds no terminal state.
	var render func() error
	var quit func()
	if o.Terminal && !o.Plain {
		program := tui.NewProgram(tui.New(eng.SubscribeLatest(), eng.Stop, force, time.Now, time.Local), o.Stdin, o.Stdout)
		render, quit = program.Run, program.Quit
	} else {
		queue := eng.SubscribeQueue(lineQueue)
		render = func() error { return lines.Run(queue, o.Stdout, time.Local, time.Now) }
	}
	engineDone := goSafely(func() error { return eng.Run(ctx) })
	rendered := goSafely(render)

	code, stopping := ExitClean, false
	for engineDone != nil || rendered != nil {
		select {
		case <-o.Signals:
			if !stopping {
				stopping = true
				eng.Stop()
				continue
			}
			force()
			if quit != nil && rendered != nil {
				quit()
				<-rendered // the TUI has restored the terminal
			}
			return o.forcedExit()
		case err := <-engineDone:
			engineDone = nil
			if err != nil {
				// The engine did not finish its stop sequence, so its
				// sessions may still run.
				o.Group.KillAll()
				o.errorf("the engine failed: %v", err)
				code = ExitFailure
			}
		case err := <-rendered:
			rendered = nil
			if forced.Load() {
				return o.forcedExit()
			}
			if err != nil {
				o.errorf("%v; stopping", err)
				code = ExitFailure
				stopping = true
				eng.Stop()
			}
		}
	}
	return code
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
