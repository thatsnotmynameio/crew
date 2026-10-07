package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

var ctrlP = tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}

// paused returns u with crew paused.
func paused(u engine.Update) updateMsg {
	u.Snapshot.Paused = true
	return updateMsg(u)
}

// pausedIdle is a paused crew that holds no issue.
func pausedIdle() updateMsg {
	return updateMsg(engine.Update{Snapshot: engine.Snapshot{View: core.View{Paused: true}}})
}

// Covers AE1 of #282 (R1, R6): Ctrl-P asks the engine to pause, and the
// header, the window title and the footer follow the snapshot.
func TestAE1CtrlPPausesAndTheHeaderCountsTheIssuesStillRunning(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))

	if cmd := h.send(ctrlP); quits(cmd) {
		t.Fatal("Ctrl-P quit the view")
	}
	if h.pauses != 1 || h.stops != 0 {
		t.Fatalf("after Ctrl-P: pause called %d times, stop %d; want 1 and 0", h.pauses, h.stops)
	}
	if got := firstLine(h.view()); strings.Contains(got, "PAUSED") {
		t.Errorf("header = %q before the engine paused, want no pause shown", got)
	}

	h.send(paused(runningSnapshot()))
	if got := firstLine(h.view()); !strings.HasSuffix(got, " PAUSED · 2 running ") {
		t.Errorf("header = %q, want the PAUSED pill with 2 running", got)
	}
	if got := h.model.View().WindowTitle; got != "crew · paused" {
		t.Errorf("title = %q, want crew · paused", got)
	}
	if got, want := h.footer(), stopKeys+" stop · ctrl+p resume · tab focus · ←→↑↓ move · enter open · ? help"; got != want {
		t.Errorf("key help = %q, want %q", got, want)
	}
}

// Covers AE1 of #282 (R6): once nothing runs, the header says so.
func TestAE1APausedCrewWithNothingRunningSaysSo(t *testing.T) {
	h := newHarness(t, 80)

	h.send(pausedIdle())

	if got := firstLine(h.view()); !strings.HasSuffix(got, " PAUSED · nothing running ") {
		t.Errorf("header = %q, want the PAUSED pill saying nothing runs", got)
	}
}

// Covers AE2 of #282 (R4): Ctrl-P again asks to resume, and the header
// drops the pause once the engine resumed.
func TestAE2CtrlPAgainResumes(t *testing.T) {
	h := newHarness(t, 80)
	h.send(pausedIdle())

	h.send(ctrlP)
	if h.pauses != 1 {
		t.Fatalf("pause called %d times, want 1", h.pauses)
	}

	h.send(updateMsg(runningSnapshot()))
	if got := firstLine(h.view()); !strings.HasSuffix(got, "? help") {
		t.Errorf("header = %q, want the key for help again", got)
	}
}

// Covers AE6 of #282 (R10): while crew winds down, Ctrl-P does nothing,
// the header shows the wind-down and the footer leaves Ctrl-P out.
func TestAE6WhileWindingDownCtrlPDoesNothing(t *testing.T) {
	h := newHarness(t, 80)
	u := windingDownSnapshot()
	u.Snapshot.Paused = true
	h.send(updateMsg(u))

	h.send(ctrlP)

	if h.pauses != 0 {
		t.Errorf("pause called %d times while winding down, want 0", h.pauses)
	}
	if got := firstLine(h.view()); !strings.HasSuffix(got, " WINDING DOWN ") {
		t.Errorf("header = %q, want the WINDING DOWN pill", got)
	}
	if got := h.footer(); strings.Contains(got, "ctrl+p") {
		t.Errorf("key help = %q while winding down, want no ctrl+p", got)
	}
}

// Covers R10 of #282: once crew is stopping, by a key or by the engine,
// Ctrl-P does nothing.
func TestWhileStoppingCtrlPDoesNothing(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))
	h.send(qKey)
	h.send(qKey)
	h.send(ctrlP)
	if h.pauses != 0 {
		t.Errorf("pause called %d times after a confirmed stop, want 0", h.pauses)
	}

	h = newHarness(t, 80)
	u := runningSnapshot()
	u.Snapshot.Stopping = true
	h.send(updateMsg(u))
	h.send(ctrlP)
	if h.pauses != 0 {
		t.Errorf("pause called %d times while the engine stops, want 0", h.pauses)
	}
}

// Ctrl-P works with the popup open, with the help open and while the stop
// is armed, which it leaves armed.
func TestCtrlPWorksOverThePopupTheHelpAndAnArmedStop(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handledSnapshot()))

	h.send(enterKey)
	h.send(ctrlP)
	if !popupShows(h) {
		t.Error("Ctrl-P closed the popup")
	}
	h.send(escKey)

	h.send(helpKey)
	h.send(ctrlP)
	h.send(helpKey)

	h.send(qKey)
	h.send(ctrlP)
	if got := h.footer(); !strings.Contains(got, "again within") {
		t.Errorf("key help = %q, want the armed notice kept", got)
	}

	if h.pauses != 3 || h.stops != 0 {
		t.Errorf("pause called %d times, stop %d; want 3 and 0", h.pauses, h.stops)
	}
}

// Covers R8 of #282: the help overlay lists Ctrl-P.
func TestTheHelpOverlayListsCtrlP(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.send(helpKey)

	if !regexp.MustCompile(`\bctrl\+p +pause or resume\b`).MatchString(h.view()) {
		t.Errorf("the keys lack ctrl+p:\n%s", h.view())
	}
}
