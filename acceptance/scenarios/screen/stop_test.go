package screen

import (
	"context"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// keyQ and keyCtrlC are the two keys that stop crew in the live view, as a
	// terminal in raw mode sends them; keyTab moves the focus.
	keyQ     = "q"
	keyCtrlC = "\x03"
	keyTab   = "\t"

	// window is how long a first press waits for a second, in the README's
	// "Stopping crew": "pressed twice within 3 seconds".
	window = 3 * time.Second

	// windowSlack is how long after the window the scenarios give the footer
	// to show its usual key help again.
	windowSlack = 2 * time.Second

	// insideWindow is a gap between two presses near the window's end, a
	// second inside it.
	insideWindow = 2 * time.Second

	// pastWindow is a gap between two presses a second past the window, as
	// issue #266's AE5 presses again "4 seconds later".
	pastWindow = 4 * time.Second

	// pressGap is how long a forcing scenario leaves between the start of a
	// stop and the press that forces it: well inside the ten seconds a stop
	// gives each running session ("giving it up to ten seconds"), so the stop
	// still waits for its session.
	pressGap = 2 * time.Second

	// forceWithin bounds how long crew may take to exit after a forcing press,
	// which "exits at once". pressGap plus forceWithin stays under those ten
	// seconds, so a crew that ignored the press and ended when the stop's
	// grace ran out still fails.
	forceWithin = 5 * time.Second

	// goneWithin bounds how long a session may outlive crew after a forcing
	// press, which "kills every process crew started".
	goneWithin = 5 * time.Second

	// limitConfig is config with a run time limit of one second. The screen
	// holds still for settle, longer than the limit, before a scenario
	// presses a key, so crew is winding down by then.
	limitConfig = "run_time_limit_seconds: 1\n" + config

	// exitClean and exitForced are the README's exit codes: "crew exits 0
	// after a stop or at its run time limit, 1 when ... a second stop forced
	// its exit".
	exitClean  = 0
	exitForced = 1
)

// TestScreenStopOnePressKeepsRunning checks a single Ctrl+C in the live view.
//
// README: "In the live view, `q` or Ctrl+C stops crew only when pressed twice
// within 3 seconds, in any mix: the first press only says in the footer that
// another stops crew, so a stray press costs nothing. When 3 seconds pass
// without a second press, crew carries on". With a session running, one
// Ctrl+C changes the footer to a notice about stopping, and within the window
// and two seconds the footer shows its usual key help again, as issue #266's
// AE1 says. crew never asked the session to stop and the issue still carries
// the running label. The session then succeeds and crew moves the issue to
// the success label.
func TestScreenStopOnePressKeepsRunning(t *testing.T) {
	r := startLive(t, config, false)
	r.press(t, keyCtrlC)
	r.waitArmed(t)
	r.waitUsual(t)
	r.wantRunning(t, "after a single Ctrl+C and the window")
	close(r.session.release)
	waitForLabel(r.sc, r.n, success)
	stop(r.sc)
}

// TestScreenStopTwoPresses checks two presses inside the window.
//
// README: "`q` or Ctrl+C stops crew only when pressed twice within 3 seconds,
// in any mix", and then "crew takes nothing new, starts no other action, and
// asks each running session and script to stop ... An action crew stopped
// gives `failed`, and every run whose action ends while crew stops ends
// through its `failed` route. ... crew exits once every run it held has
// ended", exiting "0 after a stop". Each mix of the two
// keys, pressed one after the other, and two q two seconds apart, near the
// window's end, make crew ask the running session to stop, move the issue to
// the failure label and exit 0.
func TestScreenStopTwoPresses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		first, second string
		gap           time.Duration
	}{
		{name: "q then q", first: keyQ, second: keyQ},
		{name: "q then ctrl+c", first: keyQ, second: keyCtrlC},
		{name: "ctrl+c then q", first: keyCtrlC, second: keyQ},
		{name: "ctrl+c then ctrl+c", first: keyCtrlC, second: keyCtrlC},
		{name: "q then q two seconds later", first: keyQ, second: keyQ, gap: insideWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := startLive(t, config, false)
			pressed := time.Now()
			r.press(t, tc.first)
			r.waitArmed(t)
			time.Sleep(time.Until(pressed.Add(tc.gap)))
			r.press(t, tc.second)
			r.wantStopped(t, "two presses inside the window")
			waitForLabel(r.sc, r.n, failure)
			r.wantExit(t, exitClean, timeout)
		})
	}
}

