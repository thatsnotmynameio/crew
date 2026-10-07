package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

var (
	tab     = tea.KeyPressMsg{Code: tea.KeyTab}
	upKey   = tea.KeyPressMsg{Code: tea.KeyUp}
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

// Covers R1, and R22 of #151: every section on one screen, in order, with
// no Actions or Handled section, and Queues beside Events in one band.
func TestEverySectionShowsInOrder(t *testing.T) {
	view := fitted(t, 80, 0, handledSnapshot())

	last := -1
	for _, title := range []string{"crew ╱", "Bots ─", "Board ─", "Queues ─", "Events ─", stopKeys + " stop"} {
		i := strings.Index(view, title)
		if i <= last {
			t.Fatalf("%q is out of order or missing:\n%s", title, view)
		}
		last = i
	}
	for _, title := range []string{"Actions", "Handled"} {
		if titleRow(view, title) >= 0 {
			t.Errorf("view still has the %s section:\n%s", title, view)
		}
	}
	if band := bandRows(t, view); !strings.HasPrefix(band[0], "Queues ") || !strings.Contains(band[0], "   Events ─") {
		t.Errorf("the band's first row is %q, want Queues then Events:\n%s", band[0], view)
	}
}

// Covers R21 and KTD10 of #151: a 51-row window is 3 rows short of the
// whole view (eventful's 54), so Events gives up those 3 rows, down to
// its minimum, and scrolls, while the board keeps 5 cards a column and
// Bots its cards.
func TestA51RowWindowShrinksEventsToTheirMinimum(t *testing.T) {
	view := fitted(t, 80, 51, eventful())

	golden(t, "fit-51-rows", view)
	if n := strings.Count(view, "\n") + 1; n != 51 {
		t.Errorf("view has %d lines, want the window's 51", n)
	}
	if rows := botsOf(t, view); len(rows) != botCardRows {
		t.Errorf("Bots has %d rows, want its cards' %d:\n%s", len(rows), botCardRows, view)
	}
	contains(t, view, "listed 30 issues", "listed 29 issues", "↑↓ scroll", "+6 more")
	if rows := eventsRows(t, view); len(rows) != minScroll {
		t.Errorf("Events has %d rows, want %d:\n%s", len(rows), minScroll, view)
	}
}

// Covers R21 and KTD11: tab focuses Bots then Events; the arrows scroll
// the focused Events; end follows the newest event again; a third tab
// gives the board its focus back (KTD4 of #151).
func TestFocusAndScrollMoveEvents(t *testing.T) {
	h := newHarness(t, 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 32})
	h.send(updateMsg(eventful()))

	h.send(tab)
	contains(t, h.view(), "▸ Bots")
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
	if v := h.view(); strings.Contains(v, "▸ Bots") || strings.Contains(v, "▸ Events") || !strings.Contains(v, "▸ Board") {
		t.Errorf("a third tab did not give the board its focus back:\n%s", v)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	contains(t, h.view(), "▸ Events")
}

// Covers KTD10 of #151: a window shorter than the view with the Bots strip
// is cut, with the "… N lines cut" row and the key help.
func TestAShortWindowCutsAboveTheKeyHelp(t *testing.T) {
	view := fitted(t, 80, 12, eventful())

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

// Covers R20, and R12 and KTD12 of #151, and R9 of #266: ? shows every key
// over the view, the stop's two presses and the press that forces,
// and what a card's run, bots and via rows mean, within the window; ?
// hides them again, and so does esc.
func TestQuestionMarkTogglesTheHelpOverlay(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.send(helpKey)
	view := checkFits(t, h, 80, harnessRows)
	contains(t, view, "Keys", "shift+tab", "pgdown", "card or bots left", "Cards",
		"run  the issue's actions and how long each has run",
		"bots the bots its running actions act as",
		"via  the queue its actions run in")
	for _, binding := range []string{
		`enter +open card`, `esc +close or board`, `b +bots`, `e +events`,
		stopKeys + ` +stop, within 3s`, `q +force if stopping`,
	} {
		if !regexp.MustCompile(`\b` + binding + `\b`).MatchString(view) {
			t.Errorf("the keys lack %q:\n%s", binding, view)
		}
	}
	if !strings.HasPrefix(view, "crew ╱") {
		t.Errorf("the overlay hid the header:\n%s", view)
	}

	h.send(helpKey)
	if strings.Contains(h.view(), "pgdown") {
		t.Errorf("a second ? left the keys showing:\n%s", h.view())
	}
	h.send(helpKey)
	h.send(escKey)
	if strings.Contains(h.view(), "pgdown") {
		t.Errorf("esc left the keys showing:\n%s", h.view())
	}
}

// Covers KTD12 of #151: the short key help names the keys of the board,
// and the stop's two presses (R9 of #266).
func TestTheKeyHelpNamesTheBoardsKeys(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	rows := rowsOf(h.view())
	if got, want := rows[len(rows)-1], stopKeys+" stop · tab focus · ←→↑↓ move · enter open · ? help"; got != want {
		t.Errorf("key help = %q, want %q", got, want)
	}
}

// Covers AE6: without colour, sections read by their titles and rules, and
// states by their icons.
func TestWithoutColourSectionsAndStatesStillReadApart(t *testing.T) {
	view := fitted(t, 120, 0, handledSnapshot())

	contains(t, view,
		"Board ─", "Queues ─", "Events ─",
		"⠋ code 5m", "○ check waiting", "○ idle",
	)
}

// Covers R21: pgdown and pgup move the focused section a page at a time.
func TestPageKeysScrollTheFocusedSectionByAPage(t *testing.T) {
	h := newHarness(t, 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 32})
	h.send(updateMsg(eventful()))

	h.send(tab)
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

// eventsRows are the rows under Events' title, beside Queues, down to the
// blank row above the key-help line, without the Queues side.
func eventsRows(t *testing.T, view string) []string {
	t.Helper()
	band := bandRows(t, view)
	i := strings.Index(band[0], "▸ Events ")
	if i < 0 {
		i = strings.Index(band[0], "Events ")
	}
	if i < 0 {
		t.Fatalf("the band has no Events section:\n%s", view)
	}
	x := len([]rune(band[0][:i]))
	out := make([]string, 0, len(band)-1)
	for _, r := range band[1:] {
		cells := []rune(r)
		out = append(out, strings.TrimRight(string(cells[min(x, len(cells)):]), " "))
	}
	return out
}

// bandRows are the rows of the Queues and Events band, its titles first,
// down to the blank row above the key-help line.
func bandRows(t *testing.T, view string) []string {
	t.Helper()
	all, q := rowsOf(view), titleRow(view, "Queues")
	if q < 0 {
		t.Fatalf("view lacks the band:\n%s", view)
	}
	return all[q : len(all)-2]
}

// Covers AE1 and R1, R2 of #108: Events fills its 5 rows from the top, and
// a new event takes the next row without moving the key-help line.
func TestEventsFillTheirFiveRowsFromTheTop(t *testing.T) {
	two := fitted(t, 80, 44, withEvents(2))

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

	three := fitted(t, 80, 44, withEvents(3))
	contains(t, eventsRows(t, three)[2], "listed 3 issues")
	if len(rowsOf(three)) != len(rowsOf(two)) {
		t.Errorf("a third event moved the key-help line from row %d to %d:\n%s",
			len(rowsOf(two)), len(rowsOf(three)), three)
	}
}

// Covers AE2 of #108: with no events, "none" and 4 blank rows.
func TestEventsWithNoEventsSayNoneOverBlankRows(t *testing.T) {
	view := fitted(t, 80, 44, withEvents(0))

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
	view := fitted(t, 80, 44, withEvents(30))

	rows := eventsRows(t, view)
	if len(rows) != scrollRows {
		t.Fatalf("Events has %d rows, want %d:\n%s", len(rows), scrollRows, view)
	}
	contains(t, rows[0], "listed 26 issues")
	contains(t, rows[4], "listed 30 issues")
	contains(t, view, "30 events · ↑↓ scroll")
	if n, want := len(rowsOf(view)), len(rowsOf(fitted(t, 80, 44, withEvents(2)))); n != want {
		t.Errorf("view has %d lines with 30 events, want %d as with 2:\n%s", n, want, view)
	}
}

// Covers AE5 and R4 of #108: seven queues set the band's height, and
// events do not grow it.
func TestSevenQueuesSetTheBandsHeight(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Queues = nil
	for n := 1; n <= 7; n++ {
		u.Snapshot.Queues = append(u.Snapshot.Queues, core.QueueView{Name: crew.QueueName(fmt.Sprintf("q%d", n)), Slots: 1})
	}
	u.Snapshot.Recent = nil
	few := bandRows(t, fitted(t, 80, 40, u))

	u.Snapshot.Recent = withEvents(30).Snapshot.Recent
	full := fitted(t, 80, 40, u)
	if many := bandRows(t, full); len(few) != 8 || len(many) != 8 {
		t.Errorf("band has %d rows with no event and %d with thirty, want 8 both:\n%s", len(few), len(many), full)
	}
	contains(t, eventsRows(t, full)[scrollRows-1], "listed 30 issues")
}

// Covers AE7 and R7 of #108: one row short, Events gives up one row and
// the board keeps its 5 cards a column.
func TestOneRowShortEventsGiveUpOneRow(t *testing.T) {
	u := eventful()
	whole := len(rowsOf(fitted(t, 80, 0, u)))

	view := fitted(t, 80, whole-1, u)

	if rows := eventsRows(t, view); len(rows) != scrollRows-1 {
		t.Errorf("Events has %d rows, want %d:\n%s", len(rows), scrollRows-1, view)
	}
	contains(t, boardOf(t, view), "+6 more")
}

// Covers R2 of #108: Events with fewer events than rows does not scroll.
func TestEventsWithRoomForEveryEventDoNotScroll(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(withEvents(2)))
	h.send(tab)
	h.send(tab)
	contains(t, h.view(), "▸ Events")
	before := h.view()

	h.send(upKey)

	if h.view() != before {
		t.Errorf("up scrolled Events with room for every event:\n%s", h.view())
	}
}
