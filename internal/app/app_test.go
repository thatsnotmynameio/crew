package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// The workflow's states in these tests, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	inReview       crew.State = "in review"
	needsAttention crew.State = "needs attention"
	readyToMerge   crew.State = "ready to merge"
)

// oneAction is a config with one stage of one action, run by the fakes.
const oneAction = `
config:
  harness: fake
tracker:
  name: fake
workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement development for issue {{.Issue.Ref}}"
`

// draft is the boss's draft config (KTD5), with the fakes named in place of
// github and claude.
const draft = `
config:
  poll_interval_seconds: 300
  max_parallel_issues: 2
  harness: fake
  model: claude-opus-5-5
tracker:
  name: fake
workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: acceptance
        prompt: "Implement test acceptance for issue {{.Issue.Ref}}"
      - name: development
        prompt: "Implement development for issue {{.Issue.Ref}}"
  - name: review
    label: ready to review
    moves_to: in review
    on_success: ready to merge
    on_failure: needs attention
    actions:
      - name: custom_review
        prompt: "Review implementation for issue {{.Issue.Ref}}"
`

var success = crew.Outcome{Succeeded: true, Reason: "opened a pull request"}

func issue(key string, states ...crew.State) crew.Issue {
	return crew.Issue{Key: key, Ref: "#" + key, Title: "Issue " + key, URL: "https://example.test/issues/" + key, States: states}
}

// listCounter is a fake tracker that counts its listings.
type listCounter struct {
	*fake.Tracker

	mu    sync.Mutex
	lists int
}

func (l *listCounter) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	l.mu.Lock()
	l.lists++
	l.mu.Unlock()
	return l.Tracker.List(ctx, states)
}

func (l *listCounter) listed() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lists
}

// syncBuffer is a buffer safe to write from a renderer while a test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// crewRun is one run of app.Run against the fakes.
type crewRun struct {
	opts    app.Options
	stdout  *syncBuffer
	stderr  *syncBuffer
	signals chan os.Signal
	code    chan int
}

// options returns app options over tracker and harness, registered as fake,
// for a repository whose .crew/config.yaml is body. The output is not a
// terminal.
func options(t *testing.T, body string, tracker port.Tracker, harness port.Harness) *crewRun {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew", "worktrees"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &crewRun{stdout: &syncBuffer{}, stderr: &syncBuffer{}, signals: make(chan os.Signal, 2), code: make(chan int, 1)}
	r.opts = app.Options{
		Registry: registry.New(
			map[string]port.TrackerFactory{"fake": fake.TrackerFactory(tracker)},
			map[string]port.HarnessFactory{"fake": fake.HarnessFactory(harness)},
		),
		Workspace: func(root string) port.Workspace {
			return fake.NewWorkspace(filepath.Join(root, ".crew", "worktrees"))
		},
		Root:    root,
		Home:    filepath.Dir(root),
		Stdout:  r.stdout,
		Stderr:  r.stderr,
		Group:   &proc.Group{},
		Signals: r.signals,
	}
	return r
}

// start runs app.Run in its own goroutine.
func (r *crewRun) start() {
	go func() { r.code <- app.Run(context.Background(), r.opts) }()
}

// next returns the next session the harness starts.
func next(t *testing.T, h *fake.Harness) *fake.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := h.Next(ctx)
	if err != nil {
		t.Fatalf("no session started: %v", err)
	}
	return s
}

func states(t *testing.T, tr *fake.Tracker, key string) []crew.State {
	t.Helper()
	i, ok := tr.Issue(key)
	if !ok {
		t.Fatalf("issue %s is gone", key)
	}
	return i.States
}

var stamped = regexp.MustCompile(`^\d\d:\d\d:\d\d crew: `)

