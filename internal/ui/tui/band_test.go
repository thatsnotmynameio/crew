package tui

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestHandledIssuesRenderTheGoldenView(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(handledSnapshot()))

	golden(t, "handled", h.view())
}

// A reason that spans lines reads on one row of its Handled card.
func TestAReasonWithNewlinesTakesOneRow(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handling(failedEntry("5", "Parse", 40, 30, "tests", "exit 1:\n  fail\n"))))

	if got := faceOf(t, boardOf(t, h.view()), "#5")[2]; got != "× tests failed: exit 1: fail" {
		t.Errorf("#5's reason row = %q, want the reason on one row", got)
	}
}

// Covers R16: each queue's busy and free slots.
func TestTheQueuesSectionShowsEachQueuesBusyAndFreeSlots(t *testing.T) {
	view := fitted(t, 80, 0, runningSnapshot())

	contains(t, view, "Queues ─── 2 of 3 busy", "\n default  ■□  1/2", "\n clerk    ■   1/1")
}

func TestAQueueOfNoSlotsStillHasItsRow(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Queues = []core.QueueView{{Name: "review", Slots: 2, Busy: 1}, {Name: crew.DefaultQueue}}

	view := fitted(t, 80, 0, u)

	contains(t, view, "\n default      0/0")
}

func TestBeforeTheFirstUpdateTheQueuesSectionShowsNone(t *testing.T) {
	h := newHarness(t, 80)

	contains(t, h.view(), "Queues ", "\n none ")
}
