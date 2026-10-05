package codex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// gitDirsTimeout bounds the git call Start makes before codex runs. It takes
// milliseconds; the bound keeps a stuck filesystem from holding a stop up.
const gitDirsTimeout = time.Minute

// gitDirs returns the absolute git dir and common git dir of the repository
// dir is in, through run. Codex's workspace-write sandbox keeps a worktree's
// git dir read-only unless it is itself a writable root, and a commit also
// writes objects and refs to the common dir, so both must be opened for the
// session to commit. In a main checkout they are the same dir.
func gitDirs(ctx context.Context, run proc.Runner, dir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitDirsTimeout)
	defer cancel()
	out, err := run(ctx, proc.Command{
		Name: "git",
		Args: []string{"rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir"},
		Dir:  dir,
	})
	if err != nil {
		return nil, fmt.Errorf("the codex harness needs the git dirs of the workspace %s: %w", dir, err)
	}
	dirs := strings.Split(strings.TrimRight(string(out.Stdout), "\n"), "\n")
	if len(dirs) != 2 || dirs[0] == "" || dirs[1] == "" {
		return nil, fmt.Errorf("the codex harness needs the git dirs of the workspace %s; git printed %q", dir, out.Stdout)
	}
	return dirs, nil
}
