package git

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Compile-time guard.
var _ port.Sweeper = (*Workspace)(nil)

// Workspaces implements port.Sweeper, in name order. It lists the worktrees
// `git worktree list` records directly under .crew/worktrees/, then the
// folders there that git does not list. A listed worktree whose folder is
// missing, or that git lists as prunable, is gone, and is not inspected
// further. A present one is dirty when `git status` shows any change or
// untracked file; it was created when git wrote the commondir file of its
// admin directory, which git never writes again. Errors carry git's stderr.
func (w *Workspace) Workspaces(ctx context.Context) ([]port.Found, error) {
	root, err := filepath.Abs(w.root)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	base := filepath.Join(root, worktrees)
	out, err := w.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	var found []port.Found
	listed := map[string]bool{}
	canonBase := canonical(base)
	for _, tree := range parseWorktrees(string(out.Stdout)) {
		path := canonical(tree.path)
		if filepath.Dir(path) != canonBase {
			continue
		}
		name := filepath.Base(path)
		f, err := w.inspect(ctx, tree, port.Space{Name: name, Dir: filepath.Join(base, name), Branch: tree.branch})
		if err != nil {
			return nil, err
		}
		found = append(found, f)
		listed[name] = true
	}
	entries, err := os.ReadDir(base)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read the worktrees folder %s: %w", base, err)
	}
	for _, e := range entries {
		if e.IsDir() && !listed[e.Name()] {
			found = append(found, port.Found{Space: port.Space{Name: e.Name(), Dir: filepath.Join(base, e.Name())}})
		}
	}
	slices.SortFunc(found, func(a, b port.Found) int { return cmp.Compare(a.Space.Name, b.Space.Name) })
	return found, nil
}

// inspect returns what the listed worktree tree, at space, holds.
func (w *Workspace) inspect(ctx context.Context, tree worktree, space port.Space) (port.Found, error) {
	found := port.Found{Space: space, Listed: true, Gone: tree.prunable}
	if space.Branch != "" {
		found.Tip = tree.head
	}
	if _, err := os.Lstat(space.Dir); errors.Is(err, fs.ErrNotExist) {
		found.Gone = true
	} else if err != nil {
		return port.Found{}, fmt.Errorf("check worktree folder %s: %w", space.Dir, err)
	}
	if found.Gone {
		return found, nil
	}
	// --untracked-files=normal: the boss's status.showUntrackedFiles=no must
	// not hide untracked files. --no-optional-locks: a session working in the
	// worktree must not find index.lock taken by status's index refresh.
	status, err := w.gitAt(ctx, space.Dir, "--no-optional-locks", "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return port.Found{}, fmt.Errorf("check worktree %s for uncommitted files: %w", space.Dir, err)
	}
	found.Dirty = len(bytes.TrimSpace(status.Stdout)) > 0
	found.Created = w.created(ctx, space.Dir)
	return found, nil
}

// created returns the modification time of the commondir file in the admin
// directory of the worktree at dir, or the zero time when it cannot be read.
func (w *Workspace) created(ctx context.Context, dir string) time.Time {
	out, err := w.gitAt(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(strings.TrimSpace(string(out.Stdout)), "commondir"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Beyond implements port.Sweeper. It counts the commits of the local branch
// that commit does not reach, and never fetches: a commit git does not have
// is unknown. Errors carry git's stderr.
func (w *Workspace) Beyond(ctx context.Context, branch, commit string) (int, error) {
	// --end-of-options: commit comes from the tracker, and must never be
	// read as an option.
	out, err := w.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", commit+"^{commit}")
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
		return 0, fmt.Errorf("commit %s: %w", commit, port.ErrCommitUnknown)
	} else if err != nil {
		return 0, fmt.Errorf("find commit %s: %w", commit, err)
	}
	resolved := strings.TrimSpace(string(out.Stdout))
	out, err = w.git(ctx, "rev-list", "--count", resolved+"..refs/heads/"+branch, "--")
	if err != nil {
		return 0, fmt.Errorf("count the commits of %s after %s: %w", branch, commit, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out.Stdout)))
	if err != nil {
		return 0, fmt.Errorf("count the commits of %s after %s: %w", branch, commit, err)
	}
	return n, nil
}

// Remove implements port.Sweeper. It runs `git worktree remove` on the
// worktree .crew/worktrees/<space.Name> without --force, so git refuses one
// with modified, staged or untracked files, then, when deleteBranch, `git
// branch -D` on space.Branch: -d would refuse every crew branch, as none
// has an upstream. It never touches origin. It holds the lock, so no
// creation reuses the name meanwhile. Errors carry git's stderr.
func (w *Workspace) Remove(ctx context.Context, space port.Space, deleteBranch bool) error {
	if err := w.acquire(ctx); err != nil {
		return err
	}
	defer w.release()

	root, err := filepath.Abs(w.root)
	if err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}
	dir := filepath.Join(root, worktrees, space.Name)
	if _, err := w.git(ctx, "worktree", "remove", dir); err != nil {
		return fmt.Errorf("remove worktree %s: %w", dir, err)
	}
	if !deleteBranch || space.Branch == "" {
		return nil
	}
	if _, err := w.git(ctx, "branch", "-D", space.Branch); err != nil {
		return fmt.Errorf("%w: delete branch %s: %w", port.ErrBranchKept, space.Branch, err)
	}
	return nil
}

// gitAt runs git with args in dir.
func (w *Workspace) gitAt(ctx context.Context, dir string, args ...string) (proc.Output, error) {
	return w.run(ctx, proc.Command{Name: "git", Args: args, Dir: dir})
}
