// Package engine runs crew's workflow: it owns the pure core, runs the
// core's commands through the Tracker, Harness and Workspace ports, ticks on
// the poll interval, owns crew's local files under .crew/, and publishes an
// Update after every step for the renderers (KTD2, KTD6, KTD7, KTD12).
//
// One goroutine, Run's loop, owns the core. Every result reaches it through
// one inbox and is stamped with its arrival time there. Each command runs in
// its own goroutine on a command context that only Run's return cancels, so
// a stop request never cancels the verdict moves it is waiting for.
package engine

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

const (
	// callTimeout bounds every tracker and workspace call. A timeout is a
	// transient failure, so a hung gh or git cannot stall polling or stop.
	callTimeout = 10 * time.Minute
	// stopTimeout is how long a session gets to stop before its adapter
	// kills it.
	stopTimeout = 10 * time.Second
	// checkTimeout bounds every action's check, as callTimeout bounds a
	// tracker call: a check may call gh or git too. It is fixed.
	checkTimeout = 10 * time.Minute
	// lookupTimeout bounds the lookup of an action's pull request, which
	// runs even while crew stops, so a hung gh delays a stop by no more
	// (KTD3).
	lookupTimeout = 15 * time.Second
	// saidInterval is how often the loop refreshes what the running
	// sessions last said for the latest-wins subscribers (KTD5).
	saidInterval = 2 * time.Second
	// inboxSize buffers results. Senders are bounded (running sessions plus
	// a few commands) and the loop always drains, so blocking sends cannot
	// deadlock; the buffer only spares them waiting on a busy step.
	inboxSize = 64
)

// Config is what an engine runs: a validated workflow and its adapters.
type Config struct {
	// Workflow is the stages, in config order, already validated.
	Workflow []crew.Stage
	// MaxParallelIssues is how many issues the engine holds at once (R6).
	MaxParallelIssues int
	// PollInterval is the time between listings; it must be positive.
	PollInterval time.Duration
	// RunTimeLimit is how long the engine runs from its first poll before
	// it winds down. Zero runs until stopped.
	RunTimeLimit time.Duration
	// Tracker, Harness and Workspace are the adapters. The engine detects
	// their optional interfaces, such as port.Preparer, on these values.
	Tracker   port.Tracker
	Harness   port.Harness
	Workspace port.Workspace
	// Checker runs the actions' checks. Without one, an action with a
	// check fails, saying crew has no check runner.
	Checker port.Checker
	// UsageInStatus has each issue's status show what its ended actions
	// spent and the pull requests they opened, when the tracker reports
	// statuses (KTD11).
	UsageInStatus bool
	// Root is the repository's absolute root: session logs go under its
	// .crew/logs/, and it is shortened to . in every reason.
	Root string
	// Home is the user's home directory, shortened to ~ in every reason.
	// Empty shortens nothing.
	Home string
	// ActAs, when the config names a mate, has Prepare hand a tracker that
	// implements port.Acting the Writer and the MateLogins first.
	ActAs bool
	// Writer is who the tracker's own writes go as: the default mate, or the
	// zero Identity, the boss, when it cannot act.
	Writer port.Identity
	// Identities are the identities of the mates that act, by name. An
	// action whose mate is not among them runs as the boss.
	Identities map[string]port.Identity
	// MateLogins are the logins of the configured mates crew knows, whether
	// or not they act: the tracker takes the items they opened, and every
	// session and check gets them as CREW_MATES.
	MateLogins []string
}

// Engine runs the workflow of a Config. Use New; Run it once.
type Engine struct {
	cfg      Config
	stream   *stream
	stop     chan struct{} // closed by Stop
	stopOnce sync.Once

	// prepared is set by Prepare, and preparation holds its result, so the
	// preparers run once whether Run or its caller prepares.
	prepared    bool
	preparation error

	// reporter is the tracker's port.StatusReporter; nil when the tracker
	// has none, and the core then reports no status (R13).
	reporter port.StatusReporter
	// finder is the tracker's port.PullRequestFinder; nil when the tracker
	// has none, and the core then looks up no pull request (R6).
	finder port.PullRequestFinder
	// pullRequests is the tracker's port.PullRequestReporter; nil when the
	// tracker has none, and the core then makes no pull request report.
	pullRequests port.PullRequestReporter
	// opts are the core's options; Prepare builds the core with them once it
	// has read the run journal (KTD2).
	opts []core.Option
	// boss are the boss's logins, as the tracker's port.BossFinder found
	// them in Prepare; none without one.
	boss []string

	// The fields below are owned by Run's loop.
	model    *core.Model
	inbox    chan message
	inflight int // command goroutines whose final message is still due
	wg       sync.WaitGroup
	sessions map[sessionKey]port.Session
	checks   map[sessionKey]context.CancelFunc // ends each running check
	recent   []core.Event
	lastSaid []core.Said // what the sessions last said, as of the latest said refresh
	started  time.Time   // when the first poll ran
	run      string      // this crew run's id in the run journal: started, in RFC 3339
}