// Covers AE6 (lines side).
func TestWithoutATerminalOrWithPlainItPrintsTimestampedEventLines(t *testing.T) {
	for _, tc := range []struct {
		name            string
		terminal, plain bool
	}{
		{"stdout is not a terminal", false, false},
		{"--plain on a terminal", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr := fake.NewTracker(issue("1", ready))
				h := fake.NewHarness()
				r := options(t, oneAction, tr, h)
				r.opts.Terminal, r.opts.Plain = tc.terminal, tc.plain
				r.start()

				next(t, h).End(success)
				synctest.Wait()
				r.signals <- syscall.SIGTERM

				if code := <-r.code; code != 0 {
					t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
				}
				if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
					t.Errorf("#1 is in %v, want the stage's on_success, ready to review", got)
				}
				out := r.stdout.String()
				for _, want := range []string{
					`crew: implement took #1 "Issue 1" (ready -> in progress)`,
					`crew: #1 implement/development started on branch crew/issue-1-development, log .crew/logs/issue-1-development.log`,
					`crew: #1 implement/development succeeded: opened a pull request`,
					`crew: #1 moved from in progress to ready to review`,
				} {
					if !strings.Contains(out, want) {
						t.Errorf("stdout lacks %q; it is:\n%s", want, out)
					}
				}
				for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
					if !stamped.MatchString(line) {
						t.Errorf("line %q is not a timestamped event line", line)
					}
				}
			})
		})
	}
}

// Covers AE2 through the wiring.
func TestARunTimeLimitWindsCrewDownAndExitsZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker()
		h := fake.NewHarness()
		r := options(t, strings.Replace(oneAction, "config:\n", "config:\n  run_time_limit_seconds: 3600\n", 1), tr, h)
		r.opts.Plain = true
		t0 := time.Now()
		r.start()

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if got := time.Since(t0); got != time.Hour {
			t.Errorf("crew exited after %v, want 1h0m0s", got)
		}
		out := r.stdout.String()
		for _, want := range []string{
			"crew: run time of 1h0m0s is up: taking no new issues, winding down",
			"crew: stopped",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q; it is:\n%s", want, out)
			}
		}
	})
}

// Covers AE4.
func TestAnUnregisteredHarnessExitsTwoBeforeAnyListingNamingTheRegisteredOnes(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	r := options(t, strings.Replace(oneAction, "harness: fake", "harness: codex", 1), tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	stderr := r.stderr.String()
	for _, want := range []string{"harness", `"codex"`, "fake"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not name %s", stderr, want)
		}
	}
	if out := r.stdout.String(); out != "" {
		t.Errorf("stdout = %q, want nothing", out)
	}
}

// Covers AE3.
func TestAConfigWithTrackerLabelsExitsTwoNamingTheKey(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	r := options(t, strings.Replace(oneAction, "  name: fake\n", "  name: fake\n  labels:\n    ready: ready\n", 1), tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	if stderr := r.stderr.String(); !strings.Contains(stderr, "tracker.labels (line 6): unknown key") {
		t.Errorf("stderr = %q, want it to name tracker.labels and its line", stderr)
	}
}

func TestAFailingEnvironmentCheckExitsTwoBeforeAnyListing(t *testing.T) {
	tr := fake.NewPreparingTracker(issue("1", ready))
	counter := &listCounter{Tracker: tr.Tracker}
	tr.Fail(errors.New("gh is not logged in"))
	r := options(t, oneAction, struct {
		*listCounter
		*fake.Preparation
	}{counter, tr.Preparation}, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := counter.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	if stderr := r.stderr.String(); !strings.Contains(stderr, "gh is not logged in") || !strings.Contains(stderr, "tracker") {
		t.Errorf("stderr = %q, want the tracker's failed check", stderr)
	}
	if out := r.stdout.String(); out != "" {
		t.Errorf("stdout = %q, want nothing", out)
	}
}

func TestTheDraftConfigRunsImplementThenReviewAcrossTwoTicks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		r := options(t, draft, tr, h)
		r.start()

		prompts := map[string]bool{}
		for range 2 {
			s := next(t, h)
			prompts[s.Run().Prompt] = true
			s.End(success)
		}
		want := map[string]bool{"Implement test acceptance for issue #1": true, "Implement development for issue #1": true}
		if !reflect.DeepEqual(prompts, want) {
			t.Errorf("the first tick's prompts = %v, want %v", prompts, want)
		}
		synctest.Wait()
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Fatalf("after implement, #1 is in %v, want ready to review", got)
		}

		time.Sleep(300 * time.Second) // the second tick
		review := next(t, h)
		if got := review.Run().Prompt; got != "Review implementation for issue #1" {
			t.Errorf("the second tick's prompt = %q, want the review stage's", got)
		}
		review.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGINT

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		wantMoves := []fake.Move{
			{Key: "1", From: ready, To: inProgress},
			{Key: "1", From: inProgress, To: readyToReview},
			{Key: "1", From: readyToReview, To: inReview},
			{Key: "1", From: inReview, To: readyToMerge},
		}
		if got := tr.Moves(); !reflect.DeepEqual(got, wantMoves) {
			t.Errorf("moves = %v, want %v", got, wantMoves)
		}
	})
}

