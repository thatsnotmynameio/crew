package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// sliding returns a harness whose #12 moved from triage to development.
func sliding(t *testing.T, width int) *harness {
	t.Helper()
	h := newWorkflowHarness(t, width, crewWorkflow)
	h.send(updateMsg(held(twelve, "triage", "triage", core.ClaimRunning)))
	h.send(updateMsg(held(twelve, "development", "lfg", core.ClaimRunning)))
	return h
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
	h := newWorkflowHarness(t, 120, crewWorkflow)
	h.send(updateMsg(held(twelve, "development", "lfg", core.ClaimRunning)))
	h.send(updateMsg(held(twelve, "triage", "triage", core.ClaimRunning)))

	contains(t, underlineOf(t, h), "◂ #12")
}

// A slide whose source column was dropped starts at the edge of its side.
func TestASlideFromADroppedColumnStartsAtTheEdge(t *testing.T) {
	h := newWorkflowHarness(t, 50, eightStages())
	h.send(updateMsg(held(crew.Issue{Key: "1", Ref: "#1"}, "s1", "a", core.ClaimRunning)))
	h.send(updateMsg(held(crew.Issue{Key: "1", Ref: "#1"}, "s5", "a", core.ClaimRunning)))

	row := underlineOf(t, h)
	if i := strings.Index(row, "#1 ▸"); i != 0 {
		t.Errorf("marker at %d, want the left edge:\n%s", i, row)
	}
}

func TestAnIssueThatStaysInItsColumnDoesNotSlide(t *testing.T) {
	h := newWorkflowHarness(t, 120, crewWorkflow)
	h.send(updateMsg(held(twelve, "triage", "triage", core.ClaimRunning)))
	h.send(updateMsg(handledBy(twelve, "triage", "crew:triage:done")))

	if n := len(h.current().memory.slides); n != 0 {
		t.Errorf("%d slides for a card that stayed in its column", n)
	}
}

// A slide from a dropped column between drawn ones starts in the gap where
// that column would be, and points the way the card moved.
func TestASlideFromADroppedMiddleColumnStartsBetweenItsNeighbours(t *testing.T) {
	one := crew.Issue{Key: "1", Ref: "#1"}
	seven := crew.Issue{Key: "7", Ref: "#7"}
	h := newWorkflowHarness(t, 80, eightStages())
	before := held(one, "s1", "a", core.ClaimRunning)
	before.Snapshot.Issues = append(before.Snapshot.Issues, held(seven, "s2", "a", core.ClaimRunning).Snapshot.Issues...)
	h.send(updateMsg(before))
	after := held(one, "s1", "a", core.ClaimRunning)
	after.Snapshot.Issues = append(after.Snapshot.Issues, held(seven, "s3", "a", core.ClaimRunning).Snapshot.Issues...)
	h.send(updateMsg(after))

	row := underlineOf(t, h)
	before, _, found := strings.Cut(row, "#7 ▸")
	if !found || strings.Contains(row, "◂") {
		t.Fatalf("marker does not point right from the dropped column:\n%s", row)
	}
	x := len([]rune(before))
	if col := cardColumn(t, boardOf(t, h.view()), "#7"); x >= 1+col*(maxColumn+columnGap) {
		t.Errorf("marker at column %d starts at or past its destination column:\n%s", x, row)
	}
}