// New returns an engine for cfg. It starts nothing until Run. When the
// tracker implements port.StatusReporter, the engine reports each issue's
// status through it (KTD1). When it implements port.PullRequestReporter, the
// engine follows each move that landed with a report on the issue's pull
// requests through it. When the workspace implements port.Reopener, a
// failed run's action resumes in that run's workspace (KTD4). When the
// tracker implements port.PullRequestFinder, the engine looks up the pull
// request each action opened.
func New(cfg Config) *Engine {
	reporter, _ := cfg.Tracker.(port.StatusReporter)
	finder, _ := cfg.Tracker.(port.PullRequestFinder)
	var opts []core.Option
	if reporter != nil {
		opts = append(opts, core.ReportingStatus())
		if cfg.UsageInStatus {
			opts = append(opts, core.ReportingUsage())
		}
	}
	pullRequests, _ := cfg.Tracker.(port.PullRequestReporter)
	if pullRequests != nil {
		opts = append(opts, core.ReportingPullRequests())
	}
	if _, ok := cfg.Workspace.(port.Reopener); ok {
		opts = append(opts, core.Reopening())
	}
	if finder != nil {
		opts = append(opts, core.FindingPullRequests())
	}
	return &Engine{
		cfg:          cfg,
		stream:       newStream(),
		stop:         make(chan struct{}),
		reporter:     reporter,
		pullRequests: pullRequests,
		finder:       finder,
		opts:         opts,
		inbox:        make(chan message, inboxSize),
		sessions:     map[sessionKey]port.Session{},
		checks:       map[sessionKey]context.CancelFunc{},
	}
}

// Run prepares the adapters, as Prepare does, unless Prepare was already
// called, then polls at once and every PollInterval until stopped (R8). A
// preparation error, its own or the one Prepare returned, is returned before
// any listing (R2). Preparers run on ctx, so ctx ending while they run ends
// them, and Run returns their error.
//
// Stop, or ctx ending, requests a stop: nothing new starts, running
// sessions get stopTimeout to stop, issues are judged as their actions end,
// and owed calls get one final try (R9). RunTimeLimit after the first poll,
// the core winds down instead: nothing new is taken, and running sessions
// end on their own. Run returns nil once the core holds no issue and no
// command goroutine is left. Every subscription is closed when Run returns,
// after its last update.
func (e *Engine) Run(ctx context.Context) error {
	defer e.stream.close()
	if err := e.Prepare(ctx); err != nil {
		return err
	}
	cmdCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	ticker := time.NewTicker(e.cfg.PollInterval)
	defer ticker.Stop()
	saidTicker := time.NewTicker(saidInterval)
	defer saidTicker.Stop()
	stop, done := e.stop, ctx.Done()
	// The run time counts from the first poll, so the preparers do not use
	// it up. A nil channel never fires: without a limit, nothing winds down.
	var timeUp <-chan time.Time
	if e.cfg.RunTimeLimit > 0 {
		timer := time.NewTimer(e.cfg.RunTimeLimit)
		defer timer.Stop()
		timeUp = timer.C
	}
	e.started = time.Now()
	e.run = e.started.UTC().Format(time.RFC3339Nano)
	e.step(cmdCtx, core.Tick{})
	for !e.model.Stopped() || e.inflight > 0 {
		select {
		case <-ticker.C:
			e.step(cmdCtx, core.Tick{Said: e.said()})
		case <-saidTicker.C:
			e.refreshSaid()
		case <-stop:
			stop = nil
			e.step(cmdCtx, core.StopRequested{})
		case <-done:
			done = nil
			e.step(cmdCtx, core.StopRequested{})
		case <-timeUp:
			timeUp = nil
			e.step(cmdCtx, core.TimeUp{Limit: e.cfg.RunTimeLimit})
		case m := <-e.inbox:
			e.receive(cmdCtx, m)
		}
	}
	e.wg.Wait()
	return nil
}

// Stop requests a stop (R9). It returns at once; Run returns when the stop
// has completed. Calling it again, or before Run, is safe.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() { close(e.stop) })
}

// SubscribeLatest returns a latest-wins subscription, for the TUI: it holds
// only the newest update not yet received, each publish replacing the
// previous one, and every update carries the full snapshot, so nothing a
// renderer of the snapshot needs is lost. Besides the update of every step,
// it gets one without events whenever what the running sessions last said
// changes, checked every saidInterval; ordered queues never get those. The
// channel is closed when Run returns. Subscribe before Run.
func (e *Engine) SubscribeLatest() <-chan Update {
	return e.stream.subscribeLatest()
}

// SubscribeQueue returns an ordered subscription holding up to capacity
// updates, for the line renderer; past capacity it drops new updates and
// counts their events (Queue.Dropped). Publishing never waits for it.
// Subscribe before Run.
func (e *Engine) SubscribeQueue(capacity int) *Queue {
	return e.stream.subscribeQueue(capacity)
}

