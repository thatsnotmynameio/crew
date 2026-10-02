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
	"errors"
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
	// Tracker, Harness and Workspace are the adapters. The engine detects
	// their optional interfaces, such as port.Preparer, on these values.
	Tracker   port.Tracker
	Harness   port.Harness
	Workspace port.Workspace
	// Root is the repository's absolute root: session logs go under its
	// .crew/logs/, and it is shortened to . in every reason.
	Root string
	// Home is the user's home directory, shortened to ~ in every reason.
	// Empty shortens nothing.
	Home string
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

	// The fields below are owned by Run's loop.
	model    *core.Model
	inbox    chan message
	cmdCtx   context.Context
	inflight int // command goroutines whose final message is still due
	wg       sync.WaitGroup
	sessions map[sessionKey]port.Session
	recent   []core.Event
}

// New returns an engine for cfg. It starts nothing until Run. When the
// tracker implements port.StatusReporter, the engine reports each issue's
// status through it (KTD1).
func New(cfg Config) *Engine {
	reporter, _ := cfg.Tracker.(port.StatusReporter)
	var opts []core.Option
	if reporter != nil {
		opts = append(opts, core.ReportingStatus())
	}
	return &Engine{
		cfg:      cfg,
		stream:   newStream(),
		stop:     make(chan struct{}),
		reporter: reporter,
		model:    core.New(cfg.Workflow, cfg.MaxParallelIssues, opts...),
		inbox:    make(chan message, inboxSize),
		sessions: map[sessionKey]port.Session{},
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
// and owed calls get one final try (R9). Run returns nil once the core holds
// no issue and no command goroutine is left. Every subscription is closed
// when Run returns, after its last update.
func (e *Engine) Run(ctx context.Context) error {
	defer e.stream.close()
	if err := e.Prepare(ctx); err != nil {
		return err
	}
	cmdCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	e.cmdCtx = cmdCtx

	ticker := time.NewTicker(e.cfg.PollInterval)
	defer ticker.Stop()
	stop, done := e.stop, ctx.Done()
	e.step(core.Tick{})
	for !e.model.Stopped() || e.inflight > 0 {
		select {
		case <-ticker.C:
			e.step(core.Tick{Said: e.said()})
		case <-stop:
			stop = nil
			e.step(core.StopRequested{})
		case <-done:
			done = nil
			e.step(core.StopRequested{})
		case m := <-e.inbox:
			e.receive(m)
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
// renderer of the snapshot needs is lost. The channel is closed when Run
// returns. Subscribe before Run.
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
// port.Preparer, with the states the workflow can request, and returns their
// errors joined, each naming its port. The preparers are environment checks
// (R2), so a caller can run them before starting a renderer; Run then does
// not prepare again. Call it before Run starts, never concurrently with
// Run; a second call returns the first one's result.
func (e *Engine) Prepare(ctx context.Context) error {
	if !e.prepared {
		e.prepared = true
		e.preparation = e.prepare(ctx)
	}
	return e.preparation
}

// prepare runs each port's Preparer with the states the workflow can
// request, and joins their errors, each naming its port.
func (e *Engine) prepare(ctx context.Context) error {
	states := workflowStates(e.cfg.Workflow)
	ports := []struct {
		name    string
		adapter any
	}{{"tracker", e.cfg.Tracker}, {"harness", e.cfg.Harness}, {"workspace", e.cfg.Workspace}}
	var errs []error
	for _, p := range ports {
		if err := port.Prepare(ctx, states, p.adapter); err != nil {
			errs = append(errs, fmt.Errorf("prepare the %s: %w", p.name, err))
		}
	}
	return errors.Join(errs...)
}

// workflowStates returns the states the workflow can move issues to or take
// them from, in canonical order: every stage's label, moves_to and
// on_success, and needs_attention, where failed issues go (R7).
func workflowStates(workflow []crew.Stage) []crew.State {
	used := map[crew.State]bool{crew.NeedsAttention: true}
	for _, s := range workflow {
		used[s.Label], used[s.MovesTo], used[s.OnSuccess] = true, true, true
	}
	return slices.DeleteFunc(crew.States(), func(s crew.State) bool { return !used[s] })
}

// said returns what each running session that implements port.Narrator last
// said, with local paths shortened (R10), in a stable order. Only the loop
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
			out = append(out, core.Said{IssueKey: k.issue, Action: k.action, Text: e.scrub(text)})
		}
	}
	return out
}

// receive handles a message from a command goroutine.
func (e *Engine) receive(m message) {
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
	}
	e.step(m.input)
}

// step feeds in to the core, stamped with the time now, launches the
// commands it returns and publishes the update.
func (e *Engine) step(in core.Input) {
	cmds, events := e.model.Update(in.Stamped(time.Now()))
	for _, c := range cmds {
		e.launch(c)
	}
	e.recent = append(e.recent, events...)
	if n := len(e.recent) - recentEvents; n > 0 {
		e.recent = e.recent[n:]
	}
	e.stream.publish(Update{
		Events:   events,
		Snapshot: Snapshot{View: e.model.View(), Recent: slices.Clone(e.recent)},
	})
}