// TestScreenStopPressAfterWindow checks two presses further apart than the
// window.
//
// README: "When 3 seconds pass without a second press, crew carries on and the
// next press asks again." A q, then another four seconds later, as issue
// #266's AE5 presses them: by then the footer shows its usual key help again,
// and the second q only changes it to the notice again, which then goes back
// to the usual key help. crew never asked the running session to stop, and
// the session then succeeds.
func TestScreenStopPressAfterWindow(t *testing.T) {
	r := startLive(t, config, false)
	pressed := time.Now()
	r.press(t, keyQ)
	r.waitArmed(t)
	r.waitUsual(t)
	time.Sleep(time.Until(pressed.Add(pastWindow)))
	r.press(t, keyQ)
	r.waitArmed(t)
	r.wantRunning(t, "after a second q four seconds after the first")
	r.waitUsual(t)
	r.wantRunning(t, "after the window of the second q")
	close(r.session.release)
	waitForLabel(r.sc, r.n, success)
	stop(r.sc)
}

// TestScreenStopForcesAfterPresses checks one press once two presses started
// a stop.
//
// README: "Once crew is stopping, whatever started the stop, one more `q` or
// Ctrl+C in the live view, or a second signal, kills every process crew
// started and exits at once", and crew exits "1 when ... a second stop forced
// its exit". crew holds one issue whose session ignores the request to stop.
// Two q stop crew; two seconds later, inside the ten seconds the stop gives
// the session, the issue still carries the running label. One more q, or one
// Ctrl+C, makes crew exit 1 within five seconds, and the session is gone.
func TestScreenStopForcesAfterPresses(t *testing.T) {
	t.Parallel()
	for _, key := range []string{keyQ, keyCtrlC} {
		t.Run(keyName(key), func(t *testing.T) {
			t.Parallel()
			r := startLive(t, config, true)
			r.press(t, keyQ)
			r.waitArmed(t)
			r.press(t, keyQ)
			r.wantForced(t, key)
		})
	}
}

// TestScreenStopForcesAfterSignal checks one press once a signal started a
// stop.
//
// README: "Once crew is stopping, whatever started the stop, one more `q` or
// Ctrl+C in the live view ... kills every process crew started and exits at
// once", and crew exits "1 when ... a second stop forced its exit". crew, in
// the live view, holds one issue whose session ignores the request to stop,
// and SIGTERM stops it. Two seconds later the issue still carries the running
// label, and one q, or one Ctrl+C, makes crew exit 1 within five seconds, and
// the session is gone, as issue #266's AE4 says.
func TestScreenStopForcesAfterSignal(t *testing.T) {
	t.Parallel()
	for _, key := range []string{keyQ, keyCtrlC} {
		t.Run(keyName(key), func(t *testing.T) {
			t.Parallel()
			r := startLive(t, config, true)
			r.sc.Screen().Signal(t, syscall.SIGTERM)
			r.wantForced(t, key)
		})
	}
}

// TestScreenStopWindDown checks presses during a wind-down.
//
// README: "`run_time_limit_seconds` ends crew another way. crew takes nothing
// new, lets each running action finish and starts no other. ... This
// wind-down is not a stop, so stopping it from the live view still takes two
// presses."
// With a run time limit of one second, past it, a single q only changes the
// footer, which goes back to its usual key help, and crew never asks the
// running session to stop. Two q then make crew ask the session to stop, move
// the issue to the failure label and exit 0, as issue #266's AE6 says.
func TestScreenStopWindDown(t *testing.T) {
	r := startLive(t, limitConfig, false)
	r.press(t, keyQ)
	r.waitArmed(t)
	r.waitUsual(t)
	r.wantRunning(t, "after a single q during a wind-down")
	r.press(t, keyQ)
	r.waitArmed(t)
	r.press(t, keyQ)
	r.wantStopped(t, "two q during a wind-down")
	waitForLabel(r.sc, r.n, failure)
	r.wantExit(t, exitClean, timeout)
}

