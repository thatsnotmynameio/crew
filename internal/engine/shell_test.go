package engine_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// branch is the branch of #1's run of the implement rule.
const branch = "crew/issue-1-implement"

// checkedDevelop is develop whose session the shell action pr-closes-issue
// follows.
var checkedDevelop = crew.Rule{
	Name: develop.Name, Labels: develop.Labels,
	Actions: []crew.Action{develop.Actions[0], shellAction("pr-closes-issue", "gh pr list")},
	Routes:  develop.Routes,
}

// checkedConfig is config for checkedDevelop, with sh as its shell.
func checkedConfig(t *testing.T, tr port.Tracker, sh *fake.Shell) engine.Config {
	t.Helper()
	cfg := config(t, tr, checkedDevelop)
	cfg.Shell = sh
	return cfg
}

// checkedRun runs issue 1 under cfg, ends its session with success, waits
// until every goroutine is blocked, then stops the engine unless it already
// stopped, and returns the tracker's failure reports and the reason the
// run's last action ended with.
func checkedRun(t *testing.T, tr *fake.Tracker, cfg engine.Config) ([]crew.FailureReport, string) {
	t.Helper()
	r := start(t, cfg)
	r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return tr.Reports(), r.lastReason()
}

// R8, R24: a shell action runs in the run's workspace, on its branch, and
// passes when it exits 0, with its last line.
func TestAShellActionThatExitsZeroPasses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.Script(branch, fake.ShellScript{Print: "#2 closes #1\n"})
		cfg := checkedConfig(t, tr, sh)

		reports, reason := checkedRun(t, tr, cfg)

		if len(reports) != 0 || reason != "the shell action pr-closes-issue exited with status 0: #2 closes #1" {
			t.Fatalf("reports = %+v, reason %q, want none and the script's last line", reports, reason)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
		scripts := sh.Runs()
		if len(scripts) != 1 {
			t.Fatalf("scripts = %+v, want one", scripts)
		}
		c := scripts[0]
		dir := filepath.Join(cfg.Root, ".crew", "worktrees", "issue-1-implement")
		if c.Name != "pr-closes-issue" || c.Command != "gh pr list" || c.Dir != dir || c.Branch != branch ||
			c.IssueRef != "#1" || c.IssueID != issueID("1") || c.IssueURL != "https://example.test/issues/1" {
			t.Errorf("script = %+v", c)
		}
	})
}

// R8, R49, KTD6: a shell action that fails ends the run through its failed
// route with its last line, and writes into the run's log after the
// session's output and a marker line naming it.
func TestAShellActionThatFailsFailsTheRunWithItsLastLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.Script(branch, fake.ShellScript{
			Print: "looking for a pull request\nno open pull request from crew/issue-1-implement\n\n", Exit: 1,
		})
		cfg := checkedConfig(t, tr, sh)
		r := start(t, cfg)
		s := r.session()
		if _, err := s.Run().Output.Write([]byte("session output\n")); err != nil {
			t.Fatal(err)
		}
		s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := "the shell action pr-closes-issue exited with status 1: no open pull request from crew/issue-1-implement"
		if got := r.lastReason(); got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) || len(tr.Reports()) != 1 {
			t.Errorf("#1 is in %v with reports %+v, want needs attention and one report", got, tr.Reports())
		}
		checkRunLog(t, cfg.Root, "issue-1-implement", "development", "session output\n"+
			"\ncrew: running the shell action pr-closes-issue: gh pr list\n"+
			"looking for a pull request\nno open pull request from crew/issue-1-implement\n\n")
	})
}

// R8: any status but 0 fails the action, -1, a signal crew did not send,
// too.
func TestAShellActionThatExitsAnyNonZeroStatusFails(t *testing.T) {
	for status, said := range map[int]string{3: "exited with status 3", -1: "was killed by a signal"} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
				sh.Script(branch, fake.ShellScript{Print: "no open pull request\n", Exit: status})

				reports, reason := checkedRun(t, tr, checkedConfig(t, tr, sh))

				if want := "the shell action pr-closes-issue " + said + ": no open pull request"; len(reports) != 1 ||
					reason != want {
					t.Fatalf("reports = %+v, reason %q, want reason %q", reports, reason, want)
				}
			})
		})
	}
}

