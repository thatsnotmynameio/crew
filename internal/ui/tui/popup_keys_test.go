package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// Covers KTD7 of #151: a popup taller than the window shows its title,
// as many rows as fit and ↑↓ scroll; ↓ and end scroll it, home back.
func TestATallPopupScrollsInAShortWindow(t *testing.T) {
	h := newHarness(t, 120)
	h.send(tea.WindowSizeMsg{Width: 120, Height: 14})
	h.send(updateMsg(fourActions()))
	h.send(enterKey)

	box, _, _ := popupBox(h.view(), 100)
	if box == nil || len(box) != 12 {
		t.Fatalf("the popup is %d rows tall, want 12, two short of the window:\n%s", len(box), h.view())
	}
	if !strings.Contains(box[len(box)-1], "↑↓ scroll") {
		t.Errorf("the bottom border %q does not say it scrolls", box[len(box)-1])
	}
	rows := popupRows(t, h)
	if rows[0] != "#1 Add login form" || !strings.HasPrefix(rows[1], "rule ") {
		t.Errorf("the popup starts %q, want its title then the rule", rows[:2])
	}
	h.send(downKey)
	if rows := popupRows(t, h); rows[0] != "#1 Add login form" || !strings.HasPrefix(rows[1], "labels ") {
		t.Errorf("after ↓ the popup starts %q, want its title then the labels", rows[:2])
	}
	h.send(endKey)
	taken := fourActions().Snapshot.Recent[0]
	last := taken.Time().In(zone).Format(time.TimeOnly) + " " + lines.Text(taken)
	if rows := popupRows(t, h); rows[len(rows)-1] != last {
		t.Errorf("after end the popup ends in %q, want its one event %q", rows[len(rows)-1], last)
	}
	h.send(tea.KeyPressMsg{Code: 'k', Text: "k"})
	h.send(homeKey)
	if rows := popupRows(t, h); !strings.HasPrefix(rows[1], "rule ") {
		t.Errorf("after home the popup starts %q, want the rule", rows[1])
	}
	h.send(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if rows := popupRows(t, h); !strings.HasPrefix(rows[1], "labels ") {
		t.Errorf("j scrolled the popup to %q, want the labels like ↓", rows[1])
	}
}

// Covers KTD12 of #151: h and l move the popup like ← and →.
func TestHAndLMoveThePopupLikeTheArrows(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	h.send(enterKey)

	h.send(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if got := popupRows(t, h)[0]; got != "#2 Fix the flaky stream test" {
		t.Errorf("after l the popup shows %q, want #2's", got)
	}
	h.send(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if got := popupRows(t, h)[0]; got != "#1 Add login form" {
		t.Errorf("after h the popup shows %q, want #1's", got)
	}
}

// Covers R13, R14 and KTD12 of #151: in the popup two presses of q stop
// crew (R1 of #266), ? shows the help over it, Tab, b, e and Enter change
// nothing, and no key but q acts outside the view.
func TestThePopupsKeys(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	h.send(enterKey)
	if got := h.footer(); got != "esc close · ←→ card · ↑↓ scroll · "+stopKeys+" stop" {
		t.Errorf("the key help reads %q in the popup", got)
	}

	open := h.view()
	for _, k := range []tea.KeyPressMsg{
		tab, bKey, eKey, enterKey, {Code: tea.KeyTab, Mod: tea.ModShift}, {Code: 'x', Text: "x"},
		{Code: tea.KeyPgDown}, {Code: tea.KeyPgUp}, upKey, downKey, homeKey, endKey,
	} {
		if cmd := h.send(k); cmd != nil {
			t.Errorf("%s in the popup returned a command", k)
		}
	}
	if h.view() != open || h.current().focus != focusBoard {
		t.Errorf("keys that do nothing in the popup changed it:\n%s", h.view())
	}
	if h.stops != 0 || h.forces != 0 {
		t.Errorf("keys in the popup called Stop %d and Force %d times", h.stops, h.forces)
	}

	h.send(helpKey)
	if !h.current().popup || !strings.Contains(h.view(), "Keys") {
		t.Errorf("? in the popup did not show the help over it:\n%s", h.view())
	}
	h.send(escKey)
	if !popupShows(h) || strings.Contains(h.view(), "Cards") {
		t.Errorf("esc did not close the help first:\n%s", h.view())
	}
	h.send(qKey)
	if h.stops != 0 || !popupShows(h) {
		t.Errorf("one q in the popup called Stop %d times or closed it", h.stops)
	}
	h.send(qKey)
	if h.stops != 1 {
		t.Errorf("two q in the popup called Stop %d times, want once", h.stops)
	}
}

// Covers KTD12 of #151: Enter does nothing while Bots or Events has
// focus, and on an empty board.
func TestEnterOpensAPopupOnlyFromTheBoard(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	for _, k := range []tea.KeyPressMsg{bKey, eKey} {
		h.send(k)
		h.send(enterKey)
		if popupShows(h) {
			t.Errorf("enter after %s opened a popup", k)
		}
	}

	empty := newHarness(t, 120)
	empty.send(enterKey)
	if popupShows(empty) {
		t.Error("enter on an empty board opened a popup")
	}
}