// TestScreenStopOtherKeyKeepsArmed checks a key pressed between the two
// presses.
//
// Edge case: a q, then Tab, then a q inside the window stops crew: crew asks
// the running session to stop, moves the issue to the failure label and exits
// 0. The README says the two presses may come "in any mix" of `q` and Ctrl+C,
// but not what another key between them does. A user who moves around the
// view while deciding to stop should not have to start over, and issue #266's
// R5 says other keys neither cancel nor extend the window.
func TestScreenStopOtherKeyKeepsArmed(t *testing.T) {
	r := startLive(t, config, false)
	r.press(t, keyQ)
	r.waitArmed(t)
	r.press(t, keyTab)
	r.press(t, keyQ)
	r.wantStopped(t, "q, Tab, then q inside the window")
	waitForLabel(r.sc, r.n, failure)
	r.wantExit(t, exitClean, timeout)
}

// TestScreenStopPlainInterrupt checks Ctrl+C under --plain in a terminal.
//
// README: "With `--plain`, Ctrl+C stops crew at once, as SIGINT, SIGTERM and
// SIGHUP do." crew runs with --plain in a terminal, which turns Ctrl+C into
// SIGINT. One SIGINT, with a session running, makes crew ask the session to
// stop, move the issue to the failure label and exit 0: the two presses of the
// live view do not apply, as issue #266's AE7 says.
func TestScreenStopPlainInterrupt(t *testing.T) {
	sc := newEmptyScenario(t, config, "--plain")
	n := sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Author: owner, Labels: []string{ready}})
	session := newWatchedSession()
	sc.Claude.Script(prompt, session.script(false))
	sc.Start()
	waitForLabel(sc, n, running)
	waitClosed(t, session.started, timeout, "the session did not start")
	sc.Screen().Signal(t, syscall.SIGINT)
	r := &liveRun{sc: sc, n: n, session: session}
	r.wantStopped(t, "a single Ctrl+C under --plain")
	waitForLabel(sc, n, failure)
	r.wantExit(t, exitClean, timeout)
}

// liveRun is crew running in the live view with the scenarios' issue, whose
// session is session.
type liveRun struct {
	sc      *harness.Scenario
	n       int
	session *watchedSession
	// usual is the footer's usual key help, read once the screen held still.
	usual string
}

