package screen

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// keyCtrlP is Ctrl+P, as a terminal in raw mode sends it: the key that
	// pauses and resumes crew in the live view.
	keyCtrlP = "\x10"

	// paused is what the header reads while crew is paused, and
	// pausedNothing what it reads once no held issue is left, in the
	// README's words: "The header then reads `PAUSED · 2 running`, or
	// `PAUSED · nothing running` once no held issue is left".
	paused        = "PAUSED"
	pausedTwo     = "PAUSED · 2 running"
	pausedNothing = "PAUSED · nothing running"

	// sortOrder is the title of an issue that gets the ready label while
	// crew is paused.
	sortOrder = "Add a sort order"

	// polls is how long a scenario with a poll every second watches an
	// issue crew must not take: several polls.
	polls = 4 * time.Second

	// keyEffect is how long a scenario gives a key that should change
	// nothing to show an effect on the screen.
	keyEffect = 2 * time.Second

	// limitSeconds is the run time limit of pauseLimitConfig: long enough
	// for crew to start, take the issue and pause before it, short enough
	// for a scenario to wait for it.
	limitSeconds = "15"
)

// pauseConfig is config with a poll every second, so the board shows a change
// on GitHub at once and crew would take a new ready issue within a second, and
// three slots, so two running issues leave a slot free for a third: only the
// pause keeps crew from taking it.
const pauseConfig = "poll_interval_seconds: 1\nmax_parallel_issues: 3\n" + config

// pauseLimitConfig is config with a run time limit of limitSeconds.
const pauseLimitConfig = "run_time_limit_seconds: " + limitSeconds + "\n" + config

// promptFor is the implement action's prompt for the issue titled name.
func promptFor(name string) string {
	return `Implement "` + name + `".`
}

// TestScreenPauseWorkInFlight checks a pause while two issues run.
//
// README: "Ctrl+P pauses crew: it takes no new issue while each issue it
// holds runs on to its end label, and the board keeps refreshing. The header
// then reads `PAUSED · 2 running`, or `PAUSED · nothing running` once no held
// issue is left". crew polls every second and has three slots. It holds two
// issues whose sessions run; Ctrl+P makes the header read PAUSED · 2 running.
// A third issue then gets the ready label: the board shows its card in the
// development column, and over several polls crew does not take it. Both
// sessions then succeed and both held issues reach dev:done; the header reads
// PAUSED · nothing running, and the third issue still carries only dev:ready
// and still shows on the board, as issue #282's AE1 says.
func TestScreenPauseWorkInFlight(t *testing.T) {
	sc := newEmptyScenario(t, pauseConfig)
	first := sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Author: owner, Labels: []string{ready}})
	second := sc.GitHub.AddIssue(fakegithub.Issue{Title: filter, Author: owner, Labels: []string{ready}})
	releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
	sc.Claude.Script(promptFor(title), heldSession(releaseFirst))
	sc.Claude.Script(promptFor(filter), heldSession(releaseSecond))
	sc.Start()
	waitForLabel(sc, first, running)
	waitForLabel(sc, second, running)
	sc.Screen().WaitFor(t, func(text string) bool {
		return inColumn(text, "development", title) && inColumn(text, "development", filter)
	}, timeout)
	sc.Screen().Send(t, keyCtrlP)
	waitHeader(t, sc, pausedTwo)
	third := sc.GitHub.AddIssue(fakegithub.Issue{Title: sortOrder, Author: owner, Labels: []string{ready}})
	sc.Screen().WaitFor(t, func(text string) bool { return inColumn(text, "development", sortOrder) }, timeout)
	time.Sleep(polls)
	wantOnlyLabel(t, sc, third, ready, "after several polls of a paused crew")
	close(releaseFirst)
	close(releaseSecond)
	waitForLabel(sc, first, success)
	waitForLabel(sc, second, success)
	text := waitHeader(t, sc, pausedNothing)
	wantOnlyLabel(t, sc, third, ready, "once the held issues ended")
	if !inColumn(text, "development", sortOrder) {
		t.Errorf("the development column has no card for %q, which carries %s:\n%s", sortOrder, ready, text)
	}
	stop(sc)
}

