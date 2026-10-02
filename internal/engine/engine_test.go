package engine_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The workflow's states in these tests, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	needsAttention crew.State = "needs attention"
)

const poll = 300 * time.Second

// implement is the draft config's implement stage (KTD5).
var implement = crew.Stage{
	Name: "implement", Label: ready, MovesTo: inProgress, OnSuccess: readyToReview,
	OnFailure: needsAttention,
	Actions: []crew.Action{
		{Name: "acceptance", Prompt: "Implement test acceptance for issue {{.Issue.Ref}}"},
		{Name: "development", Prompt: "Implement development for issue {{.Issue.Ref}}"},
	},
}

// develop is a stage with one action, for tests about one session per issue.
var develop = crew.Stage{
	Name: "implement", Label: ready, MovesTo: inProgress, OnSuccess: readyToReview,
	OnFailure: needsAttention,
	Actions:   []crew.Action{{Name: "development", Prompt: "Implement development for issue {{.Issue.Ref}}"}},
}

// epoch dates the issues: issue n was created n minutes after it, so #1 is
// the oldest.
var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func issue(n int, states ...crew.State) crew.Issue {
	key := fmt.Sprint(n)
	return crew.Issue{
		Key: key, Ref: "#" + key, Title: "Issue " + key, URL: "https://example.test/issues/" + key,
		Created: epoch.Add(time.Duration(n) * time.Minute), States: states,
	}
}

// rig is an engine running in its own goroutine, with fake adapters.
type rig struct {
	t       *testing.T
	root    string
	harness *fake.Harness
	engine  *engine.Engine
	cancel  context.CancelFunc
	done    chan error
	final   chan engine.Update
	// queue holds every update, for tests that read all the events.
	queue *engine.Queue
}

// config returns a config over tracker for workflow, with a fake harness and
// workspace, rooted in a fresh repository directory.
func config(t *testing.T, tracker port.Tracker, workflow ...crew.Stage) engine.Config {
	t.Helper()
	root := filepath.Join(t.TempDir(), "home", "repo")
	worktrees := filepath.Join(root, ".crew", "worktrees")
	if err := os.MkdirAll(worktrees, 0o750); err != nil {
		t.Fatal(err)
	}
	return engine.Config{
		Workflow:          workflow,
		MaxParallelIssues: 2,
		PollInterval:      poll,
		Tracker:           tracker,
		Harness:           fake.NewHarness(),
		Workspace:         fake.NewWorkspace(worktrees),
		Root:              root,
		Home:              filepath.Dir(root),
	}
}

// start runs an engine for cfg. The test must stop it and call wait.
func start(t *testing.T, cfg engine.Config) *rig {
	t.Helper()
	e := engine.New(cfg)
	latest := e.SubscribeLatest()
	queue := e.SubscribeQueue(1024)
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{
		t: t, root: cfg.Root, engine: e, cancel: cancel, done: make(chan error, 1), final: make(chan engine.Update, 1),
		queue: queue,
	}
	if h, ok := cfg.Harness.(*fake.Harness); ok {
		r.harness = h
	}
	go func() {
		var last engine.Update
		for u := range latest {
			last = u
		}
		r.final <- last
	}()
	go func() { r.done <- e.Run(ctx) }()
	return r
}

// wait waits for Run to return and the latest-wins subscription to close, and
// returns the last update published and Run's error.
func (r *rig) wait() (engine.Update, error) {
	r.t.Helper()
	err := <-r.done
	r.cancel()
	return <-r.final, err
}

// sessions waits for n sessions to start and returns them keyed by the name
// of their workspace directory.
func (r *rig) sessions(n int) map[string]*fake.Session {
	r.t.Helper()
	out := map[string]*fake.Session{}
	for range n {
		s, err := r.harness.Next(context.Background())
		if err != nil {
			r.t.Fatalf("waiting for a session: %v", err)
		}
		out[filepath.Base(s.Run().Dir)] = s
	}
	return out
}