func TestAShellActionThatPrintsNothingSaysOnlyHowItEnded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.Script(branch, fake.ShellScript{Print: "\n  \n", Exit: 1})

		reports, reason := checkedRun(t, tr, checkedConfig(t, tr, sh))

		if len(reports) != 1 || reason != "the shell action pr-closes-issue exited with status 1" {
			t.Fatalf("reports = %+v, reason %q", reports, reason)
		}
	})
}

func TestAShellActionsReasonCarriesNoControlBytes(t *testing.T) {
	tests := []struct {
		name, printed, want string
	}{
		// A NUL in the reason would make every status write fail.
		{name: "nul", printed: "a\x00b\n", want: "ab"},
		// A carriage return ends a line, as progress output uses it.
		{name: "carriage return", printed: "progress 10%\rno open pull request\r\n", want: "no open pull request"},
		{name: "escape", printed: "\x1b[31mred\x1b[0m\n", want: "[31mred[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
				sh.Script(branch, fake.ShellScript{Print: tt.printed, Exit: 1})

				_, reason := checkedRun(t, tr, checkedConfig(t, tr, sh))

				if want := "the shell action pr-closes-issue exited with status 1: " + tt.want; reason != want {
					t.Fatalf("reason %q, want %q", reason, want)
				}
			})
		})
	}
}

func TestAShellActionThatNeverEndsRunsOutOfTimeAfterTenMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.Script(branch, fake.ShellScript{Block: true})
		r := start(t, checkedConfig(t, tr, sh))
		r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		t0 := time.Now()

		time.Sleep(10*time.Minute + time.Second)
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if reports := tr.Reports(); len(reports) != 1 {
			t.Fatalf("reports = %+v, want one", reports)
		}
		if got, want := r.lastReason(), "the shell action pr-closes-issue ran out of time after 10m0s"; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
		if took := time.Since(t0); took > 11*time.Minute {
			t.Errorf("ended after %v", took)
		}
	})
}

func TestAStopEndsARunningShellAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.Script(branch, fake.ShellScript{Block: true})

		reports, reason := checkedRun(t, tr, checkedConfig(t, tr, sh))

		if len(reports) != 1 || reason != "the shell action pr-closes-issue was stopped" {
			t.Fatalf("reports = %+v, reason %q, want pr-closes-issue stopped", reports, reason)
		}
	})
}

func TestAShellActionThatCannotStartSaysWhyWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		cfg := checkedConfig(t, tr, sh)
		sh.Script(branch, fake.ShellScript{
			StartErr: errors.New("chdir " + cfg.Root + "/.crew/worktrees/issue-1-implement: no such file or directory"),
		})

		reports, reason := checkedRun(t, tr, cfg)

		want := "the shell action pr-closes-issue could not start: " +
			"chdir ./.crew/worktrees/issue-1-implement: no such file or directory"
		if len(reports) != 1 || reason != want {
			t.Fatalf("reports = %+v, reason %q, want reason %q", reports, reason, want)
		}
	})
}

func TestAShellActionWhoseLogCannotOpenSaysWhyWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		cfg := checkedConfig(t, tr, sh)
		r := start(t, cfg)
		session := r.session()
		// A directory where the log was: the session keeps the file it
		// opened, and the script cannot open the log again.
		log := filepath.Join(cfg.Root, ".crew", "logs", "issue-1-implement.log")
		if err := os.Remove(log); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(log, 0o700); err != nil {
			t.Fatal(err)
		}
		session.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := "the shell action pr-closes-issue could not start: open the session log: " +
			"open for appending: open ./.crew/logs/issue-1-implement.log: is a directory"
		if got := r.lastReason(); got != want {
			t.Fatalf("reason = %q, want %q", got, want)
		}
	})
}

func TestAnEngineWithoutAShellFailsAShellAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))

		reports, reason := checkedRun(t, tr, config(t, tr, checkedDevelop))

		if len(reports) != 1 || reason != "the shell action pr-closes-issue could not start: crew has no shell to run it" {
			t.Fatalf("reports = %+v, reason %q", reports, reason)
		}
	})
}

