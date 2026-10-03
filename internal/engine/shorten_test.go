package engine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// failingWorkspace fails every creation with an error naming local paths, as
// git's stderr does.
type failingWorkspace struct {
	root, home string
}

func (w failingWorkspace) Create(_ context.Context, issue crew.Issue, action string) (port.Space, error) {
	dir := filepath.Join(w.root, ".crew", "worktrees", "issue-"+issue.Key+"-"+action)
	config := filepath.Join(w.home, ".gitconfig")
	return port.Space{}, fmt.Errorf("git worktree add: fatal: '%s' already exists (see %s)", dir, config)
}

func TestAWorkspaceFailureReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Workspace = failingWorkspace{root: cfg.Root, home: cfg.Home}
		r := start(t, cfg)

		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		reports := tr.Reports()
		if len(reports) != 1 || len(reports[0].Failures) != 1 {
			t.Fatalf("reports = %+v, want one with one failure", reports)
		}
		want := "git worktree add: fatal: './.crew/worktrees/issue-1-development' already exists (see ~/.gitconfig)"
		if got := reports[0].Failures[0].Reason; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

// reportedReason runs cfg, calling during first when it is set, until every
// goroutine is blocked, stops it, and returns the reason of the one failure
// the tracker received.
func reportedReason(t *testing.T, tr *fake.Tracker, cfg engine.Config, during func(*rig)) string {
	t.Helper()
	r := start(t, cfg)
	if during != nil {
		during(r)
	}
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := tr.Reports()
	if len(reports) != 1 || len(reports[0].Failures) != 1 {
		t.Fatalf("reports = %+v, want one with one failure", reports)
	}
	return reports[0].Failures[0].Reason
}

func TestASessionsReasonReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)

		got := reportedReason(t, tr, cfg, func(r *rig) {
			r.sessions(1)["issue-1-development"].End(crew.Outcome{
				Reason: fmt.Sprintf("go test failed in %s/engine (cache %s/.cache), ran in %s.", cfg.Root, cfg.Home, cfg.Root),
			})
		})

		if want := "go test failed in ./engine (cache ~/.cache), ran in .."; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

// failingHarness fails every start with an error naming local paths, as a
// harness's stderr does.
type failingHarness struct {
	home string
}

func (h failingHarness) Start(_ context.Context, run port.Run) (port.Session, error) {
	return nil, fmt.Errorf("claude: cannot run in %s: no settings in %s", run.Dir, filepath.Join(h.home, ".claude"))
}

func TestAHarnessStartFailureReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Harness = failingHarness{home: cfg.Home}

		got := reportedReason(t, tr, cfg, nil)

		if want := "claude: cannot run in ./.crew/worktrees/issue-1-development: no settings in ~/.claude"; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

func TestALogThatCannotOpenReachesTheReportWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		// A file where the log directory goes makes creating it fail.
		if err := os.WriteFile(filepath.Join(cfg.Root, ".crew", "logs"), nil, 0o600); err != nil {
			t.Fatal(err)
		}

		got := reportedReason(t, tr, cfg, nil)

		if !strings.Contains(got, "./.crew/logs") || strings.Contains(got, cfg.Root) {
			t.Errorf("reason = %q, want it to name ./.crew/logs and not %s", got, cfg.Root)
		}
	})
}

// failingLister is a fake tracker whose listings fail with an error naming
// local paths, as gh's stderr does.
type failingLister struct {
	*fake.Tracker

	root, home string
}

func (f failingLister) List(context.Context, []crew.State) ([]crew.Issue, error) {
	return nil, fmt.Errorf("gh: no repository in %s (config %s)", f.root, filepath.Join(f.home, ".config", "gh"))
}

func TestATrackerCallsReasonHasLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config(t, nil, develop)
		cfg.Tracker = failingLister{Tracker: fake.NewTracker(), root: cfg.Root, home: cfg.Home}
		r := start(t, cfg)

		synctest.Wait()
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := "gh: no repository in . (config ~/.config/gh)"
		failed := slices.ContainsFunc(final.Snapshot.Recent, func(e core.Event) bool {
			f, ok := e.(core.ListingFailed)
			return ok && f.Reason == want
		})
		if !failed {
			t.Errorf("recent events = %#v, want a ListingFailed with reason %q", final.Snapshot.Recent, want)
		}
	})
}
