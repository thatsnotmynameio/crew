package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// stopGrace is how long a first stop gives each running session, in the
	// README's "Stopping crew": "giving it up to ten seconds".
	stopGrace = 10 * time.Second

	// pressGap is how long the second-stop scenario leaves between its two
	// presses: well inside stopGrace, so the first stop is still waiting.
	pressGap = 2 * time.Second

	// forceWithin bounds how long crew may take to exit after a second stop,
	// which "exits at once". pressGap plus forceWithin stays under stopGrace,
	// so a crew that ignored the second press and ended only when the first
	// stop's grace ran out still fails.
	forceWithin = 5 * time.Second

	// goneWithin bounds how long a process crew started may outlive crew after
	// a second stop, which "kills every process crew started".
	goneWithin = 5 * time.Second

	// limitPassed is how long the run time limit scenario keeps its session
	// going once it started: three times its limit of one second.
	limitPassed = 3 * time.Second

	// pollEvery is how often a wait of this file's own checks its condition.
	pollEvery = 50 * time.Millisecond
)

// The exit codes of a scripted Claude Code session.
const (
	claudeSucceeded = 0
	claudeFailed    = 1
)

// The labels of sessionRules's rule.
const (
	sessionReady   = "cli:ready"
	sessionRunning = "cli:running"
	sessionDone    = "cli:done"
	sessionFailed  = "cli:failed"
)

// The titles of the issues the stopping scenarios hold.
const (
	firstTitle  = "Add a search box"
	secondTitle = "Add a footer"
)

// sessionRules is the end of a valid config whose only rule has one action,
// implement, a Claude Code session whose prompt names the issue's title.
// Scenarios put their own settings before it.
const sessionRules = `poll_interval_seconds: 1
agents:
  developer:
    harness:
      name: claude
rules:
  development:
    labels:
      ready: cli:ready
      running: cli:running
      success: cli:done
      failure: cli:failed
    actions:
      implement:
        prompt: |-
          Implement "{{.Issue.Title}}".
`

// prompt is a phrase of the prompt crew gives the session of the issue title.
func prompt(title string) string {
	return `Implement "` + title + `".`
}

// TestCLIInterrupt checks that Ctrl+C ends an idle run cleanly.
//
// README: "Ctrl+C ... stops crew", and crew "exits 0 after a stop". crew,
// idle after moving an issue, exits 0 on SIGINT.
func TestCLIInterrupt(t *testing.T) {
	t.Parallel()
	sc := moveOneIssue(t, harness.Options{Args: []string{"--plain"}})
	sc.Stop()
	exited := sc.Exit(timeout)
	if exited.Code != exitClean {
		t.Errorf("crew stopped with SIGINT exited %d, want %d; stderr: %q", exited.Code, exitClean, exited.Stderr)
	}
}

// TestCLIStopSignals checks the other signals that stop crew.
//
// README: "Ctrl+C, or `q` in the live view, stops crew, as SIGINT, SIGTERM
// and SIGHUP do", and crew "exits 0 after a stop". crew, idle in the live view
// after moving an issue, exits 0 on SIGTERM and on SIGHUP. The harness sends
// a signal other than SIGINT only to a crew in a terminal.
func TestCLIStopSignals(t *testing.T) {
	t.Parallel()
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			sc := moveOneIssue(t, harness.Options{Screen: true})
			sc.Screen().Signal(t, sig)
			exited := sc.Exit(timeout)
			if exited.Code != exitClean {
				t.Errorf("crew stopped with %s exited %d, want %d", sig, exited.Code, exitClean)
			}
		})
	}
}

