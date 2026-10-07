// Package git is the workspace adapter: each action works in its own git
// worktree, on its own branch, made from the latest default branch of origin.
package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Compile-time guards.
var (
	_ port.Workspace = (*Workspace)(nil)
	_ port.Preparer  = (*Workspace)(nil)
	_ port.Reopener  = (*Workspace)(nil)
)

// worktrees is where the worktrees go, relative to the repository root.
const worktrees = ".crew/worktrees"

// Workspace creates each action's worktree under <root>/.crew/worktrees/,
// on a new branch from origin's latest default branch. It is safe for
// concurrent use, and creates one worktree at a time.
type Workspace struct {
	root string
	run  proc.Runner
	// lock serializes creations: concurrent fetches race on the default
	// branch's ref lock. It is a channel so a waiter can give up on ctx.
	lock chan struct{}
	// defaultBranch is origin's default branch, resolved once; guarded by
	// lock.
	defaultBranch string
}

// New returns a workspace for the repository at root, the absolute
// directory crew runs in, running git through group.
func New(group *proc.Group, root string) *Workspace {
	return &Workspace{root: root, run: group.Run, lock: make(chan struct{}, 1)}
}

// Prepare implements port.Preparer. It checks that the root is inside a git
// checkout with an origin remote, and resolves origin's default branch.
// It reports each of the two steps on ctx as it starts; Create, which may
// resolve the default branch later, reports none. states is not used.
func (w *Workspace) Prepare(ctx context.Context, _ []crew.State) error {
	port.Step(ctx, "checking the git checkout and its origin")
	if _, err := w.git(ctx, "rev-parse", "--show-toplevel"); err != nil {
		return fmt.Errorf("%s is not inside a git checkout: %w", w.root, err)
	}
	if _, err := w.git(ctx, "remote", "get-url", "origin"); err != nil {
		return fmt.Errorf("the git checkout at %s has no origin remote: %w", w.root, err)
	}
	port.Step(ctx, "resolving origin's default branch")
	if err := w.acquire(ctx); err != nil {
		return err
	}
	defer w.release()
	_, err := w.resolveDefault(ctx)
	return err
}

// Create implements port.Workspace. It fetches origin's default branch, then
// adds the worktree .crew/worktrees/<name> on the new branch crew/<name>
// from origin/<default>. The name is issue-<key>-<action>, both lowercased
// and with every character outside [a-z0-9-] replaced by '-', suffixed -2,
// -3… while the folder or the branch exists. The default branch is resolved
// on the first call when Prepare has not resolved it. Errors carry git's
// stderr.
func (w *Workspace) Create(ctx context.Context, issue crew.Issue, action crew.ActionName) (port.Space, error) {
	if err := w.acquire(ctx); err != nil {
		return port.Space{}, err
	}
	defer w.release()

	def, err := w.resolveDefault(ctx)
	if err != nil {
		return port.Space{}, err
	}
	if _, err := w.git(ctx, "fetch", "origin", def); err != nil {
		return port.Space{}, fmt.Errorf("fetch origin %s: %w", def, err)
	}
	space, err := w.free(ctx, "issue-"+sanitize(issue.ID().Key)+"-"+sanitize(string(action)))
	if err != nil {
		return port.Space{}, err
	}
	// --no-track: the branch is new work, not a copy of the default branch,
	// so a bare `git push` must not target the default branch.
	if _, err := w.git(ctx, "worktree", "add", "--no-track", "-b", space.Branch, space.Dir, "origin/"+def); err != nil {
		return port.Space{}, fmt.Errorf("create worktree %s: %w", space.Name, err)
	}
	return space, nil
}

// Reopen implements port.Reopener. It finds the worktree
// .crew/worktrees/<name> under the root and reads it from `git worktree
// list` without changing it: no fetch, merge, rebase or checkout, so the
// resumed session sees what the failed one left (R6). A folder that is
// missing, or that git lists as prunable, is gone. A folder git does not
// list is an error, not gone, so work left in it is never silently
// abandoned for a new worktree: git lists a worktree under the path it was
// created at, so after the repository moves the error offers `git worktree
// repair`, which keeps that work. The branch is the one checked out, or the
// recorded one when HEAD is detached, as in the middle of a rebase. Errors
// carry git's stderr.
func (w *Workspace) Reopen(ctx context.Context, space port.Space) (port.Space, error) {
	// The lock keeps a creation from adding a worktree under this name while
	// it is being inspected.
	if err := w.acquire(ctx); err != nil {
		return port.Space{}, err
	}
	defer w.release()

	root, err := filepath.Abs(w.root)
	if err != nil {
		return port.Space{}, fmt.Errorf("workspace root: %w", err)
	}
	dir := filepath.Join(root, worktrees, string(space.Name))
	out, err := w.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return port.Space{}, fmt.Errorf("list worktrees: %w", err)
	}
	tree, listed := findWorktree(string(out.Stdout), canonical(dir))
	if listed && tree.prunable {
		return port.Space{}, fmt.Errorf("worktree %s: git lists it as prunable: %w", dir, port.ErrWorkspaceGone)
	}
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return port.Space{}, fmt.Errorf("worktree %s: %w", dir, port.ErrWorkspaceGone)
	} else if err != nil {
		return port.Space{}, fmt.Errorf("check worktree folder %s: %w", dir, err)
	}
	if !listed {
		return port.Space{}, fmt.Errorf("the folder %s is not one of this repository's git worktrees: "+
			"if the repository moved since crew created it, run `git worktree repair %s` to keep its work; "+
			"otherwise save any work in it, then remove it so crew can create a new one", dir, dir)
	}
	branch := tree.branch
	if branch == "" {
		branch = space.Branch
	}
	return port.Space{Name: space.Name, Dir: dir, Branch: branch}, nil
}