func states(t *testing.T, tr interface {
	Issue(string) (crew.Issue, bool)
}, key string) []crew.State {
	t.Helper()
	i, ok := tr.Issue(key)
	if !ok {
		t.Fatalf("issue %s is gone", key)
	}
	return i.States
}

// span is one listing a tracker saw.
type span struct {
	start, end time.Time
	err        error
}

// slowTracker is a fake tracker whose listings take delay, and whose first
// listing never returns on its own when hangFirst is set.
type slowTracker struct {
	*fake.Tracker
	delay     time.Duration
	hangFirst bool

	mu    sync.Mutex
	calls []span
}

func (s *slowTracker) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	s.mu.Lock()
	n := len(s.calls)
	s.calls = append(s.calls, span{start: time.Now()})
	s.mu.Unlock()
	var err error
	switch {
	case s.hangFirst && n == 0:
		<-ctx.Done()
		err = ctx.Err()
	case s.delay > 0:
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	s.mu.Lock()
	s.calls[n].end, s.calls[n].err = time.Now(), err
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return s.Tracker.List(ctx, states)
}

func (s *slowTracker) spans() []span {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

func offsets(t0 time.Time, spans []span) []time.Duration {
	var out []time.Duration
	for _, s := range spans {
		out = append(out, s.start.Sub(t0))
	}
	return out
}

func TestTicksAtOnceThenEveryPollInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowTracker{Tracker: fake.NewTracker()}
		t0 := time.Now()
		r := start(t, config(t, tr, develop))

		time.Sleep(700 * time.Second)
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := []time.Duration{0, 300 * time.Second, 600 * time.Second}
		if got := offsets(t0, tr.spans()); !reflect.DeepEqual(got, want) {
			t.Errorf("listings started at %v, want %v", got, want)
		}
	})
}

func TestASlowListingCoalescesTheMissedTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowTracker{Tracker: fake.NewTracker(), delay: 400 * time.Second}
		t0 := time.Now()
		r := start(t, config(t, tr, develop))

		time.Sleep(700 * time.Second)
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		spans := tr.spans()
		for i := 1; i < len(spans); i++ {
			if spans[i].start.Before(spans[i-1].end) {
				t.Errorf("listing %d started at %v, before listing %d ended at %v",
					i, spans[i].start.Sub(t0), i-1, spans[i-1].end.Sub(t0))
			}
		}
		if got, want := offsets(t0, spans), []time.Duration{0, 600 * time.Second}; !reflect.DeepEqual(got, want) {
			t.Errorf("listings started at %v, want %v", got, want)
		}
		if got, want := time.Since(t0), 1000*time.Second; got != want {
			t.Errorf("Run returned after %v, want %v: once the listing in flight ended", got, want)
		}
	})
}

// Covers AE1.
func TestPollTakesTwoIssuesAndStartsFourSessionsEachWithItsOwnWorkspaceAndLog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready), issue(2, ready), issue(3, ready))
		r := start(t, config(t, tr, implement))

		sessions := r.sessions(4)

		names := slices.Sorted(func(yield func(string) bool) {
			for name := range sessions {
				if !yield(name) {
					return
				}
			}
		})
		want := []string{"issue-1-acceptance", "issue-1-development", "issue-2-acceptance", "issue-2-development"}
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("sessions run in workspaces %v, want %v", names, want)
		}
		if got := sessions["issue-1-acceptance"].Run().Prompt; got != "Implement test acceptance for issue #1" {
			t.Errorf("issue-1-acceptance prompt = %q", got)
		}
		if got := sessions["issue-2-development"].Run().Prompt; got != "Implement development for issue #2" {
			t.Errorf("issue-2-development prompt = %q", got)
		}
		for _, key := range []string{"1", "2"} {
			if got := states(t, tr, key); !reflect.DeepEqual(got, []crew.State{inProgress}) {
				t.Errorf("issue %s is in %v, want in progress", key, got)
			}
		}
		if got := states(t, tr, "3"); !reflect.DeepEqual(got, []crew.State{ready}) {
			t.Errorf("issue 3 is in %v, want it to wait in ready", got)
		}
		for name, s := range sessions {
			if _, err := fmt.Fprintf(s.Run().Output, "output of %s\n", name); err != nil {
				t.Fatalf("write to %s's output: %v", name, err)
			}
		}

		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		for name := range sessions {
			got, err := os.ReadFile(filepath.Join(r.root, ".crew", "logs", name+".log"))
			if err != nil {
				t.Fatalf("log of %s: %v", name, err)
			}
			if want := "output of " + name + "\n"; string(got) != want {
				t.Errorf("log of %s = %q, want %q", name, got, want)
			}
		}
		var logs []string
		for _, rep := range tr.Reports() {
			for _, f := range rep.Failures {
				logs = append(logs, f.Log)
			}
		}
		slices.Sort(logs)
		wantLogs := []string{
			".crew/logs/issue-1-acceptance.log", ".crew/logs/issue-1-development.log",
			".crew/logs/issue-2-acceptance.log", ".crew/logs/issue-2-development.log",
		}
		if !reflect.DeepEqual(logs, wantLogs) {
			t.Errorf("failure reports name logs %v, want %v", logs, wantLogs)
		}
		if !slices.ContainsFunc(final.Events, func(e core.Event) bool { _, ok := e.(core.Stopped); return ok }) {
			t.Errorf("last update's events = %#v, want a Stopped event", final.Events)
		}
	})
}

