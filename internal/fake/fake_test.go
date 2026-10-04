package fake_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The workflow's states in these tests, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	needsAttention crew.State = "needs attention"
	// waitingBrainstorm is an extra label: parked work no stage takes.
	waitingBrainstorm crew.State = "waiting brainstorm"
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
	states := []crew.State{ready, inProgress}

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

func TestWorkspaceListsAsScriptedOneListingPerCallTheLastRepeating(t *testing.T) {
	ws := fake.NewWorkspace(t.TempDir())
	var sweeper port.Sweeper = ws
	ctx := context.Background()
	if found, err := sweeper.Workspaces(ctx); err != nil || len(found) != 0 {
		t.Errorf("unscripted Workspaces = %+v, %v; want none", found, err)
	}

	clean := port.Found{Space: port.Space{Name: "issue-42-development", Branch: "crew/issue-42-development"}, Listed: true}
	dirty := clean
	dirty.Dirty = true
	broken := errors.New("git: not a git repository")
	ws.ScriptWorkspaces(
		fake.Listing{Found: []port.Found{clean}},
		fake.Listing{Found: []port.Found{dirty}},
		fake.Listing{Err: broken},
	)

	for i, want := range [][]port.Found{{clean}, {dirty}} {
		if got, err := sweeper.Workspaces(ctx); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Workspaces call %d = %+v, %v; want %+v", i+1, got, err, want)
		}
	}
	for i := range 2 {
		if _, err := sweeper.Workspaces(ctx); !errors.Is(err, broken) {
			t.Errorf("Workspaces call %d = %v, want the last listing's error", i+3, err)
		}
	}
}

func TestWorkspaceCountsBeyondAsScriptedForTheBranch(t *testing.T) {
	ws := fake.NewWorkspace(t.TempDir())
	ws.ScriptBeyond("crew/issue-1-lfg", 2, nil)
	ws.ScriptBeyond("crew/issue-2-lfg", 0, port.ErrCommitUnknown)
	offline := errors.New("git: exit status 128")
	ws.ScriptBeyond("crew/issue-3-lfg", 0, offline)
	ctx := context.Background()

	if n, err := ws.Beyond(ctx, "crew/issue-1-lfg", "abc"); n != 2 || err != nil {
		t.Errorf("Beyond = %d, %v; want 2", n, err)
	}
	if _, err := ws.Beyond(ctx, "crew/issue-2-lfg", "abc"); !errors.Is(err, port.ErrCommitUnknown) {
		t.Errorf("Beyond = %v, want port.ErrCommitUnknown", err)
	}
	if _, err := ws.Beyond(ctx, "crew/issue-3-lfg", "abc"); !errors.Is(err, offline) {
		t.Errorf("Beyond = %v, want the scripted error", err)
	}
	if n, err := ws.Beyond(ctx, "crew/issue-9-lfg", "abc"); n != 0 || err != nil {
		t.Errorf("unscripted Beyond = %d, %v; want 0", n, err)
	}
}

func TestWorkspaceRecordsRemovalsAndFailsThoseScriptedToFail(t *testing.T) {
	ws := fake.NewWorkspace(t.TempDir())
	refused := errors.New("fatal: '.crew/worktrees/issue-2-lfg' contains modified or untracked files")
	ws.FailRemove("issue-2-lfg", refused)
	ctx := context.Background()

	if err := ws.Remove(ctx, port.Space{Name: "issue-1-lfg", Branch: "crew/issue-1-lfg"}, true); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if err := ws.Remove(ctx, port.Space{Name: "issue-2-lfg", Branch: "crew/issue-2-lfg"}, true); !errors.Is(err, refused) {
		t.Errorf("Remove = %v, want the scripted error", err)
	}
	if err := ws.Remove(ctx, port.Space{Name: "issue-3-lfg", Branch: "crew/issue-3-lfg"}, false); err != nil {
		t.Errorf("Remove: %v", err)
	}
	want := []fake.Removal{{Name: "issue-1-lfg", DeleteBranch: true}, {Name: "issue-3-lfg"}}
	if got := ws.Removals(); !reflect.DeepEqual(got, want) {
		t.Errorf("Removals = %+v, want %+v", got, want)
	}
}

func TestNarratingHarnessSessionsSayWhatTheTestSets(t *testing.T) {
	h := fake.NewNarratingHarness()
	s := start(t, h, "implement #1")
	n, ok := s.(port.Narrator)
	if !ok {
		t.Fatal("a narrating harness's session is not a port.Narrator")
	}
	if got := n.Said(); got != "" {
		t.Errorf("Said before Say = %q, want empty", got)
	}
	h.Sessions()[0].Say("Starting U2.")
	if got := n.Said(); got != "Starting U2." {
		t.Errorf("Said = %q, want the text Say set", got)
	}

	if _, ok := start(t, fake.NewHarness(), "implement #2").(port.Narrator); ok {
		t.Error("a plain harness's session is a port.Narrator")
	}
}

