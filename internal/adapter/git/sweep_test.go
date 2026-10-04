package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// listOne lists w's workspaces and returns the only one, failing otherwise.
func listOne(t *testing.T, w *Workspace) port.Found {
	t.Helper()
	found, err := w.Workspaces(t.Context())
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("Workspaces = %+v, want exactly one", found)
	}
	return found[0]
}

// commondir is the file whose time is a worktree's creation time.
func commondir(t *testing.T, space port.Space) string {
	t.Helper()
	return filepath.Join(gitIn(t, space.Dir, "rev-parse", "--absolute-git-dir"), "commondir")
}

func TestWorkspacesListsAWorktreeCreateMade(t *testing.T) {
	before := time.Now()
	_, _, w, space := reopenable(t)
	after := time.Now()

	got := listOne(t, w)
	if got.Space != space {
		t.Errorf("Space = %+v, want %+v", got.Space, space)
	}
	if !got.Listed || got.Gone || got.Dirty {
		t.Errorf("Listed, Gone, Dirty = %t, %t, %t; want a listed, present, clean worktree", got.Listed, got.Gone, got.Dirty)
	}
	if tip := gitIn(t, space.Dir, "rev-parse", "HEAD"); got.Tip != tip {
		t.Errorf("Tip = %q, want the branch's tip %s", got.Tip, tip)
	}
	// File times come from the kernel's coarse clock, which may trail
	// time.Now by a tick.
	if got.Created.Before(before.Add(-time.Second)) || got.Created.After(after.Add(time.Second)) {
		t.Errorf("Created = %v, want it between %v and %v", got.Created, before, after)
	}
}

func TestWorkspacesCreationTimeOutlivesCommitsAndGC(t *testing.T) {
	_, _, w, space := reopenable(t)
	made := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(commondir(t, space), made, made); err != nil {
		t.Fatal(err)
	}

	gitIn(t, space.Dir, "commit", "--allow-empty", "-m", "work")
	gitIn(t, space.Dir, "-c", "gc.reflogExpire=now", "-c", "gc.reflogExpireUnreachable=now", "gc")

	got := listOne(t, w)
	if !got.Created.Equal(made) {
		t.Errorf("Created = %v, want %v, unchanged by a commit and git gc", got.Created, made)
	}
	if tip := gitIn(t, space.Dir, "rev-parse", "HEAD"); got.Tip != tip {
		t.Errorf("Tip = %q, want the new commit %s", got.Tip, tip)
	}
}

func TestCreationTimeUnknownWithoutCommondir(t *testing.T) {
	_, root, w, _ := reopenable(t)

	// The main checkout's git directory has no commondir file.
	if got := w.created(t.Context(), root); !got.IsZero() {
		t.Errorf("created = %v, want the zero time when there is no commondir", got)
	}
	if got := w.created(t.Context(), filepath.Join(t.TempDir(), "missing")); !got.IsZero() {
		t.Errorf("created = %v, want the zero time when git cannot run", got)
	}
}

