package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// scripted is a git that answers from a script and records every call.
type scripted struct {
	mu    sync.Mutex
	calls [][]string
	// branches are the local branches that exist.
	branches []string
	// fail makes the call whose arguments start with its key fail with
	// its value as git's stderr.
	fail map[string]string
}

func (s *scripted) run(_ context.Context, c proc.Command) (proc.Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, c.Args)
	joined := strings.Join(c.Args, " ")
	for prefix, stderr := range s.fail {
		if strings.HasPrefix(joined, prefix) {
			return proc.Output{Stderr: []byte(stderr)}, fmt.Errorf("git: exit status 128: %s", stderr)
		}
	}
	switch {
	case strings.HasPrefix(joined, "symbolic-ref"):
		return proc.Output{Stdout: []byte("origin/main\n")}, nil
	case strings.HasPrefix(joined, "branch --list"):
		if slices.Contains(s.branches, c.Args[len(c.Args)-1]) {
			return proc.Output{Stdout: []byte("  " + c.Args[len(c.Args)-1] + "\n")}, nil
		}
	}
	return proc.Output{}, nil
}

// index returns the position of the first call whose arguments start with
// prefix, or -1.
func (s *scripted) index(prefix ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return i
		}
	}
	return -1
}

func (s *scripted) call(i int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[i]
}

func scriptedWorkspace(t *testing.T, git *scripted) (*Workspace, string) {
	t.Helper()
	root := t.TempDir()
	w := New(&proc.Group{}, root)
	w.run = git.run
	return w, root
}

func TestCreateFetchesThenAddsWorktreeFromOriginDefault(t *testing.T) {
	git := &scripted{}
	w, root := scriptedWorkspace(t, git)

	space, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7"), Ref: "#7"}), "development")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	dir := filepath.Join(root, ".crew", "worktrees", "issue-7-development")
	want := port.Space{
		Workspace: crew.Workspace{Name: "issue-7-development", Branch: "crew/issue-7-development"}, Dir: dir,
	}
	if space != want {
		t.Errorf("space = %+v, want %+v", space, want)
	}
	fetch := git.index("fetch", "origin", "main")
	add := git.index("worktree", "add")
	if fetch < 0 || add < 0 || fetch > add {
		t.Fatalf("want git fetch origin main before git worktree add; calls: %q", git.calls)
	}
	args := git.call(add)
	for _, want := range []string{"crew/issue-7-development", dir, "origin/main"} {
		if !slices.Contains(args, want) {
			t.Errorf("worktree add %q lacks %q", args, want)
		}
	}
	if b := slices.Index(args, "-b"); b < 0 || b+1 >= len(args) || args[b+1] != "crew/issue-7-development" {
		t.Errorf("worktree add %q does not create branch crew/issue-7-development", args)
	}
}

func TestPrepareReportsItsSteps(t *testing.T) {
	all := []string{"checking the git checkout and its origin", "resolving origin's default branch"}
	for name, tc := range map[string]struct {
		fail map[string]string
		want []string
	}{
		"with origin":    {nil, all},
		"without origin": {map[string]string{"remote get-url origin": "error: No such remote 'origin'"}, all[:1]},
	} {
		t.Run(name, func(t *testing.T) {
			w, _ := scriptedWorkspace(t, &scripted{fail: tc.fail})
			var steps []string
			ctx := port.WithSteps(t.Context(), func(step string) { steps = append(steps, step) })

			err := w.Prepare(ctx, nil)

			if (err != nil) != (tc.fail != nil) {
				t.Errorf("Prepare = %v, want an error only without origin", err)
			}
			if !slices.Equal(steps, tc.want) {
				t.Errorf("steps = %q, want %q", steps, tc.want)
			}
		})
	}
}

func TestCreateReportsNoStep(t *testing.T) {
	w, _ := scriptedWorkspace(t, &scripted{})
	var steps []string
	ctx := port.WithSteps(t.Context(), func(step string) { steps = append(steps, step) })

	if _, err := w.Create(ctx, crew.NewIssue(crew.IssueData{ID: issueID("7")}), "development"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("Create reported steps %q, want none", steps)
	}
}

