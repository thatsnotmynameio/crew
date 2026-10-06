package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// pollEvery is how often a wait of this file's own checks its condition.
const pollEvery = 50 * time.Millisecond

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
