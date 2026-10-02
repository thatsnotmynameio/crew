package fake_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

func issue(key string, states ...crew.State) crew.Issue {
	return crew.Issue{Key: key, Ref: "#" + key, Title: "Issue " + key, States: states}
}

func keys(issues []crew.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Key)
	}
	return out
}

func TestTrackerListsOpenIssuesInAnyGivenStateWithAllTheirStates(t *testing.T) {
	tr := fake.NewTracker(
		issue("1", crew.Ready),
		issue("2", crew.ReadyToReview),
		issue("3", crew.Done),
		issue("4", crew.Ready, crew.NeedsAttention),
		issue("5", crew.Ready),
	)
	tr.Close("5")

	got, err := tr.List(context.Background(), []crew.State{crew.Ready, crew.ReadyToReview})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"1", "2", "4"}; !reflect.DeepEqual(keys(got), want) {
		t.Fatalf("List keys = %v, want %v", keys(got), want)
	}
	if want := []crew.State{crew.Ready, crew.NeedsAttention}; !reflect.DeepEqual(got[2].States, want) {
		t.Errorf("issue 4 states = %v, want %v", got[2].States, want)
	}
	if got[0].Ref != "#1" || got[0].Title != "Issue 1" {
		t.Errorf("issue 1 = %+v, want its ref and title", got[0])
	}
}

func TestTrackerMoveLeavesTheIssueInExactlyTheNewState(t *testing.T) {
	tr := fake.NewTracker(issue("1", crew.Ready))
	ctx := context.Background()

	if err := tr.Move(ctx, "1", crew.Ready, crew.InProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	got, ok := tr.Issue("1")
	if !ok {
		t.Fatal("issue 1 is gone")
	}
	if want := []crew.State{crew.InProgress}; !reflect.DeepEqual(got.States, want) {
		t.Errorf("states = %v, want %v", got.States, want)
	}
	if want := []fake.Move{{Key: "1", From: crew.Ready, To: crew.InProgress}}; !reflect.DeepEqual(tr.Moves(), want) {
		t.Errorf("Moves = %v, want %v", tr.Moves(), want)
	}
	listed, err := tr.List(ctx, []crew.State{crew.Ready})
	if err != nil || len(listed) != 0 {
		t.Errorf("List(ready) = %v, %v; want no issues", keys(listed), err)
	}
}

func TestTrackerMoveFromAStateTheIssueLeftIsMovedMeanwhile(t *testing.T) {
	tr := fake.NewTracker(issue("1", crew.NeedsAttention))

	err := tr.Move(context.Background(), "1", crew.Ready, crew.InProgress)
	if !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Fatalf("Move = %v, want ErrMovedMeanwhile", err)
	}
	got, _ := tr.Issue("1")
	if want := []crew.State{crew.NeedsAttention}; !reflect.DeepEqual(got.States, want) {
		t.Errorf("states = %v, want them unchanged at %v", got.States, want)
	}
	if len(tr.Moves()) != 0 {
		t.Errorf("Moves = %v, want none", tr.Moves())
	}
}

func TestTrackerMoveOfAClosedIssueIsMovedMeanwhile(t *testing.T) {
	tr := fake.NewTracker(issue("1", crew.InProgress))
	tr.Close("1")

	err := tr.Move(context.Background(), "1", crew.InProgress, crew.ReadyToReview)
	if !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Fatalf("Move = %v, want ErrMovedMeanwhile", err)
	}
}

func TestTrackerSetStatesChangesAnIssueFromOutside(t *testing.T) {
	tr := fake.NewTracker(issue("1", crew.InProgress))
	tr.SetStates("1", crew.Paused)

	err := tr.Move(context.Background(), "1", crew.InProgress, crew.ReadyToReview)
	if !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Fatalf("Move = %v, want ErrMovedMeanwhile", err)
	}
}

func TestTrackerScriptedMoveFailuresComeInOrderThenMovesSucceed(t *testing.T) {
	tr := fake.NewTracker(issue("1", crew.Ready))
	transient := errors.New("network down")
	tr.FailMoves("1", transient, port.ErrRefused)
	ctx := context.Background()

	err := tr.Move(ctx, "1", crew.Ready, crew.InProgress)
	if !errors.Is(err, transient) || errors.Is(err, port.ErrRefused) || errors.Is(err, port.ErrMovedMeanwhile) {
		t.Errorf("first Move = %v, want the transient error", err)
	}
	if err := tr.Move(ctx, "1", crew.Ready, crew.InProgress); !errors.Is(err, port.ErrRefused) {
		t.Errorf("second Move = %v, want ErrRefused", err)
	}
	if got, _ := tr.Issue("1"); !reflect.DeepEqual(got.States, []crew.State{crew.Ready}) {
		t.Errorf("states after failed moves = %v, want [ready]", got.States)
	}
	if err := tr.Move(ctx, "1", crew.Ready, crew.InProgress); err != nil {
		t.Errorf("third Move = %v, want success", err)
	}
}