func TestCreateNames(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		action   crew.ActionName
		branches []string
		dirs     []string
		want     string
	}{
		{name: "jira key", key: "PROJ-123", action: "development", want: "issue-proj-123-development"},
		{name: "action sanitized", key: "7", action: "Custom_Review", want: "issue-7-custom-review"},
		{name: "branch exists", key: "7", action: "development",
			branches: []string{"crew/issue-7-development"}, want: "issue-7-development-2"},
		{name: "folder exists", key: "7", action: "development",
			dirs: []string{"issue-7-development"}, want: "issue-7-development-2"},
		{name: "folder and branch exist", key: "7", action: "development",
			branches: []string{"crew/issue-7-development-2"}, dirs: []string{"issue-7-development"},
			want: "issue-7-development-3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			git := &scripted{branches: tt.branches}
			w, root := scriptedWorkspace(t, git)
			for _, d := range tt.dirs {
				if err := os.MkdirAll(filepath.Join(root, ".crew", "worktrees", d), 0o750); err != nil {
					t.Fatal(err)
				}
			}

			space, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID(tt.key)}), tt.action)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if string(space.Workspace.Name) != tt.want || space.Workspace.Branch != "crew/"+tt.want ||
				space.Dir != filepath.Join(root, ".crew", "worktrees", tt.want) {
				t.Errorf("space = %+v, want name %s", space, tt.want)
			}
		})
	}
}

func TestCreateFailingWorktreeAddCarriesGitStderr(t *testing.T) {
	stderr := "fatal: a branch named 'crew/issue-7-development' already exists"
	git := &scripted{fail: map[string]string{"worktree add": stderr}}
	w, _ := scriptedWorkspace(t, git)

	_, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "development")
	if err == nil || !strings.Contains(err.Error(), stderr) {
		t.Fatalf("err = %v, want it to carry %q", err, stderr)
	}
}

// The tests below run the real git against temporary repositories.

