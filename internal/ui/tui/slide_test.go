package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// inTriage is #12 held by triage, in triage's running label.
func inTriage() engine.Update {
	return onBoard(held(twelve, "triage", "triage", core.ClaimRunning), labeled(twelve, "crew:triage:in progress"))
}

// inDevelopment is #12 held by development, in its running label.
func inDevelopment() engine.Update {
	return onBoard(held(twelve, "development", "lfg", core.ClaimRunning),
		labeled(twelve, "crew:development:in progress"))
}

// sliding returns a harness whose #12 moved from triage to development.
func sliding(t *testing.T, width int) *harness {
	t.Helper()
	h := newBoardHarness(t, width, crewNotify, crewBoard)
	h.send(updateMsg(inTriage()))
	h.send(updateMsg(inDevelopment()))
	return h
}

// in is key's item on a board of eightColumns, in column n's label.
func in(key string, n int) crew.BoardIssue {
	return labeled(crew.NewIssue(crew.IssueData{ID: issueID(key), Ref: "#" + key}), crew.State(fmt.Sprintf("l%d", n)))
}

// underlineOf returns the board's underline row.
func underlineOf(t *testing.T, h *harness) string {
	t.Helper()
	return strings.Split(boardOf(t, h.view()), "\n")[2]
}

// Covers R14 and KTD10: the marker crosses the underline row and is gone
// after the last frame.
func TestASlidesMarkerCrossesTheUnderlineRow(t *testing.T) {
	h := sliding(t, 120)

	first := strings.Index(underlineOf(t, h), "#12 ▸")
	if first < 0 || first > 3 {
		t.Fatalf("frame 0 marker at %d, want at triage's column:\n%s", first, underlineOf(t, h))
	}
	for range slideFrames / 2 {
		h.send(slideTickMsg{})
	}
	mid := strings.Index(underlineOf(t, h), "#12 ▸")
	if mid <= first {
		t.Errorf("midway marker at %d, not past frame 0's %d:\n%s", mid, first, underlineOf(t, h))
	}
	var cmd tea.Cmd
	for range slideFrames / 2 {
		cmd = h.send(slideTickMsg{})
	}
	if row := underlineOf(t, h); strings.Contains(row, "#12") {
		t.Errorf("marker still shows after the last frame:\n%s", row)
	}
	if cmd != nil {
		t.Error("a frame tick was scheduled with no slide running")
	}
	if len(h.current().memory.slides) != 0 || h.current().memory.ticking {
		t.Error("the slide is still running after its last frame")
	}
}

func TestASlideToTheLeftPointsLeft(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(updateMsg(inDevelopment()))
	h.send(updateMsg(inTriage()))

	contains(t, underlineOf(t, h), "◂ #12")
}

// A slide whose source column was dropped starts at the edge of its side.
func TestASlideFromADroppedColumnStartsAtTheEdge(t *testing.T) {
	h := newBoardHarness(t, 50, crewNotify, eightColumns())
	h.send(updateMsg(onBoard(engine.Update{}, in("1", 1))))
	h.send(updateMsg(onBoard(engine.Update{}, in("1", 5))))

	row := underlineOf(t, h)
	if i := strings.Index(row, "#1 ▸"); i != 0 {
		t.Errorf("marker at %d, want the left edge:\n%s", i, row)
	}
}

// Covers R22: crew's take moves an issue from its column's ready label to
// its running one, so its card stays in the column, idle then running.
func TestAnIssueTakenStaysInItsColumnWithoutSliding(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(updateMsg(onBoard(engine.Update{}, labeled(twelve, "crew:triage:ready"))))
	contains(t, boardOf(t, h.view()), "run  ○ idle")

	h.send(updateMsg(inTriage()))
	board := boardOf(t, h.view())

	if col := cardColumn(t, board, "#12"); col != 0 {
		t.Errorf("#12's card is in column %d, want triage's 0:\n%s", col, board)
	}
	contains(t, board, "run  ⠋ triage 1m")
	if n := len(h.current().memory.slides); n != 0 {
		t.Errorf("%d slides for a card that stayed in its column", n)
	}
}

// A slide from a dropped column between drawn ones starts in the gap where
// that column would be, and points the way the card moved.
func TestASlideFromADroppedMiddleColumnStartsBetweenItsNeighbours(t *testing.T) {
	h := newBoardHarness(t, 80, crewNotify, eightColumns())
	h.send(updateMsg(onBoard(engine.Update{}, in("1", 1), in("7", 2))))
	h.send(updateMsg(onBoard(engine.Update{}, in("1", 1), in("7", 3))))

	row := underlineOf(t, h)
	lead, _, found := strings.Cut(row, "#7 ▸")
	if !found || strings.Contains(row, "◂") {
		t.Fatalf("marker does not point right from the dropped column:\n%s", row)
	}
	x := len([]rune(lead))
	if col := cardColumn(t, boardOf(t, h.view()), "#7"); x >= 1+col*(maxColumn+columnGap) {
		t.Errorf("marker at column %d starts at or past its destination column:\n%s", x, row)
	}
}

// triageReview is AE3's board.
var triageReview = []crew.BoardColumn{
	{Name: "triage", Labels: []crew.State{"crew:triage:ready", "crew:triage:in progress"}},
	{Name: "review", Labels: []crew.State{"crew:development:waiting review"}},
}

// Covers AE3 and KTD7: an issue development holds shows in Not on board
// (KTD13 of #151), and slides from there into review.
func TestAE3ACardSlidesFromTheColumnsItWasLastIn(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, triageReview)
	h.send(updateMsg(onBoard(held(twelve, "triage", "triage", core.ClaimRunning),
		labeled(twelve, "crew:triage:in progress"))))
	if got := cardColumns(boardOf(t, h.view()), "#12"); len(got) != 1 || got[0] != 0 {
		t.Fatalf("#12's cards are in columns %v, want triage (0)", got)
	}

	h.send(updateMsg(onBoard(held(twelve, "development", "lfg", core.ClaimRunning))))
	if got := cardColumns(boardOf(t, h.view()), "#12"); len(got) != 1 || got[0] != 2 {
		t.Errorf("#12's cards are in columns %v while development holds it, want Not on board (2)", got)
	}
	for range slideFrames {
		h.send(slideTickMsg{})
	}

	cmd := h.send(updateMsg(onBoard(engine.Update{}, labeled(twelve, "crew:development:waiting review"))))
	if got := h.current().memory.slides; len(got) != 1 || got[0].from != 2 || got[0].to != 1 {
		t.Errorf("slides = %+v, want one from Not on board (2) to review (1)", got)
	}
	if !schedulesSlideTick(cmd) {
		t.Error("the move scheduled no slide frame")
	}
}

// Covers KTD7: a card that only gains or only loses a column has nowhere
// to slide from or to.
func TestACardThatOnlyGainsOrLosesAColumnDoesNotSlide(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)
	for _, labels := range [][]crew.State{{"crew:brainstorm:ready"}, {"crew:brainstorm:ready", "bug"}, {"bug"}} {
		h.send(updateMsg(onBoard(engine.Update{}, labeled(twentyOne, labels...))))
		if got := h.current().memory.slides; len(got) != 0 {
			t.Errorf("with labels %v, slides = %+v, want none", labels, got)
		}
	}
}