func TestWhenTheRunTimeIsUpARunningShellActionFinishesBeforeTheEngineStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.Script(branch, fake.ShellScript{Block: true})
		cfg := checkedConfig(t, tr, sh)
		cfg.RunTimeLimit = 5 * time.Minute
		t0 := time.Now()
		r := start(t, cfg)
		r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})

		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got, want := time.Since(t0), 10*time.Minute; got != want {
			t.Errorf("Run returned after %v, want %v, once the script ran out of time", got, want)
		}
		reports, want := tr.Reports(), "the shell action pr-closes-issue ran out of time after 10m0s"
		if got := r.lastReason(); len(reports) != 1 || got != want {
			t.Errorf("reports = %+v, reason %q, want the script's own end, not a stop", reports, got)
		}
	})
}

// twoCheckedDevelop is develop whose session the shell actions judge, then
// pr-closes-issue, follow.
var twoCheckedDevelop = crew.Rule{
	Name: develop.Name, Labels: develop.Labels,
	Actions: []crew.Action{
		develop.Actions[0], shellAction("judge", "./judge"), shellAction("pr-closes-issue", "gh pr list"),
	},
	Routes: develop.Routes,
}

// KTD-S12, KTD-S13: each shell action gets the latest session's name, its
// prompt and its last message, kept beside the run's log.
func TestEachShellActionReadsTheLatestSessionsPromptAndLastMessage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh, h := fake.NewTracker(issue(1, ready)), fake.NewShell(), fake.NewMessagingHarness()
		sh.ScriptAction(branch, "judge", fake.ShellScript{Print: "asking Jev\ndone (0.97)\n"})
		cfg := config(t, tr, twoCheckedDevelop)
		cfg.Shell, cfg.Harnesses = sh, harnesses(h)
		r := start(t, cfg)
		s := r.session()
		s.SetLastMessage("PR #2 is open.\nMerging is yours.")
		s.End(port.SessionEnd{Succeeded: true, Reason: "PR #2 is open. Merging is yours."})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		wantInputs(t, sh.Runs(), s.Run().Prompt, "PR #2 is open.\nMerging is yours.")
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
		checkKept(t, cfg.Root, s.Run().Prompt, "PR #2 is open.\nMerging is yours.")
	})
}

// wantInputs fails unless scripts are judge then pr-closes-issue, each run
// after development with prompt and last.
func wantInputs(t *testing.T, scripts []port.Script, prompt, last string) {
	t.Helper()
	if len(scripts) != 2 || scripts[0].Name != "judge" || scripts[1].Name != "pr-closes-issue" {
		t.Fatalf("scripts = %+v, want judge then pr-closes-issue", scripts)
	}
	for _, c := range scripts {
		if c.Session != "development" || c.Prompt != prompt || c.LastMessage != last {
			t.Errorf("script %s got session %q, prompt %q, last message %q", c.Name, c.Session, c.Prompt, c.LastMessage)
		}
	}
}

// checkKept fails unless the files kept beside #1's run's log under root
// hold prompt and last.
func checkKept(t *testing.T, root, prompt, last string) {
	t.Helper()
	for name, want := range map[string]string{"prompt": prompt, "last-message": last} {
		got, err := os.ReadFile(filepath.Join(root, ".crew", "logs", "issue-1-implement."+name))
		if err != nil || string(got) != want {
			t.Errorf("kept %s = %q, %v, want %q", name, got, err, want)
		}
	}
}

// KTD10: the last message a script reads is the session's, byte for byte:
// crew neither scrubs nor strips it, as it does the text it shows.
func TestAShellActionReadsTheLastMessageAsTheSessionWroteIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh, h := fake.NewTracker(issue(1, ready)), fake.NewShell(), fake.NewMessagingHarness()
		cfg := checkedConfig(t, tr, sh)
		cfg.Harnesses = harnesses(h)
		last := "a\x00b \x1b[31mred\x1b[0m 10%\r20% in " + cfg.Root + "/main.go"
		r := start(t, cfg)
		s := r.session()
		s.SetLastMessage(last)
		s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		scripts := sh.Runs()
		if len(scripts) != 1 || scripts[0].LastMessage != last {
			t.Fatalf("scripts = %+v, want one reading the last message %q", scripts, last)
		}
	})
}

