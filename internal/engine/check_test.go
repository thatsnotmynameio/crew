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
	"github.com/thatsnotmynameio/crew/internal/port"
)

// checkedDevelop is develop with a check on its action.
var checkedDevelop = crew.Rule{
	Name: develop.Name, Labels: develop.Labels,
	Actions: []crew.Action{{
		Name: "development", Prompt: develop.Actions[0].Prompt,
		Checks: []crew.Check{{Name: "pr-closes-issue", Script: "gh pr list"}},
	}},
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
// stopped, and returns the tracker's one failure report's failures, or nil,
// and the reason development ended with.
func checkedRun(t *testing.T, tr *fake.Tracker, cfg engine.Config) ([]crew.ActionFailure, string) {
	t.Helper()
	r := start(t, cfg)
	r.sessions(1)["issue-1-development"].End(port.Verdict{Succeeded: true, Reason: "done"})
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reason := r.lastReason()
	reports := tr.Reports()
	switch len(reports) {
	case 0:
		return nil, reason
	case 1:
		return reports[0].Failures, reason
	}
	t.Fatalf("reports = %+v, want at most one", reports)
	return nil, reason
}

func TestAE2ACheckThatPassesKeepsTheSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		cfg := checkedConfig(t, tr, checker)

		if got, _ := checkedRun(t, tr, cfg); got != nil {
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
			c.IssueRef != "#1" || c.IssueID != issueID("1") || c.IssueURL != "https://example.test/issues/1" {
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

		got, reason := checkedRun(t, tr, cfg)

		want := []crew.ActionFailure{{
			Action: "development", Workspace: "issue-1-development", Log: ".crew/logs/issue-1-development.log",
		}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("failures = %+v, want %+v", got, want)
		}
		if want := "the check pr-closes-issue failed: no open pull request from crew/issue-1-development"; reason != want {
			t.Errorf("reason = %q, want %q", reason, want)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("#1 is in %v, want needs attention", got)
		}
		wantLog(t, cfg.Root, "\ncrew: running the check pr-closes-issue: gh pr list\n"+
			"looking for a pull request\nno open pull request from crew/issue-1-development\n\n")
	})
}

func TestACheckThatPrintsNothingSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{Print: "\n  \n", Exit: 1})

		got, reason := checkedRun(t, tr, checkedConfig(t, tr, checker))

		if len(got) != 1 || reason != "the check pr-closes-issue failed and printed nothing" {
			t.Fatalf("failures = %+v, reason %q", got, reason)
		}
	})
}

func TestACheckReasonCarriesNoControlBytes(t *testing.T) {
	tests := []struct {
		name, printed, want string
	}{
		// A NUL in the reason would make every status write fail.
		{name: "nul", printed: "a\x00b\n", want: "the check pr-closes-issue failed: ab"},
		// A carriage return ends a line, as progress output uses it.
		{
			name: "carriage return", printed: "progress 10%\rno open pull request\r\n",
			want: "the check pr-closes-issue failed: no open pull request",
		},
		{name: "escape", printed: "\x1b[31mred\x1b[0m\n", want: "the check pr-closes-issue failed: [31mred[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
				checker.Script("crew/issue-1-development", fake.CheckScript{Print: tt.printed, Exit: 1})

				got, reason := checkedRun(t, tr, checkedConfig(t, tr, checker))

				if len(got) != 1 || reason != tt.want {
					t.Fatalf("failures = %+v, reason %q, want reason %q", got, reason, tt.want)
				}
			})
		})
	}
}