// startLive starts crew in the live view with cfg and the scenarios' issue,
// whose session ignores the request to stop when slow is set, waits until the
// session runs and the screen holds still, and reads the footer's usual key
// help.
func startLive(t *testing.T, cfg string, slow bool) *liveRun {
	t.Helper()
	sc, n := newScenarioWith(t, cfg)
	session := newWatchedSession()
	sc.Claude.Script(prompt, session.script(slow))
	sc.Start()
	waitForLabel(sc, n, running)
	waitClosed(t, session.started, timeout, "the session did not start")
	sc.Screen().WaitForText(t, title, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	return &liveRun{sc: sc, n: n, session: session, usual: footer(text)}
}

// press types key into the live view.
func (r *liveRun) press(t *testing.T, key string) {
	t.Helper()
	r.sc.Screen().Send(t, key)
}

// waitArmed waits until the footer no longer shows its usual key help but a
// notice that names stopping.
func (r *liveRun) waitArmed(t *testing.T) {
	t.Helper()
	text := r.sc.Screen().WaitFor(t, func(text string) bool {
		line := footer(text)
		return line != r.usual && strings.Contains(strings.ToLower(line), "stop")
	}, timeout)
	t.Logf("the footer shows %q instead of %q", footer(text), r.usual)
}

// waitUsual waits, no longer than the window and its slack, until the footer
// shows its usual key help again.
func (r *liveRun) waitUsual(t *testing.T) {
	t.Helper()
	r.sc.Screen().WaitFor(t, func(text string) bool { return footer(text) == r.usual }, window+windowSlack)
}

// wantRunning fails the test when crew asked the session to stop, or the
// issue no longer carries the running label, after what happened.
func (r *liveRun) wantRunning(t *testing.T, after string) {
	t.Helper()
	select {
	case <-r.session.ended:
		t.Errorf("crew stopped the running session %s", after)
	default:
	}
	if issue, _ := r.sc.GitHub.Issue(r.n); !slices.Contains(issue.Labels, running) {
		t.Errorf("the issue carries %q %s, want %s", issue.Labels, after, running)
	}
}

// wantStopped fails the test unless crew asks the session to stop within
// timeout, after what happened.
func (r *liveRun) wantStopped(t *testing.T, after string) {
	t.Helper()
	waitClosed(t, r.session.ended, timeout, "crew did not stop the running session after "+after)
}

// wantExit fails the test unless crew exits with code within within.
func (r *liveRun) wantExit(t *testing.T, code int, within time.Duration) {
	t.Helper()
	if exited := r.sc.Exit(within); exited.Code != code {
		t.Errorf("crew exited %d, want %d", exited.Code, code)
	}
}

// wantForced waits pressGap after crew started stopping, checks that it still
// waits for the session, which ignores the request to stop, then presses key
// once and wants crew to exit 1 within forceWithin and the session gone.
func (r *liveRun) wantForced(t *testing.T, key string) {
	t.Helper()
	time.Sleep(pressGap)
	if issue, _ := r.sc.GitHub.Issue(r.n); !slices.Contains(issue.Labels, running) {
		t.Fatalf("%s into the stop, the issue whose session still worked carries %q, not %s: "+
			"the stop did not wait for it, so a press has nothing to force", pressGap, issue.Labels, running)
	}
	r.press(t, key)
	r.wantExit(t, exitForced, forceWithin)
	waitClosed(t, r.session.ended, goneWithin, "the session still ran after a press forced crew's exit")
}

// watchedSession plays the scenarios' issue's session: it says said and works
// until release is closed, when it succeeds, or until crew ends it.
type watchedSession struct {
	// started is closed once the session runs.
	started chan struct{}
	// ended is closed when crew ended the session: asked it to stop or, for
	// a session that ignores that request, killed it.
	ended chan struct{}
	// release, when closed, lets the session succeed.
	release chan struct{}
}

// newWatchedSession returns a session that has not started.
func newWatchedSession() *watchedSession {
	return &watchedSession{started: make(chan struct{}), ended: make(chan struct{}), release: make(chan struct{})}
}

// script is the session's script. With slow set, the session ignores the
// request to stop, as a session slow to stop does, so only a kill ends it.
func (w *watchedSession) script(slow bool) fakeclaude.ScriptFunc {
	return func(ctx context.Context, s *fakeclaude.Session) int {
		if slow {
			s.IgnoreStop()
		}
		_ = s.Emit(s.Init(), s.Said(said))
		close(w.started)
		select {
		case <-w.release:
			_ = s.Emit(s.Success("Added the search box."))
			return 0
		case <-ctx.Done():
			close(w.ended)
			return 1
		}
	}
}

// waitClosed fails the test with message unless ch is closed within within.
func waitClosed(t *testing.T, ch <-chan struct{}, within time.Duration, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(within):
		t.Fatalf("%s within %s", message, within)
	}
}

// footer is the footer of the screen text: its last row that is not empty.
func footer(text string) string {
	lines := strings.Split(strings.TrimRight(text, " \n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// keyName names key for a subtest.
func keyName(key string) string {
	if key == keyCtrlC {
		return "ctrl+c"
	}
	return key
}