// isolateGit keeps the developer's git config and any enclosing repository
// out of the test, for the test's own git calls and the workspace's alike.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	// A git hook running the tests sets these to the enclosing repository.
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR"} {
		if _, ok := os.LookupEnv(k); ok {
			t.Setenv(k, "")
			_ = os.Unsetenv(k)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// gitIn runs git in dir with a fixed identity and returns its trimmed stdout.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=crew test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", full...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			t.Fatalf("git %q in %s: %v: %s", args, dir, err, exit.Stderr)
		}
		t.Fatalf("git %q in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// remote is a local bare origin whose default branch is trunk, plus a
// working repository that pushes to it.
type remote struct {
	bare, seed string
}

func newRemote(t *testing.T) remote {
	t.Helper()
	isolateGit(t)
	base := t.TempDir()
	r := remote{bare: filepath.Join(base, "origin.git"), seed: filepath.Join(base, "seed")}
	gitIn(t, base, "init", "--bare", "--initial-branch=trunk", r.bare)
	gitIn(t, base, "clone", r.bare, r.seed)
	r.advance(t)
	return r
}

// advance pushes a new commit to origin's trunk and returns its hash.
func (r remote) advance(t *testing.T) string {
	t.Helper()
	gitIn(t, r.seed, "commit", "--allow-empty", "-m", "advance")
	gitIn(t, r.seed, "push", "origin", "HEAD:trunk")
	return gitIn(t, r.seed, "rev-parse", "HEAD")
}

func (r remote) clone(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	gitIn(t, filepath.Dir(dir), "clone", r.bare, dir)
	return dir
}

func checkFromLatest(t *testing.T, space port.Space, latest string) {
	t.Helper()
	if head := gitIn(t, space.Dir, "rev-parse", "HEAD"); head != latest {
		t.Errorf("%s: HEAD = %s, want the latest origin/trunk %s", space.Workspace.Name, head, latest)
	}
	if branch := gitIn(t, space.Dir, "branch", "--show-current"); branch != space.Workspace.Branch {
		t.Errorf("%s: on branch %q, want %q", space.Workspace.Name, branch, space.Workspace.Branch)
	}
}

func TestCreateFromOriginDefaultBranchInRealRepository(t *testing.T) {
	r := newRemote(t)
	root := r.clone(t)
	latest := r.advance(t)

	w := New(&proc.Group{}, root)
	if err := w.Prepare(t.Context(), nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	space, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "development")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := port.Space{
		Workspace: crew.Workspace{Name: "issue-7-development", Branch: "crew/issue-7-development"},
		Dir:       filepath.Join(root, ".crew", "worktrees", "issue-7-development"),
	}
	if space != want {
		t.Errorf("space = %+v, want %+v", space, want)
	}
	checkFromLatest(t, space, latest)
}

func TestConcurrentCreationsAfterOriginAdvanced(t *testing.T) {
	r := newRemote(t)
	root := r.clone(t)
	latest := r.advance(t)
	w := New(&proc.Group{}, root)

	spaces := make([]port.Space, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range spaces {
		wg.Go(func() {
			spaces[i], errs[i] = w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "development")
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("creation %d: %v", i, err)
		}
	}
	names := []crew.WorkspaceName{spaces[0].Workspace.Name, spaces[1].Workspace.Name}
	slices.Sort(names)
	if want := []crew.WorkspaceName{"issue-7-development", "issue-7-development-2"}; !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
	for _, s := range spaces {
		checkFromLatest(t, s, latest)
	}
}

func TestCreateFindsDefaultBranchThroughLsRemoteWhenOriginAddedByHand(t *testing.T) {
	r := newRemote(t)
	root := filepath.Join(t.TempDir(), "repo")
	gitIn(t, filepath.Dir(root), "init", "--initial-branch=elsewhere", root)
	gitIn(t, root, "remote", "add", "origin", r.bare)

	w := New(&proc.Group{}, root)
	space, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "development")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	checkFromLatest(t, space, gitIn(t, r.seed, "rev-parse", "HEAD"))
}

func TestPrepareOutsideGitCheckoutNamesDirectory(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))

	err := New(&proc.Group{}, dir).Prepare(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v, want it to name %s", err, dir)
	}
}

func TestPrepareWithoutOriginNamesOrigin(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	gitIn(t, dir, "init")

	err := New(&proc.Group{}, dir).Prepare(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("err = %v, want it to name origin", err)
	}
}

// reopenable is a real repository with one worktree Create made in it.
func reopenable(t *testing.T) (remote, string, *Workspace, port.Space) {
	t.Helper()
	r := newRemote(t)
	root := r.clone(t)
	w := New(&proc.Group{}, root)
	space, err := w.Create(t.Context(), crew.NewIssue(crew.IssueData{ID: issueID("7")}), "development")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r, root, w, space
}