func TestAE4ACheckThatNeverEndsRunsOutOfTimeAfterTenMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.Script("crew/issue-1-development", fake.CheckScript{Block: true})
		r := start(t, checkedConfig(t, tr, checker))
		r.sessions(1)["issue-1-development"].End(port.Verdict{Succeeded: true, Reason: "done"})
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
		if got, want := r.lastReason(), "the check pr-closes-issue ran out of time after 10m0s"; got != want {
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

		got, reason := checkedRun(t, tr, checkedConfig(t, tr, checker))

		if len(got) != 1 || reason != "crew stopped" {
			t.Fatalf("failures = %+v, reason %q, want development stopped", got, reason)
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

		got, reason := checkedRun(t, tr, cfg)

		want := "the check pr-closes-issue could not start: " +
			"chdir ./.crew/worktrees/issue-1-development: no such file or directory"
		if len(got) != 1 || reason != want {
			t.Fatalf("failures = %+v, reason %q, want reason %q", got, reason, want)
		}
	})
}

func TestACheckWhoseLogCannotOpenSaysWhyWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		cfg := checkedConfig(t, tr, checker)
		r := start(t, cfg)
		session := r.sessions(1)["issue-1-development"]
		// A directory where the log was: the session keeps the file it
		// opened, and the check cannot open the log again.
		log := filepath.Join(cfg.Root, ".crew", "logs", "issue-1-development.log")
		if err := os.Remove(log); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(log, 0o700); err != nil {
			t.Fatal(err)
		}
		session.End(port.Verdict{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := "the check pr-closes-issue could not start: open the session log: " +
			"open for appending: open ./.crew/logs/issue-1-development.log: is a directory"
		if got := r.lastReason(); got != want {
			t.Fatalf("reason = %q, want %q", got, want)
		}
	})
}

func TestAnEngineWithoutACheckerFailsAnActionWithACheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, checkedDevelop)

		got, reason := checkedRun(t, tr, cfg)

		if len(got) != 1 || reason != "the check pr-closes-issue could not start: crew has no check runner" {
			t.Fatalf("failures = %+v, reason %q", got, reason)
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
		r.sessions(1)["issue-1-development"].End(port.Verdict{Succeeded: true, Reason: "done"})

		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got, want := time.Since(t0), 10*time.Minute; got != want {
			t.Errorf("Run returned after %v, want %v, once the check ran out of time", got, want)
		}
		reports, want := tr.Reports(), "the check pr-closes-issue ran out of time after 10m0s"
		if got := r.lastReason(); len(reports) != 1 || got != want {
			t.Errorf("reports = %+v, reason %q, want the check's own end, not a stop", reports, got)
		}
	})
}

// twoCheckedDevelop is develop with two checks on its action: judge, then
// pr-closes-issue.
var twoCheckedDevelop = crew.Rule{
	Name: develop.Name, Labels: develop.Labels,
	Actions: []crew.Action{{Name: "development", Prompt: develop.Actions[0].Prompt, Checks: []crew.Check{
		{Name: "judge", Script: "./judge"}, {Name: "pr-closes-issue", Script: "gh pr list"},
	}}},
}

// R1, R6: each check gets the action's name, its session's prompt and last
// message, and a passing check's reason is its last line.
func TestEachCheckReadsThePromptAndTheLastMessageAndAPassSaysItsLastLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker, h := fake.NewReportingTracker(issue(1, ready)), fake.NewChecker(), fake.NewMessagingHarness()
		checker.ScriptCheck("crew/issue-1-development", "judge", fake.CheckScript{Print: "asking Jev\ndone (0.97)\n"})
		cfg := config(t, tr, twoCheckedDevelop)
		cfg.Checker, cfg.Harnesses = checker, harnesses(h)
		r := start(t, cfg)
		s := r.sessions(1)["issue-1-development"]
		s.SetLastMessage("PR #2 is open.\nMerging is yours.")
		s.End(port.Verdict{Succeeded: true, Reason: "PR #2 is open. Merging is yours."})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		wantInputs(t, checker.Checks(), s.Run().Prompt, "PR #2 is open.\nMerging is yours.")
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
		wantChecks := []crew.CheckResult{
			{Name: "judge", Passed: true, Reason: crew.NewCheckReason("the check judge passed: done (0.97)")},
			{Name: "pr-closes-issue", Passed: true, Reason: crew.NewCheckReason("the check pr-closes-issue passed")},
		}
		if got := lastStatus(t, tr).Actions[0].Checks; !reflect.DeepEqual(got, wantChecks) {
			t.Errorf("status checks = %+v, want %+v", got, wantChecks)
		}
		wantLog(t, cfg.Root, "\ncrew: running the check judge: ./judge\nasking Jev\ndone (0.97)\n"+
			"\ncrew: running the check pr-closes-issue: gh pr list\n")
	})
}

