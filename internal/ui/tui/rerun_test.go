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

	u := handledBy(crew.Issue{Key: "1", Ref: "#1", Title: "Bug"}, "fix", "done")
	h.send(updateMsg(onBoard(u, item("2", "l2"), item("3", "l3"), item("4", "l4"))))

	wantLit(t, h, "#1", 2)
	contains(t, boardOf(t, h.view()), "◂ 1")
}

// reHeld is runningSnapshot with #1 held again by implement, its code
// action saying "writing the new parser", while #1's Handled card still
// shows the run implement ended before.
func reHeld() engine.Update {
	u := saying(core.Said{IssueKey: "1", Action: "code", Text: "writing the new parser"})
	e := acted(entry("1", "Add login form", "implement", "ready to review", 30, 20),
		core.HandledAction{Name: "code", Spend: spent(1, 100_000), PullRequest: found("#50")})
	e.HeldBy = "implement"
	u.Snapshot.Handled = []core.HandledView{e}
	return u
}

// Covers R17, R18 and R20 of #151: an issue held again does not lend its
// new run's words or branch to the Handled card of the run before: its
// row shows no branch and nothing under it.
func TestAHandledPopupDoesNotShowTheNewRunsWords(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(reHeld()))
	h.send(enterKey)
	h.send(rightKey)
	h.send(rightKey)

	rows := words(popupRows(t, h))
	if rows[0] != "#1 Add login form" || field(t, popupRows(t, h), "cost") == "none" {
		t.Fatalf("the popup is not #1's Handled one:\n%v", rows)
	}
	i := hasRow(t, rows, "code ■ you default done #50")
	if i >= 0 && i+1 < len(rows) && rows[i+1] == "└ writing the new parser" {
		t.Errorf("the Handled run's code row shows the new run's words:\n%v", rows)
	}
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
