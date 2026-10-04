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