// wantInputs fails unless checks are judge then pr-closes-issue, each run
// for development with prompt and last.
func wantInputs(t *testing.T, checks []port.Check, prompt, last string) {
	t.Helper()
	if len(checks) != 2 || checks[0].Name != "judge" || checks[1].Name != "pr-closes-issue" {
		t.Fatalf("checks = %+v, want judge then pr-closes-issue", checks)
	}
	for _, c := range checks {
		if c.Action != "development" || c.Prompt != prompt || c.LastMessage != last {
			t.Errorf("check %s got action %q, prompt %q, last message %q", c.Name, c.Action, c.Prompt, c.LastMessage)
		}
	}
}

// wantLog fails unless issue 1's development log under root is want.
func wantLog(t *testing.T, root, want string) {
	t.Helper()
	log, err := os.ReadFile(filepath.Join(root, ".crew", "logs", "issue-1-development.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(log) != want {
		t.Errorf("log = %q, want %q", log, want)
	}
}

// KTD10: the last message a check reads is the session's, byte for byte:
// crew neither scrubs nor strips it, as it does the text it shows.
func TestACheckReadsTheLastMessageAsTheSessionWroteIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker, h := fake.NewTracker(issue(1, ready)), fake.NewChecker(), fake.NewMessagingHarness()
		cfg := checkedConfig(t, tr, checker)
		cfg.Harnesses = harnesses(h)
		last := "a\x00b \x1b[31mred\x1b[0m 10%\r20% in " + cfg.Root + "/main.go"
		r := start(t, cfg)
		s := r.sessions(1)["issue-1-development"]
		s.SetLastMessage(last)
		s.End(port.Verdict{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		checks := checker.Checks()
		if len(checks) != 1 || checks[0].LastMessage != last {
			t.Fatalf("checks = %+v, want one reading the last message %q", checks, last)
		}
	})
}

// R5: each check has its own ten minutes, not what the one before it left.
func TestEachCheckRunsOutOfTimeOnItsOwnLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, checker := fake.NewTracker(issue(1, ready)), fake.NewChecker()
		checker.ScriptCheck("crew/issue-1-development", "judge", fake.CheckScript{Delay: 9 * time.Minute})
		checker.ScriptCheck("crew/issue-1-development", "pr-closes-issue", fake.CheckScript{Block: true})
		cfg := config(t, tr, twoCheckedDevelop)
		cfg.Checker = checker
		r := start(t, cfg)
		r.sessions(1)["issue-1-development"].End(port.Verdict{Succeeded: true, Reason: "done"})
		synctest.Wait()
		t0 := time.Now()

		time.Sleep(10*time.Minute + time.Second)
		synctest.Wait()
		if reports := tr.Reports(); len(reports) != 0 {
			t.Fatalf("reports after 10 minutes = %+v, want pr-closes-issue still running", reports)
		}
		time.Sleep(9 * time.Minute)
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		reports, want := tr.Reports(), "the check pr-closes-issue ran out of time after 10m0s"
		if got := r.lastReason(); len(reports) != 1 || got != want {
			t.Fatalf("reports = %+v, reason %q, want pr-closes-issue out of time", reports, got)
		}
		if took := time.Since(t0); took > 20*time.Minute {
			t.Errorf("judged after %v", took)
		}
	})
}
