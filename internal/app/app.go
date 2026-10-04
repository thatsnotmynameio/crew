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
	"path/filepath"
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
	// Stdout receives the boot log, one line as each step of loading the
	// config and the environment checks starts, then the TUI or the event
	// lines; Stderr receives errors.
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
	// Mates makes the mates the config names act: names lists each once,
	// the default mate def first. It runs among the environment checks, on
	// their context, and only when the config names a mate. nil makes such
	// a config an environment error.
	Mates func(ctx context.Context, def string, names []string) (Mates, error)
}

// Mates are the mates that act this run, as Options.Mates made them.
type Mates struct {
	// Identities are the identities of the mates that act, by name: what
	// their actions' sessions and checks act as. A mate that cannot act has
	// none, and its actions act as the boss.
	Identities map[string]port.Identity
	// Writer is what crew's own writes on the tracker act as: the default
	// mate, or the zero Identity, the boss, when it cannot act.
	Writer port.Identity
	// Logins are the logins of the configured mates crew knows, whether or
	// not they act this run: crew takes the issues they opened, and every
	// session and check gets them as CREW_MATES.
	Logins []string
	// Warnings say, one line each, which mate cannot act or adds no
	// co-author, why, and the fix.
	Warnings []string
	// Unable holds, by name, the short reason of each configured mate that
	// cannot act this run, such as "no key"; nil when every mate acts.
	Unable map[string]string
	// Failing returns, by name, the warning of each mate whose last token
	// renewal failed; the engine reads it while it runs. nil reads none.
	Failing func() map[string]string
	// Close stops renewing the mates' tokens and removes them. nil does
	// nothing.
	Close func()
}

// Run runs crew until it stops and returns its exit code. It first prints
// the boot log to Stdout, whatever renders after it, so its last line names
// the step crew is waiting on, or the one that failed. A config or
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
	o.boot("loading .crew/config.yaml")
	b, err := build(o)
	if err != nil {
		o.errorf("%v", err)
		return ExitConfig
	}
	var eng *engine.Engine
	var mates Mates
	signalled, err := prepare(port.WithSteps(ctx, o.boot), o, func(ctx context.Context) error {
		if mates, err = b.mates(ctx, o); err != nil {
			return err
		}
		eng = b.engine(o, mates)
		return eng.Prepare(ctx)
	})
	if mates.Close != nil {
		// After the engine's stop sequence, on every way out of Run.
		defer mates.Close()
	}
	if err != nil {
		o.errorf("%v", err)
		return ExitConfig
	}
	return run(ctx, eng, o, signalled, b.cfg, mates.Warnings)
}

// prepare runs the environment checks, check, within prepareTimeout, and a
// first signal meanwhile ends them. It reports whether a signal came, so one
// that came just as the checks succeeded still stops the run. When the
// checks end early, it kills every process in Group, since a check's process
// may outlive its context.
func prepare(ctx context.Context, o Options, check func(context.Context) error) (bool, error) {
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
	err := check(ctx)
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

// built is the config and the adapters build made.
type built struct {
	cfg     *config.Config
	tracker port.Tracker
	harness port.Harness
}

// build loads the config and builds its adapters. A board needs a tracker
// that lists issues by any label, a port.BoardLister (KTD3).
func build(o Options) (built, error) {
	cfg, err := config.Load(o.Root)
	if err != nil {
		return built{}, err
	}
	states := crew.WorkflowStates(cfg.Workflow)
	tracker, trackerErr := o.Registry.Tracker(cfg.Tracker, cfg.TrackerSection, states, cfg.Extras)
	harness, harnessErr := o.Registry.Harness(cfg.Harness, cfg.HarnessSection)
	if err := errors.Join(trackerErr, harnessErr); err != nil {
		return built{}, err
	}
	if _, ok := tracker.(port.BoardLister); len(cfg.Board) > 0 && !ok {
		return built{}, fmt.Errorf("board: tracker %q cannot list issues by any label", cfg.Tracker)
	}
	return built{cfg: cfg, tracker: tracker, harness: harness}, nil
}

// mates makes the mates the config names act, through Options.Mates, and
// none when it names none.
func (b built) mates(ctx context.Context, o Options) (Mates, error) {
	if len(b.cfg.Mates) == 0 {
		return Mates{}, nil
	}
	if o.Mates == nil {
		return Mates{}, errors.New("the config names mates, and crew cannot make them act here")
	}
	m, err := o.Mates(ctx, b.cfg.Mate, b.cfg.Mates)
	if err != nil {
		return Mates{}, fmt.Errorf("make the mates act: %w", err)
	}
	return m, nil
}

// engine builds the engine of the config and its adapters, whose actions
// act as mates.
func (b built) engine(o Options, mates Mates) *engine.Engine {
	return engine.New(engine.Config{
		Workflow:          b.cfg.Workflow,
		MaxParallelIssues: b.cfg.MaxParallelIssues,
		PollInterval:      b.cfg.PollInterval,
		RunTimeLimit:      b.cfg.RunTimeLimit,
		UsageInStatus:     b.cfg.UsageInStatus,
		Tracker:           b.tracker,
		Harness:           b.harness,
		Workspace:         o.Workspace(o.Root),
		Checker:           o.Checker,
		Root:              o.Root,
		Home:              o.Home,
		ActAs:             len(b.cfg.Mates) > 0,
		Writer:            mates.Writer,
		Identities:        mates.Identities,
		MateLogins:        mates.Logins,
		DefaultMate:       b.cfg.Mate,
		Mates:             b.cfg.Mates,
		Unable:            mates.Unable,
		MateFailures:      mates.Failing,
		Board:             b.cfg.Board,
		Extras:            b.cfg.Extras,
	})
}

// run runs the engine and the renderer until both have returned, and handles
// the stop signals meanwhile. stopping tells that a first signal came
// already, so the engine stops at once and the next signal forces the exit.
// The renderer shows warnings before anything else.
func run(
	ctx context.Context, eng *engine.Engine, o Options, stopping bool, cfg *config.Config, warnings []string,
) int {
	r := &runner{eng: eng, o: o, code: ExitClean, warnings: warnings, workflow: cfg.Workflow, board: cfg.Board}
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
	// warnings are the startup warnings the renderer shows.
	warnings []string
	// workflow is the configured stages, for the live view's board.
	workflow []crew.Stage
	// board is the board the config draws; nil draws the stages.
	board []crew.BoardColumn
}

// renderer returns the TUI on a terminal without Plain, and the event lines
// otherwise.
func (r *runner) renderer() func() error {
	if r.o.Terminal && !r.o.Plain {
		model := tui.New(tui.Config{
			Updates: r.eng.SubscribeLatest(), Stop: r.eng.Stop, Force: r.force, Now: time.Now, Location: time.Local,
			Workflow: r.workflow, Board: r.board, Repository: filepath.Base(r.o.Root), Warnings: r.warnings,
		})
		program := tui.NewProgram(model, r.o.Stdin, r.o.Stdout)
		r.quit = program.Quit
		return program.Run
	}
	queue := r.eng.SubscribeQueue(lineQueue)
	return func() error { return lines.Run(queue, r.o.Stdout, time.Local, time.Now, r.warnings...) }
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

// boot prints step as one line of the boot log to Stdout, stamped with the
// local time. A failing Stdout fails the renderer's first write, which stops
// crew, so its error is dropped here.
func (o Options) boot(step string) {
	_ = lines.Line(o.Stdout, time.Now(), step)
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
