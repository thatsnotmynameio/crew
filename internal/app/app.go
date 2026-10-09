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
	"slices"
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
	// Registry resolves the config's tracker name and its agents' harness
	// names.
	Registry registry.Registry
	// Workspace returns the workspace adapter for the repository at root.
	Workspace func(root string) port.Workspace
	// Shell runs the scripts of the shell actions and route steps; with
	// nil, every script fails to start.
	Shell port.Shell
	// Journal returns the run journal of the repository at root, at
	// engine.JournalPath; nil journals nothing, so no failed run resumes.
	Journal func(root string) port.Journal
	// Root is the repository's absolute root, where .crew/ lives.
	Root string
	// GlobalConfig is the path of the user's global config file, read
	// before the repository's .crew/ files, whose top-level keys replace
	// its keys; empty reads none.
	GlobalConfig string
	// Home is the user's home directory, shortened to ~ in failure reports;
	// empty shortens nothing.
	Home string
	// DataDir is crew's data folder, where the statistics store the config
	// names keeps its data. An empty or relative one is no data folder,
	// which the store reports when it first records.
	DataDir string
	// Version is crew's version, which the recorded crew process carries.
	Version string
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
	// Bots makes the bots the config names act: names lists each once,
	// tracker.bot, def, first when set. It runs among the environment
	// checks, on their context, and only when the config names a bot. nil
	// makes such a config an environment error.
	Bots func(ctx context.Context, def crew.BotName, names []crew.BotName) (Bots, error)
}

// Bots are the bots that act this run, as Options.Bots made them.
type Bots struct {
	// Identities are the identities of the bots that act, by name: what
	// their sessions and the shell actions after them act as. A bot that
	// cannot act has none, and its actions act as you.
	Identities map[crew.BotName]port.Identity
	// Writer is what crew's own writes on the tracker act as: the default
	// bot, or the zero Identity, you, when there is none or it cannot act.
	Writer port.Identity
	// Logins are the logins of the configured bots crew knows, whether or
	// not they act this run: crew takes the issues they opened, and every
	// session and script gets them as CREW_BOTS.
	Logins []string
	// Warnings say, one line each, which bot cannot act or adds no
	// co-author, why, and the fix.
	Warnings []string
	// Unable holds, by name, the short reason of each configured bot that
	// cannot act this run, such as "no key"; nil when every bot acts.
	Unable map[crew.BotName]string
	// Failing returns, by name, the warning of each bot whose last token
	// renewal failed; the engine reads it while it runs. nil reads none.
	Failing func() map[crew.BotName]string
	// Close stops renewing the bots' tokens and removes them. nil does
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
	o.boot("loading config")
	b, err := build(o)
	if err != nil {
		o.errorf("%v", err)
		return ExitConfig
	}
	var eng *engine.Engine
	var bots Bots
	signalled, err := prepare(port.WithSteps(ctx, o.boot), o, func(ctx context.Context) error {
		if bots, err = b.bots(ctx, o); err != nil {
			return err
		}
		if err := questionWriter(b.cfg.Tracker, b.tracker, b.cfg.Questions, bots.Writer); err != nil {
			return err
		}
		eng = b.engine(o, bots)
		return eng.Prepare(ctx)
	})
	if bots.Close != nil {
		// After the engine's stop sequence, on every way out of Run.
		defer bots.Close()
	}
	if err != nil {
		o.errorf("%v", err)
		b.closeStatistics(o)
		return ExitConfig
	}
	return run(ctx, eng, o, signalled, b, bots.Warnings)
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
	cfg       *config.Config
	tracker   port.Tracker
	harnesses []engine.AgentHarness
	functions map[crew.FunctionUse]engine.Function
	// statistics is the statistics store, nil when the config turns
	// recording off.
	statistics port.Statistics
}