// TestCLIInterruptStopsSessions checks what a first Ctrl+C does to a run that
// holds an issue.
//
// README: "crew takes nothing new and asks each running session to stop,
// giving it up to ten seconds. An action whose session crew stopped fails, so
// its issue moves to the rule's failure label like any failed action. crew
// exits once it has judged every issue it held", and it "exits 0 after a
// stop". With one slot, crew holds one issue while a second waits on the ready
// label. On SIGINT, crew asks the held session to stop, which it does at once;
// crew moves the held issue to the failure label, never starts the waiting
// one, and exits 0, well before the ten seconds a session that did not stop
// would get.
func TestCLIInterruptStopsSessions(t *testing.T) {
	t.Parallel()
	sc := harness.New(t, harness.Options{Config: "max_parallel_issues: 1\n" + sessionRules, Args: []string{"--plain"}})
	ss := newSessions(t)
	issues := addIssues(sc, firstTitle, secondTitle)
	for title := range issues {
		sc.Claude.Script(prompt(title), ss.held(title))
	}
	sc.Start()
	held, waiting := ss.waitHeld(sc, issues)
	sc.Stop()
	exited := sc.Exit(stopGrace)
	if exited.Code != exitClean {
		t.Errorf("crew stopped with SIGINT while it held an issue exited %d, want %d; stderr: %q",
			exited.Code, exitClean, exited.Stderr)
	}
	if len(ss.stopped) != 1 {
		t.Errorf("crew did not ask the running session of %q to stop", held)
	}
	wantOnly(t, sc, issues[held], sessionFailed, "the issue whose session crew stopped")
	wantOnly(t, sc, issues[waiting], sessionReady, "the issue waiting for a slot when crew was stopped")
	if len(ss.started) != 0 {
		t.Errorf("crew started the session of %q after it was stopped", waiting)
	}
}

// TestCLIRunTimeLimitLetsSessionsEnd checks what the run time limit does to a
// run that holds an issue, as against a stop.
//
// README: "`run_time_limit_seconds` ends a run another way: crew winds down,
// taking nothing new while its running sessions end on their own", and crew
// "exits 0 ... at its run time limit". With a limit of one second and one
// slot, crew holds one issue while a second waits on the ready label. The held
// session runs past the limit, then succeeds: crew never asked it to stop,
// moves its issue to the success label, never starts the waiting one, and
// exits 0.
func TestCLIRunTimeLimitLetsSessionsEnd(t *testing.T) {
	t.Parallel()
	config := "run_time_limit_seconds: 1\nmax_parallel_issues: 1\n" + sessionRules
	sc := harness.New(t, harness.Options{Config: config, Args: []string{"--plain"}})
	ss := newSessions(t)
	issues := addIssues(sc, firstTitle, secondTitle)
	for title := range issues {
		sc.Claude.Script(prompt(title), ss.held(title))
	}
	sc.Start()
	held, waiting := ss.waitHeld(sc, issues)
	time.Sleep(limitPassed)
	close(ss.release)
	exited := sc.Exit(timeout)
	if exited.Code != exitClean {
		t.Errorf("crew at its run time limit exited %d, want %d; stderr: %q", exited.Code, exitClean, exited.Stderr)
	}
	if len(ss.stopped) != 0 {
		t.Errorf("crew asked the session of %q to stop at the run time limit, instead of letting it end", held)
	}
	wantOnly(t, sc, issues[held], sessionDone, "the issue whose session succeeded after the run time limit")
	wantOnly(t, sc, issues[waiting], sessionReady, "the issue waiting for a slot at the run time limit")
	if len(ss.started) != 0 {
		t.Errorf("crew started the session of %q after its run time limit", waiting)
	}
}