// listCounter is a preparing fake tracker that counts its listings.
type listCounter struct {
	fake.PreparingTracker

	mu    sync.Mutex
	lists int
}

func (l *listCounter) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	l.mu.Lock()
	l.lists++
	l.mu.Unlock()
	return l.PreparingTracker.List(ctx, states)
}

func TestAFailingPreparerStopsTheEngineBeforeAnyListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker(issue(1, ready))}
		notLoggedIn := errors.New("gh is not logged in")
		tr.Fail(notLoggedIn)
		harness := fake.NewPreparingHarness()
		cfg := config(t, tr, develop)
		cfg.Harness = harness

		err := engine.New(cfg).Run(context.Background())

		if !errors.Is(err, notLoggedIn) {
			t.Fatalf("Run = %v, want the preparer's error", err)
		}
		if !strings.Contains(err.Error(), "tracker") {
			t.Errorf("Run = %q, want it to name the tracker", err)
		}
		if tr.lists != 0 {
			t.Errorf("tracker listed %d times, want none", tr.lists)
		}
		want := [][]crew.State{{ready, inProgress, readyToReview, needsAttention}}
		if got := tr.Calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("tracker prepared for %v, want the workflow's states %v", got, want)
		}
		if got := harness.Calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("harness prepared for %v, want %v", got, want)
		}
	})
}

func TestPrepareGetsOnlyTheStatesTheWorkflowNames(t *testing.T) {
	blocked := develop
	blocked.OnFailure = "blocked"
	tr := fake.NewPreparingTracker()

	if err := engine.New(config(t, tr, blocked)).Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	want := [][]crew.State{{ready, inProgress, readyToReview, "blocked"}}
	if got := tr.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("tracker prepared for %v, want %v and no needs attention", got, want)
	}
}

func TestRunAfterPrepareDoesNotPrepareAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker()}
		e := engine.New(config(t, tr, develop))

		if err := e.Prepare(context.Background()); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if got := len(tr.Calls()); got != 1 {
			t.Fatalf("Prepare ran the tracker's Preparer %d times, want 1", got)
		}
		if tr.lists != 0 {
			t.Fatalf("Prepare listed %d times, want none", tr.lists)
		}

		e.Stop()
		if err := e.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := len(tr.Calls()); got != 1 {
			t.Errorf("the tracker's Preparer ran %d times in all, want once", got)
		}
		if tr.lists != 1 {
			t.Errorf("Run listed %d times, want the first poll's listing", tr.lists)
		}
	})
}

// gatedTracker holds every move to gate until release is closed, and fails
// such a move when its context ended meanwhile.
type gatedTracker struct {
	*fake.Tracker
	gate    crew.State
	entered chan struct{}
	release chan struct{}
}

