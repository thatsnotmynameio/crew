package engine_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// checkedDevelop is develop with a check on its action.
var checkedDevelop = crew.Stage{
	Name: develop.Name, Label: develop.Label, MovesTo: develop.MovesTo, OnSuccess: develop.OnSuccess,
	OnFailure: develop.OnFailure,
	Actions:   []crew.Action{{Name: "development", Prompt: develop.Actions[0].Prompt, Check: "gh pr list"}},
}

// checkedConfig is config for checkedDevelop, with checker as its checker.
func checkedConfig(t *testing.T, tr *fake.Tracker, checker *fake.Checker) engine.Config {
	t.Helper()
	cfg := config(t, tr, checkedDevelop)
	cfg.Checker = checker
	return cfg
}

// checkedRun runs issue 1 under cfg, ends its session with success, waits
// until every goroutine is blocked, then stops the engine unless it already
// stopped, and returns the tracker's one failure report's failures, or nil.
func checkedRun(t *testing.T, tr *fake.Tracker, cfg engine.Config) []crew.ActionFailure {
	t.Helper()
	r := start(t, cfg)
	r.sessions(1)["issue-1-development"].End(crew.Outcome{Succeeded: true, Reason: "done"})
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := tr.Reports()
	switch len(reports) {
	case 0:
		return nil
	case 1:
		return reports[0].Failures
	}
	t.Fatalf("reports = %+v, want at most one", reports)
	return nil
}

func TestAE2ACheckThatPassesKeepsTheSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		cfg := checkedConfig(t, tr, checker)

		if got := checkedRun(t, tr, cfg); got != nil {
			t.Fatalf("failures = %+v, want none", got)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
		checks := checker.Checks()
		if len(checks) != 1 {
			t.Fatalf("checks = %+v, want one", checks)
		}
		c := checks[0]
		dir := filepath.Join(cfg.Root, ".crew", "worktrees", "issue-1-development")
		if c.Command != "gh pr list" || c.Dir != dir || c.Branch != "crew/issue-1-development" ||
			c.IssueRef != "#1" || c.IssueKey != "1" || c.IssueURL != "https://example.test/issues/1" {
			t.Errorf("check = %+v", c)
		}
	})
}

func TestAE1ACheckThatFailsFailsTheActionWithItsLastLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{
			Print: "looking for a pull request\nno open pull request from crew/issue-1-development\n\n", Exit: 1,
		})
		cfg := checkedConfig(t, tr, checker)

		got := checkedRun(t, tr, cfg)

		want := []crew.ActionFailure{{
			Action: "development", Reason: "the check failed: no open pull request from crew/issue-1-development",
			Workspace: "issue-1-development", Log: ".crew/logs/issue-1-development.log",
		}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("failures = %+v, want %+v", got, want)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("#1 is in %v, want needs attention", got)
		}
		log, err := os.ReadFile(filepath.Join(cfg.Root, ".crew", "logs", "issue-1-development.log"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "\ncrew: running the check: gh pr list\nlooking for a pull request\nno open pull request from crew/issue-1-development\n\n"; string(log) != want {
			t.Errorf("log = %q, want %q", log, want)
		}
	})
}

func TestACheckThatPrintsNothingSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{Print: "\n  \n", Exit: 1})

		got := checkedRun(t, tr, checkedConfig(t, tr, checker))

		if len(got) != 1 || got[0].Reason != "the check failed and printed nothing" {
			t.Fatalf("failures = %+v", got)
		}
	})
}

func TestAE4ACheckThatNeverEndsRunsOutOfTimeAfterTenMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{Block: true})
		r := start(t, checkedConfig(t, tr, checker))
		r.sessions(1)["issue-1-development"].End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		t0 := time.Now()

		time.Sleep(10*time.Minute + time.Second)
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		reports := tr.Reports()
		if len(reports) != 1 || len(reports[0].Failures) != 1 {
			t.Fatalf("reports = %+v", reports)
		}
		if got, want := reports[0].Failures[0].Reason, "the check ran out of time after 10m0s"; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
		if took := time.Since(t0); took > 11*time.Minute {
			t.Errorf("judged after %v", took)
		}
	})
}

func TestAE9AStopEndsARunningCheckAndTheActionCountsAsStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{Block: true})

		got := checkedRun(t, tr, checkedConfig(t, tr, checker))

		if len(got) != 1 || got[0].Reason != "crew stopped" {
			t.Fatalf("failures = %+v, want development stopped", got)
		}
	})
}

func TestACheckThatCannotStartSaysWhyWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		cfg := checkedConfig(t, tr, checker)
		checker.Script("crew/issue-1-development", fake.CheckScript{
			StartErr: errors.New("chdir " + cfg.Root + "/.crew/worktrees/issue-1-development: no such file or directory"),
		})

		got := checkedRun(t, tr, cfg)

		want := "the check could not start: chdir ./.crew/worktrees/issue-1-development: no such file or directory"
		if len(got) != 1 || got[0].Reason != want {
			t.Fatalf("failures = %+v, want reason %q", got, want)
		}
	})
}

func TestAnEngineWithoutACheckerFailsAnActionWithACheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, checkedDevelop)

		got := checkedRun(t, tr, cfg)

		if len(got) != 1 || got[0].Reason != "the check could not start: crew has no check runner" {
			t.Fatalf("failures = %+v", got)
		}
	})
}

func TestWhenTheRunTimeIsUpARunningCheckFinishesBeforeTheEngineStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{Block: true})
		cfg := checkedConfig(t, tr, checker)
		cfg.RunTimeLimit = 5 * time.Minute
		t0 := time.Now()
		r := start(t, cfg)
		r.sessions(1)["issue-1-development"].End(crew.Outcome{Succeeded: true, Reason: "done"})

		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got, want := time.Since(t0), 10*time.Minute; got != want {
			t.Errorf("Run returned after %v, want %v, once the check ran out of time", got, want)
		}
		reports := tr.Reports()
		if len(reports) != 1 || reports[0].Failures[0].Reason != "the check ran out of time after 10m0s" {
			t.Errorf("reports = %+v, want the check's own end, not a stop", reports)
		}
	})
}
