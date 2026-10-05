package harness

import (
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

const (
	// defaultCols and defaultRows are the size of a screen whose Size
	// leaves them zero: clear of the live view's height budgets (KTD8).
	defaultCols = 120
	defaultRows = 50
	// defaultTerm is the TERM a program on the screen gets when its
	// command sets none: the emulator is an xterm.
	defaultTerm = "TERM=xterm-256color"
	// pollInterval is how often the waits read the screen.
	pollInterval = 20 * time.Millisecond
	// stopTimeout bounds how long cleanup waits for a killed program to be
	// reaped and for its output to end.
	stopTimeout = 5 * time.Second
)

// Size is a screen's size in cells. A zero field takes the default,
// 120 columns by 50 rows.
type Size struct {
	Cols, Rows int
}

// Screen is a program running in a pseudo-terminal of a fixed size, whose
// output a terminal emulator draws. The emulator's replies to the program's
// queries, such as the background colour Bubble Tea asks for, go back to
// the program.
type Screen struct {
	// Check, when set, runs on every poll of WaitFor, WaitForText and
	// WaitStable; an error fails the test at once with the last screen.
	Check func() error

	cmd *exec.Cmd
	pid int
	ptm *os.File
	emu *vt.SafeEmulator
	// exited closes once the program is reaped; cmd.ProcessState is set.
	exited chan struct{}
	// outputDone closes once the program's output ends; outputErr is
	// nil when it ended as it does at exit, with EIO.
	outputDone chan struct{}
	outputErr  error
}

// StartScreen starts cmd in a pseudo-terminal of size, as the leader of its
// own session with the terminal as its controlling terminal. cmd.Env is the
// caller's; when it sets no TERM, the program gets TERM=xterm-256color, as
// it does when cmd.Env is nil and the environment is inherited. A cleanup
// kills the program's process group if it still runs.
func StartScreen(tb testing.TB, cmd *exec.Cmd, size Size) *Screen {
	tb.Helper()
	cols, rows := orDefault(size.Cols, defaultCols), orDefault(size.Rows, defaultRows)
	if cols > math.MaxUint16 || rows > math.MaxUint16 || cols < 0 || rows < 0 {
		tb.Fatalf("screen size %dx%d is out of range", cols, rows)
		return nil
	}
	cmd.Env = screenEnv(cmd.Env)
	ptm, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		tb.Fatalf("start %s in a pseudo-terminal: %v", cmd.Path, err)
		return nil
	}
	s := &Screen{
		cmd: cmd, pid: cmd.Process.Pid, ptm: ptm, emu: vt.NewSafeEmulator(cols, rows),
		exited: make(chan struct{}), outputDone: make(chan struct{}),
	}
	go s.reap()
	go s.pumpOutput()
	// The emulator writes its replies to a synchronous pipe: unless they
	// are read, the program's first query blocks the emulator's Write, and
	// with it every read of the screen. The emulator is never closed, as
	// closing it while this goroutine reads is a data race in x/vt.
	go func() { _, _ = io.Copy(ptm, s.emu) }()
	tb.Cleanup(s.stop)
	return s
}

