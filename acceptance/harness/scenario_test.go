package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
)

// innerEnv guards TestInnerScenarioWithAViolation: it runs only when
// TestAViolationFailsTheTestBinary re-runs the test binary with it set.
const innerEnv = "ACCEPTANCE_INNER_VIOLATION"

// recordingT is a testing.TB for a scenario under test: failures, skips and
// cleanups are recorded instead of acting on the real test, which still
// provides temporary and artifact directories and the context.
type recordingT struct {
	*testing.T

	mu       sync.Mutex
	msgs     []string
	failed   bool
	skipped  bool
	cleanups []func()
}

func (r *recordingT) Helper() {}

func (r *recordingT) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recordingT) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

func (r *recordingT) Skipf(format string, args ...any) {
	r.mu.Lock()
	r.skipped = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
	r.mu.Unlock()
	runtime.Goexit()
}

func (r *recordingT) Skip(args ...any) { r.Skipf("%s", fmt.Sprint(args...)) }

func (r *recordingT) SkipNow() { r.Skipf("SkipNow") }

func (r *recordingT) Failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed
}

func (r *recordingT) Cleanup(f func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanups = append(r.cleanups, f)
}

// recorded is what a recorded scenario reported.
type recorded struct {
	failed, skipped bool
	text            string
}

// record runs fn with a recordingT, in a goroutine so Fatalf can stop it,
// then runs the cleanups it registered, last-in first-out, and returns what
// both reported.
func record(t *testing.T, fn func(tb testing.TB)) recorded {
	t.Helper()
	r := &recordingT{T: t}
	inGoroutine(func() { fn(r) })
	inGoroutine(func() {
		for _, f := range slices.Backward(r.cleanups) {
			f()
		}
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	return recorded{failed: r.failed, skipped: r.skipped, text: strings.Join(r.msgs, "\n")}
}

// inGoroutine runs fn in a goroutine and waits for it to end, by returning
// or by runtime.Goexit.
func inGoroutine(fn func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	<-done
}

// standInOptions returns options that run the stand-in for crew with args,
// from a link to the test binary in a new temporary directory.
func standInOptions(t *testing.T, args ...string) Options {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), standInName)
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	return Options{Args: args, bin: link}
}

// Covers U4 (KTD5): without CREW_BIN, a scenario that needs crew fails, and
// does not skip, naming the command that builds crew and runs the suite.
func TestWithoutCrewBinAScenarioFailsNamingTheLocalCommand(t *testing.T) {
	t.Setenv(CrewBinEnv, "")

	got := record(t, func(tb testing.TB) {
		tb.Helper()
		New(tb, Options{})
	})

	if !got.failed || got.skipped || !strings.Contains(got.text, "go -C acceptance run ./cmd/acceptance") {
		t.Fatalf("New without %s = %+v, want a failure naming the local command", CrewBinEnv, got)
	}
}

// Covers U4 (KTD7): with the doubles' bin directory missing claude, Start
// fails before any process starts, naming claude.
func TestAMissingDoubleFailsThePathCheckBeforeCrewStarts(t *testing.T) {
	opts := standInOptions(t, "env")
	var s *Scenario

	got := record(t, func(tb testing.TB) {
		tb.Helper()
		s = New(tb, opts)
		if err := os.Remove(filepath.Join(s.server.Bin(), claudeName)); err != nil {
			tb.Fatalf("remove the claude double: %v", err)
		}
		s.Start()
	})

	if !got.failed || !strings.Contains(got.text, "claude") || strings.Contains(got.text, "remove the claude") {
		t.Fatalf("Start without claude = %+v, want a failure naming claude", got)
	}
	if s.cmd != nil {
		t.Fatalf("crew was started: %v", s.cmd)
	}
}

// Covers U4 (KTD7): crew's environment holds only the allowlisted
// variables, even when the test process holds GitHub tokens.
func TestCrewGetsOnlyTheAllowlistedEnvironment(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh-secret")
	t.Setenv("GITHUB_TOKEN", "github-secret")
	allowed := []string{"PATH", "HOME", "XDG_CONFIG_HOME", "TZ", "LANG", "GIT_CONFIG_NOSYSTEM",
		"GIT_CONFIG_GLOBAL", "GORACE", SocketEnv}
	s := New(t, standInOptions(t, "env"))
	s.Start()

	exited := s.Exit(wait)

	if exited.Code != 0 {
		t.Fatalf("the stand-in exited %d: %s", exited.Code, exited.Stderr)
	}
	for line := range strings.Lines(exited.Stdout) {
		// The name only: a value may be a secret of the developer's.
		if name, _, _ := strings.Cut(line, "="); !slices.Contains(allowed, name) {
			t.Errorf("crew's environment holds %s, outside the allowlist", name)
		}
	}
	if !strings.Contains(exited.Stdout, "PATH="+s.server.Bin()+string(os.PathListSeparator)) {
		t.Errorf("PATH does not start with the doubles' bin directory:\n%s", exited.Stdout)
	}
}

