package cli

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// timeout bounds every wait and every exit: generous, since a wait
	// returns as soon as its condition holds.
	timeout = time.Minute

	// usageExit is the exit code of a command line the program cannot use, as
	// shells and Go's flag package give it.
	usageExit = 2

	// readyLabel and doneLabel are the labels of the rule without actions in
	// moveConfig.
	readyLabel = "cli:approved"
	doneLabel  = "cli:done"
)

// moveConfig is a valid config whose only rule has no actions: crew moves an
// issue from its ready label to its success label without a session.
const moveConfig = `poll_interval_seconds: 1
rules:
  approve:
    labels:
      ready: cli:approved
      running: cli:approving
      success: cli:done
`

// releaseVersion is a release's version, vX.Y.Z, alone on its line or after
// the program's name.
var releaseVersion = regexp.MustCompile(`^(crew )?v[0-9]+\.[0-9]+\.[0-9]+$`)

// clock is a time of day, as a timestamp on an event line shows it.
var clock = regexp.MustCompile(`[0-9]{2}:[0-9]{2}:[0-9]{2}`)

// run starts crew with args in a repository holding config, and returns how
// it ended without being stopped.
func run(t *testing.T, config string, args ...string) harness.Exited {
	t.Helper()
	sc := harness.New(t, harness.Options{Config: config, Args: args})
	sc.Start()
	return sc.Exit(timeout)
}

// moveOneIssue starts crew with args and moveConfig, waits until it moved one
// issue to doneLabel, and returns the scenario, still running.
func moveOneIssue(t *testing.T, args ...string) *harness.Scenario {
	t.Helper()
	sc := harness.New(t, harness.Options{Config: moveConfig, Args: args})
	sc.GitHub.AddLabel(readyLabel, "cli:approving", doneLabel)
	n := sc.GitHub.AddIssue(fakegithub.Issue{Title: "Approve the search box", Labels: []string{readyLabel}})
	sc.Start()
	sc.Wait(func() bool {
		issue, _ := sc.GitHub.Issue(n)
		return slices.Contains(issue.Labels, doneLabel)
	}, timeout)
	return sc
}

// TestCLIVersion checks the version flag of the release build.
//
// README: "crew --version", the last line of the quick start's install, and
// "GoReleaser builds crew and publishes it as `vX.Y.Z`" (the Release workflow
// row). The binary under test is built with the release config, so its
// --version is one line naming a version of that form, on standard output,
// with nothing on standard error, and it exits 0. It needs no
// .crew/config.yaml: the install runs it outside any repository.
func TestCLIVersion(t *testing.T) {
	t.Parallel()
	exited := run(t, "", "--version")
	if exited.Code != 0 {
		t.Errorf("crew --version exited %d, want 0; stderr: %q", exited.Code, exited.Stderr)
	}
	if exited.Stderr != "" {
		t.Errorf("crew --version wrote to standard error: %q", exited.Stderr)
	}
	lines := strings.Split(strings.TrimSuffix(exited.Stdout, "\n"), "\n")
	if len(lines) != 1 || !releaseVersion.MatchString(lines[0]) {
		t.Errorf("crew --version printed %q, want exactly one line with a version vX.Y.Z", exited.Stdout)
	}
}

// TestCLIHelp checks the help flag.
//
// Edge case: `crew --help` exits 0 and its usage names both flags, --plain
// and --version. Asking for help is not an error, and the usage is the only
// list of crew's flags a user has besides the README.
func TestCLIHelp(t *testing.T) {
	t.Parallel()
	exited := run(t, "", "--help")
	if exited.Code != 0 {
		t.Errorf("crew --help exited %d, want 0", exited.Code)
	}
	usage := exited.Stdout + exited.Stderr
	for _, flag := range []string{"plain", "version"} {
		if !strings.Contains(usage, flag) {
			t.Errorf("crew --help does not name the flag %s:\n%s", flag, usage)
		}
	}
}

// TestCLIUnknownFlag checks a flag crew does not have.
//
// Edge case: `crew --bogus` exits 2 at once and names the flag on standard
// error. A mistyped flag is a usage error: crew should refuse to start rather
// than run with a setting the user did not get, and 2 is the exit code shells
// and Go's flag package give a usage error.
func TestCLIUnknownFlag(t *testing.T) {
	t.Parallel()
	exited := run(t, moveConfig, "--bogus")
	if exited.Code != usageExit {
		t.Errorf("crew --bogus exited %d, want %d", exited.Code, usageExit)
	}
	if !strings.Contains(exited.Stderr, "bogus") {
		t.Errorf("crew --bogus does not name the flag on standard error: %q", exited.Stderr)
	}
}