func TestReopenLeavesWorktreeAsItWasAfterOriginMovedOn(t *testing.T) {
	r, root, w, space := reopenable(t)
	gitIn(t, space.Dir, "commit", "--allow-empty", "-m", "work")
	own := gitIn(t, space.Dir, "rev-parse", "HEAD")
	notes := filepath.Join(space.Dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("half done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetched := gitIn(t, root, "rev-parse", "origin/trunk")
	r.advance(t)

	got, err := w.Reopen(t.Context(), space.Workspace)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if got != space {
		t.Errorf("space = %+v, want %+v", got, space)
	}
	if head := gitIn(t, space.Dir, "rev-parse", "HEAD"); head != own {
		t.Errorf("HEAD = %s, want the worktree's own commit %s", head, own)
	}
	if b, err := os.ReadFile(notes); err != nil || string(b) != "half done\n" {
		t.Errorf("uncommitted file = %q, %v; want it untouched", b, err)
	}
	if now := gitIn(t, root, "rev-parse", "origin/trunk"); now != fetched {
		t.Errorf("origin/trunk moved from %s to %s: Reopen fetched", fetched, now)
	}
}

func TestReopenGoneWorktree(t *testing.T) {
	tests := []struct {
		name   string
		remove func(t *testing.T, root string, space port.Space)
	}{
		{name: "removed with git", remove: func(t *testing.T, root string, space port.Space) {
			t.Helper()
			gitIn(t, root, "worktree", "remove", "--force", space.Dir)
		}},
		{name: "folder deleted without git", remove: func(t *testing.T, _ string, space port.Space) {
			t.Helper()
			if err := os.RemoveAll(space.Dir); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, root, w, space := reopenable(t)
			tt.remove(t, root, space)

			_, err := w.Reopen(t.Context(), space.Workspace)
			if !errors.Is(err, port.ErrWorkspaceGone) {
				t.Fatalf("err = %v, want it to wrap port.ErrWorkspaceGone", err)
			}
		})
	}
}

func TestReopenFolderGitDoesNotListNamesFolder(t *testing.T) {
	r := newRemote(t)
	root := r.clone(t)
	dir := filepath.Join(root, ".crew", "worktrees", "issue-7-development")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	_, err := New(&proc.Group{}, root).Reopen(t.Context(),
		crew.Workspace{Name: "issue-7-development", Branch: "crew/issue-7-development"})
	if err == nil || errors.Is(err, port.ErrWorkspaceGone) {
		t.Fatalf("err = %v, want an error that is not port.ErrWorkspaceGone", err)
	}
	if !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "remove") {
		t.Errorf("err = %v, want it to name %s and say to remove it", err, dir)
	}
}

func TestReopenAfterTheRepositoryMovedSaysToRepairTheWorktree(t *testing.T) {
	_, root, _, space := reopenable(t)
	if err := os.WriteFile(filepath.Join(space.Dir, "notes.txt"), []byte("half done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}

	_, err := New(&proc.Group{}, moved).Reopen(t.Context(), space.Workspace)
	if err == nil || errors.Is(err, port.ErrWorkspaceGone) {
		t.Fatalf("err = %v, want an error that is not port.ErrWorkspaceGone", err)
	}
	if !strings.Contains(err.Error(), "git worktree repair") {
		t.Errorf("err = %v, want it to offer git worktree repair, which keeps the work", err)
	}
	notes := filepath.Join(moved, ".crew", "worktrees", string(space.Workspace.Name), "notes.txt")
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("the worktree's uncommitted file: %v", err)
	}
}

func TestReopenReturnsBranchCheckedOut(t *testing.T) {
	tests := []struct {
		name     string
		checkout []string
		want     string
	}{
		{name: "detached HEAD keeps the recorded branch", checkout: []string{"checkout", "--detach"},
			want: "crew/issue-7-development"},
		{name: "another branch", checkout: []string{"checkout", "-b", "crew/elsewhere"}, want: "crew/elsewhere"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, w, space := reopenable(t)
			gitIn(t, space.Dir, tt.checkout...)

			got, err := w.Reopen(t.Context(), space.Workspace)
			if err != nil {
				t.Fatalf("Reopen: %v", err)
			}
			if got.Workspace.Branch != tt.want || got.Dir != space.Dir || got.Workspace.Name != space.Workspace.Name {
				t.Errorf("space = %+v, want %+v on branch %s", got, space, tt.want)
			}
		})
	}
}

func TestReopenThroughSymlinkedRoot(t *testing.T) {
	_, root, _, space := reopenable(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}

	got, err := New(&proc.Group{}, link).Reopen(t.Context(), space.Workspace)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	want := port.Space{
		Workspace: space.Workspace,
		Dir:       filepath.Join(link, ".crew", "worktrees", string(space.Workspace.Name)),
	}
	if got != want {
		t.Errorf("space = %+v, want %+v", got, want)
	}
}

// issueID returns the id of the issue keyed key, in no repository.
func issueID(key string) crew.IssueID { return crew.IssueID{Key: key} }