// Text is the screen as plain text: one line per row, every row, without
// styles or trailing spaces.
func (s *Screen) Text() string {
	lines := strings.Split(ansi.Strip(s.emu.Render()), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// Masked is the screen's text with masks applied, in order.
func (s *Screen) Masked(masks ...Mask) string {
	return MaskText(s.Text(), masks...)
}

// WaitFor waits until cond holds for the screen's text, unmasked, and
// returns that text. It fails the test with the last screen when timeout
// passes first.
func (s *Screen) WaitFor(tb testing.TB, cond func(text string) bool, timeout time.Duration) string {
	tb.Helper()
	return s.waitFor(tb, "the condition", cond, timeout)
}

// WaitForText waits until the screen shows substr and returns its text.
func (s *Screen) WaitForText(tb testing.TB, substr string, timeout time.Duration) string {
	tb.Helper()
	return s.waitFor(tb, strconv.Quote(substr), func(text string) bool { return strings.Contains(text, substr) }, timeout)
}

// WaitStable waits until the screen's text with masks applied has not
// changed for settle, and returns it. A program with a clock never stops
// writing, so stable means the masked text, not the output, is still. It
// fails the test with the last screen when timeout passes first.
func (s *Screen) WaitStable(tb testing.TB, settle, timeout time.Duration, masks ...Mask) string {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	last, since := s.Masked(masks...), time.Now()
	for {
		s.check(tb)
		now := time.Now()
		if text := s.Masked(masks...); text != last {
			last, since = text, now
		} else if now.Sub(since) >= settle {
			return text
		}
		if now.After(deadline) {
			tb.Fatalf("the screen did not hold still for %v within %v; last screen:\n%s",
				settle, timeout, framed(last))
			return last
		}
		time.Sleep(pollInterval)
	}
}

// Send types keys into the terminal. It fails the test before the first
// frame: until the program puts the terminal in raw mode, the terminal
// echoes keys and turns ^C into SIGINT (KTD8).
func (s *Screen) Send(tb testing.TB, keys string) {
	tb.Helper()
	if strings.TrimSpace(s.Text()) == "" {
		tb.Fatalf("send %s before the first frame: wait for the screen first", strconv.Quote(keys))
		return
	}
	if _, err := s.ptm.WriteString(keys); err != nil {
		tb.Fatalf("send %s: %v", strconv.Quote(keys), err)
	}
}

// Signal sends sig to the program's process group, as a terminal does.
func (s *Screen) Signal(tb testing.TB, sig syscall.Signal) {
	tb.Helper()
	if err := syscall.Kill(-s.pid, sig); err != nil {
		tb.Fatalf("signal %v to process group %d: %v", sig, s.pid, err)
	}
}

// Wait waits for the program to exit and its output to end, and returns how
// it exited. It fails the test with the last screen when timeout passes
// first.
func (s *Screen) Wait(tb testing.TB, timeout time.Duration) *os.ProcessState {
	tb.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	select {
	case <-s.exited:
	case <-deadline.C:
		tb.Fatalf("the program still runs after %v; last screen:\n%s", timeout, framed(s.Text()))
		return nil
	}
	select {
	case <-s.outputDone:
	case <-deadline.C:
		tb.Fatalf("the program exited but its output is still open after %v; last screen:\n%s",
			timeout, framed(s.Text()))
		return nil
	}
	if s.outputErr != nil {
		tb.Fatalf("read the program's output: %v", s.outputErr)
		return nil
	}
	return s.cmd.ProcessState
}

// reap waits for the program to exit.
func (s *Screen) reap() {
	_ = s.cmd.Wait()
	close(s.exited)
}

// pumpOutput draws the program's output until it ends. Reading the
// terminal's master fails with EIO once the program and every process that
// shares the terminal exit: that is the end of the output, not an error.
func (s *Screen) pumpOutput() {
	_, err := io.Copy(s.emu, s.ptm)
	if errors.Is(err, syscall.EIO) {
		err = nil
	}
	s.outputErr = err
	close(s.outputDone)
}

// stop kills the program's process group if the program still runs, waits
// a bounded time for it and for its output to end, and closes the terminal.
func (s *Screen) stop() {
	select {
	case <-s.exited:
	default:
		_ = syscall.Kill(-s.pid, syscall.SIGKILL)
	}
	timeout := time.NewTimer(stopTimeout)
	defer timeout.Stop()
	select {
	case <-s.exited:
	case <-timeout.C:
	}
	select {
	case <-s.outputDone:
	case <-timeout.C:
	}
	_ = s.ptm.Close()
}

func (s *Screen) waitFor(tb testing.TB, what string, cond func(string) bool, timeout time.Duration) string {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for {
		s.check(tb)
		text := s.Text()
		if cond(text) {
			return text
		}
		if time.Now().After(deadline) {
			tb.Fatalf("the screen did not show %s within %v; last screen:\n%s", what, timeout, framed(text))
			return text
		}
		time.Sleep(pollInterval)
	}
}

// check fails the test at once when the Check hook reports an error.
func (s *Screen) check(tb testing.TB) {
	tb.Helper()
	if s.Check == nil {
		return
	}
	if err := s.Check(); err != nil {
		tb.Fatalf("%v\nlast screen:\n%s", err, framed(s.Text()))
	}
}

// orDefault is n, or def when n is zero.
func orDefault(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

// screenEnv is env with TERM=xterm-256color when env sets no TERM. A nil
// env is the inherited environment, whose TERM describes another terminal.
func screenEnv(env []string) []string {
	if env == nil {
		return append(os.Environ(), defaultTerm)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "TERM=") {
			return env
		}
	}
	return append(slices.Clip(env), defaultTerm)
}

// framed is a screen's text between rules, so its edges read in a failure.
func framed(text string) string {
	rule := strings.Repeat("─", defaultCols)
	return rule + "\n" + text + "\n" + rule
}
