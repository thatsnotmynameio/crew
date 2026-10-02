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
// states is not used.
func (w *Workspace) Prepare(ctx context.Context, _ []crew.State) error {
	if _, err := w.git(ctx, "rev-parse", "--show-toplevel"); err != nil {
		return fmt.Errorf("%s is not inside a git checkout: %w", w.root, err)
	}
	if _, err := w.git(ctx, "remote", "get-url", "origin"); err != nil {
		return fmt.Errorf("the git checkout at %s has no origin remote: %w", w.root, err)
	}
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
func (w *Workspace) Create(ctx context.Context, issue crew.Issue, action string) (port.Space, error) {
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
	space, err := w.free(ctx, "issue-"+sanitize(issue.Key)+"-"+sanitize(action))
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
		space := port.Space{Name: name, Dir: filepath.Join(root, worktrees, name), Branch: "crew/" + name}
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