func TestWorkspacesTellsUncommittedFiles(t *testing.T) {
	tests := []struct {
		name string
		file string
		want bool
	}{
		{name: "no file", want: false},
		{name: "untracked file", file: "notes.txt", want: true},
		{name: "ignored file only", file: "debug.log", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, root, w, space := reopenable(t)
			exclude := filepath.Join(root, ".git", "info", "exclude")
			if err := os.WriteFile(exclude, []byte("*.log\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.file != "" {
				if err := os.WriteFile(filepath.Join(space.Dir, tt.file), []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if got := listOne(t, w); got.Dirty != tt.want {
				t.Errorf("Dirty = %t, want %t", got.Dirty, tt.want)
			}
		})
	}
}

func TestWorkspacesListsADeletedFolderAsGone(t *testing.T) {
	_, _, w, space := reopenable(t)
	if err := os.RemoveAll(space.Dir); err != nil {
		t.Fatal(err)
	}

	got := listOne(t, w)
	if got.Space.Name != space.Name || got.Space.Dir != space.Dir {
		t.Errorf("Space = %+v, want %s at %s", got.Space, space.Name, space.Dir)
	}
	if !got.Listed || !got.Gone {
		t.Errorf("Listed, Gone = %t, %t; want a listed worktree that is gone", got.Listed, got.Gone)
	}
}

func TestWorkspacesListsAFolderGitDoesNotList(t *testing.T) {
	r := newRemote(t)
	root := r.clone(t)
	dir := filepath.Join(root, ".crew", "worktrees", "issue-9-development")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	got := listOne(t, New(&proc.Group{}, root))
	if want := (port.Space{Name: "issue-9-development", Dir: dir}); got.Space != want {
		t.Errorf("Space = %+v, want %+v", got.Space, want)
	}
	if got.Listed || got.Gone {
		t.Errorf("Listed, Gone = %t, %t; want a folder git does not list, present", got.Listed, got.Gone)
	}
}

func TestWorkspacesListsADetachedWorktreeWithoutBranch(t *testing.T) {
	_, _, w, space := reopenable(t)
	gitIn(t, space.Dir, "checkout", "--detach")

	got := listOne(t, w)
	if got.Space.Branch != "" || got.Tip != "" {
		t.Errorf("Branch, Tip = %q, %q; want both empty for a detached HEAD", got.Space.Branch, got.Tip)
	}
	if got.Space.Name != space.Name || !got.Listed {
		t.Errorf("found %+v, want the listed worktree %s", got, space.Name)
	}
}

func TestWorkspacesListsOnlyCrewWorktrees(t *testing.T) {
	_, root, w, space := reopenable(t)
	outside := filepath.Join(t.TempDir(), "outside")
	gitIn(t, root, "worktree", "add", "-b", "elsewhere", outside)
	nested := filepath.Join(root, ".crew", "other")
	gitIn(t, root, "worktree", "add", "-b", "nested", nested)

	if got := listOne(t, w); got.Space.Name != space.Name {
		t.Errorf("listed %+v, want only %s", got, space.Name)
	}
}

func TestWorkspacesInAnEmptyRepositoryListsNothing(t *testing.T) {
	r := newRemote(t)
	found, err := New(&proc.Group{}, r.clone(t)).Workspaces(t.Context())
	if err != nil || len(found) != 0 {
		t.Errorf("Workspaces = %+v, %v; want none", found, err)
	}
}

func TestBeyondCountsTheBranchCommitsTheCommitLacks(t *testing.T) {
	tests := []struct {
		name string
		// prepare changes the worktree and returns the commit to compare.
		prepare func(t *testing.T, space port.Space) string
		want    int
	}{
		{name: "tip is the commit", want: 0, prepare: func(t *testing.T, space port.Space) string {
			t.Helper()
			gitIn(t, space.Dir, "commit", "--allow-empty", "-m", "work")
			return gitIn(t, space.Dir, "rev-parse", "HEAD")
		}},
		{name: "tip is behind the commit", want: 0, prepare: func(t *testing.T, space port.Space) string {
			t.Helper()
			gitIn(t, space.Dir, "commit", "--allow-empty", "-m", "merged later")
			head := gitIn(t, space.Dir, "rev-parse", "HEAD")
			gitIn(t, space.Dir, "reset", "--hard", "HEAD~1")
			return head
		}},
		{name: "one commit after the commit", want: 1, prepare: func(t *testing.T, space port.Space) string {
			t.Helper()
			head := gitIn(t, space.Dir, "rev-parse", "HEAD")
			gitIn(t, space.Dir, "commit", "--allow-empty", "-m", "after the merge")
			return head
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, w, space := reopenable(t)
			commit := tt.prepare(t, space)

			got, err := w.Beyond(t.Context(), space.Branch, commit)
			if err != nil || got != tt.want {
				t.Errorf("Beyond = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestBeyondACommitNotInTheRepository(t *testing.T) {
	_, _, w, space := reopenable(t)
	for _, commit := range []string{"0123456789abcdef0123456789abcdef01234567", "--output=x"} {
		_, err := w.Beyond(t.Context(), space.Branch, commit)
		if !errors.Is(err, port.ErrCommitUnknown) {
			t.Errorf("Beyond(%q) = %v, want it to wrap port.ErrCommitUnknown", commit, err)
		}
	}
}

func TestBeyondAMissingBranchFails(t *testing.T) {
	_, _, w, space := reopenable(t)
	head := gitIn(t, space.Dir, "rev-parse", "HEAD")

	_, err := w.Beyond(t.Context(), "crew/no-such-branch", head)
	if err == nil || errors.Is(err, port.ErrCommitUnknown) {
		t.Errorf("Beyond = %v, want an error that is not port.ErrCommitUnknown", err)
	}
}

// branchIn reports whether the repository at dir has the local branch.
func branchIn(t *testing.T, dir, branch string) bool {
	t.Helper()
	return gitIn(t, dir, "branch", "--list", branch) != ""
}

// checkRemoved fails unless git no longer has the worktree of space in root.
func checkRemoved(t *testing.T, root string, space port.Space) {
	t.Helper()
	if _, err := os.Lstat(space.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("folder %s: %v, want it removed", space.Dir, err)
	}
	if list := gitIn(t, root, "worktree", "list", "--porcelain"); strings.Contains(list, space.Name) {
		t.Errorf("git still lists the worktree:\n%s", list)
	}
}

func TestRemoveDeletesTheWorktreeAndTheBranchWhenAsked(t *testing.T) {
	tests := []struct {
		name         string
		deleteBranch bool
	}{
		{name: "branch deleted", deleteBranch: true},
		{name: "branch kept", deleteBranch: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, root, w, space := reopenable(t)
			gitIn(t, space.Dir, "commit", "--allow-empty", "-m", "work")
			gitIn(t, space.Dir, "push", "origin", space.Branch)

			if err := w.Remove(t.Context(), space, tt.deleteBranch); err != nil {
				t.Fatalf("Remove: %v", err)
			}
			checkRemoved(t, root, space)
			if got := branchIn(t, root, space.Branch); got == tt.deleteBranch {
				t.Errorf("branch %s exists = %t, want %t", space.Branch, got, !tt.deleteBranch)
			}
			if !branchIn(t, r.bare, space.Branch) {
				t.Errorf("origin's branch %s is gone, want it untouched", space.Branch)
			}
		})
	}
}

func TestRemoveRefusesAWorktreeWithAnUntrackedFile(t *testing.T) {
	_, root, w, space := reopenable(t)
	notes := filepath.Join(space.Dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("half done\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := w.Remove(t.Context(), space, true)
	if err == nil || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("Remove = %v, want git's refusal naming the untracked files", err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("the untracked file: %v, want it kept", err)
	}
	if !branchIn(t, root, space.Branch) {
		t.Errorf("branch %s is gone, want it kept", space.Branch)
	}
}

func TestSweepErrorsCarryGitStderr(t *testing.T) {
	const stderr = "fatal: something broke"
	tests := []struct {
		name string
		fail string
		call func(t *testing.T, w *Workspace) error
	}{
		{name: "listing", fail: "worktree list", call: func(t *testing.T, w *Workspace) error {
			t.Helper()
			_, err := w.Workspaces(t.Context())
			return err
		}},
		{name: "finding the commit", fail: "rev-parse", call: func(t *testing.T, w *Workspace) error {
			t.Helper()
			_, err := w.Beyond(t.Context(), "crew/issue-7-development", "abc")
			if errors.Is(err, port.ErrCommitUnknown) {
				t.Errorf("Beyond = %v, want git's failure, not an unknown commit", err)
			}
			return err
		}},
		{name: "counting", fail: "rev-list", call: func(t *testing.T, w *Workspace) error {
			t.Helper()
			_, err := w.Beyond(t.Context(), "crew/issue-7-development", "abc")
			return err
		}},
		{name: "deleting the branch", fail: "branch -D", call: func(t *testing.T, w *Workspace) error {
			t.Helper()
			err := w.Remove(t.Context(), port.Space{Name: "issue-7-development", Branch: "crew/issue-7-development"}, true)
			if !errors.Is(err, port.ErrBranchKept) {
				t.Errorf("Remove = %v, want port.ErrBranchKept: the worktree went, the branch did not", err)
			}
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := scriptedWorkspace(t, &scripted{fail: map[string]string{tt.fail: stderr}})
			if err := tt.call(t, w); err == nil || !strings.Contains(err.Error(), stderr) {
				t.Errorf("err = %v, want it to carry %q", err, stderr)
			}
		})
	}
}

func TestBeyondFailsOnACountGitDoesNotPrint(t *testing.T) {
	w, _ := scriptedWorkspace(t, &scripted{})
	if _, err := w.Beyond(t.Context(), "crew/issue-7-development", "abc"); err == nil {
		t.Error("Beyond = nil error, want one for git's empty output")
	}
}

func TestWorkspacesFailsWhenItCannotCheckAWorktree(t *testing.T) {
	_, _, w, space := reopenable(t)
	stderr := "fatal: index file corrupt"
	run := w.run
	w.run = func(ctx context.Context, c proc.Command) (proc.Output, error) {
		if slices.Contains(c.Args, "status") {
			return proc.Output{}, errors.New(stderr)
		}
		return run(ctx, c)
	}

	_, err := w.Workspaces(t.Context())
	if err == nil || !strings.Contains(err.Error(), stderr) || !strings.Contains(err.Error(), space.Dir) {
		t.Errorf("err = %v, want it to name %s and carry %q", err, space.Dir, stderr)
	}
}

func TestWorkspacesReadsStatusWithoutTakingGitsOptionalLocks(t *testing.T) {
	_, _, w, space := reopenable(t)
	var status []string
	run := w.run
	w.run = func(ctx context.Context, c proc.Command) (proc.Output, error) {
		if slices.Contains(c.Args, "status") {
			status = c.Args
		}
		return run(ctx, c)
	}

	if _, err := w.Workspaces(t.Context()); err != nil {
		t.Fatal(err)
	}
	// A session working in the worktree must not find index.lock taken by
	// a refresh git status would otherwise make.
	if len(status) == 0 || status[0] != "--no-optional-locks" {
		t.Errorf("git status args in %s = %q, want them to start with --no-optional-locks", space.Name, status)
	}
}

func TestWorkspacesFailsWhenTheWorktreesFolderIsAFile(t *testing.T) {
	r := newRemote(t)
	root := r.clone(t)
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "worktrees"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := New(&proc.Group{}, root).Workspaces(t.Context()); err == nil {
		t.Error("Workspaces = nil error, want one for a worktrees folder that cannot be read")
	}
}
