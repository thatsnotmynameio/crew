package fake

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guards.
var (
	_ port.Workspace = (*Workspace)(nil)
	_ port.Reopener  = (*Workspace)(nil)
	_ port.Sweeper   = (*Workspace)(nil)
)

// dirMode is the permission of a workspace's directory: the owner's, and
// read-only for the group.
const dirMode = 0o700

// Workspace creates plain directories under a root, such as t.TempDir(). It
// is not in the registry, as no config key selects a workspace; tests build
// it directly. As a port.Sweeper it answers from scripted state, not from
// the directories it created.
type Workspace struct {
	root string

	mu         sync.Mutex
	spaces     []port.Space
	listings   []Listing
	beyond     map[string]beyondScript
	failRemove map[string]error
	failBranch map[string]error
	removals   []Removal
}

// Listing is what one Workspaces call returns.
type Listing struct {
	// Found is the workspaces listed.
	Found []port.Found
	// Err, when set, makes the call fail with it.
	Err error
}

// Removal is one workspace a Workspace removed.
type Removal struct {
	// Name is the removed workspace's name.
	Name string
	// DeleteBranch is whether its branch was deleted too.
	DeleteBranch bool
}

// beyondScript is what Beyond returns for a branch.
type beyondScript struct {
	count int
	err   error
}

// NewWorkspace returns a workspace that creates directories under root.
func NewWorkspace(root string) *Workspace {
	return &Workspace{root: root}
}

// Create implements port.Workspace. It names the workspace
// issue-<key>-<action>, adding -2, -3 and so on when that directory exists,
// and its branch crew/<name>.
func (w *Workspace) Create(_ context.Context, issue crew.Issue, action string) (port.Space, error) {
	root, err := filepath.Abs(w.root)
	if err != nil {
		return port.Space{}, fmt.Errorf("workspace root: %w", err)
	}
	base := fmt.Sprintf("issue-%s-%s", issue.Key, action)
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		dir := filepath.Join(root, name)
		err := os.Mkdir(dir, dirMode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return port.Space{}, fmt.Errorf("create workspace %s: %w", name, err)
		}
		space := port.Space{Name: name, Dir: dir, Branch: "crew/" + name}
		w.mu.Lock()
		w.spaces = append(w.spaces, space)
		w.mu.Unlock()
		return space, nil
	}
}

// Reopen implements port.Reopener. It returns the workspace named
// space.Name as it is, keeping the recorded branch, or an error wrapping
// port.ErrWorkspaceGone when its directory no longer exists.
func (w *Workspace) Reopen(_ context.Context, space port.Space) (port.Space, error) {
	root, err := filepath.Abs(w.root)
	if err != nil {
		return port.Space{}, fmt.Errorf("workspace root: %w", err)
	}
	dir := filepath.Join(root, space.Name)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return port.Space{}, fmt.Errorf("reopen workspace %s: %w", space.Name, port.ErrWorkspaceGone)
	} else if err != nil {
		return port.Space{}, fmt.Errorf("reopen workspace %s: %w", space.Name, err)
	}
	return port.Space{Name: space.Name, Dir: dir, Branch: space.Branch}, nil
}

// Spaces returns the workspaces created so far, in creation order.
func (w *Workspace) Spaces() []port.Space {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.spaces)
}

// ScriptWorkspaces makes the later Workspaces calls return listings, one per
// call, in order; the last one repeats. Unscripted, Workspaces lists none.
func (w *Workspace) ScriptWorkspaces(listings ...Listing) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.listings = slices.Clone(listings)
}

// Workspaces implements port.Sweeper with the next scripted listing.
func (w *Workspace) Workspaces(context.Context) ([]port.Found, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.listings) == 0 {
		return nil, nil
	}
	l := w.listings[0]
	if len(w.listings) > 1 {
		w.listings = w.listings[1:]
	}
	if l.Err != nil {
		return nil, fmt.Errorf("list workspaces: %w", l.Err)
	}
	return slices.Clone(l.Found), nil
}

// ScriptBeyond makes every later Beyond call for branch return count and
// err, whatever the commit. Unscripted, a branch has nothing beyond it.
func (w *Workspace) ScriptBeyond(branch string, count int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.beyond == nil {
		w.beyond = map[string]beyondScript{}
	}
	w.beyond[branch] = beyondScript{count: count, err: err}
}

// Beyond implements port.Sweeper as ScriptBeyond scripted it.
func (w *Workspace) Beyond(_ context.Context, branch, commit string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.beyond[branch]
	if s.err != nil {
		return 0, fmt.Errorf("count the commits of %s after %s: %w", branch, commit, s.err)
	}
	return s.count, nil
}

// FailRemove makes every later Remove of the workspace named name fail with
// err, removing nothing.
func (w *Workspace) FailRemove(name string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failRemove == nil {
		w.failRemove = map[string]error{}
	}
	w.failRemove[name] = err
}

// FailBranchDelete makes every later Remove of the workspace named name
// that deletes its branch remove the workspace, then fail with err wrapped in
// port.ErrBranchKept, keeping the branch.
func (w *Workspace) FailBranchDelete(name string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failBranch == nil {
		w.failBranch = map[string]error{}
	}
	w.failBranch[name] = err
}

// Remove implements port.Sweeper: it records the removal, unless FailRemove
// scripted it to fail, or FailBranchDelete to keep the branch. The scripted
// listings do not change.
func (w *Workspace) Remove(_ context.Context, space port.Space, deleteBranch bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.failRemove[space.Name]; err != nil {
		return fmt.Errorf("remove workspace %s: %w", space.Name, err)
	}
	if err := w.failBranch[space.Name]; err != nil && deleteBranch {
		w.removals = append(w.removals, Removal{Name: space.Name})
		return fmt.Errorf("%w: %w", port.ErrBranchKept, err)
	}
	w.removals = append(w.removals, Removal{Name: space.Name, DeleteBranch: deleteBranch})
	return nil
}

// Removals returns the workspaces removed so far, in order.
func (w *Workspace) Removals() []Removal {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.removals)
}
