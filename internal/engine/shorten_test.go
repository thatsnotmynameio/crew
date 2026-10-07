package engine_test

import (
	"context"
	"errors"
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

func TestAWorkspaceFailureEndsTheActionWithLocalPathsShortened(t *testing.T) {
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
		if got := r.lastReason(); got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

// reportedReason runs cfg, calling during first when it is set, until every
// goroutine is blocked, stops it, and returns the reason development ended
// with once the tracker received one report with one failure.
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
	return r.lastReason()
}

func TestASessionsReasonEndsTheActionWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)

		got := reportedReason(t, tr, cfg, func(r *rig) {
			r.sessions(1)["issue-1-development"].End(port.Verdict{
				Reason: fmt.Sprintf("go test failed in %s/engine (cache %s/.cache), ran in %s.", cfg.Root, cfg.Home, cfg.Root),
			})
		})

		if want := "go test failed in ./engine (cache ~/.cache), ran in .."; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

// endedReason runs cfg until every goroutine is blocked, ends the one
// session with reason, stops the engine and returns the reason of the
// action's ActionEnded event.
func endedReason(t *testing.T, cfg engine.Config, reason string) string {
	t.Helper()
	r := start(t, cfg)
	r.session().End(port.Verdict{Reason: reason})
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var reasons []string
	for _, e := range r.events() {
		if ended, ok := e.(core.ActionEnded); ok {
			reasons = append(reasons, ended.Outcome.Reason.String())
		}
	}
	if len(reasons) != 1 {
		t.Fatalf("ActionEnded reasons = %q, want one", reasons)
	}
	return reasons[0]
}

func TestASessionsReasonEndsTheActionWithoutControlBytesAndWithTokensRedacted(t *testing.T) {
	tests := []struct {
		name, reason, want string
	}{
		{name: "colour and nul", reason: "exit 1 after: \x1b[31mbo\x00om\x1b[0m", want: "exit 1 after: bo om"},
		// Removing the escape sequence joins the token's prefix, so the
		// scrub runs again after the strip.
		{name: "token split by an escape", reason: "token gh\x1b[0mp_abc123 leaked", want: "token [redacted token] leaked"},
		{name: "token after a nul", reason: "foo\x00ghp_abc123", want: "foo [redacted token]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr := fake.NewTracker(issue(1, ready))

				if got := endedReason(t, config(t, tr, develop), tt.reason); got != tt.want {
					t.Errorf("reason = %q, want %q", got, tt.want)
				}
			})
		})
	}
}

// multiLineWorkspace fails every creation with git's multi-line stderr.
type multiLineWorkspace struct{}

func (multiLineWorkspace) Create(context.Context, crew.Issue, string) (port.Space, error) {
	return port.Space{}, errors.New("git worktree add: fatal: x\nhint: y")
}

func TestAWorkspaceFailureWithSeveralLinesEndsTheActionOnOneLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Workspace = multiLineWorkspace{}
		r := start(t, cfg)

		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := "git worktree add: fatal: x hint: y"
		events := r.events()
		if !slices.ContainsFunc(events, func(e core.Event) bool {
			ended, ok := e.(core.ActionEnded)
			return ok && ended.Outcome.Reason.String() == want
		}) {
			t.Errorf("events = %#v, want an ActionEnded with reason %q", events, want)
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

func TestAHarnessStartFailureEndsTheActionWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Harnesses = harnesses(failingHarness{home: cfg.Home})

		got := reportedReason(t, tr, cfg, nil)

		if want := "claude: cannot run in ./.crew/worktrees/issue-1-development: no settings in ~/.claude"; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

func TestALogThatCannotOpenEndsTheActionWithLocalPathsShortened(t *testing.T) {
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
