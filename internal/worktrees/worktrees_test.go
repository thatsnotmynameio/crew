package worktrees

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// dir is the folder the tests say they looked in.
const dir = "/repo/.crew/worktrees"

// rig is the fake workspace, tracker and journal one Clean runs against.
type rig struct {
	ws  *fake.Workspace
	prs *fake.PullRequests

	mu    sync.Mutex
	runs  []map[string]time.Time // one per read of the journal; the last repeats
	reads int
}

func newRig(t *testing.T, found ...port.Found) *rig {
	t.Helper()
	r := &rig{ws: fake.NewWorkspace(t.TempDir()), prs: &fake.PullRequests{}}
	r.ws.ScriptWorkspaces(fake.Listing{Found: found})
	return r
}

// unended returns the next scripted unended runs.
func (r *rig) unended() (map[string]time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	if len(r.runs) == 0 {
		return map[string]time.Time{}, nil
	}
	runs := r.runs[0]
	if len(r.runs) > 1 {
		r.runs = r.runs[1:]
	}
	return runs, nil
}

func (r *rig) options(in io.Reader, terminal bool) (Options, *strings.Builder) {
	out := &strings.Builder{}
	return Options{
		Sweeper: r.ws, Finder: r.prs, Runs: r.unended, Dir: dir,
		In: in, Out: out, Terminal: terminal,
	}, out
}

// clean runs Clean at a terminal answering answer.
func (r *rig) clean(t *testing.T, answer string) (Result, string) {
	t.Helper()
	o, out := r.options(strings.NewReader(answer), true)
	res, err := Clean(context.Background(), o)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	return res, out.String()
}

// merge scripts the pull request from name's branch as merged.
func (r *rig) merge(name, ref string) {
	r.prs.ScriptLookup("crew/"+name, pullRequest(ref, crew.PullRequestMerged))
}

func wantRemovals(t *testing.T, ws *fake.Workspace, want ...fake.Removal) {
	t.Helper()
	if got := ws.Removals(); !slices.Equal(got, want) {
		t.Fatalf("removals: got %#v, want %#v", got, want)
	}
}

func wantResult(t *testing.T, got, want Result) {
	t.Helper()
	if got != want {
		t.Fatalf("result: got %v, want %v", got, want)
	}
}

func wantLines(t *testing.T, out string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !slices.Contains(strings.Split(out, "\n"), l) {
			t.Fatalf("output has no line %q:\n%s", l, out)
		}
	}
}

func wantNoQuestion(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "[y/N]") {
		t.Fatalf("output asks:\n%s", out)
	}
}

// answering is an answer that runs before when it is first read.
type answering struct {
	before func()
	answer string
	once   sync.Once
}

func (a *answering) Read(p []byte) (int, error) {
	a.once.Do(a.before)
	if a.answer == "" {
		return 0, io.EOF
	}
	n := copy(p, a.answer)
	a.answer = a.answer[n:]
	return n, nil
}

func TestAE1CleanRemovesAMergedWorktreeAndItsBranchAndReportsBoth(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.merge("issue-42-development", "#45")

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws, fake.Removal{Name: "issue-42-development", DeleteBranch: true})
	want := "1 worktree in /repo/.crew/worktrees:\n" +
		"  remove worktree and branch  issue-42-development  " +
		"pull request #45 merged; crew/issue-42-development has nothing after its head\n" +
		"Remove 1 worktree and delete 1 branch? [y/N] \n" +
		"  removed worktree and branch  issue-42-development  " +
		"pull request #45 merged; crew/issue-42-development has nothing after its head\n" +
		"Removed 1 worktree and deleted 1 branch.\n"
	if out != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out, want)
	}
	since := fake.Lookup{Branch: "crew/issue-42-development", Since: created}
	if got := r.prs.Lookups(); !slices.Equal(got, []fake.Lookup{since, since}) {
		t.Fatalf("lookups: got %#v, want the list's and the check's, since its creation", got)
	}
}

func TestAE2CleanKeepsAWorktreeWhosePullRequestIsOpen(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.prs.ScriptLookup("crew/issue-42-development", pullRequest("#47", crew.PullRequestOpen))

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantLines(t, out, "  keep  issue-42-development  pull request #47 is open")
}

func TestAE3CleanKeepsAWorktreeWithoutAPullRequest(t *testing.T) {
	r := newRig(t, worktree("issue-34-triage"))

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantLines(t, out, "  keep  issue-34-triage  no pull request from crew/issue-34-triage")
}