// Covers U4 (KTD7): TERM=xterm-256color is set in the pseudo-terminal
// only, and crew's output is then on the screen.
func TestTERMIsSetOnTheScreenOnly(t *testing.T) {
	plain := New(t, standInOptions(t, "env"))
	plain.Start()
	opts := standInOptions(t, "env")
	opts.Screen = true
	screen := New(t, opts)
	screen.Start()

	plainOut := plain.Exit(wait).Stdout
	screen.Exit(wait)

	if strings.Contains(plainOut, "TERM=") {
		t.Errorf("crew's environment without a screen holds TERM:\n%s", plainOut)
	}
	if text := screen.Screen().Text(); !strings.Contains(text, "TERM=xterm-256color") {
		t.Errorf("the screen does not show TERM=xterm-256color:\n%s", text)
	}
}

// Covers U4 (R20, KTD6): the test binary, re-run on a scenario whose crew
// makes a gh call the fake does not know, exits non-zero and names the
// call; the wait fails at the violation, not at its timeout.
func TestAViolationFailsTheTestBinary(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestInnerScenarioWithAViolation$", "-test.count=1")
	cmd.Env = append(os.Environ(), innerEnv+"=1")

	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("the re-run test binary = %v, want a non-zero exit; output:\n%s", err, out)
	}
	if !strings.Contains(string(out), "acceptance: unknown call: gh repo view --json name") {
		t.Fatalf("the output does not name the violating call:\n%s", out)
	}
	if strings.Contains(string(out), "did not hold within") {
		t.Fatalf("the wait failed at its timeout, not at the violation:\n%s", out)
	}
}

// TestInnerScenarioWithAViolation is the scenario TestAViolationFailsTheTestBinary
// re-runs: crew makes a gh call the fake does not know while the scenario
// waits for a condition that never holds.
func TestInnerScenarioWithAViolation(t *testing.T) {
	if os.Getenv(innerEnv) != "1" {
		t.Skip("runs only when TestAViolationFailsTheTestBinary re-runs the test binary")
	}
	s := New(t, standInOptions(t, "run", ghName, "repo", "view", "--json", "name"))
	s.Start()

	s.Wait(func() bool { return false }, time.Minute)
}

// Covers U4 (KTD6): crew that ignores SIGINT still runs at the stop
// deadline; Exit kills it and the test reports the forced kill.
func TestCrewThatIgnoresSIGINTIsKilledAtTheDeadline(t *testing.T) {
	opts := standInOptions(t, "ignore-sigint")
	var exited Exited

	got := record(t, func(tb testing.TB) {
		tb.Helper()
		s := New(tb, opts)
		s.Start()
		s.Wait(func() bool { return strings.Contains(s.Stdout(), "ignoring SIGINT") }, wait)
		s.Stop()
		exited = s.Exit(300 * time.Millisecond)
	})

	if !got.failed || !strings.Contains(got.text, "killed") {
		t.Fatalf("Exit of crew ignoring SIGINT = %+v, want a failure reporting the forced kill", got)
	}
	if !exited.Killed || exited.Code == 0 {
		t.Fatalf("Exit = %+v, want killed with a non-zero code", exited)
	}
}

// Covers U4: teardown kills the claude double whose script still holds it,
// waits for the script, and the scenario's temporary directories are
// removed without an error.
func TestTeardownKillsAHeldClaudeDoubleAndRemovesTheTempDirs(t *testing.T) {
	opts := standInOptions(t, append([]string{"run", claudeName}, claudeArgs("hold on")...)...)
	var pid int
	var repo string
	t.Run("scenario", func(t *testing.T) {
		s := New(t, opts)
		repo = s.Repo
		s.Claude.Script("hold on", func(ctx context.Context, ses *fakeclaude.Session) int {
			_ = ses.Emit(ses.Init())
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond) // a write after the stop, into the scenario's repository
			_ = os.WriteFile(filepath.Join(ses.Dir, "late"), []byte("late\n"), 0o600)
			return 1
		})
		s.Start()
		s.Wait(func() bool { pid = heldDouble(s.server); return pid != 0 }, wait)
	})

	if !gone(pid, wait) {
		t.Errorf("the claude double %d still runs after teardown", pid)
	}
	if _, err := os.Stat(repo); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scenario's repository %s is still there: %v", repo, err)
	}
}

// heldDouble returns the pid of a double whose invocation is in flight on
// s, or 0 when none is.
func heldDouble(s *Server) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid := range s.pids {
		return pid
	}
	return 0
}

// gone reports whether process pid is gone within timeout.
func gone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}