// TestCLIInterruptGivesSessionsTime checks the time a first Ctrl+C gives a
// session that does not stop at once.
//
// README: on a stop, crew "asks each running session to stop, giving it up to
// ten seconds", and "crew exits once it has judged every issue it held",
// exiting "0 after a stop". crew holds one issue whose session keeps working
// when asked to stop. Two seconds after a SIGINT, inside those ten seconds,
// the issue still carries the running label: crew has not judged it, so it
// has not exited. The session then ends, and crew exits 0 within the ten
// seconds.
func TestCLIInterruptGivesSessionsTime(t *testing.T) {
	t.Parallel()
	sc := harness.New(t, harness.Options{Config: sessionRules, Args: []string{"--plain"}})
	ss := newSessions(t)
	issues := addIssues(sc, firstTitle)
	sc.Claude.Script(prompt(firstTitle), ss.slow(firstTitle))
	sc.Start()
	ss.waitHeld(sc, issues)
	sc.Stop()
	time.Sleep(pressGap)
	if !isRunning(sc, issues[firstTitle]) {
		issue, _ := sc.GitHub.Issue(issues[firstTitle])
		t.Errorf("%s after a Ctrl+C, the issue whose session still worked carries %q, want %s: "+
			"crew did not give the session its ten seconds", pressGap, issue.Labels, sessionRunning)
	}
	close(ss.release)
	exited := sc.Exit(stopGrace)
	if exited.Code != exitClean {
		t.Errorf("crew stopped with SIGINT exited %d, want %d; stderr: %q", exited.Code, exitClean, exited.Stderr)
	}
}

// TestCLIInterruptEndsSlowSession checks the end of the time a first Ctrl+C
// gives a session that does not stop.
//
// README: on a stop, crew "asks each running session to stop, giving it up to
// ten seconds. An action whose session crew stopped fails, so its issue moves
// to the rule's failure label like any failed action. crew exits once it has
// judged every issue it held", exiting "0 after a stop". crew holds one issue
// whose session ignores the request to stop and never ends by itself. After a
// SIGINT, crew waits no more than the ten seconds: it exits 0 within five
// seconds of them, the issue carries the failure label, and no forcing second
// stop was needed.
//
// Edge case: the session's process is gone within five seconds of crew's exit.
// "Up to ten seconds" means crew stops waiting then; a session it left running
// would go on working in the repository after crew exited, unseen.
func TestCLIInterruptEndsSlowSession(t *testing.T) {
	t.Parallel()
	sc := harness.New(t, harness.Options{Config: sessionRules, Args: []string{"--plain"}})
	ss := newSessions(t)
	issues := addIssues(sc, firstTitle)
	sc.Claude.Script(prompt(firstTitle), ss.slow(firstTitle))
	sc.Start()
	ss.waitHeld(sc, issues)
	sc.Stop()
	exited := sc.Exit(stopGrace + forceWithin)
	if exited.Code != exitClean {
		t.Errorf("crew stopped with SIGINT while a session would not stop exited %d, want %d; stderr: %q",
			exited.Code, exitClean, exited.Stderr)
	}
	wantOnly(t, sc, issues[firstTitle], sessionFailed, "the issue whose session would not stop")
	if !ended(ss.killed, goneWithin) {
		t.Errorf("the session of %q, which would not stop, still ran %s after crew exited", firstTitle, goneWithin)
	}
}

// TestCLISecondInterruptKillsSession checks what a second Ctrl+C does to a
// session that does not stop.
//
// README: "A second Ctrl+C, `q` or signal does not wait: it kills every
// process crew started and exits at once", and crew exits "1 when ... a
// second stop forced its exit". crew holds one issue whose session, a process
// crew started, ignores the request to stop. Two seconds after the first
// SIGINT, inside its ten seconds, the issue still carries the running label. A
// second SIGINT then makes crew exit 1 within five seconds, before the first
// stop's ten seconds end, and the session's process is gone.
func TestCLISecondInterruptKillsSession(t *testing.T) {
	t.Parallel()
	sc := harness.New(t, harness.Options{Config: sessionRules, Args: []string{"--plain"}})
	ss := newSessions(t)
	issues := addIssues(sc, firstTitle)
	sc.Claude.Script(prompt(firstTitle), ss.slow(firstTitle))
	sc.Start()
	ss.waitHeld(sc, issues)
	sc.Stop()
	time.Sleep(pressGap)
	if !isRunning(sc, issues[firstTitle]) {
		t.Fatalf("%s after the first Ctrl+C, the issue whose session still worked left %s: "+
			"the first stop did not wait for it, so a second has nothing to force", pressGap, sessionRunning)
	}
	sc.Stop()
	exited := sc.Exit(forceWithin)
	if exited.Code != exitFailed {
		t.Errorf("crew forced to exit by a second Ctrl+C exited %d, want %d; stderr: %q",
			exited.Code, exitFailed, exited.Stderr)
	}
	if !ended(ss.killed, goneWithin) {
		t.Errorf("the session of %q still ran %s after a second Ctrl+C forced crew's exit", firstTitle, goneWithin)
	}
}