// TestScreenPauseResume checks a pause with nothing running, then a resume.
//
// README: "The header then reads ... `PAUSED · nothing running` once no held
// issue is left, and Events says when crew pauses and when it resumes. Ctrl+P
// again resumes crew, which lists at once." crew keeps the default poll
// interval of 300 seconds and holds no issue. Ctrl+P makes the header read
// PAUSED · nothing running and Events say crew paused. An issue then gets the
// ready label. Ctrl+P again: the header no longer reads PAUSED, Events says
// crew resumed, and, since crew lists at once rather than at its next poll
// minutes later, it takes the issue within a minute and its session moves it
// to dev:done, as issue #282's AE2 says.
func TestScreenPauseResume(t *testing.T) {
	sc := newEmptyScenario(t, config)
	sc.Claude.Script(prompt, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	sc.Screen().WaitForText(t, "Board", timeout)
	sc.Screen().WaitStable(t, settle, timeout, masks()...)
	sc.Screen().Send(t, keyCtrlP)
	waitHeader(t, sc, pausedNothing)
	sc.Screen().WaitFor(t, func(text string) bool { return inEvents(text, "pause") }, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	wantHeader(t, text, pausedNothing)
	wantOneFrame(t, text)
	harness.MatchSnapshot(t, "paused-nothing-running", text)
	n := sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Author: owner, Labels: []string{ready}})
	sc.Screen().Send(t, keyCtrlP)
	sc.Screen().WaitFor(t, func(text string) bool {
		return !strings.Contains(header(text), paused) && inEvents(text, "resume")
	}, timeout)
	waitForLabel(sc, n, success)
	stop(sc)
}

// TestScreenPauseStopNothingRunning checks stopping a paused crew once nothing
// runs.
//
// README: "To stop crew without losing work ... press Ctrl+P in the live view,
// wait for the header to read `PAUSED · nothing running`, then stop crew:
// nothing runs, so nothing fails", and crew "exits 0 after a stop". crew polls
// every second and holds one issue whose session runs. Ctrl+P pauses it, and
// a second issue gets the ready label and shows on the board. The session then
// succeeds and the held issue reaches dev:done; the header reads PAUSED ·
// nothing running. Two q stop crew, which exits 0: the first issue still
// carries dev:done, not dev:failed, and the second only dev:ready, as issue
// #282's AE3 says.
func TestScreenPauseStopNothingRunning(t *testing.T) {
	sc, n := newScenarioWith(t, pauseConfig)
	session := newWatchedSession()
	sc.Claude.Script(prompt, session.script(false))
	sc.Start()
	waitForLabel(sc, n, running)
	waitClosed(t, session.started, timeout, "the session did not start")
	sc.Screen().WaitForText(t, title, timeout)
	sc.Screen().Send(t, keyCtrlP)
	waitHeader(t, sc, paused)
	other := sc.GitHub.AddIssue(fakegithub.Issue{Title: filter, Author: owner, Labels: []string{ready}})
	sc.Screen().WaitFor(t, func(text string) bool { return inColumn(text, "development", filter) }, timeout)
	close(session.release)
	waitForLabel(sc, n, success)
	text := waitHeader(t, sc, pausedNothing)
	r := &liveRun{sc: sc, n: n, session: session, usual: footer(text)}
	r.press(t, keyQ)
	r.waitArmed(t)
	r.press(t, keyQ)
	r.wantExit(t, exitClean, timeout)
	wantOnlyLabel(t, sc, n, success, "after a paused crew with nothing running stopped")
	wantOnlyLabel(t, sc, other, ready, "after a paused crew with nothing running stopped")
}

// TestScreenPauseStopWhileRunning checks stopping a paused crew while an issue
// still runs.
//
// README: "Stopping a paused crew while issues still run stops them as
// above": crew "asks each running session and script to stop", "An action
// crew stopped gives `failed`, and every run whose action ends while crew
// stops ends through its `failed` route", and crew "exits 0 after a stop".
// crew holds one issue whose session runs; Ctrl+P pauses it, then two q make
// crew ask the session to stop, move the issue to dev:failed and exit 0, as
// issue #282's AE4 says.
func TestScreenPauseStopWhileRunning(t *testing.T) {
	r := startLive(t, config, false)
	r.press(t, keyCtrlP)
	waitHeader(t, r.sc, paused)
	r.press(t, keyQ)
	r.waitArmed(t)
	r.press(t, keyQ)
	r.wantStopped(t, "two q on a paused crew")
	waitForLabel(r.sc, r.n, failure)
	r.wantExit(t, exitClean, timeout)
}