func TestTrackerRecordsFailureReportsAndScriptsTheirFailures(t *testing.T) {
	tr := fake.NewTracker(issue("1", crew.NeedsAttention))
	report := crew.FailureReport{IssueKey: "1", IssueRef: "#1", Failures: []crew.ActionFailure{
		{Action: "development", Reason: "tests fail", Workspace: "issue-1-development", Log: ".crew/logs/issue-1-development.log"},
	}}
	tr.FailReports("1", port.ErrRefused)
	ctx := context.Background()

	if err := tr.ReportFailure(ctx, report); !errors.Is(err, port.ErrRefused) {
		t.Fatalf("first ReportFailure = %v, want ErrRefused", err)
	}
	if len(tr.Reports()) != 0 {
		t.Fatalf("Reports = %v, want none after a failed report", tr.Reports())
	}
	if err := tr.ReportFailure(ctx, report); err != nil {
		t.Fatalf("second ReportFailure: %v", err)
	}
	if want := []crew.FailureReport{report}; !reflect.DeepEqual(tr.Reports(), want) {
		t.Errorf("Reports = %+v, want %+v", tr.Reports(), want)
	}
}

func TestTrackerIsSafeForConcurrentUse(t *testing.T) {
	tr := fake.NewTracker()
	for i := range 20 {
		tr.Add(issue(string(rune('a'+i)), crew.Ready))
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			key := string(rune('a' + i))
			if err := tr.Move(context.Background(), key, crew.Ready, crew.InProgress); err != nil {
				t.Errorf("Move %s: %v", key, err)
			}
			if _, err := tr.List(context.Background(), []crew.State{crew.Ready}); err != nil {
				t.Errorf("List: %v", err)
			}
		})
	}
	wg.Wait()
	if got := len(tr.Moves()); got != 20 {
		t.Errorf("Moves = %d, want 20", got)
	}
}