func TestASignalStopsCrewWithExitZeroAfterTheStopSequence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		r := options(t, oneAction, tr, h)
		r.start()
		session := next(t, h)

		r.signals <- syscall.SIGTERM

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if !session.Stopped() {
			t.Error("the running session was not stopped")
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("#1 is in %v when crew returned, want needs attention", got)
		}
		if n := len(tr.Reports()); n != 1 {
			t.Errorf("crew posted %d failure reports, want 1", n)
		}
	})
}

// child starts a long sleep in group, standing in for a session's process.
func child(t *testing.T, group *proc.Group) *proc.Process {
	t.Helper()
	p, err := group.Start(proc.Command{Name: "sleep", Args: []string{"60"}}, nil, nil)
	if err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p
}

// killed fails the test unless p ends within a few seconds.
func killed(t *testing.T, p *proc.Process) {
	t.Helper()
	ended := make(chan error, 1)
	go func() { ended <- p.Wait() }()
	select {
	case err := <-ended:
		if err == nil {
			t.Error("the child exited cleanly, want it killed")
		}
	case <-time.After(5 * time.Second):
		t.Error("the child still runs: the forced exit did not kill it")
	}
}

// code waits for app.Run's exit code, failing the test after a few seconds.
func (r *crewRun) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case code := <-r.code:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("crew did not return")
		return -1
	}
}

// eventually reports whether stdout satisfies ok within a few seconds.
func (r *crewRun) eventually(ok func(stdout string) bool) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok(r.stdout.String()) {
			return true
		}
	}
	return false
}

// release ends session and waits for the engine's last event line, so the
// engine is done with the repository before the test removes it.
func (r *crewRun) release(session *fake.Session) {
	session.End(crew.Outcome{Reason: "released by the test"})
	r.eventually(func(out string) bool { return strings.Contains(out, "crew: stopped") })
}

func TestASecondSignalKillsEveryProcessAndExitsOneAtOnce(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	h.IgnoreStop(true) // the stop sequence would wait 10 seconds for it
	r := options(t, oneAction, tr, h)
	sleeper := child(t, r.opts.Group)
	r.start()
	session := next(t, h)

	r.signals <- syscall.SIGINT
	r.signals <- syscall.SIGINT

	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	killed(t, sleeper)
	r.release(session)
}

// tuiRun returns a run whose stdout is a terminal, with keys typed through
// the returned writer.
func tuiRun(t *testing.T, tr port.Tracker, h port.Harness) (*crewRun, io.WriteCloser) {
	t.Helper()
	r := options(t, oneAction, tr, h)
	keys, typed := io.Pipe()
	t.Cleanup(func() { _ = typed.Close() })
	r.opts.Terminal, r.opts.Stdin = true, keys
	return r, typed
}

func TestOnATerminalQuittingTheTUIStopsCrewWithExitZero(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	r, keys := tuiRun(t, tr, h)
	r.start()
	session := next(t, h)

	if _, err := io.WriteString(keys, "q"); err != nil {
		t.Fatal(err)
	}

	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	if !session.Stopped() {
		t.Error("the running session was not stopped")
	}
	if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
		t.Errorf("#1 is in %v when crew returned, want needs attention", got)
	}
	// What the TUI draws is its own tests' business; here the output only
	// shows that the TUI ran instead of the line renderer.
	if out := r.stdout.String(); !strings.Contains(out, "\x1b[") || strings.Contains(out, " crew: ") {
		t.Errorf("stdout is not the TUI's alone:\n%q", out)
	}
}

