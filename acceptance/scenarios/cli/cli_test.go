package cli

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

// timeout bounds every wait and every exit that the README does not bound:
// generous, since a wait returns as soon as its condition holds.
const timeout = time.Minute

// The exit codes of the README's "Stopping crew", where "crew exits 0 after a
// stop or at its run time limit, 1 when it failed while running or a second
// stop forced its exit, and 2 on a command line it cannot use or a config or
// environment error, such as a repository without `.crew/config.yaml`".
const (
	exitClean   = 0
	exitFailed  = 1
	exitRefused = 2
)

// readyLabel, runningLabel and doneLabel are the labels of the rule without
// actions in moveConfig.
const (
	readyLabel   = "cli:approved"
	runningLabel = "cli:approving"
	doneLabel    = "cli:done"
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

// moveOneIssue starts crew as opts says, with moveConfig, waits until it moved
// one issue to doneLabel, and returns the scenario, still running.
func moveOneIssue(t *testing.T, opts harness.Options) *harness.Scenario {
	t.Helper()
	opts.Config = moveConfig
	sc := harness.New(t, opts)
	sc.GitHub.AddLabel(readyLabel, runningLabel, doneLabel)
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
	if exited.Code != exitClean {
		t.Errorf("crew --version exited %d, want %d; stderr: %q", exited.Code, exitClean, exited.Stderr)
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
// and --version. Asking for help is not a command line crew cannot use, so
// it is not the README's exit 2, and the usage is the only list of crew's
// flags a user has besides the README.
func TestCLIHelp(t *testing.T) {
	t.Parallel()
	exited := run(t, "", "--help")
	if exited.Code != exitClean {
		t.Errorf("crew --help exited %d, want %d", exited.Code, exitClean)
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
// README: crew exits "2 on a command line it cannot use". `crew --bogus` is
// one, so crew exits 2 at once. Edge case: it names the flag on standard
// error, so the user learns which word it refused.
func TestCLIUnknownFlag(t *testing.T) {
	t.Parallel()
	exited := run(t, moveConfig, "--bogus")
	if exited.Code != exitRefused {
		t.Errorf("crew --bogus exited %d, want %d", exited.Code, exitRefused)
	}
	if !strings.Contains(exited.Stderr, "bogus") {
		t.Errorf("crew --bogus does not name the flag on standard error: %q", exited.Stderr)
	}
}

// TestCLIUnexpectedArgument checks an argument crew does not take.
//
// README: crew exits "2 on a command line it cannot use". The usage,
// `crew [--plain] [--version]` and `crew bots create <name>`, takes no other
// argument, so `crew run` is one: crew exits 2 at once rather than start a
// run. Edge case: it says why on standard error.
func TestCLIUnexpectedArgument(t *testing.T) {
	t.Parallel()
	exited := run(t, moveConfig, "run")
	if exited.Code != exitRefused {
		t.Errorf("crew run exited %d, want %d", exited.Code, exitRefused)
	}
	if strings.TrimSpace(exited.Stderr) == "" {
		t.Error("crew run printed nothing on standard error")
	}
}

// TestCLIWithoutConfig checks a repository without .crew/config.yaml.
//
// README: crew exits "2 on ... a config or environment error, such as a
// repository without `.crew/config.yaml`." Edge case: it names
// .crew/config.yaml on standard error, so the user learns which file is
// missing.
func TestCLIWithoutConfig(t *testing.T) {
	t.Parallel()
	exited := run(t, "", "--plain")
	if exited.Code != exitRefused {
		t.Errorf("crew without .crew/config.yaml exited %d, want %d", exited.Code, exitRefused)
	}
	if !strings.Contains(exited.Stderr, "config.yaml") {
		t.Errorf("crew does not name .crew/config.yaml on standard error: %q", exited.Stderr)
	}
}

// TestCLIPollIntervalZero checks the lower bound of poll_interval_seconds.
//
// README: `.crew/config.example.yaml`, which the README makes the reference of
// every key, says "Seconds between polls of the tracker. Must be positive."
// So 0 is a config error, and crew exits "2 on ... a config or environment
// error". Edge case: it names the key on standard error. The other scenarios
// run with 1, the smallest value allowed.
func TestCLIPollIntervalZero(t *testing.T) {
	t.Parallel()
	config := strings.Replace(moveConfig, "poll_interval_seconds: 1", "poll_interval_seconds: 0", 1)
	exited := run(t, config, "--plain")
	if exited.Code != exitRefused {
		t.Errorf("crew with poll_interval_seconds: 0 exited %d, want %d", exited.Code, exitRefused)
	}
	if !strings.Contains(exited.Stderr, "poll_interval_seconds") {
		t.Errorf("crew does not name poll_interval_seconds on standard error: %q", exited.Stderr)
	}
}

// TestCLIRunTimeLimit checks that crew ends by itself at its run time limit.
//
// README: crew "exits 0 ... at its run time limit", and
// `.crew/config.example.yaml` says of run_time_limit_seconds "How long crew
// runs, counted from its first poll, before it winds down." With a limit of
// one second and nothing to do, crew exits 0 without being stopped.
func TestCLIRunTimeLimit(t *testing.T) {
	t.Parallel()
	exited := run(t, "run_time_limit_seconds: 1\n"+moveConfig, "--plain")
	if exited.Code != exitClean {
		t.Errorf("crew at its run time limit exited %d, want %d; stderr: %q", exited.Code, exitClean, exited.Stderr)
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
	sc := moveOneIssue(t, harness.Options{Args: []string{"--plain"}})
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
