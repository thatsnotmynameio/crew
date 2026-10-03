package app_test

import (
	"bytes"
	"context"
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
	return crew.Issue{
		Key: key, Ref: "#" + key, Title: "Issue " + key, URL: "https://example.test/issues/" + key, States: states,
	}
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
	n, _ := b.buf.Write(p) // a bytes.Buffer's Write always returns a nil error
	return n, nil
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

// states returns the states of #1, the issue every test here runs.
func states(t *testing.T, tr *fake.Tracker) []crew.State {
	t.Helper()
	i, ok := tr.Issue("1")
	if !ok {
		t.Fatal("issue 1 is gone")
	}
	return i.States
}

var stamped = regexp.MustCompile(`^\d\d:\d\d:\d\d crew: `)

// containsAll fails the test for each of wants that out lacks.
func containsAll(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q; it is:\n%s", want, out)
		}
	}
}

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
				printsTimestampedEventLines(t, tc.terminal, tc.plain)
			})
		})
	}
}

// printsTimestampedEventLines runs one issue through one action, with the
// given terminal and plain options, and checks the event lines it prints.
func printsTimestampedEventLines(t *testing.T, terminal, plain bool) {
	t.Helper()
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	r := options(t, oneAction, tr, h)
	r.opts.Terminal, r.opts.Plain = terminal, plain
	r.start()

	next(t, h).End(success)
	synctest.Wait()
	r.signals <- syscall.SIGTERM

	if code := <-r.code; code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
		t.Errorf("#1 is in %v, want the stage's on_success, ready to review", got)
	}
	out := r.stdout.String()
	containsAll(t, out,
		`crew: implement took #1 "Issue 1" (ready -> in progress)`,
		`crew: #1 implement/development started on branch crew/issue-1-development, `+
			`log .crew/logs/issue-1-development.log`,
		`crew: #1 implement/development succeeded: opened a pull request`,
		`crew: #1 moved from in progress to ready to review`,
	)
	for line := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
		if !stamped.MatchString(line) {
			t.Errorf("line %q is not a timestamped event line", line)
		}
	}
}

// Covers AE1 through the wiring: the check in the config runs through the
// checker the options carry, and its failure fails the stage.
func TestAnActionsCheckRunsThroughTheOptionsChecker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		checker := fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{Print: "no open pull request\n", Exit: 1})
		body := strings.Replace(oneAction, `{{.Issue.Ref}}"`+"\n", `{{.Issue.Ref}}"`+"\n        check: gh pr list\n", 1)
		r := options(t, body, tr, h)
		r.opts.Plain = true
		r.opts.Checker = checker
		r.start()

		next(t, h).End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if got := len(checker.Checks()); got != 1 {
			t.Fatalf("checks run = %d, want 1", got)
		}
		if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("#1 is in %v, want needs attention", got)
		}
		if out, want := r.stdout.String(), "the check failed: no open pull request"; !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q; it is:\n%s", want, out)
		}
	})
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
		containsAll(t, r.stdout.String(),
			"crew: run time of 1h0m0s is up: taking no new issues, winding down",
			"crew: stopped",
		)
	})
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
		if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
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

// recordingExtras returns a registry of tr and h that records in extras the
// extra labels the tracker is built with.
func recordingExtras(tr *fake.Tracker, h *fake.Harness, extras *[]crew.State) registry.Registry {
	return registry.New(
		map[string]port.TrackerFactory{"fake": func(decode port.Decode, states, got []crew.State) (port.Tracker, error) {
			*extras = got
			return fake.TrackerFactory(tr)(decode, states, got)
		}},
		map[string]port.HarnessFactory{"fake": fake.HarnessFactory(h)},
	)
}

// Covers AE4 and R8: an issue with an extra label and a stage's label is
// taken by that stage and loses the extra; an issue whose only crew label is
// an extra is never taken. The tracker is built with the config's extras.
func TestAnExtraLabelNeverBlocksAStageAndNeverStartsOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const waitingBrainstorm crew.State = "waiting brainstorm"
		tr := fake.NewTracker(issue("1", ready), issue("2"))
		tr.SetExtras("1", waitingBrainstorm)
		tr.SetExtras("2", waitingBrainstorm)
		h := fake.NewHarness()
		r := options(t, "extra_labels:\n  - label: waiting brainstorm\n"+oneAction, tr, h)
		var extras []crew.State
		r.opts.Registry = recordingExtras(tr, h, &extras)
		r.start()

		s := next(t, h)
		if got := s.Run().Prompt; got != "Implement development for issue #1" {
			t.Errorf("the session's prompt = %q, want #1's", got)
		}
		if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{inProgress}) {
			t.Errorf("#1 is in %v, want in progress", got)
		}
		if got := tr.Extras("1"); len(got) != 0 {
			t.Errorf("#1 still has the extras %v, want none", got)
		}
		s.End(success)
		synctest.Wait()
		time.Sleep(time.Hour) // later ticks
		synctest.Wait()
		r.signals <- syscall.SIGTERM

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if want := []crew.State{waitingBrainstorm}; !reflect.DeepEqual(extras, want) {
			t.Errorf("the tracker was built with the extras %v, want %v", extras, want)
		}
		wantMoves := []fake.Move{
			{Key: "1", From: ready, To: inProgress},
			{Key: "1", From: inProgress, To: readyToReview},
		}
		if got := tr.Moves(); !reflect.DeepEqual(got, wantMoves) {
			t.Errorf("moves = %v, want %v, and none of #2", got, wantMoves)
		}
		if want := []crew.State{waitingBrainstorm}; !reflect.DeepEqual(tr.Extras("2"), want) {
			t.Errorf("#2 has the extras %v, want %v", tr.Extras("2"), want)
		}
	})
}