// Prepare runs, once, the Preparer of each adapter that implements
// port.Preparer, with the workflow's states, then reads the run journal. It
// stops at the first that fails and returns its error, naming its port or the
// journal, so the last step reported on ctx is the one that failed. These are
// environment checks (R2), so a caller can run them before starting a
// renderer; Run then does not prepare again. Call it before Run starts, never
// concurrently with Run; a second call returns the first one's result.
func (e *Engine) Prepare(ctx context.Context) error {
	if !e.prepared {
		e.prepared = true
		e.preparation = e.prepare(ctx)
	}
	return e.preparation
}

// prepare hands the tracker its writer when the config names a mate, runs
// each port's Preparer with crew.WorkflowStates, the states the workflow
// names, asks the tracker who the boss is, then reads the run journal and
// builds the core from it. It returns the first error, naming its port or
// the journal, without running what comes after it (R6). The core is then
// left unbuilt, which is safe because Run returns the error before its loop,
// the only place that reads it.
func (e *Engine) prepare(ctx context.Context) error {
	states := crew.WorkflowStates(e.cfg.Workflow)
	if a, ok := e.cfg.Tracker.(port.Acting); ok && e.cfg.ActAs {
		a.ActAs(e.cfg.Writer, slices.Clone(e.cfg.MateLogins))
	}
	ports := []struct {
		name    string
		adapter any
	}{{"tracker", e.cfg.Tracker}, {"harness", e.cfg.Harness}, {"workspace", e.cfg.Workspace}}
	for _, p := range ports {
		if err := port.Prepare(ctx, states, p.adapter); err != nil {
			return fmt.Errorf("prepare the %s: %w", p.name, err)
		}
	}
	if b, ok := e.cfg.Tracker.(port.BossFinder); ok {
		e.boss = b.Boss()
	}
	port.Step(ctx, "reading the run journal")
	past, err := e.readJournal()
	if err != nil {
		return err
	}
	e.model = core.New(e.cfg.Workflow, e.cfg.MaxParallelIssues, append(e.opts, core.RecordingRuns(past))...)
	return nil
}

// said returns what each running session that implements port.Narrator last
// said, with local paths shortened (R10), then cut to its last maxSaid
// characters, in a stable order. Cutting after shortening keeps the end of a
// cut path from reaching the tracker. Only the loop
// calls it, as it owns the sessions.
func (e *Engine) said() []core.Said {
	var out []core.Said
	for _, k := range slices.SortedFunc(maps.Keys(e.sessions), func(a, b sessionKey) int {
		return cmp.Or(cmp.Compare(a.issue, b.issue), cmp.Compare(a.action, b.action))
	}) {
		n, ok := e.sessions[k].(port.Narrator)
		if !ok {
			continue
		}
		if text := n.Said(); text != "" {
			out = append(out, core.Said{IssueKey: k.issue, Action: k.action, Text: lastWords(e.scrub(text))})
		}
	}
	return out
}

// receive handles a message from a command goroutine; ctx is the command
// context.
func (e *Engine) receive(ctx context.Context, m message) {
	if m.final {
		e.inflight--
	}
	switch in := m.input.(type) {
	case nil:
		return
	case core.SessionStarted:
		e.sessions[sessionKey{in.IssueKey, in.Action}] = m.session
	case core.SessionEnded:
		delete(e.sessions, sessionKey{in.IssueKey, in.Action})
	case core.CheckEnded:
		delete(e.checks, sessionKey{in.IssueKey, in.Action})
	}
	e.step(ctx, m.input)
}

// step feeds in to the core, stamped with the time now, launches the
// commands it returns on the command context ctx and publishes the update.
func (e *Engine) step(ctx context.Context, in core.Input) {
	cmds, events := e.model.Update(in.Stamped(time.Now()))
	for _, c := range cmds {
		e.launch(ctx, c)
	}
	e.recent = append(e.recent, events...)
	if n := len(e.recent) - recentEvents; n > 0 {
		e.recent = e.recent[n:]
	}
	e.stream.publish(Update{Events: events, Snapshot: e.snapshot()})
}

// refreshSaid stores what the running sessions last said and, when it
// changed, publishes an update without events to the latest-wins
// subscribers only, without stepping the core (KTD5). The ordered queues
// never get it, so it cannot crowd out their events.
func (e *Engine) refreshSaid() {
	said := e.said()
	if slices.Equal(said, e.lastSaid) {
		return
	}
	e.lastSaid = said
	e.stream.publishLatest(Update{Snapshot: e.snapshot()})
}

// snapshot returns the engine's view now, sharing no memory with the core
// or with an earlier snapshot.
func (e *Engine) snapshot() Snapshot {
	return Snapshot{
		View: e.model.View(), Recent: slices.Clone(e.recent),
		Started: e.started, RunTimeLimit: e.cfg.RunTimeLimit, Said: slices.Clone(e.lastSaid),
	}
}
