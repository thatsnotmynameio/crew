package harness

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
)

// Options say how a scenario runs crew.
type Options struct {
	// Config is the text of the repository's .crew/config.yaml, committed
	// in its first commit. "" leaves the repository without one.
	Config string
	// GlobalConfig is the text of crew's global config file,
	// crew/config.yaml in the scenario's own XDG_CONFIG_HOME. "" leaves
	// that directory empty.
	GlobalConfig string
	// Args are crew's command-line arguments, such as --plain.
	Args []string
	// Screen runs crew in a pseudo-terminal, as a user runs it in a
	// terminal, instead of with its output captured.
	Screen bool
	// Size is the pseudo-terminal's size in screen mode; a zero field takes
	// the default, 120 columns by 50 rows.
	Size Size

	// bin is the program run in place of crew, for the harness's own
	// tests; "" runs crew from CREW_BIN.
	bin string
}

// Scenario is one run of crew against the doubles, in a repository of its
// own. Build it with New, set the starting state on GitHub and script the
// Claude Code sessions, then Start crew, Wait for the end state, Stop crew
// and read its Exit. Its methods fail the test they were built for, so call
// them from that test's goroutine.
type Scenario struct {
	// GitHub is the repository RepositoryOwner/RepositoryName on the fake
	// GitHub: set its starting state before Start and read its state after.
	GitHub *fakegithub.GitHub
	// Claude is the fake Claude Code: register a script for every session
	// crew is expected to start. Its sessions get GitHub as their handle.
	Claude *fakeclaude.Claude
	// Repo is the path of the repository crew runs in: a clone, named
	// RepositoryName, of a local bare origin.
	Repo string

	tb     testing.TB
	opts   Options
	server *Server
	env    []string

	cmd            *exec.Cmd
	screen         *Screen
	exited         <-chan struct{} // closes once crew is reaped
	stdout, stderr syncBuffer

	mu       sync.Mutex
	reported int // violations already named by a failure
}

// Exited is how crew's run ended.
type Exited struct {
	// Code is crew's exit code, or -1 when a signal ended it.
	Code int
	// Killed is true when crew still ran at Exit's deadline and the harness
	// killed it; the test has failed then.
	Killed bool
	// Stdout and Stderr are what crew printed, without a screen; in screen
	// mode its output is on the Screen.
	Stdout, Stderr string
}

// New builds a scenario for tb: an empty fake GitHub repository, a fake
// Claude Code without scripts, the doubles that crew finds on its PATH in
// place of gh and claude, a home holding opts.GlobalConfig, and a
// repository holding opts.Config, cloned from a bare origin in a temporary
// directory. crew does not run until Start.
//
// At the end of the test, in this order, the scenario stops crew if it
// still runs, fails the test for each call the doubles did not know that no
// failure named yet, kills the doubles still running, saves crew's
// .crew/logs, its output or last screen, the gh calls and the unknown calls
// to the test's artifact directory, and closes the doubles' server; then
// the temporary directories are removed.
func New(tb testing.TB, opts Options) *Scenario {
	tb.Helper()
	bin := opts.bin
	if bin == "" {
		bin = Binary(tb)
	}
	opts.bin = bin
	tools, git, err := toolDirs()
	if err != nil {
		tb.Fatalf("%v", err)
	}
	base := tb.TempDir() // removed after the teardown below, registered later
	gh := fakegithub.New(RepositoryOwner, RepositoryName)
	claude := fakeclaude.New(gh)
	server, err := NewServer(gh, claude)
	if err != nil {
		tb.Fatalf("start the doubles' server: %v", err)
	}
	s := &Scenario{GitHub: gh, Claude: claude, tb: tb, opts: opts, server: server}
	tb.Cleanup(s.teardown)
	s.env = environment(server, newHome(tb, base, opts.GlobalConfig), tools)
	s.Repo = newRepo(tb, git, s.env, base, opts.Config)
	return s
}

// Start starts crew in the repository, with the scenario's environment: in
// a pseudo-terminal when Options.Screen is set, otherwise with its output
// captured. It first checks that gh and claude on crew's PATH are the
// doubles, and fails naming the one that is not.
func (s *Scenario) Start() {
	s.tb.Helper()
	if s.cmd != nil {
		s.tb.Fatalf("crew is already started")
	}
	if err := checkPath(s.env, s.server.Bin()); err != nil {
		s.tb.Fatalf("crew's PATH: %v", err)
	}
	//nolint:gosec // G204: the crew binary under test, with the arguments its scenario gives it
	cmd := exec.CommandContext(s.tb.Context(), s.opts.bin, s.opts.Args...)
	cmd.Dir, cmd.Env = s.Repo, s.env
	// The test's end kills crew's whole process group, not only crew.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = stopTimeout
	if s.opts.Screen {
		screen := StartScreen(s.tb, cmd, s.opts.Size)
		screen.Check = s.unreported
		s.cmd, s.screen, s.exited = cmd, screen, screen.exited
		return
	}
	cmd.Stdout, cmd.Stderr = &s.stdout, &s.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		s.tb.Fatalf("start crew: %v", err)
	}
	s.cmd = cmd
	exited := make(chan struct{})
	s.exited = exited
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
}

