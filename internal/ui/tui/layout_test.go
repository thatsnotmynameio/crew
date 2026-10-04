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
	// Covers AE6 of #108: starting from 5 rows each, Events gives up 3 and
	// Handled the 1 more the window needs.
	if rows := eventsRows(t, view); len(rows) != minScroll {
		t.Errorf("Events has %d rows, want %d:\n%s", len(rows), minScroll, view)
	}
	if band := bandRows(t, view); len(band) != 1+scrollRows-1 {
		t.Errorf("band has %d rows, want Handled's 4 under its title:\n%s", len(band), view)
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

// Covers R1 of #108: before a window size the view is not cut, and Events
// still takes its 5 rows.
func TestBeforeAWindowSizeTheViewIsNotCutAndEventsTakeTheirRows(t *testing.T) {
	view := fitted(t, 80, 0, eventful())

	contains(t, view, "listed 30 issues", "30 events · ↑↓ scroll", "#11 Parse")
	if strings.Contains(view, "lines cut") {
		t.Errorf("the view was cut before a window size:\n%s", view)
	}
	if rows := eventsRows(t, view); len(rows) != scrollRows {
		t.Errorf("Events has %d rows, want %d:\n%s", len(rows), scrollRows, view)
	}
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
		" GIVEN UP ", " NEEDS ATTENTION ", "×",
	)
	contains(t, handledText(t, 120, handledSnapshot()), " READY TO MERGE ")
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

// withEvents is runningSnapshot with n numbered poll events, the newest
// last.
func withEvents(n int) engine.Update {
	u := runningSnapshot()
	u.Snapshot.Recent = nil
	for k := 1; k <= n; k++ {
		u.Snapshot.Recent = append(u.Snapshot.Recent,
			core.PollDone{At: start.Add(time.Duration(k-n-1) * time.Second), Listed: k, Taken: 0})
	}
	return u
}

// rowsOf splits view into its rows.
func rowsOf(view string) []string { return strings.Split(view, "\n") }

// titleRow is the index of the row of view whose title is title, focused or
// not, or -1.
func titleRow(view, title string) int {
	for i, l := range rowsOf(view) {
		if strings.HasPrefix(l, title+" ") || strings.HasPrefix(l, "▸ "+title+" ") {
			return i
		}
	}
	return -1
}

// eventsRows are the rows under Events' title, down to the blank row above
// the key-help line.
func eventsRows(t *testing.T, view string) []string {
	t.Helper()
	all, i := rowsOf(view), titleRow(view, "Events")
	if i < 0 {
		t.Fatalf("view has no Events section:\n%s", view)
	}
	return all[i+1 : len(all)-2]
}

// bandRows are the rows of the Queues and Handled band, its titles first.
func bandRows(t *testing.T, view string) []string {
	t.Helper()
	all, q, e := rowsOf(view), titleRow(view, "Queues"), titleRow(view, "Events")
	if q < 0 || e < 0 {
		t.Fatalf("view lacks the band or Events:\n%s", view)
	}
	return all[q : e-1]
}

// Covers AE1 and R1, R2 of #108: Events fills its 5 rows from the top, and
// a new event takes the next row without moving the key-help line.
func TestEventsFillTheirFiveRowsFromTheTop(t *testing.T) {
	two := fitted(t, 80, 40, withEvents(2))

	rows := eventsRows(t, two)
	if len(rows) != scrollRows {
		t.Fatalf("Events has %d rows, want %d:\n%s", len(rows), scrollRows, two)
	}
	contains(t, rows[0], "listed 1 issue,")
	contains(t, rows[1], "listed 2 issues")
	for _, r := range rows[2:] {
		if r != "" {
			t.Errorf("row %q under the events is not blank:\n%s", r, two)
		}
	}

	three := fitted(t, 80, 40, withEvents(3))
	contains(t, eventsRows(t, three)[2], "listed 3 issues")
	if len(rowsOf(three)) != len(rowsOf(two)) {
		t.Errorf("a third event moved the key-help line from row %d to %d:\n%s",
			len(rowsOf(two)), len(rowsOf(three)), three)
	}
}

// Covers AE2 of #108: with no events, "none" and 4 blank rows.
func TestEventsWithNoEventsSayNoneOverBlankRows(t *testing.T) {
	view := fitted(t, 80, 40, withEvents(0))

	rows := eventsRows(t, view)
	if len(rows) != scrollRows || !strings.Contains(rows[0], "none") {
		t.Fatalf("Events rows are %q, want none then blanks:\n%s", rows, view)
	}
	for _, r := range rows[1:] {
		if r != "" {
			t.Errorf("row %q under none is not blank:\n%s", r, view)
		}
	}
}

// Covers AE3 and R3 of #108: 30 events show the newest 5, scroll, and keep
// the height of 2.
func TestThirtyEventsShowTheNewestFiveAndScroll(t *testing.T) {
	view := fitted(t, 80, 40, withEvents(30))

	rows := eventsRows(t, view)
	if len(rows) != scrollRows {
		t.Fatalf("Events has %d rows, want %d:\n%s", len(rows), scrollRows, view)
	}
	contains(t, rows[0], "listed 26 issues")
	contains(t, rows[4], "listed 30 issues")
	contains(t, view, "30 events · ↑↓ scroll")
	if n, want := len(rowsOf(view)), len(rowsOf(fitted(t, 80, 40, withEvents(2)))); n != want {
		t.Errorf("view has %d lines with 30 events, want %d as with 2:\n%s", n, want, view)
	}
}

// Covers AE4 and R4, R5 of #108: one handled issue and its reason fill
// Handled from the top, and the band stays 6 rows as issues are handled.
func TestHandledFillsItsFiveRowsAndTheBandHoldsItsHeight(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{failedEntry("5", "Parse the config once", 40, 30, "tests", "exited 1")}
	view := fitted(t, 80, 40, u)

	band := bandRows(t, view)
	if len(band) != 1+scrollRows {
		t.Fatalf("band has %d rows, want %d:\n%s", len(band), 1+scrollRows, view)
	}
	contains(t, band[1], "NEEDS ATTENTION")
	contains(t, band[2], "× tests failed")

	u.Snapshot.Handled = append(u.Snapshot.Handled, entry("8", "Trim the README", "review", "ready to merge", 9, 3))
	if band := bandRows(t, fitted(t, 80, 40, u)); len(band) != 1+scrollRows {
		t.Errorf("a second handled issue made the band %d rows, want %d", len(band), 1+scrollRows)
	}
}

// Covers AE5 and R4 of #108: seven queues set the band's height, and
// handling issues does not grow it.
func TestSevenQueuesSetTheBandsHeight(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Queues = nil
	for n := 1; n <= 7; n++ {
		u.Snapshot.Queues = append(u.Snapshot.Queues, core.QueueView{Name: fmt.Sprintf("q%d", n), Slots: 1})
	}
	empty := bandRows(t, fitted(t, 80, 40, u))

	u.Snapshot.Handled = manySnapshot().Snapshot.Handled
	full := bandRows(t, fitted(t, 80, 40, u))
	if len(empty) != 8 || len(full) != 8 {
		t.Errorf("band has %d rows with nothing handled and %d with ten, want 8 both:\n%s",
			len(empty), len(full), strings.Join(full, "\n"))
	}
}

// Covers AE7 and R7 of #108: one row short, Events gives up one row and
// Handled keeps its 5.
func TestOneRowShortEventsGiveUpOneRow(t *testing.T) {
	u := eventful()
	whole := len(rowsOf(fitted(t, 80, 0, u)))

	view := fitted(t, 80, whole-1, u)

	if rows := eventsRows(t, view); len(rows) != scrollRows-1 {
		t.Errorf("Events has %d rows, want %d:\n%s", len(rows), scrollRows-1, view)
	}
	if band := bandRows(t, view); len(band) != 1+scrollRows {
		t.Errorf("band has %d rows, want Handled's %d under its title:\n%s", len(band), 1+scrollRows, view)
	}
}

// Covers R2 of #108: Events with fewer events than rows does not scroll.
func TestEventsWithRoomForEveryEventDoNotScroll(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(withEvents(2)))
	h.send(tab)
	h.send(tab)
	before := h.view()

	h.send(upKey)

	if h.view() != before {
		t.Errorf("up scrolled Events with room for every event:\n%s", h.view())
	}
}
