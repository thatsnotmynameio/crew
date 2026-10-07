package engine_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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

// The rules' states in these tests, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	needsAttention crew.State = "needs attention"
)

const poll = 300 * time.Second

// implement is the draft config's implement rule (KTD5).
var implement = crew.Rule{
	Name:   "implement",
	Labels: crew.Labels{Ready: ready, Running: inProgress, Success: readyToReview, Failure: needsAttention},
	Actions: []crew.Action{
		{Name: "acceptance", Prompt: parsedPrompt("acceptance", "Implement test acceptance for issue {{.Issue.Ref}}")},
		{Name: "development", Prompt: parsedPrompt("development", "Implement development for issue {{.Issue.Ref}}")},
	},
}

// develop is a rule with one action, for tests about one session per issue.
var develop = crew.Rule{
	Name:   "implement",
	Labels: crew.Labels{Ready: ready, Running: inProgress, Success: readyToReview, Failure: needsAttention},
	Actions: []crew.Action{
		{Name: "development", Prompt: parsedPrompt("development", "Implement development for issue {{.Issue.Ref}}")},
	},
}

// parsedPrompt parses text as the prompt of the action named action, and
// panics when it does not parse, since the rules above are built from known
// prompts.
func parsedPrompt(action crew.ActionName, text string) crew.Prompt {
	p, err := crew.ParsePrompt(action, text)
	if err != nil {
		panic(err)
	}
	return p
}

// epoch dates the issues: issue n was created n minutes after it, so #1 is
// the oldest.
var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// issue is the issue keyed n as a tracker lists it, without its repository.
func issue(n int, states ...crew.State) crew.Issue {
	key := strconv.Itoa(n)
	return crew.NewIssue(crew.IssueData{
		ID: crew.IssueID{Key: key}, Ref: "#" + key, Title: "Issue " + key, URL: "https://example.test/issues/" + key,
		Created: epoch.Add(time.Duration(n) * time.Minute), States: states,
	})
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

// config returns a config over tracker for rules, with a fake harness and
// workspace, rooted in a fresh repository directory.
func config(t *testing.T, tracker port.Tracker, rules ...crew.Rule) engine.Config {
	t.Helper()
	root := filepath.Join(t.TempDir(), "home", "repo")
	worktrees := filepath.Join(root, ".crew", "worktrees")
	if err := os.MkdirAll(worktrees, 0o750); err != nil {
		t.Fatal(err)
	}
	return engine.Config{
		Rules:             rules,
		MaxParallelIssues: 2,
		PollInterval:      poll,
		Tracker:           tracker,
		Harnesses:         harnesses(fake.NewHarness()),
		Workspace:         fake.NewWorkspace(worktrees),
		Root:              root,
		Home:              filepath.Dir(root),
	}
}

// harnesses gives h to the actions that name no agent, as the only harness.
func harnesses(h port.Harness) []engine.AgentHarness {
	return []engine.AgentHarness{{Harness: h}}
}

// start runs an engine for cfg. The test must stop it and call wait.
func start(t *testing.T, cfg engine.Config) *rig {
	t.Helper()
	return run(t, cfg, engine.New(cfg))
}

// run runs e, the engine New made for cfg, as start does, for tests that
// subscribe to it first.
func run(t *testing.T, cfg engine.Config, e *engine.Engine) *rig {
	t.Helper()
	latest := e.SubscribeLatest()
	queue := e.SubscribeQueue(1024)
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{
		t: t, root: cfg.Root, engine: e, cancel: cancel, done: make(chan error, 1), final: make(chan engine.Update, 1),
		queue: queue,
	}
	if h, ok := cfg.Harnesses[0].Harness.(*fake.Harness); ok {
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
	Issue(key string) (crew.Issue, bool)
}, key string) []crew.State {
	t.Helper()
	i, ok := tr.Issue(key)
	if !ok {
		t.Fatalf("issue %s is gone", key)
	}
	return i.States()
}

// fakeWorkspace returns cfg's workspace, the fake one config made.
func fakeWorkspace(t *testing.T, cfg engine.Config) *fake.Workspace {
	t.Helper()
	w, ok := cfg.Workspace.(*fake.Workspace)
	if !ok {
		t.Fatalf("workspace is %T, want the fake one", cfg.Workspace)
	}
	return w
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
	out := make([]time.Duration, 0, len(spans))
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

		want := []string{"issue-1-acceptance", "issue-1-development", "issue-2-acceptance", "issue-2-development"}
		if names := slices.Sorted(maps.Keys(sessions)); !reflect.DeepEqual(names, want) {
			t.Fatalf("sessions run in workspaces %v, want %v", names, want)
		}
		checkTookTwoIssues(t, tr, sessions)
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

		checkEachLogHoldsItsOutput(t, r.root, sessions)
		wantLogs := []string{
			".crew/logs/issue-1-acceptance.log", ".crew/logs/issue-1-development.log",
			".crew/logs/issue-2-acceptance.log", ".crew/logs/issue-2-development.log",
		}
		if logs := reportedLogs(tr); !reflect.DeepEqual(logs, wantLogs) {
			t.Errorf("failure reports name logs %v, want %v", logs, wantLogs)
		}
		if !slices.ContainsFunc(final.Events, func(e core.Published) bool { _, ok := e.(core.Stopped); return ok }) {
			t.Errorf("last update's events = %#v, want a Stopped event", final.Events)
		}
	})
}

// checkTookTwoIssues checks that sessions run the prompts of issues 1 and 2,
// which moved to in progress, while issue 3 waits in ready.
func checkTookTwoIssues(t *testing.T, tr *fake.Tracker, sessions map[string]*fake.Session) {
	t.Helper()
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
}

// checkEachLogHoldsItsOutput checks that the log of each session, under
// root, holds what that session wrote.
func checkEachLogHoldsItsOutput(t *testing.T, root string, sessions map[string]*fake.Session) {
	t.Helper()
	for name := range sessions {
		got, err := os.ReadFile(filepath.Join(root, ".crew", "logs", name+".log"))
		if err != nil {
			t.Fatalf("log of %s: %v", name, err)
		}
		if want := "output of " + name + "\n"; string(got) != want {
			t.Errorf("log of %s = %q, want %q", name, got, want)
		}
	}
}

// reportedLogs returns the logs the tracker's failure reports name, sorted.
func reportedLogs(tr *fake.Tracker) []string {
	var logs []string
	for _, rep := range tr.Reports() {
		for _, f := range rep.Failures {
			logs = append(logs, f.Log)
		}
	}
	slices.Sort(logs)
	return logs
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
		failed := slices.ContainsFunc(final.Snapshot.Recent, func(e core.Published) bool {
			f, ok := e.(core.ListingFailed)
			return ok && f.At.Sub(t0) == 10*time.Minute
		})
		if !failed {
			t.Errorf("recent events = %#v, want the listing failing at 10m", final.Snapshot.Recent)
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
		session.End(port.Verdict{Succeeded: true, Reason: "done"})

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

// issueID returns the id the engine gives the issue keyed key: config roots
// it in a directory named repo, and the fake trackers name no repository.
func issueID(key string) crew.IssueID { return crew.IssueID{Repository: "repo", Key: key} }