func TestAE4CleanKeepsAMergedWorktreeWithUncommittedFilesWithoutLookingItUp(t *testing.T) {
	notes := worktree("issue-42-development")
	notes.Dirty = true
	r := newRig(t, notes)
	r.merge("issue-42-development", "#45")

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantLines(t, out, "  keep  issue-42-development  it holds uncommitted changes or untracked files")
	if got := r.prs.Lookups(); len(got) != 0 {
		t.Fatalf("lookups: got %#v, want none", got)
	}
}

func TestAE5CleanRemovesAMergedWorktreeAndKeepsABranchWithACommitAfterTheHead(t *testing.T) {
	r := newRig(t, worktree("issue-43-development"))
	r.merge("issue-43-development", "#47")
	r.ws.ScriptBeyond("crew/issue-43-development", 1, nil)

	res, out := r.clean(t, "y\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws, fake.Removal{Name: "issue-43-development", DeleteBranch: false})
	reason := "pull request #47 merged; crew/issue-43-development has 1 commit after its head"
	wantLines(t, out,
		"  remove worktree, keep branch  issue-43-development  "+reason,
		"Remove 1 worktree? [y/N] ",
		"  removed worktree, kept branch  issue-43-development  "+reason,
		"Removed 1 worktree.",
	)
}

func TestAE6CleanAnsweredNoRemovesNothing(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"), worktree("issue-44-development"))
	r.merge("issue-42-development", "#45")
	r.merge("issue-44-development", "#46")

	res, out := r.clean(t, "no\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantLines(t, out, "Remove 2 worktrees and delete 2 branches? [y/N] ", "Nothing was removed.")
}

func TestAE7CleanWithoutATerminalListsAndRemovesNothing(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.merge("issue-42-development", "#45")
	o, out := r.options(strings.NewReader("yes\n"), false)

	res, err := Clean(context.Background(), o)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantNoQuestion(t, out.String())
	wantLines(t, out.String(),
		"  remove worktree and branch  issue-42-development  "+
			"pull request #45 merged; crew/issue-42-development has nothing after its head",
		"Removing worktrees needs a confirmation at a terminal; nothing was removed.",
	)
}

func TestAE8CleanKeepsAWorktreeThatGainedAFileBeforeTheYes(t *testing.T) {
	changed := worktree("issue-42-development")
	changed.Dirty = true
	r := newRig(t)
	r.ws.ScriptWorkspaces(
		fake.Listing{Found: []port.Found{worktree("issue-42-development")}},
		fake.Listing{Found: []port.Found{changed}},
	)
	r.merge("issue-42-development", "#45")

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantLines(t, out,
		"  kept  issue-42-development  changed since the list: it holds uncommitted changes or untracked files",
		"Removed nothing.",
	)
}

func TestAE9CleanKeepsEveryWorktreeWhenTheLookupsFail(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"), worktree("issue-44-development"))
	notLoggedIn := fake.LookupScript{Err: errors.New("gh: not logged in")}
	r.prs.ScriptLookup("crew/issue-42-development", notLoggedIn)
	r.prs.ScriptLookup("crew/issue-44-development", notLoggedIn)

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Failed)
	wantRemovals(t, r.ws)
	wantNoQuestion(t, out)
	wantLines(t, out,
		"  keep  issue-42-development  its pull request could not be looked up: "+
			"find the pull request from crew/issue-42-development: gh: not logged in",
		"  keep  issue-44-development  its pull request could not be looked up: "+
			"find the pull request from crew/issue-44-development: gh: not logged in",
		"Nothing to remove.",
	)
}

func TestAE10CleanKeepsAMergedWorktreeARunHasNotEnded(t *testing.T) {
	r := newRig(t, worktree("issue-50-development"))
	r.merge("issue-50-development", "#51")
	started := time.Date(2026, 10, 3, 14, 2, 0, 0, time.Local)
	r.runs = []map[string]time.Time{{"issue-50-development": started}}

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantLines(t, out, "  keep  issue-50-development  a run started in it at 2026-10-03 14:02 and has not ended: "+
		"an action may be using it (if crew is not running, the run was cut short and you can remove it by hand)")
}

func TestAE11CleanRemovesTheWorktreeOfAnEndedFailedRunWhosePullRequestMerged(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.merge("issue-42-development", "#45")
	// The failed run ended, so the journal's unended runs hold only another.
	r.runs = []map[string]time.Time{{"issue-7-development": created}}

	res, _ := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws, fake.Removal{Name: "issue-42-development", DeleteBranch: true})
}