func TestTrackerFactoryValidatesItsSectionAndReturnsTheTracker(t *testing.T) {
	tr := fake.NewTracker()
	factory := fake.TrackerFactory(tr)

	var got any
	built, err := factory(func(target any) error {
		got = target
		return nil
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if built != port.Tracker(tr) {
		t.Errorf("factory built %v, want the given tracker", built)
	}
	if _, ok := got.(*fake.TrackerSettings); !ok {
		t.Errorf("factory decoded into %T, want *fake.TrackerSettings", got)
	}

	invalid := errors.New("tracker.lables (line 3): unknown key")
	if _, err := factory(func(any) error { return invalid }); !errors.Is(err, invalid) {
		t.Errorf("factory with an invalid section = %v, want the decode error", err)
	}
}

// start starts a session on h, failing the test on error.
func start(t *testing.T, h *fake.Harness, prompt string) port.Session {
	t.Helper()
	s, err := h.Start(context.Background(), port.Run{Dir: t.TempDir(), Prompt: prompt})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s
}

// waitOutcome waits for s to end, failing the test after a second.
func waitOutcome(t *testing.T, s port.Session) crew.Outcome {
	t.Helper()
	done := make(chan crew.Outcome, 1)
	go func() { done <- s.Wait() }()
	select {
	case o := <-done:
		return o
	case <-time.After(time.Second):
		t.Fatal("the session did not end")
		return crew.Outcome{}
	}
}

func TestHarnessSessionEndsWithTheOutcomeTheTestReleasesItWith(t *testing.T) {
	h := fake.NewHarness()
	s := start(t, h, "implement #1")

	ended := make(chan crew.Outcome, 1)
	go func() { ended <- s.Wait() }()
	select {
	case o := <-ended:
		t.Fatalf("Wait returned %+v before the session was released", o)
	case <-time.After(50 * time.Millisecond):
	}

	sessions := h.Sessions()
	if len(sessions) != 1 || sessions[0].Run().Prompt != "implement #1" {
		t.Fatalf("Sessions = %v, want the one started with its prompt", sessions)
	}
	want := crew.Outcome{Succeeded: true, Reason: "opened a pull request"}
	sessions[0].End(want)
	if got := waitOutcome(t, s); got != want {
		t.Errorf("Wait = %+v, want %+v", got, want)
	}
	if got := s.Wait(); got != want {
		t.Errorf("second Wait = %+v, want %+v", got, want)
	}
}

func TestHarnessNextReturnsSessionsInStartOrder(t *testing.T) {
	h := fake.NewHarness()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() {
		for _, prompt := range []string{"first", "second"} {
			if _, err := h.Start(context.Background(), port.Run{Prompt: prompt}); err != nil {
				t.Errorf("Start: %v", err)
			}
		}
	}()
	for _, want := range []string{"first", "second"} {
		s, err := h.Next(ctx)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if s.Run().Prompt != want {
			t.Errorf("Next prompt = %q, want %q", s.Run().Prompt, want)
		}
	}

	short, cancelShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShort()
	if s, err := h.Next(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Next with no new session = %v, %v; want the context's error", s, err)
	}
}

func TestHarnessStopEndsTheSessionAsFailed(t *testing.T) {
	h := fake.NewHarness()
	s := start(t, h, "implement #1")

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := waitOutcome(t, s); got.Succeeded || got.Reason == "" {
		t.Errorf("Wait = %+v, want a failure with a reason", got)
	}
	if !h.Sessions()[0].Stopped() {
		t.Error("Stopped = false, want true")
	}
}

func TestHarnessIgnoringStopEndsTheSessionOnlyAtTheDeadline(t *testing.T) {
	h := fake.NewHarness()
	h.IgnoreStop(true)
	s := start(t, h, "implement #1")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	began := time.Now()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(began); elapsed < 100*time.Millisecond {
		t.Errorf("Stop returned after %v, want it to wait for the deadline", elapsed)
	}
	if got := waitOutcome(t, s); got.Succeeded {
		t.Errorf("Wait = %+v, want a failure", got)
	}
}

func TestHarnessFactoryValidatesItsSectionAndReturnsTheHarness(t *testing.T) {
	h := fake.NewHarness()
	factory := fake.HarnessFactory(h)

	var got any
	built, err := factory(func(target any) error {
		got = target
		return nil
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if built != port.Harness(h) {
		t.Errorf("factory built %v, want the given harness", built)
	}
	if _, ok := got.(*fake.HarnessSettings); !ok {
		t.Errorf("factory decoded into %T, want *fake.HarnessSettings", got)
	}

	invalid := errors.New("harness.effort (line 4): unknown key")
	if _, err := factory(func(any) error { return invalid }); !errors.Is(err, invalid) {
		t.Errorf("factory with an invalid section = %v, want the decode error", err)
	}
}

func TestPreparationRecordsStatesAndFailsWhenScripted(t *testing.T) {
	tr := fake.NewPreparingTracker()
	states := []crew.State{crew.Ready, crew.InProgress}

	if err := tr.Prepare(context.Background(), states); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	broken := errors.New("gh is not installed")
	tr.Fail(broken)
	if err := tr.Prepare(context.Background(), states); !errors.Is(err, broken) {
		t.Errorf("Prepare = %v, want the scripted error", err)
	}
	if want := [][]crew.State{states, states}; !reflect.DeepEqual(tr.Calls(), want) {
		t.Errorf("Calls = %v, want %v", tr.Calls(), want)
	}
}

func TestWorkspaceCreatesUniqueDirectoriesPerIssueAndAction(t *testing.T) {
	root := t.TempDir()
	ws := fake.NewWorkspace(root)
	ctx := context.Background()

	first, err := ws.Create(ctx, issue("42"), "development")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first.Name != "issue-42-development" {
		t.Errorf("Name = %q, want issue-42-development", first.Name)
	}
	if !filepath.IsAbs(first.Dir) || filepath.Dir(first.Dir) != root {
		t.Errorf("Dir = %q, want an absolute directory under %q", first.Dir, root)
	}
	if info, err := os.Stat(first.Dir); err != nil || !info.IsDir() {
		t.Errorf("Dir %q is not a directory: %v", first.Dir, err)
	}
	if first.Branch == "" {
		t.Error("Branch is empty")
	}

	second, err := ws.Create(ctx, issue("42"), "development")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.Name == first.Name || second.Dir == first.Dir || second.Branch == first.Branch {
		t.Errorf("second workspace %+v repeats the first %+v", second, first)
	}
}

func TestWorkspaceNamesStayUniqueUnderConcurrentCreates(t *testing.T) {
	ws := fake.NewWorkspace(t.TempDir())
	var (
		mu    sync.Mutex
		names = map[string]bool{}
		wg    sync.WaitGroup
	)
	for range 10 {
		wg.Go(func() {
			s, err := ws.Create(context.Background(), issue("7"), "review")
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			names[s.Name] = true
		})
	}
	wg.Wait()
	if len(names) != 10 {
		t.Errorf("got %d distinct names, want 10: %v", len(names), names)
	}
	if got := len(ws.Spaces()); got != 10 {
		t.Errorf("Spaces = %d, want 10", got)
	}
}
