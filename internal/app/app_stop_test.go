package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

func TestASignalStopsCrewWithExitZeroAfterTheStopSequence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		r := options(t, oneAction, tr, h)
		r.start()
		session := next(t, h)

		r.signals <- syscall.SIGTERM

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
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

// child starts a long sleep in group, standing in for a session's process.
func child(t *testing.T, group *proc.Group) *proc.Process {
	t.Helper()
	p, err := group.Start(proc.Command{Name: "sleep", Args: []string{"60"}}, nil, nil)
	if err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p
}

// killed fails the test unless p ends within a few seconds.
func killed(t *testing.T, p *proc.Process) {
	t.Helper()
	ended := make(chan error, 1)
	go func() { ended <- p.Wait() }()
	select {
	case err := <-ended:
		if err == nil {
			t.Error("the child exited cleanly, want it killed")
		}
	case <-time.After(5 * time.Second):
		t.Error("the child still runs: the forced exit did not kill it")
	}
}

// code waits for app.Run's exit code, failing the test after a few seconds.
func (r *crewRun) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case code := <-r.code:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("crew did not return")
		return -1
	}
}

// eventually reports whether stdout satisfies ok within a few seconds.
func (r *crewRun) eventually(ok func(stdout string) bool) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok(r.stdout.String()) {
			return true
		}
	}
	return false
}

// release ends session and waits for the engine's last event line, so the
// engine is done with the repository before the test removes it.
func (r *crewRun) release(session *fake.Session) {
	session.End(port.SessionEnd{Reason: "released by the test"})
	r.eventually(func(out string) bool { return strings.Contains(out, "crew: stopped") })
}

func TestASecondSignalKillsEveryProcessAndExitsOneAtOnce(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	h.IgnoreStop(true) // the stop sequence would wait 10 seconds for it
	r := options(t, oneAction, tr, h)
	sleeper := child(t, r.opts.Group)
	r.start()
	session := next(t, h)

	r.signals <- syscall.SIGINT
	r.signals <- syscall.SIGINT

	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	killed(t, sleeper)
	r.release(session)
}

// tuiRun returns a run whose stdout is a terminal, with keys typed through
// the returned writer.
func tuiRun(t *testing.T, tr port.Tracker, h port.Harness) (*crewRun, io.WriteCloser) {
	t.Helper()
	r := options(t, oneAction, tr, h)
	keys, typed := io.Pipe()
	t.Cleanup(func() { _ = typed.Close() })
	r.opts.Terminal, r.opts.Stdin = true, keys
	return r, typed
}

func TestOnATerminalQuittingTheTUIStopsCrewWithExitZero(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	r, keys := tuiRun(t, tr, h)
	r.start()
	session := next(t, h)

	if _, err := io.WriteString(keys, "qq"); err != nil {
		t.Fatal(err)
	}

	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	if !session.Stopped() {
		t.Error("the running session was not stopped")
	}
	if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
		t.Errorf("#1 is in %v when crew returned, want needs attention", got)
	}
	// What the TUI draws is its own tests' business; here the output only
	// shows that the boot log came first, then the TUI instead of the line
	// renderer.
	out := r.stdout.String()
	boot, tui, ok := strings.Cut(out, "\x1b[")
	want := []string{"loading config", "reading the run journal"}
	if got := unstamped(t, boot); !ok || !slices.Equal(got, want) || strings.Contains(tui, " crew: ") {
		t.Errorf("stdout is not the boot log %q then the TUI's alone:\n%q", want, out)
	}
}

func TestOnATerminalPressingQOnceMoreWhileStoppingKillsEveryProcessAndExitsOne(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := fake.NewHarness()
	h.IgnoreStop(true)
	r, keys := tuiRun(t, tr, h)
	sleeper := child(t, r.opts.Group)
	r.start()
	session := next(t, h)

	if _, err := io.WriteString(keys, "qqq"); err != nil {
		t.Fatal(err)
	}

	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	killed(t, sleeper)
	releaseAfterTUI(t, r, session)
}

// releaseAfterTUI ends session after a forced exit from the TUI. The engine
// outlives the forced exit, which a real crew would not, and records the
// released session's end; it waits for that to land before the repository
// is removed. The TUI prints no "crew: stopped" for release to wait for.
func releaseAfterTUI(t *testing.T, r *crewRun, session *fake.Session) {
	t.Helper()
	session.End(port.SessionEnd{Reason: "released by the test"})
	journal := filepath.Join(r.opts.Root, ".crew", "logs", "runs.jsonl")
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if data, _ := os.ReadFile(journal); strings.Contains(string(data), `"event":"ended"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the released session's end never reached %s", journal)
		}
	}
}

// SIGHUP comes when the terminal crew runs in closes.
func TestSIGHUPStopsCrewLikeSIGTERM(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		r := options(t, oneAction, tr, h)
		r.start()
		session := next(t, h)

		r.signals <- syscall.SIGHUP

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if !session.Stopped() {
			t.Error("the running session was not stopped")
		}
		if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("#1 is in %v when crew returned, want needs attention", got)
		}
	})
}