// TestCLIUnexpectedArgument checks an argument crew does not take.
//
// Edge case: `crew run` exits 2 at once and says why on standard error.
// The usage, `crew [--plain] [--version]` and `crew bots create <name>`,
// takes no other argument, so a stray word is a usage error that crew should
// not ignore by starting a run.
func TestCLIUnexpectedArgument(t *testing.T) {
	t.Parallel()
	exited := run(t, moveConfig, "run")
	if exited.Code != usageExit {
		t.Errorf("crew run exited %d, want %d", exited.Code, usageExit)
	}
	if strings.TrimSpace(exited.Stderr) == "" {
		t.Error("crew run printed nothing on standard error")
	}
}

// TestCLIWithoutConfig checks a repository without .crew/config.yaml.
//
// Edge case: crew exits with a non-zero code and names .crew/config.yaml on
// standard error. The README asks the user to commit that file before running
// crew, and only rules is required in it, so without it crew has nothing to
// do and the user must learn which file is missing.
func TestCLIWithoutConfig(t *testing.T) {
	t.Parallel()
	exited := run(t, "", "--plain")
	if exited.Code == 0 {
		t.Error("crew without .crew/config.yaml exited 0, want a non-zero code")
	}
	if !strings.Contains(exited.Stderr, "config.yaml") {
		t.Errorf("crew does not name .crew/config.yaml on standard error: %q", exited.Stderr)
	}
}

// TestCLIPollIntervalZero checks the lower bound of poll_interval_seconds.
//
// README: `.crew/config.example.yaml`, which the README makes the reference of
// every key, says "Seconds between polls of the tracker. Must be positive."
// So 0 is refused: crew exits with a non-zero code and names the key on
// standard error. The other scenarios run with 1, the smallest value allowed.
func TestCLIPollIntervalZero(t *testing.T) {
	t.Parallel()
	config := strings.Replace(moveConfig, "poll_interval_seconds: 1", "poll_interval_seconds: 0", 1)
	exited := run(t, config, "--plain")
	if exited.Code == 0 {
		t.Error("crew with poll_interval_seconds: 0 exited 0, want a non-zero code")
	}
	if !strings.Contains(exited.Stderr, "poll_interval_seconds") {
		t.Errorf("crew does not name poll_interval_seconds on standard error: %q", exited.Stderr)
	}
}

// TestCLIRunTimeLimit checks that crew ends by itself at its run time limit.
//
// README: `.crew/config.example.yaml` says of run_time_limit_seconds "How long
// crew runs, counted from its first poll, before it winds down." With a limit
// of one second and nothing to do, crew exits without being stopped. Edge
// case: it exits 0, since ending at the limit the user set is the planned end
// of a run, not a failure.
func TestCLIRunTimeLimit(t *testing.T) {
	t.Parallel()
	exited := run(t, "run_time_limit_seconds: 1\n"+moveConfig, "--plain")
	if exited.Code != 0 {
		t.Errorf("crew at its run time limit exited %d, want 0; stderr: %q", exited.Code, exited.Stderr)
	}
}

// TestCLIInterrupt checks that Ctrl+C ends an idle run cleanly.
//
// Edge case: crew, idle after moving an issue, exits 0 on SIGINT (Ctrl+C).
// The config reference says that without run_time_limit_seconds "crew runs
// until you stop it", so stopping it is the normal end of a run and not a
// failure.
func TestCLIInterrupt(t *testing.T) {
	t.Parallel()
	sc := moveOneIssue(t, "--plain")
	sc.Stop()
	exited := sc.Exit(timeout)
	if exited.Code != 0 {
		t.Errorf("crew stopped with SIGINT exited %d, want 0; stderr: %q", exited.Code, exited.Stderr)
	}
}

// TestCLIPlainEventLines checks the output of --plain.
//
// README: "`--plain` prints one line per event instead", and `crew --help`
// says "print timestamped event lines instead of the TUI". So, after crew
// moved an issue, standard output holds at least one line, and each of its
// lines carries a time of day.
func TestCLIPlainEventLines(t *testing.T) {
	t.Parallel()
	sc := moveOneIssue(t, "--plain")
	sc.Stop()
	exited := sc.Exit(timeout)
	out := strings.TrimSpace(exited.Stdout)
	if out == "" {
		t.Fatal("crew --plain printed no event line after moving an issue")
	}
	for line := range strings.SplitSeq(out, "\n") {
		if !clock.MatchString(line) {
			t.Errorf("crew --plain printed a line without a timestamp: %q", line)
		}
	}
}