// build loads the config and builds its adapters: the tracker, and the
// harness of every agent, each from its section, so a harness name no
// adapter has stops crew whether or not an action names the agent. Only the
// agents some action names keep theirs, so an agent in no use is never
// prepared. A written board needs a tracker that lists issues by any label,
// a port.BoardLister (KTD3); the default board needs none, since the core
// fills it from its listings (KTD10). A route that comments or closes needs
// a tracker that can, a port.Commenter or a port.Closer (KTD7), and a
// session that may wait for answers one that lists comments, a
// port.CommentLister (KTD-W5). A question needs a tracker that comments,
// and questions one that lists comments and delegates, a port.Delegator
// (KTD10). Every function use is built once, from its parameters, so a
// parameter its function refuses stops crew before it polls (R28). The
// statistics store the config names is built in DataDir, and none when
// the config turns recording off (KTD3).
func build(o Options) (built, error) {
	cfg, err := config.Load(o.Root, o.GlobalConfig, o.Registry.Functions())
	if err != nil {
		return built{}, err
	}
	tracker, err := o.Registry.Tracker(cfg.Tracker, cfg.TrackerSection, crew.RuleStates(cfg.Rules))
	errs := []error{err}
	var statistics port.Statistics
	if cfg.Statistics != "" {
		statistics, err = o.Registry.Statistics(cfg.Statistics, cfg.StatisticsSection, o.DataDir)
		errs = append(errs, err)
	}
	var harnesses []engine.AgentHarness
	for _, a := range cfg.Agents {
		harness, err := o.Registry.Harness(a.HarnessKey(), string(a.Harness), a.HarnessSection)
		errs = append(errs, err)
		if err == nil && a.Used {
			harnesses = append(harnesses, engine.AgentHarness{Agent: a.Name, Harness: harness})
		}
	}
	functions, err := buildFunctions(o.Registry, cfg.Functions)
	errs = append(errs, err)
	if err := errors.Join(errs...); err != nil {
		return built{}, err
	}
	if _, ok := tracker.(port.BoardLister); cfg.BoardWritten && !ok {
		return built{}, fmt.Errorf("board: tracker %q cannot list issues by any label", cfg.Tracker)
	}
	if err := errors.Join(routeSteps(cfg.Tracker, tracker, cfg.Rules),
		waitingSessions(cfg.Tracker, tracker, cfg.Rules), questionRule(cfg.Tracker, tracker, cfg.Questions)); err != nil {
		return built{}, err
	}
	return built{cfg: cfg, tracker: tracker, harnesses: harnesses, functions: functions, statistics: statistics}, nil
}

// buildFunctions builds the function of each use through r, from the use's
// parameters as they render for a sample issue, and returns them by use
// with the binding that renders the parameters for each call (KTD-F7). A
// parameter the function refuses is an error at that parameter's line; any
// other error is at the use's (KTD-F11).
func buildFunctions(r registry.Registry, uses []config.FunctionUse) (map[crew.FunctionUse]engine.Function, error) {
	out := make(map[crew.FunctionUse]engine.Function, len(uses))
	var errs []error
	for _, use := range uses {
		f, err := r.Function(string(use.Use), string(use.Function), use.Section)
		if refused, ok := errors.AsType[port.RefusedParameterError](err); ok {
			errs = append(errs, use.Refused(refused.Parameter, refused.Reason))
			continue
		}
		if err != nil {
			errs = append(errs, use.Failed(err))
			continue
		}
		out[use.Use] = engine.Function{Function: f, Bind: use.Bind}
	}
	return out, errors.Join(errs...)
}

// bots makes the bots the config names act, through Options.Bots, and
// none when it names none.
func (b built) bots(ctx context.Context, o Options) (Bots, error) {
	if len(b.cfg.Bots) == 0 {
		return Bots{}, nil
	}
	if o.Bots == nil {
		return Bots{}, errors.New("the config names bots, and crew cannot make them act here")
	}
	m, err := o.Bots(ctx, b.cfg.Bot.Name, b.cfg.BotNames())
	if err != nil {
		return Bots{}, fmt.Errorf("make the bots act: %w", err)
	}
	return m, nil
}

// engine builds the engine of the config and its adapters, whose actions
// act as bots.
func (b built) engine(o Options, bots Bots) *engine.Engine {
	return engine.New(b.engineConfig(o, bots))
}

