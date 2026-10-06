package tui

import (
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// Covers R10 and KTD5 of #151: when an update moves the highlighted issue
// into a column scrolled off the board, the board scrolls to draw it, as
// → does.
func TestAnUpdateMovingTheHighlightOffTheBoardScrollsToIt(t *testing.T) {
	h := newBoardHarness(t, 80, crewRules, eightColumns())
	h.send(updateMsg(onBoard(engine.Update{}, item("1", "l1"), item("2", "l2"), item("3", "l3"), item("4", "l4"))))
	wantLit(t, h, "#1", 0)

	u := handledBy(crew.Issue{Key: "1", Ref: "#1", Title: "Bug"}, "fix", "l5")
	h.send(updateMsg(onBoard(u, item("1", "l5"), item("2", "l2"), item("3", "l3"), item("4", "l4"))))

	wantLit(t, h, "#1", 2)
	contains(t, boardOf(t, h.view()), "◂ 1")
}

// Covers R17 and KTD9 of #151: a new run of an action shows none of the
// last run's words, even before its session starts.
func TestANewRunWaitingToStartShowsNoneOfTheLastRunsWords(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(saying(core.Said{IssueKey: "1", Action: "code", Text: "the last run's words"})))

	again := runningSnapshot()
	again.Snapshot.Issues[0].Claim = core.ClaimTaking
	again.Snapshot.Issues[0].Actions = []core.ActionView{{Name: "code", Phase: core.PhaseWaiting}}
	h.send(updateMsg(again))
	h.send(enterKey)

	rows := words(popupRows(t, h))
	if slices.Contains(rows, "└ the last run's words") {
		t.Errorf("the new run's code row shows the last run's words:\n%v", rows)
	}
}