// forceTimeout is how long crew may take to exit after a second Ctrl+C.
const forceTimeout = 15 * time.Second

// pressGap is how long the scenario leaves crew between the first Ctrl+C and
// the end of a session, so the session ends after crew got the first press.
const pressGap = 2 * time.Second

// sessionConfig is a valid config whose only rule has one action, implement.
// The default max_parallel_issues, 2, lets it work on two issues at once.
const sessionConfig = `poll_interval_seconds: 1
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

// The titles of the second interrupt's issues: crew must wait for the first,
// whose session never ends, and finishes the second, whose session ends after
// the first Ctrl+C.
const (
	stuckTitle    = "Add a search box"
	finishedTitle = "Add a footer"
)

// TestCLISecondInterrupt checks what a second Ctrl+C adds to the first.
//
// Edge case: two sessions work and neither stops when asked. A first SIGINT
// makes crew wind down, as the config reference says it does at its run time
// limit: the sessions that run go on and crew waits for them, so when one of
// them succeeds after the press, crew still moves its issue to the success
// label, and the other issue keeps its running label. That session never
// ends, and a second SIGINT then ends crew within seconds, without waiting for
// it. A first Ctrl+C that stopped the sessions at once would throw away work in
// flight and leave nothing for a second press to force; a user who presses
// Ctrl+C twice wants crew gone now, and must not have to kill it by hand. The
// README says nothing about how crew stops.
func TestCLISecondInterrupt(t *testing.T) {
	t.Parallel()
	sc := harness.New(t, harness.Options{Config: sessionConfig, Args: []string{"--plain"}})
	sc.GitHub.AddLabel("cli:ready", "cli:running", "cli:done", "cli:failed")
	stuck := sc.GitHub.AddIssue(fakegithub.Issue{Title: stuckTitle, Labels: []string{"cli:ready"}})
	finished := sc.GitHub.AddIssue(fakegithub.Issue{Title: finishedTitle, Labels: []string{"cli:ready"}})
	done, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(done) })
	started := make(chan struct{}, 2)
	sc.Claude.Script(`Implement "`+stuckTitle+`".`, stubbornSession(started, nil, done))
	sc.Claude.Script(`Implement "`+finishedTitle+`".`, stubbornSession(started, release, done))
	sc.Start()
	sc.Wait(func() bool {
		return isRunning(sc, stuck) && isRunning(sc, finished) && len(started) == 2
	}, timeout)
	sc.Stop()
	time.Sleep(pressGap)
	close(release)
	sc.Wait(func() bool { return !isRunning(sc, finished) }, timeout)
	if issue, _ := sc.GitHub.Issue(finished); !slices.Contains(issue.Labels, "cli:done") {
		t.Fatalf("after the first Ctrl+C, the issue whose session then succeeded carries %q, want cli:done: "+
			"the first press did not let crew wind down and wait for the running sessions, "+
			"so a second press has nothing to force", issue.Labels)
	}
	if !isRunning(sc, stuck) {
		t.Fatal("the issue whose session never ended left cli:running before the second Ctrl+C")
	}
	sc.Stop()
	sc.Exit(forceTimeout)
}

// stubbornSession is a session that ignores crew asking it to stop. It says
// it works and sends on started, then succeeds when release closes; it ends
// without a result when done closes, at the end of the test. A nil release
// never closes.
func stubbornSession(started chan<- struct{}, release, done <-chan struct{}) fakeclaude.ScriptFunc {
	return func(_ context.Context, s *fakeclaude.Session) int {
		_ = s.Emit(s.Init(), s.Said("Writing it."))
		started <- struct{}{}
		select {
		case <-release:
			_ = s.Emit(s.Success("Done."))
			return 0
		case <-done:
			return 1
		}
	}
}

// isRunning reports whether the issue number carries the running label of
// sessionConfig's rule.
func isRunning(sc *harness.Scenario, number int) bool {
	issue, _ := sc.GitHub.Issue(number)
	return slices.Contains(issue.Labels, "cli:running")
}