func (g *gatedTracker) Move(ctx context.Context, key string, from, to crew.State) error {
	if to == g.gate {
		g.entered <- struct{}{}
		<-g.release
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("move issue %s: %w", key, err)
		}
	}
	return g.Tracker.Move(ctx, key, from, to)
}

// Covers AE9 through the loop.
func TestStopLetsAVerdictMoveInFlightFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &gatedTracker{
			Tracker: fake.NewTracker(issue(1, ready), issue(2, ready)),
			gate:    readyToReview, entered: make(chan struct{}, 1), release: make(chan struct{}),
		}
		r := start(t, config(t, tr, develop))
		sessions := r.sessions(2)

		sessions["issue-1-development"].End(crew.Outcome{Succeeded: true, Reason: "done"})
		<-tr.entered
		r.cancel() // Run's context ending is a stop request, not an abort.
		synctest.Wait()

		select {
		case err := <-r.done:
			t.Fatalf("Run returned %v while a verdict move was in flight", err)
		default:
		}
		if !sessions["issue-2-development"].Stopped() {
			t.Error("issue 2's running session was not stopped")
		}
		close(tr.release)
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
		if got := states(t, tr, "2"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("issue 2 is in %v, want needs attention", got)
		}
	})
}

func TestStopKillsASessionIgnoringItAtTheTenSecondDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.harness.IgnoreStop(true)
		s := r.sessions(1)["issue-1-development"]

		t0 := time.Now()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if got := time.Since(t0); got != 10*time.Second {
			t.Errorf("Run returned %v after the stop request, want 10s", got)
		}
		if !s.Stopped() {
			t.Error("the session was not stopped")
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("issue 1 is in %v, want needs attention", got)
		}
		reports := tr.Reports()
		if len(reports) != 1 || len(reports[0].Failures) != 1 || reports[0].Failures[0].Reason != fake.KilledReason {
			t.Errorf("reports = %+v, want one naming the killed session", reports)
		}
	})
}

// moveCounter is a fake tracker that counts the moves asked for to one state,
// failed ones included.
type moveCounter struct {
	*fake.Tracker
	to crew.State

	mu    sync.Mutex
	moves int
}

func (m *moveCounter) Move(ctx context.Context, key string, from, to crew.State) error {
	if to == m.to {
		m.mu.Lock()
		m.moves++
		m.mu.Unlock()
	}
	return m.Tracker.Move(ctx, key, from, to)
}

func TestStopGivesAFailingVerdictMoveOneFinalTryAndReturns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &moveCounter{Tracker: fake.NewTracker(issue(1, ready)), to: needsAttention}
		r := start(t, config(t, tr, develop))
		r.sessions(1)
		down := errors.New("tracker is down")
		tr.FailMoves("1", down, down, down)

		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		if tr.moves != 2 {
			t.Errorf("tried the needs attention move %d times, want 2: the first and one final try", tr.moves)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{inProgress}) {
			t.Errorf("issue 1 is in %v, want it left in in progress", got)
		}
		dropped := slices.ContainsFunc(final.Snapshot.Recent, func(e core.Event) bool {
			d, ok := e.(core.CallDropped)
			return ok && d.Call.Kind == core.CallMove && d.Call.To == needsAttention &&
				d.Result == core.ResultFailed && strings.Contains(d.Reason, "tracker is down")
		})
		if !dropped {
			t.Errorf("last update's recent events = %#v, want the dropped needs attention move", final.Snapshot.Recent)
		}
	})
}

