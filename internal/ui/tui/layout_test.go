package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

var (
	tab     = tea.KeyPressMsg{Code: tea.KeyTab}
	upKey   = tea.KeyPressMsg{Code: tea.KeyUp}
	downKey = tea.KeyPressMsg{Code: tea.KeyDown}
	endKey  = tea.KeyPressMsg{Code: tea.KeyEnd}
	homeKey = tea.KeyPressMsg{Code: tea.KeyHome}
	helpKey = tea.KeyPressMsg{Code: '?', Text: "?"}
)

// eventful is manySnapshot with 30 numbered poll events, the newest last.
func eventful() engine.Update {
	u := manySnapshot()
	u.Snapshot.Recent = nil
	for n := 1; n <= 30; n++ {
		u.Snapshot.Recent = append(u.Snapshot.Recent,
			core.PollDone{At: start.Add(time.Duration(n-31) * time.Second), Listed: n, Taken: 0})
	}
	return u
}

// Covers R1: every section on one screen, in order.
func TestEverySectionShowsInOrder(t *testing.T) {
	view := fitted(t, 80, 0, handledSnapshot())

	last := -1
	for _, title := range []string{"crew ╱", "Workflow ─", "Actions ─", "Queues ─", "Handled ─", "Events ─", "q stop"} {
		i := strings.Index(view, title)
		if i <= last {
			t.Fatalf("%q is out of order or missing:\n%s", title, view)
		}
		last = i
	}
}

// Covers R21 and KTD8: a 24-row window gives Events, then Handled, their
// minimum, and both scroll.
func TestA24RowWindowShrinksEventsThenHandledToTheirMinimum(t *testing.T) {
	view := fitted(t, 80, 24, eventful())

	golden(t, "fit-24-rows", view)
	if n := strings.Count(view, "\n") + 1; n != 24 {
		t.Errorf("view has %d lines, want the window's 24", n)
	}
	contains(t, view, "listed 30 issues", "listed 29 issues", "Handled ─", "↑↓ scroll", "NEEDS ATTENTION")
	if strings.Contains(view, "listed 28 issues") {
		t.Errorf("Events kept more than two rows:\n%s", view)
	}
}

// Covers R21 and KTD11: tab focuses Handled then Events; the arrows scroll
// the focused section; end follows the newest event again.
func TestFocusAndScrollMoveHandledAndEvents(t *testing.T) {
	h := newHarness(t, 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(updateMsg(eventful()))

	h.send(tab)
	contains(t, h.view(), "▸ Handled")
	before := h.view()
	h.send(downKey)
	if h.view() == before {
		t.Error("down did not scroll the focused Handled")
	}
	h.send(homeKey)
	if h.view() != before {
		t.Errorf("home did not scroll Handled back to its top:\n%s", h.view())
	}

	h.send(tab)
	contains(t, h.view(), "▸ Events", "listed 30 issues")
	h.send(upKey)
	view := h.view()
	contains(t, view, "listed 28 issues", "listed 29 issues")
	if strings.Contains(view, "listed 30 issues") {
		t.Errorf("up did not scroll Events back a row:\n%s", view)
	}
	h.send(homeKey)
	contains(t, h.view(), "listed 1 issue,", "listed 2 issues")
	h.send(endKey)
	contains(t, h.view(), "listed 30 issues")

	h.send(tab)
	if v := h.view(); strings.Contains(v, "▸ ") {
		t.Errorf("a third tab left a section focused:\n%s", v)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	contains(t, h.view(), "▸ Events")
}

func TestAShortWindowDropsTheSaidLinesThenCutsAboveTheKeyHelp(t *testing.T) {
	u := withSaid("Running the tests")
	u.Snapshot.Handled = manySnapshot().Snapshot.Handled

	view := fitted(t, 80, 12, u)

	if strings.Contains(view, "└") {
		t.Errorf("a 12-row window kept the said lines:\n%s", view)
	}
	contains(t, view, "lines cut")
	if !strings.HasSuffix(view, "? help") {
		t.Errorf("view does not end with the key-help line:\n%s", view)
	}
}

func TestBeforeAWindowSizeTheViewShowsEverything(t *testing.T) {
	view := fitted(t, 80, 0, eventful())

	contains(t, view, "listed 1 issue,", "listed 30 issues", "#13 Success")
}

func TestANarrowShortWindowRendersWithoutPanicking(t *testing.T) {
	for _, size := range [][2]int{{12, 5}, {1, 1}, {40, 2}, {30, 60}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			fitted(t, size[0], size[1], eventful())
		})
	}
}

// Covers R20: ? shows the keys over the view and hides them again.
func TestQuestionMarkTogglesTheHelpOverlay(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.send(helpKey)
	view := h.view()
	contains(t, view, "Keys", "shift+tab", "pgdown", "board left")
	if !strings.HasPrefix(view, "crew ╱") {
		t.Errorf("the overlay hid the header:\n%s", view)
	}

	h.send(helpKey)
	if strings.Contains(h.view(), "pgdown") {
		t.Errorf("a second ? left the keys showing:\n%s", h.view())
	}
}

// Covers AE6: without colour, sections read by their titles and rules, and
// states by their icons and pills.
func TestWithoutColourSectionsAndStatesStillReadApart(t *testing.T) {
	view := fitted(t, 120, 0, handledSnapshot())

	contains(t, view,
		"Workflow ─", "Actions ─", "Queues ─", "Handled ─", "Events ─",
		"⠋ running", "◌ taking", "→ ready to review", "○ #2",
		" GIVEN UP ", " NEEDS ATTENTION ", " READY TO MERGE ", "×",
	)
}

// Covers R21: pgdown and pgup move the focused section a page at a time.
func TestPageKeysScrollTheFocusedSectionByAPage(t *testing.T) {
	h := newHarness(t, 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(updateMsg(eventful()))

	h.send(tab)
	top := h.view()
	h.send(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if h.view() == top || strings.Contains(h.view(), "#11 Parse") {
		t.Errorf("pgdown did not move Handled past its first entry:\n%s", h.view())
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if h.view() != top {
		t.Errorf("pgup did not bring Handled back to its top:\n%s", h.view())
	}

	h.send(tab)
	h.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
	view := h.view()
	contains(t, view, "listed 19 issues", "listed 20 issues")
	if strings.Contains(view, "listed 30 issues") {
		t.Errorf("pgup did not move Events back a page of 10 events:\n%s", view)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyPgDown})
	contains(t, h.view(), "listed 30 issues")
}
