package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// eventfulRows is the whole of eventful's view at 80 columns: the header,
// Bots' blank row, rule and 7 card rows, the board's blank row, rule,
// names, underline, 5 cards of 6 rows and "+6 more", the band's blank row,
// Events' rule and 5 rows, then the blank row and the key help.
const eventfulRows = 1 + (1 + 1 + botCardRows) + (1 + 1 + 2 + maxCards*cardRows + 1) + (1 + 1 + scrollRows) + 2

// Covers KTD10 of #151: a tall window draws every section whole, Bots'
// cards, 5 cards a column and 5 Events rows.
func TestATallWindowShowsFiveCardsAColumnAndFiveEventsRows(t *testing.T) {
	view := fitted(t, 80, 200, eventful())

	if n := len(rowsOf(view)); n != eventfulRows {
		t.Errorf("view has %d rows, want the whole view's %d:\n%s", n, eventfulRows, view)
	}
	if rows := eventsRows(t, view); len(rows) != scrollRows {
		t.Errorf("Events has %d rows, want %d:\n%s", len(rows), scrollRows, view)
	}
	if rows := botsOf(t, view); len(rows) != botCardRows {
		t.Errorf("Bots has %d rows, want its cards' %d:\n%s", len(rows), botCardRows, view)
	}
	contains(t, boardOf(t, view), "+6 more")
}

// Covers KTD10 of #151: shrinking the window takes Events from 5 rows to 2,
// then the board's cards one at a time down to 1 a column, then collapses
// Bots to its strip, then cuts the view, each step at the height where the
// one before no longer fits. Review holds eventful's eleven issues, so its
// "+N more" says how many cards a column shows.
func TestAShrinkingWindowGivesRowsUpInOrder(t *testing.T) {
	for _, tt := range []struct {
		short, events int
		more          string
		strip, cut    bool
	}{
		{short: 0, events: scrollRows, more: "+6 more"},
		{short: 1, events: scrollRows - 1, more: "+6 more"},
		{short: 3, events: minScroll, more: "+6 more"},
		{short: 4, events: minScroll, more: "+7 more"},
		{short: 3 + cardRows, events: minScroll, more: "+7 more"},
		{short: 4 + cardRows, events: minScroll, more: "+8 more"},
		{short: 3 + 4*cardRows, events: minScroll, more: "+10 more"},
		{short: 4 + 4*cardRows, events: minScroll, more: "+10 more", strip: true},
		{short: 3 + 4*cardRows + botCardRows - 1, events: minScroll, more: "+10 more", strip: true},
		{short: 4 + 4*cardRows + botCardRows - 1, cut: true},
	} {
		height := eventfulRows - tt.short
		t.Run(strconv.Itoa(height), func(t *testing.T) {
			view := fitted(t, 80, height, eventful())

			if cut := strings.Contains(view, "lines cut"); cut != tt.cut {
				t.Fatalf("at %d rows the view was cut: %v, want %v:\n%s", height, cut, tt.cut, view)
			}
			if tt.cut {
				return
			}
			if rows := eventsRows(t, view); len(rows) != tt.events {
				t.Errorf("Events has %d rows, want %d:\n%s", len(rows), tt.events, view)
			}
			contains(t, boardOf(t, view), tt.more)
			if strip := len(botsOf(t, view)) == 1; strip != tt.strip {
				t.Errorf("Bots is its strip: %v, want %v:\n%s", strip, tt.strip, view)
			}
		})
	}
}

// sevenBugs are seven issues, #1 to #7, each labeled bug.
func sevenBugs() engine.Update {
	issues := make([]crew.BoardIssue, 0, 7)
	for n := 1; n <= 7; n++ {
		issues = append(issues, labeled(crew.Issue{ID: issueID(strconv.Itoa(n)), Ref: fmt.Sprintf("#%d", n),
			Title: "Bug"}, "bug"))
	}
	return onBoard(engine.Update{}, issues...)
}

// Covers AE3 of #151: a column of seven issues in a window with room for
// three cards shows three cards and "+4 more". The whole view is as tall
// as eventful's, as Queues' "none" row fits beside Events; the window
// takes Events' 3 rows and two cards off it. ↓ on the third card
// highlights the fourth, and the column scrolls to show cards two to
// four, "+4 more" still counting the cards not shown; ↑ then keeps them
// (KTD6 of #151).
func TestAColumnOfSevenWithRoomForThreeShowsThreeAndFourMore(t *testing.T) {
	height := eventfulRows - (scrollRows - minScroll) - 2*cardRows
	h := newBoardHarness(t, 80, crewRules, ideasBugsDone)
	h.send(updateMsg(sevenBugs()))
	h.send(tea.WindowSizeMsg{Width: 80, Height: height})

	view := checkFits(t, h, 80, height)
	board := boardOf(t, view)
	contains(t, board, "│ ▸ #1 Bug", "│ #2 Bug", "│ #3 Bug", "+4 more")
	if strings.Contains(board, "#4 ") || strings.Contains(view, "lines cut") {
		t.Errorf("at %d rows the column shows a fourth card or the view was cut:\n%s", height, view)
	}

	h.send(downKey)
	h.send(downKey)
	h.send(downKey)
	board = boardOf(t, checkFits(t, h, 80, height))
	wantLit(t, h, "#4", 1)
	contains(t, board, "│ #2 Bug", "│ #3 Bug", "+4 more")
	if strings.Contains(board, "#1 ") || strings.Contains(board, "#5 ") {
		t.Errorf("the column does not show cards two to four:\n%s", board)
	}

	h.send(upKey)
	wantLit(t, h, "#3", 1)
	if got := boardOf(t, h.view()); strings.Contains(got, "#1 ") || !strings.Contains(got, "│ #4 Bug") {
		t.Errorf("↑ to a card already shown scrolled the column:\n%s", got)
	}
}
