package tui

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// topBorder is the top row of a card width cells wide.
func topBorder(width int) string { return "╭" + strings.Repeat("─", width-2) + "╮" }

// Covers AE6 and R7 of #230: a rule that ended on an issue and moved it to
// a label a column shows leaves the issue one card, in that column, which
// slides there, and the board draws no Handled column.
func TestAE6AnIssueWhoseRuleEndedHasOneCardWhereItsLabelsPutIt(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(updateMsg(inTriage()))
	h.send(updateMsg(onBoard(handledBy(twelve, "triage", "crew:development:ready"),
		labeled(twelve, "crew:development:ready"))))
	board := boardOf(t, h.view())

	if got := cardColumns(board, "#12"); !slices.Equal(got, []int{1}) {
		t.Errorf("#12's cards are in columns %v, want one in development (1):\n%s", got, board)
	}
	if got := columnNamesOf(t, board); got != "triage development fix" {
		t.Errorf("columns = %q, want triage development fix:\n%s", got, board)
	}
	want := []slide{{id: issueID("12"), ref: "#12", from: 0, to: 1}}
	if got := h.current().memory.slides; !slices.Equal(got, want) {
		t.Errorf("slides = %v, want %v", got, want)
	}
}

// Covers AE7, R7 and R8 of #230: a rule that failed on an issue moved to
// a label no column shows sends its notification and writes its event, and
// the issue gets no card.
func TestAE7AFailedRuleNotifiesAndLeavesNoHandledCard(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(updateMsg(inTriage()))
	h.send(tea.BlurMsg{})

	u := onBoard(handledBy(twelve, "triage", "crew:triage:failed"), labeled(twelve, "crew:triage:failed"))
	u.Snapshot.Handled[0] = failing(u.Snapshot.Handled[0], "triage")
	u.Snapshot.Recent = []core.Published{core.RouteStepEnded{
		At: start, IssueID: issueID("12"), IssueRef: "#12", Rule: "triage", Route: crew.FailedRoute,
		Plan: crew.StepPlan{Kind: crew.StepMove, To: "crew:triage:failed"}, From: "crew:triage:in progress",
		Outcome: crew.StepLanded{},
	}}
	notes := raws(h.send(updateMsg(u)))
	if len(notes) != 1 || !strings.Contains(notes[0], "triage ended through failed on #12 Rule labels") {
		t.Errorf("notifications = %q, want one for triage failing on #12", notes)
	}
	view := h.view()
	if board := boardOf(t, view); strings.Contains(board, "#12") || strings.Contains(board, "Handled") {
		t.Errorf("the board shows #12 or a Handled column:\n%s", board)
	}
	contains(t, view, "#12 moved from crew:triage:in progress to crew:triage:failed")
}

// Covers AE8 and R9 of #230: a run that handled issues and spent money
// shows its cost in the header, and no handled count anywhere.
func TestAE8TheHeaderShowsTheRunsCostAndNoHandledCount(t *testing.T) {
	view := fitted(t, 120, 0, handledSnapshot())

	contains(t, firstLine(view), "$19.86")
	if strings.Contains(view, "Handled") || strings.Contains(view, "4 · $") {
		t.Errorf("the view shows a handled count:\n%s", view)
	}
	if got := columnNamesOf(t, boardOf(t, view)); got != "implement review" {
		t.Errorf("columns = %q, want implement review", got)
	}
}

// Covers KTD13 of #151: on a written board none of whose columns names the
// running label, a held issue shows as a live card in Not on board,
// after the configured columns.
func TestAHeldIssueNoColumnShowsIsInNotOnBoard(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)
	h.send(updateMsg(onBoard(held(twelve, "development", "lfg", core.ClaimRunning))))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "ideas bugs done Not on board" {
		t.Errorf("columns = %q, want ideas bugs done Not on board:\n%s", got, board)
	}
	if got := cardColumns(board, "#12"); !slices.Equal(got, []int{3}) {
		t.Errorf("#12's cards are in columns %v, want one in Not on board (3):\n%s", got, board)
	}
	if got := faceOf(t, board, "#12")[1]; got != "run  ⠋ lfg 1m" {
		t.Errorf("run row = %q, want run  ⠋ lfg 1m:\n%s", got, board)
	}
}

// Covers KTD13 of #151: a pull request a rule holds has no column on a
// written board of issues, so it shows in Not on board.
func TestAHeldPullRequestShowsInNotOnBoard(t *testing.T) {
	pr := crew.NewIssue(crew.IssueData{ID: issueID("90"), Ref: "#90", Title: "Fix the review", Kind: crew.KindPullRequest})
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)
	h.send(updateMsg(onBoard(held(pr, "fix review", "review", core.ClaimRunning), labeled(pr, "bug"))))
	board := boardOf(t, h.view())

	if got := cardColumns(board, "#90"); !slices.Equal(got, []int{3}) {
		t.Errorf("#90's cards are in columns %v, want one in Not on board (3):\n%s", got, board)
	}
}

// Covers KTD13 of #151: a board that shows every held issue never draws
// Not on board, even with room for every column.
func TestNotOnBoardIsNotDrawnWhileEveryHeldIssueHasACard(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(updateMsg(inTriage()))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "triage development fix" {
		t.Errorf("columns = %q, want triage development fix:\n%s", got, board)
	}
}

// sameColour reports whether a and b are one colour.
func sameColour(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// Covers KTD11 of #151: the highlight is drawn in each palette's title
// colour, distinct from every other border a card takes.
func TestTheHighlightIsTheTitleColourInBothPalettes(t *testing.T) {
	for _, dark := range []bool{true, false} {
		p := lightPalette()
		if dark {
			p = darkPalette()
		}
		got := newStyles(dark).highlight.GetForeground()
		if !sameColour(got, p.title) {
			t.Errorf("dark %v: highlight = %v, want the title colour %v", dark, got, p.title)
		}
		for _, border := range []color.Color{p.strongAccent, p.subtle, p.muted, p.error, p.success} {
			if sameColour(got, border) {
				t.Errorf("dark %v: highlight %v is also a card border's colour", dark, got)
			}
		}
	}
}