// worktree is one entry of `git worktree list --porcelain`.
type worktree struct {
	// branch is the branch checked out, or "" when HEAD is detached.
	branch   string
	prunable bool
}

// findWorktree returns the entry of the porcelain listing out whose path is
// dir, which is canonical. The listing is records of "key value" lines
// separated by blank lines, each starting with "worktree <path>".
func findWorktree(out, dir string) (worktree, bool) {
	var tree worktree
	found := false
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\n")
		key, value, _ := strings.Cut(line, " ")
		switch {
		case key == "worktree":
			if found {
				return tree, true
			}
			found = canonical(value) == dir
			tree = worktree{}
		case !found:
		case key == "branch":
			tree.branch = strings.TrimPrefix(value, "refs/heads/")
		case key == "prunable":
			tree.prunable = true
		}
	}
	return tree, found
}

// canonical resolves the symlinks in p's longest existing ancestor, so two
// spellings of one path compare equal even when its last folders are gone:
// git may record a worktree through a symlink the root does not use, as
// macOS's /var does for /private/var.
func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(canonical(parent), filepath.Base(p))
}

// free returns the first of base, base-2, base-3… whose folder and branch do
// not exist.
func (w *Workspace) free(ctx context.Context, base string) (port.Space, error) {
	root, err := filepath.Abs(w.root)
	if err != nil {
		return port.Space{}, fmt.Errorf("workspace root: %w", err)
	}
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		space := port.Space{Name: crew.WorkspaceName(name), Dir: filepath.Join(root, worktrees, name), Branch: "crew/" + name}
		if _, err := os.Lstat(space.Dir); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return port.Space{}, fmt.Errorf("check worktree folder %s: %w", space.Dir, err)
		}
		out, err := w.git(ctx, "branch", "--list", space.Branch)
		if err != nil {
			return port.Space{}, fmt.Errorf("check branch %s: %w", space.Branch, err)
		}
		if strings.TrimSpace(string(out.Stdout)) == "" {
			return space, nil
		}
	}
}

// resolveDefault returns origin's default branch, resolving it the first
// time: from origin/HEAD, which only a clone sets, else by asking origin.
// The caller holds lock.
func (w *Workspace) resolveDefault(ctx context.Context) (string, error) {
	if w.defaultBranch != "" {
		return w.defaultBranch, nil
	}
	if out, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b, ok := strings.CutPrefix(strings.TrimSpace(string(out.Stdout)), "origin/"); ok && b != "" {
			w.defaultBranch = b
			return b, nil
		}
	}
	out, err := w.git(ctx, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", fmt.Errorf("find origin's default branch: %w", err)
	}
	// The symref line reads "ref: refs/heads/<branch>\tHEAD".
	for line := range strings.Lines(string(out.Stdout)) {
		ref, ok := strings.CutPrefix(line, "ref: ")
		if !ok {
			continue
		}
		ref, _, _ = strings.Cut(ref, "\t")
		if b, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/"); ok && b != "" {
			w.defaultBranch = b
			return b, nil
		}
	}
	return "", errors.New("find origin's default branch: origin reports no HEAD branch")
}

// git runs git with args in the root.
func (w *Workspace) git(ctx context.Context, args ...string) (proc.Output, error) {
	return w.run(ctx, proc.Command{Name: "git", Args: args, Dir: w.root})
}

// acquire takes lock, or gives up when ctx is done.
func (w *Workspace) acquire(ctx context.Context) error {
	select {
	case w.lock <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for another worktree creation: %w", ctx.Err())
	}
}

func (w *Workspace) release() { <-w.lock }

// sanitize lowercases s and replaces every character outside [a-z0-9-]
// with '-', so it is safe in a folder and a branch name.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r - 'A' + 'a'
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		}
		return '-'
	}, s)
}
