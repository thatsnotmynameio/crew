package git

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

func TestCreateFetchesTheBaseThenAddsWorktreeFromIt(t *testing.T) {
	git := &scripted{}
	w, root := scriptedWorkspace(t, git)

	space, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "implement",
		"crew/issue-3-implement")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	dir := filepath.Join(root, ".crew", "worktrees", "issue-7-implement")
	want := port.Space{
		Workspace: crew.Workspace{Name: "issue-7-implement", Branch: "crew/issue-7-implement"}, Dir: dir,
		DefaultBranch: "main",
	}
	if space != want {
		t.Errorf("space = %+v, want %+v", space, want)
	}
	check := git.index("check-ref-format", "--branch", "crew/issue-3-implement")
	if check < 0 || check > git.index("fetch") {
		t.Errorf("want git check-ref-format --branch crew/issue-3-implement before the fetch; calls: %q", git.calls)
	}
	checkFetchedThenAdded(t, git, "crew/issue-3-implement", "crew/issue-7-implement", dir)
}

// checkFetchedThenAdded checks that git fetched base from origin into its
// remote-tracking branch, then added the worktree dir on the new branch
// from it, with no upstream.
func checkFetchedThenAdded(t *testing.T, git *scripted, base, branch, dir string) {
	t.Helper()
	fetch := git.index("fetch", "origin", "+refs/heads/"+base+":refs/remotes/origin/"+base)
	add := git.index("worktree", "add")
	if fetch < 0 || add < 0 || fetch > add {
		t.Fatalf("want git fetch origin %s before git worktree add; calls: %q", base, git.calls)
	}
	args := git.call(add)
	for _, want := range []string{"--no-track", dir, "origin/" + base} {
		if !slices.Contains(args, want) {
			t.Errorf("worktree add %q lacks %q", args, want)
		}
	}
	if b := slices.Index(args, "-b"); b < 0 || b+1 >= len(args) || args[b+1] != branch {
		t.Errorf("worktree add %q does not create branch %s", args, branch)
	}
}

func TestCreateRefusesABaseBeforeAnyGitCall(t *testing.T) {
	for name, base := range map[string]string{
		"leading dash": "-c", "NUL": "crew/a\x00b", "line feed": "crew/a\nb", "DEL": "crew/a\x7fb",
	} {
		t.Run(name, func(t *testing.T) {
			git := &scripted{}
			w, _ := scriptedWorkspace(t, git)

			_, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "implement", base)
			if err == nil {
				t.Fatalf("Create from %q succeeded, want it refused", base)
			}
			if len(git.calls) != 0 {
				t.Errorf("git calls = %q, want none", git.calls)
			}
		})
	}
}

func TestCreateRefusesABaseGitRejectsBeforeFetching(t *testing.T) {
	stderr := "fatal: 'a..b' is not a valid branch name"
	git := &scripted{fail: map[string]string{"check-ref-format": stderr}}
	w, _ := scriptedWorkspace(t, git)

	_, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "implement", "a..b")
	if err == nil || !strings.Contains(err.Error(), stderr) {
		t.Fatalf("err = %v, want it to carry %q", err, stderr)
	}
	if git.index("fetch") >= 0 || git.index("worktree") >= 0 {
		t.Errorf("git calls = %q, want no fetch and no worktree", git.calls)
	}
}

func TestCreateFromABaseBranchInRealRepository(t *testing.T) {
	r := newRemote(t)
	gitIn(t, r.seed, "push", "origin", "HEAD:refs/heads/crew/issue-3-implement")
	root := r.clone(t)
	gitIn(t, r.seed, "commit", "--allow-empty", "-m", "blocker's work")
	gitIn(t, r.seed, "push", "origin", "HEAD:refs/heads/crew/issue-3-implement")
	tip := gitIn(t, r.seed, "rev-parse", "HEAD")

	w := New(&proc.Group{}, root)
	space, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "implement",
		"crew/issue-3-implement")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if space.DefaultBranch != "trunk" {
		t.Errorf("DefaultBranch = %q, want trunk", space.DefaultBranch)
	}
	if head := gitIn(t, space.Dir, "rev-parse", "HEAD"); head != tip {
		t.Errorf("HEAD = %s, want the base's latest commit %s", head, tip)
	}
	if branch := gitIn(t, space.Dir, "branch", "--show-current"); branch != "crew/issue-7-implement" {
		t.Errorf("on branch %q, want crew/issue-7-implement", branch)
	}
	if up := gitIn(t, root, "for-each-ref", "--format=%(upstream)", "refs/heads/crew/issue-7-implement"); up != "" {
		t.Errorf("upstream = %q, want none", up)
	}
}

func TestCreateFromABaseOriginDoesNotHaveCarriesGitStderr(t *testing.T) {
	r := newRemote(t)
	root := r.clone(t)

	_, err := New(&proc.Group{}, root).Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}),
		"implement", "crew/issue-3-implement")
	if err == nil || !strings.Contains(err.Error(), "couldn't find remote ref refs/heads/crew/issue-3-implement") {
		t.Fatalf("err = %v, want it to carry git's stderr naming the missing branch", err)
	}
}