// engineConfig is what engine hands the engine: the config, its adapters
// and bots, the bots that act.
func (b built) engineConfig(o Options, bots Bots) engine.Config {
	return engine.Config{
		Rules:             b.cfg.Rules,
		MaxParallelIssues: b.cfg.MaxParallelIssues,
		PollInterval:      b.cfg.PollInterval,
		RunTimeLimit:      b.cfg.RunTimeLimit,
		UsageInStatus:     b.cfg.UsageInStatus,
		Tracker:           b.tracker,
		TrackerName:       crew.TrackerName(b.cfg.Tracker),
		Harnesses:         b.harnesses,
		Functions:         b.functions,
		Workspace:         o.Workspace(o.Root),
		Shell:             o.Shell,
		Journal:           o.journal(),
		Statistics:        b.statistics,
		Version:           o.Version,
		Root:              o.Root,
		Home:              o.Home,
		ActAs:             len(b.cfg.Bots) > 0,
		Writer:            bots.Writer,
		Identities:        bots.Identities,
		BotLogins:         bots.Logins,
		AnsweringApps:     answeringApps(b.cfg, bots),
		Answerer:          answerer(b.cfg),
		DefaultBot:        b.cfg.Bot.Name,
		Bots:              b.cfg.BotNames(),
		Unable:            bots.Unable,
		BotFailures:       bots.Failing,
		Board:             b.cfg.Board,
		BoardWritten:      b.cfg.BoardWritten,
	}
}

// answeringApps returns the answering list: the config's answering_apps
// when it writes them, [] included, and otherwise the logins of crew's bots
// (R38, KTD-W4).
func answeringApps(cfg *config.Config, bots Bots) []string {
	if cfg.AnsweringAppsWritten {
		return slices.Clone(cfg.AnsweringApps)
	}
	return slices.Clone(bots.Logins)
}

// answerer returns the config's questions.answerer; empty without
// questions.
func answerer(cfg *config.Config) string {
	if cfg.Questions == nil {
		return ""
	}
	return cfg.Questions.Answerer
}

// closeStatistics closes the statistics store, when there is one. A store
// that fails to close is a warning: it changes no exit code. The store's
// error says what failed, as the sqlite store's does.
func (b built) closeStatistics(o Options) {
	if b.statistics == nil {
		return
	}
	if err := b.statistics.Close(); err != nil {
		o.errorf("warning: %v", err)
	}
}

// journal returns the run journal of the repository at Root, through
// Journal; nil without one.
func (o Options) journal() port.Journal {
	if o.Journal == nil {
		return nil
	}
	return o.Journal(o.Root)
}

// run runs the engine and the renderer until both have returned, and handles
// the stop signals meanwhile. stopping tells that a first signal came
// already, so the engine stops at once and the next signal forces the exit.
// The renderer shows warnings before anything else.
func run(
	ctx context.Context, eng *engine.Engine, o Options, stopping bool, b built, warnings []string,
) int {
	r := &runner{eng: eng, o: o, code: ExitClean, warnings: warnings, notify: b.cfg.Notify, board: b.cfg.Board}
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
			// Only an engine that finished its stop sequence has ended its
			// writer. A failed engine, a panic included, and a forced exit
			// leave the store open: its writer may be inside a record.
			if err == nil {
				b.closeStatistics(o)
			}
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
	// notify tells which rules' ends the live view notifies.
	notify map[crew.RuleName]bool
	// board is the live view's board: the columns the config writes, or
	// its default columns.
	board []crew.BoardColumn
}

// renderer returns the TUI on a terminal without Plain, and the event lines
// otherwise.
func (r *runner) renderer() func() error {
	if r.o.Terminal && !r.o.Plain {
		model := tui.New(tui.Config{
			Updates: r.eng.SubscribeLatest(), Stop: r.eng.Stop, Force: r.force, Pause: r.eng.TogglePause,
			Now: time.Now, Location: time.Local,
			Notify: r.notify, Board: r.board, Repository: filepath.Base(r.o.Root), Warnings: r.warnings,
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