func TestAListingThatNeverReturnsTimesOutAndALaterTickListsAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowTracker{Tracker: fake.NewTracker(), hangFirst: true}
		cfg := config(t, tr, develop)
		cfg.PollInterval = 7 * time.Minute
		t0 := time.Now()
		r := start(t, cfg)

		time.Sleep(15 * time.Minute)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		spans := tr.spans()
		if len(spans) < 2 {
			t.Fatalf("tracker saw %d listings, want 2", len(spans))
		}
		if got := spans[0].end.Sub(t0); got != 10*time.Minute || !errors.Is(spans[0].err, context.DeadlineExceeded) {
			t.Errorf("first listing ended after %v with %v, want a deadline after 10m", got, spans[0].err)
		}
		if got := spans[1].start.Sub(t0); got != 14*time.Minute {
			t.Errorf("second listing started at %v, want at the 14m tick", got)
		}
		failed := slices.ContainsFunc(final.Snapshot.Recent, func(e core.Event) bool {
			f, ok := e.(core.ListingFailed)
			return ok && f.At.Sub(t0) == 10*time.Minute
		})
		if !failed {
			t.Errorf("recent events = %#v, want the listing failing at 10m", final.Snapshot.Recent)
		}
	})
}

// failingWorkspace fails every creation with an error naming local paths, as
// git's stderr does.
type failingWorkspace struct {
	root, home string
}

func (w failingWorkspace) Create(_ context.Context, issue crew.Issue, action string) (port.Space, error) {
	dir := filepath.Join(w.root, ".crew", "worktrees", "issue-"+issue.Key+"-"+action)
	return port.Space{}, fmt.Errorf("git worktree add: fatal: '%s' already exists (see %s)", dir, filepath.Join(w.home, ".gitconfig"))
}

func TestAWorkspaceFailureReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Workspace = failingWorkspace{root: cfg.Root, home: cfg.Home}
		r := start(t, cfg)

		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		reports := tr.Reports()
		if len(reports) != 1 || len(reports[0].Failures) != 1 {
			t.Fatalf("reports = %+v, want one with one failure", reports)
		}
		want := "git worktree add: fatal: './.crew/worktrees/issue-1-development' already exists (see ~/.gitconfig)"
		if got := reports[0].Failures[0].Reason; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

// reportedReason runs cfg, calling during first when it is set, until every
// goroutine is blocked, stops it, and returns the reason of the one failure
// the tracker received.
func reportedReason(t *testing.T, tr *fake.Tracker, cfg engine.Config, during func(*rig)) string {
	t.Helper()
	r := start(t, cfg)
	if during != nil {
		during(r)
	}
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := tr.Reports()
	if len(reports) != 1 || len(reports[0].Failures) != 1 {
		t.Fatalf("reports = %+v, want one with one failure", reports)
	}
	return reports[0].Failures[0].Reason
}

func TestASessionsReasonReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)

		got := reportedReason(t, tr, cfg, func(r *rig) {
			r.sessions(1)["issue-1-development"].End(crew.Outcome{
				Reason: fmt.Sprintf("go test failed in %s/engine (cache %s/.cache), ran in %s.", cfg.Root, cfg.Home, cfg.Root),
			})
		})

		if want := "go test failed in ./engine (cache ~/.cache), ran in .."; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

// failingHarness fails every start with an error naming local paths, as a
// harness's stderr does.
type failingHarness struct {
	home string
}

func (h failingHarness) Start(_ context.Context, run port.Run) (port.Session, error) {
	return nil, fmt.Errorf("claude: cannot run in %s: no settings in %s", run.Dir, filepath.Join(h.home, ".claude"))
}

func TestAHarnessStartFailureReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Harness = failingHarness{home: cfg.Home}

		got := reportedReason(t, tr, cfg, nil)

		if want := "claude: cannot run in ./.crew/worktrees/issue-1-development: no settings in ~/.claude"; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

func TestALogThatCannotOpenReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		// A file where the log directory goes makes creating it fail.
		if err := os.WriteFile(filepath.Join(cfg.Root, ".crew", "logs"), nil, 0o600); err != nil {
			t.Fatal(err)
		}

		got := reportedReason(t, tr, cfg, nil)

		if !strings.Contains(got, "./.crew/logs") || strings.Contains(got, cfg.Root) {
			t.Errorf("reason = %q, want it to name ./.crew/logs and not %s", got, cfg.Root)
		}
	})
}

// failingLister is a fake tracker whose listings fail with an error naming
// local paths, as gh's stderr does.
type failingLister struct {
	*fake.Tracker
	root, home string
}

