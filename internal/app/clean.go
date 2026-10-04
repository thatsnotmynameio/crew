package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/registry"
	"github.com/thatsnotmynameio/crew/internal/worktrees"
)

// CleanOptions are what Clean needs from the process it runs in.
type CleanOptions struct {
	// Registry resolves the config's tracker name; Clean builds no harness.
	Registry registry.Registry
	// Workspace returns the workspace adapter for the repository at root.
	Workspace func(root string) port.Workspace
	// Root is the repository's absolute root, where .crew/ lives.
	Root string
	// Stdin is where the boss's answer is read from.
	Stdin io.Reader
	// Stdout receives the list, the question and the report; Stderr
	// receives errors.
	Stdout, Stderr io.Writer
	// Terminal tells whether Stdin is a terminal, so the boss can be asked.
	Terminal bool
}

// Clean runs crew worktrees clean in the repository at o.Root and returns its
// exit code (KTD10). It loads the config and builds its tracker, never
// preparing it and never making a mate act, so GitHub is read as the boss
// (KTD1, KTD11). A config that does not load, a tracker that cannot look up
// pull requests, a workspace that cannot list its worktrees or a listing that
// fails is printed to Stderr, and Clean returns ExitConfig having listed
// nothing. Otherwise it returns ExitClean when every check, lookup and
// removal worked, and ExitFailure when one failed or ctx ended first.
func Clean(ctx context.Context, o CleanOptions) int {
	errorf := func(format string, args ...any) int {
		_, _ = fmt.Fprintf(o.Stderr, "crew: "+format+"\n", args...)
		return ExitConfig
	}
	cfg, err := config.Load(o.Root)
	if err != nil {
		return errorf("%v", err)
	}
	tracker, err := o.Registry.Tracker(cfg.Tracker, cfg.TrackerSection, crew.WorkflowStates(cfg.Workflow), cfg.Extras)
	if err != nil {
		return errorf("%v", err)
	}
	finder, ok := tracker.(port.PullRequestFinder)
	if !ok {
		return errorf("the tracker %s cannot look up pull requests, so no worktree can be told merged", cfg.Tracker)
	}
	sweeper, ok := o.Workspace(o.Root).(port.Sweeper)
	if !ok {
		return errorf("the workspace cannot list its worktrees")
	}
	result, err := worktrees.Clean(ctx, worktrees.Options{
		Sweeper:  sweeper,
		Finder:   finder,
		Runs:     func() (map[string]time.Time, error) { return engine.UnendedRuns(o.Root) },
		Dir:      filepath.Join(o.Root, ".crew", "worktrees"),
		In:       o.Stdin,
		Out:      o.Stdout,
		Terminal: o.Terminal,
	})
	switch {
	case err != nil:
		return errorf("%v", err)
	case result == worktrees.Done:
		return ExitClean
	default:
		return ExitFailure
	}
}