// Each shell action has its own ten minutes, not what the one before it
// left.
func TestEachShellActionRunsOutOfTimeOnItsOwnLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.ScriptAction(branch, "judge", fake.ShellScript{Delay: 9 * time.Minute})
		sh.ScriptAction(branch, "pr-closes-issue", fake.ShellScript{Block: true})
		cfg := config(t, tr, twoCheckedDevelop)
		cfg.Shell = sh
		r := start(t, cfg)
		r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})
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
		reports, want := tr.Reports(), "the shell action pr-closes-issue ran out of time after 10m0s"
		if got := r.lastReason(); len(reports) != 1 || got != want {
			t.Fatalf("reports = %+v, reason %q, want pr-closes-issue out of time", reports, got)
		}
		if took := time.Since(t0); took > 20*time.Minute {
			t.Errorf("ended after %v", took)
		}
	})
}

// gatedDevelop is develop whose session the shell action judge precedes.
var gatedDevelop = crew.Rule{
	Name: develop.Name, Labels: develop.Labels,
	Actions: []crew.Action{shellAction("judge", "./judge"), develop.Actions[0]},
	Routes:  develop.Routes,
}

// KTD-S11, KTD-S12: a shell action before any session of a fresh run gets
// no session's name, prompt or last message, even when a gone run's
// session left words beside a log of the same name.
func TestAShellActionBeforeAnySessionGetsNoSessionsWords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		cfg := config(t, tr, gatedDevelop)
		cfg.Shell = sh
		keep(t, cfg.Root, "an earlier prompt", "an earlier message")
		r := start(t, cfg)
		r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		scripts := sh.Runs()
		if len(scripts) != 1 || scripts[0].Session != "" || scripts[0].Prompt != "" || scripts[0].LastMessage != "" {
			t.Fatalf("scripts = %+v, want judge without a session's name or words", scripts)
		}
	})
}

// keep writes prompt and last beside the log of #1's run of implement under
// root, as a session of an earlier run would have.
func keep(t *testing.T, root, prompt, last string) {
	t.Helper()
	logs := filepath.Join(root, ".crew", "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"prompt": prompt, "last-message": last} {
		if err := os.WriteFile(filepath.Join(logs, "issue-1-implement."+name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// KTD22: a fresh run that creates its workspace under a name a gone one
// had clears the words its session left beside the log, so no later
// script reads them as its own.
func TestAFreshRunUnderAReusedNameClearsTheKeptWords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		keep(t, cfg.Root, "an earlier prompt", "an earlier message")
		r := start(t, cfg)
		s := r.session()

		for _, name := range []string{"prompt", "last-message"} {
			path := filepath.Join(cfg.Root, ".crew", "logs", "issue-1-implement."+name)
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s while the new run's session runs: %v, want it gone", path, err)
			}
		}
		s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		checkKept(t, cfg.Root, "Implement development for issue #1", "")
	})
}

// Covers AE11, KTD-S7: a shell action crew stopped while it ran resumes
// at itself after a restart, and still reads the last message of the
// session that ran in the earlier run.
func TestAE11AResumedShellActionReadsTheEarlierRunsLastMessageAfterARestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, h := fake.NewTracker(issue(1, ready)), fake.NewMessagingHarness()
		blocking := fake.NewShell()
		blocking.Script(branch, fake.ShellScript{Block: true})
		cfg := checkedConfig(t, tr, blocking)
		cfg.Harnesses = harnesses(h)
		first := start(t, cfg)
		s := first.session()
		s.SetLastMessage("PR #2 is open.")
		s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		first.engine.Stop()
		if _, err := first.wait(); err != nil {
			t.Fatalf("first Run: %v", err)
		}

		tr.SetStates("1", ready)
		restarted, sh := cfg, fake.NewShell()
		journal, _ := cfg.Journal.(*fake.Journal)
		restarted.Journal, restarted.Shell = fake.NewJournal(journal.Appended()...), sh
		second := start(t, restarted)
		synctest.Wait()
		second.engine.Stop()
		if _, err := second.wait(); err != nil {
			t.Fatalf("second Run: %v", err)
		}

		if n := len(h.Sessions()); n != 1 {
			t.Errorf("%d sessions started, want the first run's alone", n)
		}
		scripts := sh.Runs()
		if len(scripts) != 1 || scripts[0].Session != "development" || scripts[0].LastMessage != "PR #2 is open." ||
			!strings.HasPrefix(scripts[0].Prompt, "Implement development for issue #1") {
			t.Fatalf("scripts after the restart = %+v, want pr-closes-issue reading development's words", scripts)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
	})
}