func TestAE12CleanWithNothingToRemoveSaysSoAndAsksNothing(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"), worktree("issue-34-triage"))
	r.prs.ScriptLookup("crew/issue-42-development", pullRequest("#47", crew.PullRequestOpen))

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	want := "2 worktrees in /repo/.crew/worktrees:\n" +
		"  keep  issue-42-development  pull request #47 is open\n" +
		"  keep  issue-34-triage       no pull request from crew/issue-34-triage\n" +
		"Nothing to remove.\n"
	if out != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out, want)
	}
}

func TestCleanWithoutWorktreesSaysSoAndAsksNothing(t *testing.T) {
	r := newRig(t)

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	if want := "No crew worktrees in /repo/.crew/worktrees.\n"; out != want {
		t.Fatalf("output: got %q, want %q", out, want)
	}
}

func TestCleanReturnsTheErrorWhenItCannotListTheWorktrees(t *testing.T) {
	r := newRig(t)
	r.ws.ScriptWorkspaces(fake.Listing{Err: errors.New("not a git repository")})
	o, out := r.options(strings.NewReader("yes\n"), true)

	_, err := Clean(context.Background(), o)

	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("Clean: got %v, want the listing's error", err)
	}
	if out.Len() != 0 {
		t.Fatalf("output: got %q, want none", out.String())
	}
}

func TestCleanKeepsAWorktreeWhoseRunStartedBeforeTheYes(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.merge("issue-42-development", "#45")
	started := time.Date(2026, 10, 3, 14, 2, 0, 0, time.Local)
	r.runs = []map[string]time.Time{{}, {"issue-42-development": started}}

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	if r.reads != 2 {
		t.Fatalf("journal reads: got %d, want 2: before the list and before the removals", r.reads)
	}
	wantLines(t, out, "  kept  issue-42-development  changed since the list: "+
		"a run started in it at 2026-10-03 14:02 and has not ended: an action may be using it "+
		"(if crew is not running, the run was cut short and you can remove it by hand)")
}

func TestCleanKeepsABranchThatGainedACommitBeforeTheYes(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.merge("issue-42-development", "#45")
	commit := func() { r.ws.ScriptBeyond("crew/issue-42-development", 1, nil) }

	o, out := r.options(&answering{before: commit, answer: "yes\n"}, true)
	res, err := Clean(context.Background(), o)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	wantResult(t, res, Done)
	wantRemovals(t, r.ws, fake.Removal{Name: "issue-42-development", DeleteBranch: false})
	wantLines(t, out.String(),
		"Remove 1 worktree and delete 1 branch? [y/N] ",
		"  removed worktree, kept branch  issue-42-development  changed since the list: "+
			"pull request #45 merged; crew/issue-42-development has 1 commit after its head",
		"Removed 1 worktree.",
	)
}

func TestCleanKeepsAWorktreeGoneFromTheSecondListing(t *testing.T) {
	r := newRig(t)
	r.ws.ScriptWorkspaces(
		fake.Listing{Found: []port.Found{worktree("issue-42-development")}},
		fake.Listing{},
	)
	r.merge("issue-42-development", "#45")

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws)
	wantLines(t, out, "  kept  issue-42-development  changed since the list: it is no longer there")
}

func TestCleanKeepsEveryWorktreeWhenTheSecondListingFails(t *testing.T) {
	r := newRig(t)
	r.ws.ScriptWorkspaces(
		fake.Listing{Found: []port.Found{worktree("issue-42-development")}},
		fake.Listing{Err: errors.New("git: broken")},
	)
	r.merge("issue-42-development", "#45")

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Failed)
	wantRemovals(t, r.ws)
	wantLines(t, out, "  kept  issue-42-development  it could not be checked again: list workspaces: git: broken")
}

func TestCleanReportsARemovalGitRefusesAndStillRunsTheOthers(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"), worktree("issue-44-development"))
	r.merge("issue-42-development", "#45")
	r.merge("issue-44-development", "#46")
	r.ws.FailRemove("issue-42-development", errors.New("fatal: 'issue-42-development' contains untracked files"))

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Failed)
	wantRemovals(t, r.ws, fake.Removal{Name: "issue-44-development", DeleteBranch: true})
	wantLines(t, out,
		"  kept                         issue-42-development  removing it failed: "+
			"remove workspace issue-42-development: fatal: 'issue-42-development' contains untracked files",
		"Removed 1 worktree and deleted 1 branch.",
	)
}

