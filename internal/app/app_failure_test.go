package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// errStdoutGone is what a failingStdout returns once it fails.
var errStdoutGone = errors.New("stdout is gone")

// failingStdout records what is written to it, until a write holds failOn;
// that write, and every later one, returns errStdoutGone, as a closed pipe
// would.
type failingStdout struct {
	syncBuffer

	failOn string
	failed bool
}

func (w *failingStdout) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed || strings.Contains(string(p), w.failOn) {
		w.failed = true
		return 0, errStdoutGone
	}
	n, _ := w.buf.Write(p) // a bytes.Buffer's Write always returns a nil error
	return n, nil
}

func TestAFailingRendererStopsTheEngineThroughItsStopSequenceAndExitsOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		r := options(t, oneAction, tr, h)
		// The line for the session's start fails, so the session runs when
		// the renderer ends.
		r.opts.Stdout = &failingStdout{failOn: " started its session"}
		r.start()
		session := next(t, h)

		var code int
		select {
		case code = <-r.code:
		case <-time.After(time.Hour):
			r.signals <- syscall.SIGTERM
			<-r.code
			t.Fatal("crew ran on for an hour after its renderer failed")
		}

		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if stderr, want := r.stderr.String(), "crew: print event line: stdout is gone; stopping\n"; stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
		if !session.Stopped() {
			t.Error("the running session was not stopped")
		}
		if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("#1 is in %v when crew returned, want needs attention", got)
		}
		if n := len(tr.Reports()); n != 1 {
			t.Errorf("crew posted %d failure reports, want 1", n)
		}
	})
}

// panickingHarness is a fake harness whose sessions implement port.Narrator
// with a Said that panics, so the engine's loop panics on the first tick
// after a session started: a defect in the loop, as nothing else makes the
// engine's Run fail once the environment checks passed.
type panickingHarness struct {
	*fake.Harness
}

func (h panickingHarness) Start(ctx context.Context, run port.Run) (port.Session, error) {
	s, err := h.Harness.Start(ctx, run)
	return panickingSession{s}, err
}

type panickingSession struct {
	port.Session
}

func (panickingSession) Said() string {
	panic("the narrator broke")
}

// A real child process and a real one-second tick, not synctest: the forced
// kill is only seen on a real process.
func TestAFailingEngineKillsEveryProcessAndExitsOne(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := panickingHarness{fake.NewHarness()}
	body := "poll_interval_seconds: 1\n" + oneAction
	r := options(t, body, tr, h)
	sleeper := child(t, r.opts.Group) // a session's process, still running
	r.start()
	session := next(t, h.Harness)

	code := r.exitCode(t)
	// The engine's loop is gone, so nothing stops the session but the test.
	session.End(port.SessionEnd{Reason: "released by the test"})
	sessionKept(t, r.opts.Root, "issue-1-implement")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr := r.stderr.String(); !strings.HasPrefix(stderr, "crew: the engine failed: panic: the narrator broke\n") {
		t.Errorf("stderr = %q, want it to say the engine failed, with the panic", stderr)
	}
	killed(t, sleeper)
}

// sessionKept waits until the engine kept the ended session's prompt and
// last message beside the log of the run in workspace, under root, which
// it does after the session ended even when its loop is gone; the test's
// cleanup would otherwise race those writes. It fails the test after a few
// seconds.
func sessionKept(t *testing.T, root string, workspace crew.WorkspaceName) {
	t.Helper()
	base := filepath.Join(root, ".crew", "logs", string(workspace))
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		_, prompt := os.Stat(base + ".prompt")
		_, last := os.Stat(base + ".last-message")
		if prompt == nil && last == nil {
			return
		}
	}
	t.Fatal("the engine never kept the ended session's prompt and last message")
}

// cursorShown is how the TUI restores the terminal's cursor as it ends.
const cursorShown = "\x1b[?25h"

func TestOnATerminalASecondSignalEndsTheTUIKillsEveryProcessAndExitsOne(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	h.IgnoreStop(true) // the stop sequence would wait 10 seconds for it
	r, _ := tuiRun(t, tr, h)
	sleeper := child(t, r.opts.Group)
	r.start()
	session := next(t, h)

	r.signals <- syscall.SIGINT
	r.signals <- syscall.SIGINT

	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	// Read as crew returns: the TUI restored the terminal before that.
	if out := r.stdout.String(); !strings.Contains(out, cursorShown) {
		t.Errorf("stdout lacks the TUI's restored cursor %q; it is:\n%q", cursorShown, out)
	}
	if stderr, want := r.stderr.String(), "crew: forced exit; every process crew started was killed\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	killed(t, sleeper)
	releaseAfterTUI(t, r, session)
}