// TestScreenPauseRunTimeLimit checks the run time limit reached while crew is
// paused.
//
// README: "A pause lasts only for the running crew: ... a stop or the run time
// limit ends it", and at `run_time_limit_seconds` "crew takes nothing new,
// lets each running action finish and starts no other", exiting "0 ... at its
// run time limit". With a limit of 15 seconds, crew holds one issue whose
// session runs and Ctrl+P pauses it. Once the limit passes, the header no
// longer reads PAUSED, crew never asked the session to stop, and Ctrl+P
// brings no pause back. The session then succeeds, the issue leaves
// dev:running for one of the rule's end labels, and crew exits 0 by itself,
// as issue #282's AE6 says.
func TestScreenPauseRunTimeLimit(t *testing.T) {
	r := startLive(t, pauseLimitConfig, false)
	r.press(t, keyCtrlP)
	waitHeader(t, r.sc, paused)
	r.sc.Screen().WaitFor(t, func(text string) bool { return !strings.Contains(header(text), paused) }, timeout)
	r.wantRunning(t, "once the run time limit ended the pause")
	r.press(t, keyCtrlP)
	time.Sleep(keyEffect)
	if text := r.sc.Screen().Text(); strings.Contains(header(text), paused) {
		t.Errorf("Ctrl+P paused crew during its wind-down: the header reads %q:\n%s", header(text), text)
	}
	r.wantRunning(t, "after Ctrl+P during the wind-down")
	close(r.session.release)
	r.sc.Wait(func() bool {
		issue, _ := r.sc.GitHub.Issue(r.n)
		return slices.Contains(issue.Labels, success) || slices.Contains(issue.Labels, failure)
	}, timeout)
	r.wantExit(t, exitClean, timeout)
}

// TestScreenPauseFromEvents checks Ctrl+P while Events has focus.
//
// Edge case: with Events focused, Ctrl+P pauses crew, the header reads
// PAUSED, and Ctrl+P again resumes it, the header no longer reading PAUSED.
// The README says "Ctrl+P pauses crew" with no word on which section must
// have focus, unlike the arrows, which it ties to the board, the box or Bots.
// A user who reads Events while deciding to stop for the day should not have
// to go back to the board first.
func TestScreenPauseFromEvents(t *testing.T) {
	r := startLive(t, config, false)
	r.press(t, "e")
	r.sc.Screen().WaitFor(t, func(text string) bool { return focused(text, "Events") }, timeout)
	r.press(t, keyCtrlP)
	waitHeader(t, r.sc, paused)
	r.press(t, keyCtrlP)
	r.sc.Screen().WaitFor(t, func(text string) bool { return !strings.Contains(header(text), paused) }, timeout)
	r.wantRunning(t, "after a pause and a resume")
	close(r.session.release)
	waitForLabel(r.sc, r.n, success)
	stop(r.sc)
}

// TestScreenPauseKey checks that the list of keys names Ctrl+P.
//
// README: "`?` lists every key", and Ctrl+P is one: "Ctrl+P pauses crew".
// The scenario reads only the rows `?` changed, which name Esc, b and e, as
// TestScreenKeys checks, and wants them to name Ctrl+P too, written ctrl+p or
// ^P, in any case.
func TestScreenPauseKey(t *testing.T) {
	r := startLive(t, config, false)
	board := r.sc.Screen().Masked(masks()...)
	r.press(t, "?")
	text := r.sc.Screen().WaitFor(t, func(text string) bool {
		added := addedRows(board, harness.MaskText(text, masks()...))
		return namesKey(added, "esc") && namesKey(added, "b") && namesKey(added, "e")
	}, timeout)
	added := strings.ToLower(addedRows(board, harness.MaskText(text, masks()...)))
	if !strings.Contains(added, "ctrl+p") && !strings.Contains(added, "^p") {
		t.Errorf("the list of keys does not name Ctrl+P:\n%s", text)
	}
	close(r.session.release)
	waitForLabel(r.sc, r.n, success)
	stop(r.sc)
}

// header is the live view's header row in the screen text: the row that
// starts with crew and names the repository, not a line crew printed while it
// started.
func header(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "crew ") && strings.Contains(line, harness.RepositoryName) {
			return line
		}
	}
	return ""
}

// waitHeader waits until the header row holds want, and returns the screen.
func waitHeader(t *testing.T, sc *harness.Scenario, want string) string {
	t.Helper()
	return sc.Screen().WaitFor(t, func(text string) bool { return strings.Contains(header(text), want) }, timeout)
}

// wantHeader fails the test unless the header row of text holds want.
func wantHeader(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(header(text), want) {
		t.Errorf("the header does not read %q:\n%s", want, text)
	}
}

// wantOnlyLabel fails the test unless the issue number carries label and no
// other of the rule's labels, after what happened.
func wantOnlyLabel(t *testing.T, sc *harness.Scenario, number int, label, after string) {
	t.Helper()
	issue, _ := sc.GitHub.Issue(number)
	for _, other := range []string{ready, running, success, failure} {
		if slices.Contains(issue.Labels, other) != (other == label) {
			t.Errorf("#%d carries %q %s, want only %s of the rule's labels", number, issue.Labels, after, label)
			return
		}
	}
}

// focused reports whether the section name has focus: the focus marker comes
// right before its title, which may share its row with another section's.
func focused(text, name string) bool {
	return strings.Contains(text, "▸ "+name+" ")
}