func (f failingLister) List(context.Context, []crew.State) ([]crew.Issue, error) {
	return nil, fmt.Errorf("gh: no repository in %s (config %s)", f.root, filepath.Join(f.home, ".config", "gh"))
}

func TestATrackerCallsReasonHasLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config(t, nil, develop)
		cfg.Tracker = failingLister{Tracker: fake.NewTracker(), root: cfg.Root, home: cfg.Home}
		r := start(t, cfg)

		synctest.Wait()
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := "gh: no repository in . (config ~/.config/gh)"
		failed := slices.ContainsFunc(final.Snapshot.Recent, func(e core.Event) bool {
			f, ok := e.(core.ListingFailed)
			return ok && f.Reason == want
		})
		if !failed {
			t.Errorf("recent events = %#v, want a ListingFailed with reason %q", final.Snapshot.Recent, want)
		}
	})
}

// lastStatus returns the last status the tracker holds for key, failing
// without one.
func lastStatus(t *testing.T, tr fake.ReportingTracker, key string) crew.Status {
	t.Helper()
	got := tr.Statuses(key)
	if len(got) == 0 {
		t.Fatalf("no status written for %s", key)
	}
	return got[len(got)-1]
}

func TestAE2AE6RunningStatusCarriesTheSessionsWordsWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Harness = fake.NewNarratingHarness()
		r := start(t, cfg)
		s := r.sessions(1)["issue-1-development"]
		begun := time.Now()

		s.Say(fmt.Sprintf("Edited %s/internal/core/update.go for @someone", cfg.Root))
		time.Sleep(poll)
		synctest.Wait()

		got := lastStatus(t, tr, "1")
		if got.Kind != crew.StatusRunning || len(got.Actions) != 1 {
			t.Fatalf("status = %#v, want development running", got)
		}
		a := got.Actions[0]
		if want := "Edited ./internal/core/update.go for @someone"; a.Said != want {
			t.Errorf("Said = %q, want %q", a.Said, want)
		}
		if !a.Started.Equal(begun) || !got.Updated.Equal(begun.Add(poll)) {
			t.Errorf("started %v and updated %v, want %v and a poll later", a.Started, got.Updated, begun)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestR9SessionThatCannotNarrateGivesAStatusWithoutWords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.sessions(1)
		time.Sleep(poll)
		synctest.Wait()

		got := lastStatus(t, tr, "1")
		if a := got.Actions[0]; a.State != crew.ActionRunning || a.Started.IsZero() || a.Said != "" {
			t.Errorf("action = %#v, want running with a start time and no words", a)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestAE3AE4StopLeavesTheMoveOnTheStatusAndTheFailureReportApart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.sessions(1)

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := crew.Status{
			IssueKey: "1", IssueRef: "#1", Stage: "implement", Kind: crew.StatusEnded,
			Actions: []crew.ActionStatus{{Name: "development", State: crew.ActionFailed}},
			To:      needsAttention, Move: crew.MoveDone,
		}
		got := lastStatus(t, tr, "1")
		got.Updated = time.Time{}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("last status = %#v, want %#v", got, want)
		}
		if len(tr.Reports()) != 1 {
			t.Errorf("reports = %+v, want the failure report as well", tr.Reports())
		}
	})
}

// statusCounter is a reporting fake tracker that counts its status writes.
type statusCounter struct {
	fake.ReportingTracker

	mu     sync.Mutex
	writes int
}

func (c *statusCounter) ReportStatus(ctx context.Context, s crew.Status) error {
	c.mu.Lock()
	c.writes++
	c.mu.Unlock()
	return c.ReportingTracker.ReportStatus(ctx, s)
}

func (c *statusCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

func TestARefusedEndedStatusIsNotRetriedAndStopDoesNotWaitForIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &statusCounter{ReportingTracker: fake.NewReportingTracker(issue(1, ready))}
		r := start(t, config(t, tr, develop))
		s := r.sessions(1)["issue-1-development"]
		synctest.Wait()

		locked := fmt.Errorf("issue is locked: %w", port.ErrRefused)
		tr.FailStatuses("1", locked, locked)
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		writes := tr.count()

		time.Sleep(poll)
		synctest.Wait()
		if got := tr.count(); got != writes {
			t.Errorf("%d status writes after the next tick, want still %d", got, writes)
		}
		for _, st := range tr.Statuses("1") {
			if st.Kind == crew.StatusEnded {
				t.Errorf("an ended status was written: %#v", st)
			}
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
	})
}