// ended waits up to within for a title on killed, and reports whether one
// came.
func ended(killed <-chan string, within time.Duration) bool {
	select {
	case <-killed:
		return true
	case <-time.After(within):
		return false
	}
}

// TestCLISecondInterrupt checks what a second Ctrl+C adds to the first.
//
// README: "A second Ctrl+C, `q` or signal does not wait: it kills every
// process crew started and exits at once", and crew exits "1 when ... a
// second stop forced its exit", where a first stop exits 0
// (TestCLIInterruptStopsSessions). crew holds one issue whose session
// succeeded and whose check, a process crew started, ignores SIGINT, SIGTERM
// and SIGHUP, so only a kill ends it. A second SIGINT, two seconds after the
// first, makes crew exit 1 within five seconds, before the first stop's ten
// seconds end, and the check's process is gone.
//
// Edge case: two seconds after the first SIGINT, crew still holds the issue,
// which still carries the running label. The README's Checks says a check that
// "is stopped fails the action", but not how long a stop waits for one: a
// first stop that "does not wait" only on the second press waits for a
// running check as for a running session.
func TestCLISecondInterrupt(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "check.pid")
	sc := harness.New(t, harness.Options{Config: checkedConfig(pidFile), Args: []string{"--plain"}})
	issues := addIssues(sc, firstTitle)
	sc.Claude.Script(prompt(firstTitle), fakeclaude.Succeed("Done."))
	sc.Start()
	pid := 0
	sc.Wait(func() bool {
		pid = readPID(pidFile)
		return pid > 0 && isRunning(sc, issues[firstTitle])
	}, timeout)
	t.Cleanup(func() { killLeft(pid) })
	sc.Stop()
	time.Sleep(pressGap)
	if !isRunning(sc, issues[firstTitle]) {
		t.Fatalf("%s after the first Ctrl+C, the issue whose check still ran left %s: "+
			"the first stop did not wait for it, so a second has nothing to force", pressGap, sessionRunning)
	}
	sc.Stop()
	exited := sc.Exit(forceWithin)
	if exited.Code != exitFailed {
		t.Errorf("crew forced to exit by a second Ctrl+C exited %d, want %d; stderr: %q",
			exited.Code, exitFailed, exited.Stderr)
	}
	if !gone(pid, goneWithin) {
		t.Errorf("the check crew started (pid %d) still ran %s after a second Ctrl+C forced crew's exit", pid, goneWithin)
	}
}

// checkedConfig is sessionRules with a check on the action: a script that
// ignores SIGINT, SIGTERM and SIGHUP, writes its process id to pidFile and
// sleeps far longer than any scenario runs.
func checkedConfig(pidFile string) string {
	check := fmt.Sprintf(`checks:
  hang: |-
    trap '' INT TERM HUP
    printf '%%s\n' "$$" > '%s'
    exec sleep 300
`, pidFile)
	return check + sessionRules + "        check: hang\n"
}

// sessions are the scripted sessions of a stopping scenario, and what they
// saw.
type sessions struct {
	// started receives the title of each session as it starts working.
	started chan string
	// stopped receives the title of each session crew asked to stop.
	stopped chan string
	// killed receives the title of each slow session crew killed.
	killed chan string
	// release, once closed, lets every held session succeed.
	release chan struct{}
	// done closes at the end of the test, ending every session still running.
	done chan struct{}
}