func TestCleanTakesOnlyYOrYesInAnyCaseAsAYes(t *testing.T) {
	for _, c := range []struct {
		answer string
		yes    bool
	}{
		{"Y\n", true}, {"yes\n", true}, {" YES \n", true},
		{"n\n", false}, {"\n", false}, {"", false}, {"yess\n", false},
	} {
		t.Run(strings.TrimSpace(c.answer), func(t *testing.T) {
			r := newRig(t, worktree("issue-42-development"))
			r.merge("issue-42-development", "#45")

			res, out := r.clean(t, c.answer)

			wantResult(t, res, Done)
			if c.yes {
				wantRemovals(t, r.ws, fake.Removal{Name: "issue-42-development", DeleteBranch: true})
				return
			}
			wantRemovals(t, r.ws)
			if !strings.HasSuffix(out, "[y/N] \nNothing was removed.\n") {
				t.Fatalf("output does not end with nothing removed:\n%s", out)
			}
		})
	}
}

// waiting is an answer that never comes: it ends ctx when read, then blocks
// until the test ends.
type waiting struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *waiting) Read([]byte) (int, error) {
	w.cancel()
	<-w.done
	return 0, io.EOF
}

func TestCleanStoppedWhileAskingRemovesNothing(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.merge("issue-42-development", "#45")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := &waiting{cancel: cancel, done: make(chan struct{})}
	t.Cleanup(func() { close(in.done) })
	o, out := r.options(in, true)

	res, err := Clean(ctx, o)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	wantResult(t, res, Stopped)
	wantRemovals(t, r.ws)
	if !strings.HasSuffix(out.String(), "[y/N] \nStopped; nothing was removed.\n") {
		t.Fatalf("output does not end stopped:\n%s", out.String())
	}
}

// stoppingSweeper ends ctx while it removes, as a signal arriving during a
// removal, and records whether the removal's own context ended.
type stoppingSweeper struct {
	*fake.Workspace

	cancel    context.CancelFunc
	removeCtx []error
}

func (s *stoppingSweeper) Remove(ctx context.Context, space port.Space, deleteBranch bool) error {
	s.cancel()
	s.removeCtx = append(s.removeCtx, ctx.Err())
	return s.Workspace.Remove(ctx, space, deleteBranch)
}

func TestCleanStoppedDuringARemovalFinishesItAndStartsNoOther(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"), worktree("issue-44-development"))
	r.merge("issue-42-development", "#45")
	r.merge("issue-44-development", "#46")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sweeper := &stoppingSweeper{Workspace: r.ws, cancel: cancel}
	o, out := r.options(strings.NewReader("yes\n"), true)
	o.Sweeper = sweeper

	res, err := Clean(ctx, o)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	wantResult(t, res, Stopped)
	wantRemovals(t, r.ws, fake.Removal{Name: "issue-42-development", DeleteBranch: true})
	if !slices.Equal(sweeper.removeCtx, []error{nil}) {
		t.Fatalf("removal contexts' errors: got %v, want one removal whose context did not end", sweeper.removeCtx)
	}
	wantLines(t, out.String(),
		"  kept                         issue-44-development  stopped before it was removed",
		"Removed 1 worktree and deleted 1 branch.",
	)
}

func TestCleanReportsTheWorktreesItKeptWithTheirReasonsAfterAYes(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"), worktree("issue-34-triage"))
	r.merge("issue-42-development", "#45")

	res, out := r.clean(t, "yes\n")

	wantResult(t, res, Done)
	wantRemovals(t, r.ws, fake.Removal{Name: "issue-42-development", DeleteBranch: true})
	wantLines(t, out,
		"  removed worktree and branch  issue-42-development  "+
			"pull request #45 merged; crew/issue-42-development has nothing after its head",
		"  kept                         issue-34-triage       no pull request from crew/issue-34-triage",
		"Removed 1 worktree and deleted 1 branch.",
	)
}

// stopFinder ends ctx during the lookup numbered at, as a signal
// arriving while a worktree is checked again, and answers as its
// PullRequests do.
type stopFinder struct {
	*fake.PullRequests

	cancel context.CancelFunc
	at     int
	calls  int
}

func (s *stopFinder) FindPullRequest(ctx context.Context, branch string, since time.Time) (crew.PullRequest, error) {
	s.calls++
	if s.calls == s.at {
		s.cancel()
	}
	return s.PullRequests.FindPullRequest(ctx, branch, since)
}

func TestCleanStoppedWhileCheckingAgainRemovesNothing(t *testing.T) {
	r := newRig(t, worktree("issue-42-development"))
	r.merge("issue-42-development", "#45")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o, out := r.options(strings.NewReader("yes\n"), true)
	o.Finder = &stopFinder{PullRequests: r.prs, cancel: cancel, at: 2}

	res, err := Clean(ctx, o)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	wantResult(t, res, Stopped)
	wantRemovals(t, r.ws)
	wantLines(t, out.String(), "  kept  issue-42-development  stopped before it was removed", "Removed nothing.")
}
