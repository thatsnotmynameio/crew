package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// wait is the timeout of every wait in these tests: generous, since a
// failing wait shows the screen it got.
const wait = 10 * time.Second

// startProbe runs the test binary as the probe on a screen of the default
// size, with env added to the inherited environment, and waits for its
// first frame.
func startProbe(t *testing.T, env ...string) *Screen {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := &exec.Cmd{Path: exe, Args: []string{probeName}, Env: append(os.Environ(), env...)}
	s := StartScreen(t, cmd, Size{})
	s.WaitForText(t, "q quit", wait)
	return s
}

// Covers U5: the OSC 11 query gets the emulator's reply and the probe goes
// on to draw. Without the reply pump, the emulator's Write blocks on the
// query and this test hangs; the probe gives up after its own timeout with
// "no reply to the OSC 11 background-colour query".
func TestTheProbesBackgroundQueryIsAnsweredAndItDraws(t *testing.T) {
	s := startProbe(t)

	s.WaitForText(t, "background rgb:0000/0000/0000", wait)
}

// Covers U5: q after the first frame makes the probe exit 0, and the end of
// its output, EIO on the terminal's master, is not an error: the last words
// it wrote before exiting are on the screen.
func TestQAfterTheFirstFrameExitsZeroAndEIOEndsTheOutput(t *testing.T) {
	s := startProbe(t)

	s.Send(t, "q")
	state := s.Wait(t, wait)

	if code := state.ExitCode(); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if text := s.Text(); !strings.Contains(text, "bye") {
		t.Fatalf("the screen after exit misses the probe's last words:\n%s", text)
	}
}

// Covers U5: a signal to the process group ends the probe, and the exit
// status reports it; SIGINT is the probe's own way out, exit 0.
func TestASignalToTheProcessGroupEndsTheProbe(t *testing.T) {
	t.Run("SIGTERM is reported", func(t *testing.T) {
		s := startProbe(t)

		s.Signal(t, syscall.SIGTERM)
		state := s.Wait(t, wait)

		status, ok := state.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
			t.Fatalf("exit status = %v, want killed by SIGTERM", state)
		}
	})
	t.Run("SIGINT exits 0", func(t *testing.T) {
		s := startProbe(t)

		s.Signal(t, syscall.SIGINT)
		state := s.Wait(t, wait)

		if code := state.ExitCode(); code != 0 {
			t.Fatalf("exit status = %v, want exit 0", state)
		}
	})
}

// Covers U5: at 120x50, the screen has 50 rows and the probe's text sits
// at the row and column it drew it at; its header ends at the right edge.
func TestTheProbesScreenHasFiftyRowsAndItsTextWhereItWasDrawn(t *testing.T) {
	s := startProbe(t)

	lines := strings.Split(s.Text(), "\n")

	if len(lines) != defaultRows {
		t.Fatalf("the screen has %d rows, want %d", len(lines), defaultRows)
	}
	if want := strings.Repeat(" ", 19) + "row 10, column 20"; lines[9] != want {
		t.Errorf("row 10 = %q, want %q", lines[9], want)
	}
	if lines[49] != "row 50" {
		t.Errorf("row 50 = %q, want %q", lines[49], "row 50")
	}
	if width := ansi.StringWidth(lines[0]); width != defaultCols || !strings.HasSuffix(lines[0], "q quit") {
		t.Errorf("header = %q (%d columns), want %d columns ending in q quit", lines[0], width, defaultCols)
	}
}

// Covers U5: with the clock, duration and spinner masked, WaitStable
// returns while the probe keeps redrawing them; without masks, the screen
// never holds still and WaitStable fails with the last screen.
func TestWaitStableHoldsOnlyForTheMaskedScreen(t *testing.T) {
	s := startProbe(t)

	s.WaitStable(t, 300*time.Millisecond, wait, DefaultMasks()...)

	failed, msg := capture(func(tb testing.TB) { tb.Helper(); s.WaitStable(tb, 300*time.Millisecond, time.Second) })
	if !failed || !strings.Contains(msg, "did not hold still") || !strings.Contains(msg, "q quit") {
		t.Fatalf("WaitStable without masks: failed = %v, message:\n%s", failed, msg)
	}
}

// Covers U5: the probe's header, with a duration that grows from 9s to
// 10s, a fill run sized from the width left and a right-aligned item,
// masks to the same text at 9s and at 10s.
func TestTheProbesHeaderMasksTheSameAtNineAndTenSeconds(t *testing.T) {
	s := startProbe(t, probeStartEnv+"=9")

	nine := header(s.WaitForText(t, "up 9s ", wait))
	ten := header(s.WaitForText(t, "up 10s ", wait))

	if strings.Count(nine, "╱") != strings.Count(ten, "╱")+1 {
		t.Fatalf("the fill run did not shrink as the duration grew:\n%s\n%s", nine, ten)
	}
	if a, b := MaskText(nine, DefaultMasks()...), MaskText(ten, DefaultMasks()...); a != b {
		t.Fatalf("masked headers differ:\n%s\n%s", a, b)
	}
}

// header is the first row of a screen's text.
func header(text string) string {
	first, _, _ := strings.Cut(text, "\n")
	return first
}

// Covers U5: the probe's masked screen equals its snapshot.
func TestTheProbesMaskedScreenMatchesItsSnapshot(t *testing.T) {
	s := startProbe(t)

	MatchSnapshot(t, "probe", s.WaitStable(t, 300*time.Millisecond, wait, DefaultMasks()...))
}

// Covers U5: the Check hook runs on every poll of the waits, and its error
// fails the wait at once, with the last screen.
func TestCheckFailsAWaitAtOnce(t *testing.T) {
	s := startProbe(t)
	s.Check = func() error { return errors.New("a violation") }

	for name, waitFor := range map[string]func(testing.TB){
		"WaitForText": func(tb testing.TB) { tb.Helper(); s.WaitForText(tb, "never shown", wait) },
		"WaitStable":  func(tb testing.TB) { tb.Helper(); s.WaitStable(tb, wait, 2*wait) },
	} {
		began := time.Now()
		failed, msg := capture(waitFor)
		if !failed || !strings.Contains(msg, "a violation") || !strings.Contains(msg, "q quit") {
			t.Errorf("%s: failed = %v, message:\n%s", name, failed, msg)
		}
		if took := time.Since(began); took > wait/2 {
			t.Errorf("%s failed after %v, not at once", name, took)
		}
	}
}

// Covers U5 (KTD8): keys sent before the first frame fail the test, since
// the terminal would echo them.
func TestSendBeforeTheFirstFrameFails(t *testing.T) {
	s := StartScreen(t, exec.CommandContext(t.Context(), "sleep", "10"), Size{})

	failed, msg := capture(func(tb testing.TB) { tb.Helper(); s.Send(tb, "q") })

	if !failed || !strings.Contains(msg, "before the first frame") {
		t.Fatalf("Send on a blank screen: failed = %v, message:\n%s", failed, msg)
	}
}

// recorder is a testing.TB that records failures instead of failing the
// test, so a test can assert on the harness's own failures.
type recorder struct {
	testing.TB

	mu     sync.Mutex
	failed bool
	msgs   []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

// capture runs fn with a recorder, in a goroutine so Fatalf can stop it,
// and returns whether it failed and its messages.
func capture(fn func(testing.TB)) (bool, string) {
	r := &recorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(r)
	}()
	<-done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed, strings.Join(r.msgs, "\n")
}