// newSessions returns the sessions of a scenario, with room for two issues.
// Call it after harness.New, so the sessions end before the doubles stop.
func newSessions(t *testing.T) *sessions {
	t.Helper()
	ss := &sessions{
		started: make(chan string, 2),
		stopped: make(chan string, 2),
		killed:  make(chan string, 2),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	t.Cleanup(func() { close(ss.done) })
	return ss
}

// held is the session of the issue title: it says it works, then succeeds
// when release closes, or gives up when crew asks it to stop.
func (ss *sessions) held(title string) fakeclaude.ScriptFunc {
	return func(ctx context.Context, s *fakeclaude.Session) int {
		_ = s.Emit(s.Init(), s.Said("Writing it."))
		ss.started <- title
		select {
		case <-ss.release:
			_ = s.Emit(s.Success("Done."))
			return claudeSucceeded
		case <-ctx.Done():
			ss.stopped <- title
			return claudeFailed
		case <-ss.done:
			return claudeFailed
		}
	}
}

// slow is the session of the issue title that is slow to stop: it ignores
// crew's request to stop, says it works, then succeeds only when release
// closes. Only a kill ends it before that, and it then reports itself on
// killed.
func (ss *sessions) slow(title string) fakeclaude.ScriptFunc {
	return func(ctx context.Context, s *fakeclaude.Session) int {
		s.IgnoreStop()
		_ = s.Emit(s.Init(), s.Said("Writing it."))
		ss.started <- title
		select {
		case <-ss.release:
			_ = s.Emit(s.Success("Done."))
			return claudeSucceeded
		case <-ctx.Done():
			ss.killed <- title
			return claudeFailed
		case <-ss.done:
			return claudeFailed
		}
	}
}

// waitHeld waits until one of the issues' sessions started and its issue
// carries the running label, and returns its title and the other's.
func (ss *sessions) waitHeld(sc *harness.Scenario, issues map[string]int) (string, string) {
	sc.Wait(func() bool { return len(ss.started) > 0 }, timeout)
	held := <-ss.started
	sc.Wait(func() bool { return isRunning(sc, issues[held]) }, timeout)
	waiting := ""
	for title := range issues {
		if title != held {
			waiting = title
		}
	}
	return held, waiting
}

// addIssues adds an issue on sessionRules's ready label for each title, and
// returns their numbers by title.
func addIssues(sc *harness.Scenario, titles ...string) map[string]int {
	sc.GitHub.AddLabel(sessionReady, sessionRunning, sessionDone, sessionFailed)
	issues := make(map[string]int, len(titles))
	for _, title := range titles {
		issues[title] = sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Labels: []string{sessionReady}})
	}
	return issues
}

// isRunning reports whether the issue number carries sessionRules's running
// label.
func isRunning(sc *harness.Scenario, number int) bool {
	issue, _ := sc.GitHub.Issue(number)
	return slices.Contains(issue.Labels, sessionRunning)
}

// wantOnly fails the test unless the issue number carries want and no other
// label of sessionRules's rule.
func wantOnly(t *testing.T, sc *harness.Scenario, number int, want, what string) {
	t.Helper()
	issue, _ := sc.GitHub.Issue(number)
	for _, label := range []string{sessionReady, sessionRunning, sessionDone, sessionFailed} {
		if slices.Contains(issue.Labels, label) != (label == want) {
			t.Errorf("%s carries %q, want %s and no other label of the rule", what, issue.Labels, want)
			return
		}
	}
}

// readPID returns the process id written in file, or 0 while the file is
// missing or not yet whole.
func readPID(file string) int {
	data, err := os.ReadFile(file)
	if err != nil || !strings.HasSuffix(string(data), "\n") {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// alive reports whether the process pid exists.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// gone waits up to within for the process pid to end, and reports whether it
// did.
func gone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for alive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollEvery)
	}
	return true
}

// killLeft kills the process pid when it still runs at the end of the test,
// so a check crew left behind does not outlive the suite.
func killLeft(pid int) {
	if pid > 0 && alive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
