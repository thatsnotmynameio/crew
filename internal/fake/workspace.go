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

// Compile-time guard.
var _ port.Workspace = (*Workspace)(nil)

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
		err := os.Mkdir(dir, 0o750)
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

// Spaces returns the workspaces created so far, in creation order.
func (w *Workspace) Spaces() []port.Space {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.spaces)
}