// Screen returns crew's screen in screen mode, nil otherwise or before
// Start. Its waits fail the test as soon as a call the doubles do not know
// is made.
func (s *Scenario) Screen() *Screen {
	return s.screen
}

// Stdout returns what crew printed on standard output so far, without a
// screen.
func (s *Scenario) Stdout() string {
	return s.stdout.String()
}

// Stderr returns what crew printed on standard error so far, without a
// screen.
func (s *Scenario) Stderr() string {
	return s.stderr.String()
}

// Wait waits until cond holds, checking it whenever the fake GitHub's state
// changes and every few milliseconds, for the effects of Claude Code
// sessions and of crew itself. It fails the test as soon as crew makes a
// call the doubles do not know, naming the call, and when timeout passes
// first.
func (s *Scenario) Wait(cond func() bool, timeout time.Duration) {
	s.tb.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		changed := s.GitHub.Changed()
		s.failOnViolation()
		if cond() {
			return
		}
		select {
		case <-changed:
		case <-ticker.C:
		case <-deadline.C:
			s.failOnViolation()
			s.tb.Fatalf("the condition did not hold within %v", timeout)
			return
		}
	}
}

// Stop asks crew to stop as Ctrl+C in a terminal does: SIGINT to crew's
// process group. It does not wait; Exit does.
func (s *Scenario) Stop() {
	s.tb.Helper()
	if s.cmd == nil {
		s.tb.Fatalf("stop crew before Start")
	}
	if err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGINT); err != nil && !errors.Is(err, syscall.ESRCH) {
		s.tb.Fatalf("send SIGINT to crew: %v", err)
	}
}

// Exit waits up to timeout for crew to exit and returns how it ended. When
// crew still runs at the deadline, Exit kills crew's process group and
// fails the test, reporting the forced kill. It fails the test, naming the
// call, when crew made a call the doubles do not know, before the exit code
// is returned: crew retries a failing gh call, so its exit code alone would
// not show it.
func (s *Scenario) Exit(timeout time.Duration) Exited {
	s.tb.Helper()
	if s.cmd == nil {
		s.tb.Fatalf("wait for crew's exit before Start")
	}
	killed := !s.waitExit(timeout)
	if killed {
		s.kill()
		if !s.waitExit(stopTimeout) {
			s.tb.Fatalf("crew still runs %v after SIGKILL", stopTimeout)
		}
		s.tb.Errorf("crew still ran %v after Exit began waiting; the harness killed it", timeout)
	}
	if s.screen != nil {
		s.screen.Wait(s.tb, stopTimeout)
	}
	s.failOnViolation()
	return Exited{Code: s.cmd.ProcessState.ExitCode(), Killed: killed, Stdout: s.Stdout(), Stderr: s.Stderr()}
}

// waitExit reports whether crew is reaped within timeout; with a timeout of
// 0, whether it is reaped already.
func (s *Scenario) waitExit(timeout time.Duration) bool {
	select {
	case <-s.exited:
		return true
	default:
		if timeout <= 0 {
			return false
		}
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-s.exited:
		return true
	case <-t.C:
		return false
	}
}

// kill kills crew's process group.
func (s *Scenario) kill() {
	_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
}

// failOnViolation fails the test at once when crew made a call the doubles
// do not know that no failure named yet.
func (s *Scenario) failOnViolation() {
	s.tb.Helper()
	if err := s.unreported(); err != nil {
		s.tb.Fatalf("%v", err)
	}
}

// unreported returns an error naming every call the doubles did not know
// that no failure named yet, one per line, and counts them as named; nil
// when there is none.
func (s *Scenario) unreported() error {
	all := s.server.Violations()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(all) <= s.reported {
		return nil
	}
	fresh := all[s.reported:]
	s.reported = len(all)
	return errors.New("acceptance: unknown call: " + strings.Join(fresh, "\nacceptance: unknown call: "))
}

// teardown ends the scenario in the order of the lifecycle: stop crew,
// check the violations, kill the doubles, save the artifacts, close the
// server. In screen mode, the screen's own cleanup, which runs first, has
// already killed crew.
func (s *Scenario) teardown() {
	if s.cmd != nil && !s.waitExit(0) {
		s.kill()
		if !s.waitExit(stopTimeout) {
			s.tb.Errorf("crew still runs %v after SIGKILL", stopTimeout)
		}
	}
	if err := s.unreported(); err != nil {
		s.tb.Errorf("%v", err)
	}
	s.server.KillDoubles()
	s.saveArtifacts()
	if err := s.server.Close(); err != nil {
		s.tb.Errorf("close the doubles' server: %v", err)
	}
}

// syncBuffer is a bytes.Buffer safe for one writer and concurrent readers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("buffer crew's output: %w", err)
	}
	return n, nil
}

// String returns what was written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
