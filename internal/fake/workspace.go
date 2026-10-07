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
)

// dirMode is the permission of a workspace's directory: the owner's, and
// read-only for the group.
const dirMode = 0o700

// Workspace creates plain directories under a root, such as t.TempDir(). It
// is not in the registry, as no config key selects a workspace; tests build
// it directly.
type Workspace struct {
	root string

	mu     sync.Mutex
	spaces []port.Space
}

// NewWorkspace returns a workspace that creates directories under root.
func NewWorkspace(root string) *Workspace {
	return &Workspace{root: root}
}

// Create implements port.Workspace. It names the workspace
// issue-<key>-<action>, adding -2, -3 and so on when that directory exists,
// and its branch crew/<name>.
func (w *Workspace) Create(_ context.Context, issue crew.Issue, action crew.ActionName) (port.Space, error) {
	root, err := filepath.Abs(w.root)
	if err != nil {
		return port.Space{}, fmt.Errorf("workspace root: %w", err)
	}
	base := fmt.Sprintf("issue-%s-%s", issue.ID.Key, action)
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
		space := port.Space{Name: crew.WorkspaceName(name), Dir: dir, Branch: "crew/" + name}
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
	dir := filepath.Join(root, string(space.Name))
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