func TestUsageHarnessSessionsReportWhatTheTestSets(t *testing.T) {
	h := fake.NewUsageHarness()
	s := start(t, h, "implement #1")
	r, ok := s.(port.UsageReporter)
	if !ok {
		t.Fatal("a usage harness's session is not a port.UsageReporter")
	}
	if got := r.Usage(); !reflect.DeepEqual(got, crew.Usage{}) {
		t.Errorf("Usage before SetUsage = %+v, want nothing reported", got)
	}
	want := crew.Usage{Cost: 12.4, HasCost: true, Tokens: crew.Tokens{Input: 10, Output: 20}, HasTokens: true,
		Models: []string{"claude-opus"}}
	h.Sessions()[0].SetUsage(want)
	if got := r.Usage(); !reflect.DeepEqual(got, want) {
		t.Errorf("Usage = %+v, want %+v", got, want)
	}

	if _, ok := start(t, fake.NewHarness(), "implement #2").(port.UsageReporter); ok {
		t.Error("a plain harness's session is a port.UsageReporter")
	}
}

func TestPullRequestsFindAsScriptedForTheBranchAndRecordEachLookup(t *testing.T) {
	tr := fake.NewFindingTracker()
	var finder port.PullRequestFinder = tr
	pr45 := crew.PullRequest{Lookup: crew.PullRequestFound, Ref: "#45", URL: "https://example.com/pull/45"}
	tr.ScriptLookup("crew/issue-31-lfg", fake.LookupScript{Found: pr45})
	tr.ScriptLookup("crew/issue-32-lfg", fake.LookupScript{Err: errors.New("HTTP 502")})
	since := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	if got, err := finder.FindPullRequest(ctx, "crew/issue-31-lfg", since); err != nil || got != pr45 {
		t.Errorf("scripted lookup = %+v, %v; want #45", got, err)
	}
	if _, err := finder.FindPullRequest(ctx, "crew/issue-32-lfg", time.Time{}); err == nil {
		t.Error("failing lookup = nil error, want the scripted error")
	}
	none, err := finder.FindPullRequest(ctx, "crew/issue-9-lfg", since)
	if err != nil || none.Lookup != crew.PullRequestNone {
		t.Errorf("unscripted lookup = %+v, %v; want no pull request", none, err)
	}
	want := []fake.Lookup{
		{Branch: "crew/issue-31-lfg", Since: since},
		{Branch: "crew/issue-32-lfg"},
		{Branch: "crew/issue-9-lfg", Since: since},
	}
	if got := tr.Lookups(); !reflect.DeepEqual(got, want) {
		t.Errorf("Lookups = %+v, want %+v", got, want)
	}
	if _, ok := any(fake.NewReportingTracker()).(port.PullRequestFinder); ok {
		t.Error("a ReportingTracker finds pull requests; only a FindingTracker should")
	}
}

func TestALookupScriptedToBlockWaitsUntilItsContextEnds(t *testing.T) {
	tr := fake.NewFindingTracker()
	tr.ScriptLookup("crew/hangs", fake.LookupScript{Block: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tr.FindPullRequest(ctx, "crew/hangs", time.Time{}); !errors.Is(err, context.Canceled) {
		t.Errorf("blocking lookup = %v, want the context's error", err)
	}
}

func TestCheckerRunsEachCheckAsScriptedForItsBranchAndRecordsIt(t *testing.T) {
	c := fake.NewChecker()
	c.Script("crew/fails", fake.CheckScript{Print: "no pull request\n", Exit: 1})
	c.Script("crew/no-sh", fake.CheckScript{StartErr: errors.New("sh: not found")})

	if err := c.Check(context.Background(), port.Check{Branch: "crew/passes"}); err != nil {
		t.Errorf("unscripted check = %v, want nil", err)
	}
	var out strings.Builder
	err := c.Check(context.Background(), port.Check{Branch: "crew/fails", Output: &out})
	if !errors.Is(err, port.ErrCheckFailed) {
		t.Errorf("failing check = %v, want ErrCheckFailed", err)
	}
	if out.String() != "no pull request\n" {
		t.Errorf("output = %q", out.String())
	}
	err = c.Check(context.Background(), port.Check{Branch: "crew/no-sh"})
	if err == nil || errors.Is(err, port.ErrCheckFailed) {
		t.Errorf("check that cannot start = %v", err)
	}
	if got := len(c.Checks()); got != 3 {
		t.Errorf("recorded %d checks, want 3", got)
	}
}

func TestHarnessAndCheckerRecordTheIdentityAndTheLogins(t *testing.T) {
	developer := port.Identity{
		Mate: "developer", Login: "crew-developer[bot]",
		Env: []string{"GH_CONFIG_DIR=/run/crew/developer"},
	}
	boss, mates := []string{"octocat"}, []string{"crew-developer[bot]"}
	run := port.Run{Prompt: "Implement #80", Identity: developer, Boss: boss, Mates: mates}
	check := port.Check{Branch: "crew/issue-80-lfg", Identity: developer, Boss: boss, Mates: mates}
	h, c := fake.NewHarness(), fake.NewChecker()

	if _, err := h.Start(context.Background(), run); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Check(context.Background(), check); err != nil {
		t.Fatalf("Check: %v", err)
	}

	if got := h.Sessions()[0].Run(); !reflect.DeepEqual(got, run) {
		t.Errorf("run = %+v, want %+v", got, run)
	}
	if got := c.Checks()[0]; !reflect.DeepEqual(got, check) {
		t.Errorf("check = %+v, want %+v", got, check)
	}
}

func TestCheckerScriptedToBlockRunsUntilItsContextEnds(t *testing.T) {
	c := fake.NewChecker()
	c.Script("crew/hangs", fake.CheckScript{Block: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Check(ctx, port.Check{Branch: "crew/hangs"}); !errors.Is(err, context.Canceled) {
		t.Errorf("blocking check = %v, want the context's error", err)
	}
}
