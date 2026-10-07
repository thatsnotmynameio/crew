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

// The rules' states in these tests, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	needsAttention crew.State = "needs attention"
	// waitingBrainstorm is a label no rule names: parked work no rule takes.
	waitingBrainstorm crew.State = "waiting brainstorm"
)

func issue(key string, states ...crew.State) crew.Issue {
	return crew.NewIssue(crew.IssueData{ID: issueID(key), Ref: "#" + key, Title: "Issue " + key, States: states})
}

func keys(issues []crew.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.ID().Key)
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
func waitOutcome(t *testing.T, s port.Session) port.SessionEnd {
	t.Helper()
	done := make(chan port.SessionEnd, 1)
	go func() { done <- s.Wait() }()
	select {
	case o := <-done:
		return o
	case <-time.After(time.Second):
		t.Fatal("the session did not end")
		return port.SessionEnd{}
	}
}

func TestHarnessSessionEndsWithTheOutcomeTheTestReleasesItWith(t *testing.T) {
	h := fake.NewHarness()
	s := start(t, h, "implement #1")

	ended := make(chan port.SessionEnd, 1)
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
	want := port.SessionEnd{Succeeded: true, Reason: "opened a pull request"}
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

func TestPreparationReportsItsStepOnlyWhenScripted(t *testing.T) {
	h := fake.NewPreparingHarness()
	var steps []string
	ctx := port.WithSteps(context.Background(), func(step string) { steps = append(steps, step) })

	if err := h.Prepare(ctx, nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if steps != nil {
		t.Fatalf("steps = %q, want none from an unscripted Prepare", steps)
	}
	h.ReportStep("checking claude")
	if err := h.Prepare(ctx, nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if want := []string{"checking claude"}; !reflect.DeepEqual(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}
}

func TestWorkspaceCreatesUniqueDirectoriesPerIssueAndRule(t *testing.T) {
	root := t.TempDir()
	ws := fake.NewWorkspace(root)
	ctx := context.Background()

	first, err := ws.Create(ctx, issue("42"), "Code Review")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first.Workspace.Name != "issue-42-code-review" {
		t.Errorf("Name = %q, want issue-42-code-review", first.Workspace.Name)
	}
	if !filepath.IsAbs(first.Dir) || filepath.Dir(first.Dir) != root {
		t.Errorf("Dir = %q, want an absolute directory under %q", first.Dir, root)
	}
	if info, err := os.Stat(first.Dir); err != nil || !info.IsDir() {
		t.Errorf("Dir %q is not a directory: %v", first.Dir, err)
	}
	if first.Workspace.Branch == "" {
		t.Error("Branch is empty")
	}

	second, err := ws.Create(ctx, issue("42"), "Code Review")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.Workspace.Name == first.Workspace.Name || second.Workspace.Branch == first.Workspace.Branch ||
		second.Dir == first.Dir {
		t.Errorf("second workspace %+v repeats the first %+v", second, first)
	}
}

func TestWorkspaceNamesStayUniqueUnderConcurrentCreates(t *testing.T) {
	ws := fake.NewWorkspace(t.TempDir())
	var (
		mu    sync.Mutex
		names = map[crew.WorkspaceName]bool{}
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
			names[s.Workspace.Name] = true
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
	want := crew.Usage{Cost: crew.Some(12.4), Tokens: crew.Some(crew.Tokens{Input: 10, Output: 20}),
		Models: []string{"claude-opus"}}
	h.Sessions()[0].SetUsage(want)
	if got := r.Usage(); !reflect.DeepEqual(got, want) {
		t.Errorf("Usage = %+v, want %+v", got, want)
	}

	if _, ok := start(t, fake.NewHarness(), "implement #2").(port.UsageReporter); ok {
		t.Error("a plain harness's session is a port.UsageReporter")
	}
}

func TestMessagingHarnessSessionsReportTheLastMessageTheTestSets(t *testing.T) {
	h := fake.NewMessagingHarness()
	s := start(t, h, "implement #1")
	m, ok := s.(port.LastMessageReporter)
	if !ok {
		t.Fatal("a messaging harness's session is not a port.LastMessageReporter")
	}
	if got := m.LastMessage(); got != "" {
		t.Errorf("LastMessage before SetLastMessage = %q, want empty", got)
	}
	h.Sessions()[0].SetLastMessage("PR #9 is open.\nMerging is yours.")
	if got := m.LastMessage(); got != "PR #9 is open.\nMerging is yours." {
		t.Errorf("LastMessage = %q, want what SetLastMessage set", got)
	}

	if _, ok := start(t, fake.NewHarness(), "implement #2").(port.LastMessageReporter); ok {
		t.Error("a plain harness's session is a port.LastMessageReporter")
	}
}

func TestPullRequestsFindAsScriptedForTheBranchAndRecordEachLookup(t *testing.T) {
	tr := fake.NewFindingTracker()
	var finder port.PullRequestFinder = tr
	pr45 := crew.PullRequestFound{Ref: "#45", URL: "https://example.com/pull/45"}
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
	if err != nil || none != (crew.PullRequestNone{}) {
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

func TestShellRunsEachScriptAsScriptedForItsBranchAndRecordsIt(t *testing.T) {
	sh := fake.NewShell()
	sh.Script("crew/fails", fake.ShellScript{Print: "no pull request\n", Exit: 2})
	sh.Script("crew/no-sh", fake.ShellScript{StartErr: errors.New("sh: not found")})

	if got, err := sh.Run(context.Background(), port.Script{Branch: "crew/passes"}); err != nil || got.Status != 0 {
		t.Errorf("unscripted script = %+v, %v, want status 0", got, err)
	}
	var out strings.Builder
	got, err := sh.Run(context.Background(), port.Script{Branch: "crew/fails", Output: &out})
	if err != nil || got.Status != 2 {
		t.Errorf("failing script = %+v, %v, want status 2 and no error", got, err)
	}
	if out.String() != "no pull request\n" {
		t.Errorf("output = %q", out.String())
	}
	if _, err := sh.Run(context.Background(), port.Script{Branch: "crew/no-sh"}); err == nil {
		t.Error("script that cannot start = nil, want its start error")
	}
	if got := len(sh.Runs()); got != 3 {
		t.Errorf("recorded %d scripts, want 3", got)
	}
}

func TestShellRunsAScriptScriptedByNameOverItsBranchsScript(t *testing.T) {
	sh := fake.NewShell()
	sh.Script("crew/issue-9-lfg", fake.ShellScript{Exit: 1})
	sh.ScriptAction("crew/issue-9-lfg", "judge", fake.ShellScript{Print: "done (0.97)\n"})

	var out strings.Builder
	judge := port.Script{Branch: "crew/issue-9-lfg", Name: "judge", Output: &out}
	if got, err := sh.Run(context.Background(), judge); err != nil || got.Status != 0 {
		t.Errorf("judge = %+v, %v, want it to pass as scripted by name", got, err)
	}
	if out.String() != "done (0.97)\n" {
		t.Errorf("judge's output = %q", out.String())
	}
	got, err := sh.Run(context.Background(), port.Script{Branch: "crew/issue-9-lfg", Name: "pr-closes-issue"})
	if err != nil || got.Status != 1 {
		t.Errorf("pr-closes-issue = %+v, %v, want the branch's script to exit 1", got, err)
	}
}

func TestHarnessAndShellRecordTheIdentityAndTheLogins(t *testing.T) {
	developer := port.Identity{
		Bot: "developer", Login: "crew-developer[bot]",
		Env: []string{"GH_CONFIG_DIR=/run/crew/developer"},
	}
	codeOwners, bots := []string{"octocat"}, []string{"crew-developer[bot]"}
	run := port.Run{Prompt: "Implement #80", Identity: developer, CodeOwners: codeOwners, Bots: bots}
	script := port.Script{Branch: "crew/issue-80-lfg", Identity: developer, CodeOwners: codeOwners, Bots: bots}
	h, sh := fake.NewHarness(), fake.NewShell()

	if _, err := h.Start(context.Background(), run); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := sh.Run(context.Background(), script); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := h.Sessions()[0].Run(); !reflect.DeepEqual(got, run) {
		t.Errorf("run = %+v, want %+v", got, run)
	}
	if got := sh.Runs()[0]; !reflect.DeepEqual(got, script) {
		t.Errorf("script = %+v, want %+v", got, script)
	}
}

func TestShellScriptedToBlockRunsUntilItsContextEnds(t *testing.T) {
	sh := fake.NewShell()
	sh.Script("crew/hangs", fake.ShellScript{Block: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sh.Run(ctx, port.Script{Branch: "crew/hangs"}); !errors.Is(err, context.Canceled) {
		t.Errorf("blocking script = %v, want the context's error", err)
	}
}

// issueID returns the id of the issue keyed key, in no repository.
func issueID(key string) crew.IssueID { return crew.IssueID{Key: key} }

func TestJournalLoadsItsPastThenWhatWasAppendedUntilItsAppendsFail(t *testing.T) {
	h := crew.EventHead{Run: "run-1", IssueID: issueID("1"), IssueRef: "#1", Rule: "implement"}
	past := crew.RunStopped{EventHead: h}
	j := fake.NewJournal(past)
	appended := crew.RunReleased{EventHead: h}
	if err := j.Append(appended); err != nil {
		t.Fatalf("Append: %v", err)
	}

	full := errors.New("disk full")
	j.FailAppends(full)
	if err := j.Append(crew.RouteChosen{EventHead: h, Route: crew.FailedRoute}); !errors.Is(err, full) {
		t.Fatalf("Append = %v, want the failure set", err)
	}

	if got := j.Appended(); !reflect.DeepEqual(got, []crew.RunEvent{appended}) {
		t.Errorf("Appended = %#v, want the one append that did not fail", got)
	}
	got, err := j.Load("repo")
	if want := []crew.RunEvent{past, appended}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load = %#v, %v, want the past then the append", got, err)
	}
}