func TestOnATerminalQuittingTheTUITwiceKillsEveryProcessAndExitsOne(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	h.IgnoreStop(true)
	r, keys := tuiRun(t, tr, h)
	sleeper := child(t, r.opts.Group)
	r.start()
	session := next(t, h)

	if _, err := io.WriteString(keys, "qq"); err != nil {
		t.Fatal(err)
	}

	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	killed(t, sleeper)
	session.End(crew.Outcome{Reason: "released by the test"})
}

// SIGHUP comes when the terminal crew runs in closes.
func TestSIGHUPStopsCrewLikeSIGTERM(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		r := options(t, oneAction, tr, h)
		r.start()
		session := next(t, h)

		r.signals <- syscall.SIGHUP

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if !session.Stopped() {
			t.Error("the running session was not stopped")
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("#1 is in %v when crew returned, want needs attention", got)
		}
	})
}

// hangingTracker is a fake tracker whose environment check runs until its
// context ends, as a hung gh would. atCheck, when set, is the first thing
// the check calls; ignoreEnd makes the check succeed all the same.
type hangingTracker struct {
	*listCounter

	checking  chan struct{}
	atCheck   func()
	ignoreEnd bool
}

func newHangingTracker(issues ...crew.Issue) *hangingTracker {
	return &hangingTracker{listCounter: &listCounter{Tracker: fake.NewTracker(issues...)}, checking: make(chan struct{})}
}

// Prepare implements port.Preparer.
func (h *hangingTracker) Prepare(ctx context.Context, _ []crew.State) error {
	close(h.checking)
	if h.atCheck != nil {
		h.atCheck()
	}
	<-ctx.Done()
	if h.ignoreEnd {
		return nil
	}
	return ctx.Err()
}

func TestASignalDuringTheEnvironmentChecksKillsEveryProcessAndExitsTwo(t *testing.T) {
	tr := newHangingTracker(issue("1", ready))
	r := options(t, oneAction, tr, fake.NewHarness())
	sleeper := child(t, r.opts.Group) // a check's process, still running
	r.start()
	<-tr.checking

	r.signals <- syscall.SIGINT

	if code := r.exitCode(t); code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, r.stderr)
	}
	killed(t, sleeper)
	if stderr := r.stderr.String(); !strings.Contains(stderr, "stopped during the environment checks") {
		t.Errorf("stderr = %q, want it to say crew stopped during the environment checks", stderr)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	if out := r.stdout.String(); out != "" {
		t.Errorf("stdout = %q, want nothing", out)
	}
}

func TestHungEnvironmentChecksTimeOutAfterTenMinutesAndExitTwo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := newHangingTracker(issue("1", ready))
		r := options(t, oneAction, tr, fake.NewHarness())
		start := time.Now()
		r.start()

		code := <-r.code

		if code != 2 {
			t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, r.stderr)
		}
		if took := time.Since(start); took != 10*time.Minute {
			t.Errorf("the checks ended after %v, want 10m0s", took)
		}
		if stderr := r.stderr.String(); !strings.Contains(stderr, "timed out") {
			t.Errorf("stderr = %q, want it to say the environment checks timed out", stderr)
		}
		if n := tr.listed(); n != 0 {
			t.Errorf("the tracker listed %d times, want none", n)
		}
	})
}

func TestASignalAsTheEnvironmentChecksSucceedStillStopsCrew(t *testing.T) {
	tr := newHangingTracker(issue("1", ready))
	r := options(t, oneAction, tr, fake.NewHarness())
	tr.atCheck = func() { r.signals <- syscall.SIGTERM }
	tr.ignoreEnd = true // the checks finish as the signal arrives
	r.start()

	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	if !strings.Contains(r.stdout.String(), "crew: stopped") {
		t.Errorf("stdout lacks the stop; it is:\n%s", r.stdout)
	}
}