func TestALongSaidTextIsCutOnlyAfterItsLocalPathsAreShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Harness = fake.NewNarratingHarness()
		r := start(t, cfg)
		s := r.sessions(1)["issue-1-development"]

		// The repository's path starts before the last 200 characters, so a
		// cut before shortening would leave the end of it in the text.
		tail := "/internal/core/update.go " + strings.Repeat("x", 190)
		s.Say("Edited " + cfg.Root + tail)
		time.Sleep(poll)
		synctest.Wait()

		got := lastStatus(t, tr, "1").Actions[0].Said
		if want := "…" + string([]rune("." + tail)[len([]rune("."+tail))-199:]); got != want {
			t.Errorf("Said = %q, want %q", got, want)
		}
		if strings.Contains(got, filepath.Base(cfg.Root)) || strings.Contains(got, "home") {
			t.Errorf("Said = %q names part of a local path", got)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

// slowPreparer is a tracker whose environment check takes delay.
type slowPreparer struct {
	*slowTracker
	delay time.Duration
}

func (s *slowPreparer) Prepare(context.Context, []crew.State) error {
	time.Sleep(s.delay)
	return nil
}

// Covers AE1.
func TestWithoutARunTimeLimitTheEngineKeepsPollingUntilStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowTracker{Tracker: fake.NewTracker()}
		t0 := time.Now()
		r := start(t, config(t, tr, develop))

		time.Sleep(72 * time.Hour)
		synctest.Wait()
		select {
		case err := <-r.done:
			t.Fatalf("Run returned %v without a stop", err)
		default:
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		spans := tr.spans()
		if got, want := spans[len(spans)-1].start.Sub(t0), 72*time.Hour; got != want {
			t.Errorf("last listing started at %v, want %v", got, want)
		}
	})
}

// Covers AE2.
func TestTheRunTimeLimitCountsFromTheFirstPollAndStopsAnIdleEngine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowPreparer{slowTracker: &slowTracker{Tracker: fake.NewTracker()}, delay: 10 * time.Minute}
		cfg := config(t, tr, develop)
		cfg.RunTimeLimit = time.Hour
		t0 := time.Now()
		r := start(t, cfg)

		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got, want := time.Since(t0), 70*time.Minute; got != want {
			t.Errorf("Run returned after %v, want %v: the checks' 10 minutes plus the hour", got, want)
		}
		if got := tr.spans()[0].start.Sub(t0); got != 10*time.Minute {
			t.Errorf("first listing started at %v, want 10m0s", got)
		}
	})
}

// Covers AE3.
func TestWhenTheRunTimeIsUpARunningSessionFinishesAndNothingNewIsTaken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(42, ready))
		cfg := config(t, tr, develop)
		cfg.RunTimeLimit = time.Hour
		t0 := time.Now()
		r := start(t, cfg)
		session := r.sessions(1)["issue-42-development"]

		time.Sleep(time.Hour + time.Second)
		tr.Add(issue(43, ready))
		time.Sleep(90*time.Minute - time.Since(t0))
		session.End(crew.Outcome{Succeeded: true, Reason: "done"})

		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got, want := time.Since(t0), 90*time.Minute; got != want {
			t.Errorf("Run returned after %v, want %v, once #42 was judged", got, want)
		}
		if session.Stopped() {
			t.Error("#42's session was stopped; it should have run to its end")
		}
		if got := states(t, tr, "42"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#42 is in %v, want ready to review", got)
		}
		if got := states(t, tr, "43"); !reflect.DeepEqual(got, []crew.State{ready}) {
			t.Errorf("#43 is in %v, want it still ready, never taken", got)
		}
	})
}
