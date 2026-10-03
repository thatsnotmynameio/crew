package tui

import (
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Covers AE1 (TUI side).
func TestTheQueuesSectionShowsEachQueuesSizeAndBusyAndFreeSlots(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(runningSnapshot()))

	want := "handled 0\n\nQueues\n" +
		"  default  2 slots  1 busy  1 free\n" +
		"  clerk    1 slot   1 busy  0 free\n\nIssues\n"
	if view := h.view(); !strings.Contains(view, want) {
		t.Errorf("view lacks the Queues section under the counts line:\n%s\nwant:\n%s", view, want)
	}
}

// Covers AE1 (TUI side).
func TestEachActionNamesItsQueueInAlignedColumns(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(runningSnapshot()))

	want := "Actions\n" +
		"  #1 implement/code   default  running 5m00s\n" +
		"  #1 implement/tests  default  running 7m00s\n" +
		"  #2 review/check     clerk    waiting\n"
	if view := h.view(); !strings.Contains(view, want) {
		t.Errorf("view lacks the actions with their queues:\n%s\nwant:\n%s", view, want)
	}
}

// Covers AE4 (TUI side).
func TestAQueueOfNoSlotsStillHasItsLine(t *testing.T) {
	h := newHarness(t, 80)
	u := runningSnapshot()
	u.Snapshot.Queues = []core.QueueView{{Name: "review", Slots: 2, Busy: 1}, {Name: crew.DefaultQueue}}

	h.send(updateMsg(u))

	if view := h.view(); !strings.Contains(view, "  default  0 slots  0 busy  0 free\n") {
		t.Errorf("view lacks default's line of 0 slots:\n%s", view)
	}
}

func TestBeforeTheFirstUpdateTheQueuesSectionShowsNone(t *testing.T) {
	h := newHarness(t, 80)

	if view := h.view(); !strings.Contains(view, "Queues\n  none\n") {
		t.Errorf("view lacks an empty Queues section:\n%s", view)
	}
}
