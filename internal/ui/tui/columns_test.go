package tui

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// handling is a snapshot that handled entries and holds nothing.
func handling(entries ...core.HandledView) engine.Update {
	return engine.Update{Snapshot: engine.Snapshot{View: core.View{Handled: entries}}}
}

// besideIdle is u with an idle issue in the board's first column, which
// takes the highlight, so the other cards show their own borders.
func besideIdle(u engine.Update) engine.Update {
	return onBoard(u, labeled(twenty, "ready"))
}

// topBorder is the top row of a card width cells wide.
func topBorder(width int) string { return "╭" + strings.Repeat("─", width-2) + "╮" }

// columnRefs returns the references of the cards in drawn column col of
// board, top to bottom.
func columnRefs(board string, col int) []string {
	var out []string
	step := columnWidth(board) + columnGap
	for l := range strings.SplitSeq(board, "\n") {
		rest, x := l, 0
		for {
			i, ref := nextCard(rest, "#")
			if i < 0 {
				break
			}
			x += len([]rune(rest[:i]))
			if x/step == col {
				out = append(out, ref)
			}
			rest, x = rest[i+1:], x+1
		}
	}
	return out
}

// Covers AE7 of #151: an issue that needs attention because two actions
// failed reads so in the error colour, then its first reason and how many
// more, then its rule and time, in an error border.
func TestAE7AHandledCardReadsItsGroupAndFirstReason(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(besideIdle(handling(
		failedEntry("5", "Parse the config once", 40, 30, "tests", "exited 1", "code", "prompt did not render")))))
	board := boardOf(t, h.view())

	want := []string{"#5 Parse the config once", "▲ needs attention", "× tests failed: exited 1 +1", "implement 10m00s"}
	if got := faceOf(t, board, "#5"); !slices.Equal(got, want) {
		t.Errorf("#5's card = %q, want %q:\n%s", got, want, board)
	}
	if col := cardColumn(t, board, "#5"); col != 2 {
		t.Errorf("#5's card is in column %d, want Handled's 2:\n%s", col, board)
	}
	s := h.current().styles
	for _, span := range []string{s.error.Render("▲ needs attention"), s.error.Render(topBorder(maxColumn))} {
		if !strings.Contains(h.raw(), span) {
			t.Errorf("the view lacks %q in the error colour", ansi.Strip(span))
		}
	}
}

// The colours a Handled card's status line and border take.
func mutedStyle(s styles) lipgloss.Style   { return s.muted }
func successStyle(s styles) lipgloss.Style { return s.success }
func errorStyle(s styles) lipgloss.Style   { return s.error }

// Covers R8 and KTD3 of #151: each group of Handled reads its own status
// line, in its colour, in a border of the group's colour.
func TestEachHandledGroupReadsItsStatusInItsColour(t *testing.T) {
	heldAgain := failedEntry("9", "Retry the poll", 8, 5, "tests", "exited 1")
	heldAgain.HeldBy = "fix"
	for _, tt := range []struct {
		name           string
		entry          core.HandledView
		face           []string
		status, border func(styles) lipgloss.Style
	}{
		{
			"given up", givenUpEntry(entry("6", "Drop the old flag", "implement", "done", 25, 20), "gone"),
			[]string{"#6 Drop the old flag", "■ given up", "× move to done given up: gone", "implement 5m00s"},
			mutedStyle, mutedStyle,
		},
		{
			"success", entry("8", "Trim the README", "review", "crew:review:done", 9, 3),
			[]string{"#8 Trim the README", "✓ done", "", "review 6m00s"},
			successStyle, successStyle,
		},
		{
			"held again", heldAgain,
			[]string{"#9 Retry the poll", "× needs attention", "× tests failed: exited 1", "implement 3m00s · now in fix"},
			errorStyle, mutedStyle,
		},
	} {
		h := newHarness(t, 120)
		h.send(updateMsg(besideIdle(handling(tt.entry))))
		board := boardOf(t, h.view())

		if got := faceOf(t, board, tt.entry.Issue.Ref); !slices.Equal(got, tt.face) {
			t.Errorf("%s: card = %q, want %q:\n%s", tt.name, got, tt.face, board)
		}
		s := h.current().styles
		if border := tt.border(s).Render(topBorder(maxColumn)); !strings.Contains(h.raw(), border) {
			t.Errorf("%s: the card's border is not in the group's colour", tt.name)
		}
		if status := tt.status(s).Render(tt.face[1]); !strings.Contains(h.raw(), status) {
			t.Errorf("%s: %q is not in the group's colour", tt.name, tt.face[1])
		}
	}
}

// Covers R8 of #151: a failure a rule holds again reads its state as an
// error, and sorts among the rest by when it ended (#109).
func TestAFailureHeldAgainReadsAsAnErrorAndSortsByWhenItEnded(t *testing.T) {
	heldAgain := failedEntry("9", "Retry the poll", 8, 5, "tests", "exited 1")
	heldAgain.HeldBy = "fix"
	h := newHarness(t, 120)
	h.send(updateMsg(handling(
		entry("8", "Trim the README", "review", "done", 9, 3),
		heldAgain,
		failedEntry("5", "Parse the config once", 40, 30, "tests", "exited 1"))))

	if want := h.current().styles.error.Render("× needs attention"); !strings.Contains(h.raw(), want) {
		t.Error("the held-again failure's state is not in the error colour")
	}
	board := boardOf(t, h.view())
	if got := columnRefs(board, 2); !slices.Equal(got, []string{"#5", "#8", "#9"}) {
		t.Errorf("Handled holds %v, want #5 then #8 then #9:\n%s", got, board)
	}
}

// Covers R8 of #151: the Handled column comes last, named with its count
// and the run's cost, holds the handled issues needing attention first,
// then the most recently ended, and the board's summary counts only the
// issues in the configured columns.
func TestTheHandledColumnHoldsTheHandledIssuesInTodaysOrder(t *testing.T) {
	board := boardOf(t, fitted(t, 120, 0, handledSnapshot()))

	if got := columnNamesOf(t, board); got != "implement review Handled 4 · $19.86" {
		t.Errorf("columns = %q, want implement review Handled 4 · $19.86:\n%s", got, board)
	}
	if got := columnRefs(board, 2); !slices.Equal(got, []string{"#6", "#5", "#8", "#7"}) {
		t.Errorf("Handled holds %v, want #6, #5, #8, #7:\n%s", got, board)
	}
	if got := cardColumns(board, "#7"); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("#7's cards are in columns %v, want review (1) and Handled (2):\n%s", got, board)
	}
	if rule, _, _ := strings.Cut(board, "\n"); !strings.HasSuffix(rule, " 3 issues") {
		t.Errorf("summary = %q, want the three issues in implement and review", rule)
	}
}

// Covers R8 of #151: with nothing handled and room for every column, the
// Handled column shows empty.
func TestAnEmptyHandledColumnShowsWhenEveryColumnFits(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "implement review Handled 0" {
		t.Errorf("columns = %q, want implement review Handled 0:\n%s", got, board)
	}
	if got := columnRefs(board, 2); len(got) != 0 {
		t.Errorf("the empty Handled column holds %v:\n%s", got, board)
	}
}

// Covers KTD3 of #151: a card moving into the Handled column slides there.
func TestACardMovingIntoHandledSlides(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, crewBoard)
	h.send(updateMsg(inTriage()))
	h.send(updateMsg(handledBy(twelve, "triage", "crew:triage:failed")))

	want := []slide{{key: "12", ref: "#12", from: 0, to: len(crewBoard) + 1}}
	if got := h.current().memory.slides; !slices.Equal(got, want) {
		t.Errorf("slides = %v, want %v", got, want)
	}
	contains(t, underlineOf(t, h), "#12 ▸")
}

// Covers KTD13 of #151: on a written board none of whose columns names the
// running label, a held issue shows as a live card in Not on board,
// before Handled.
func TestAHeldIssueNoColumnShowsIsInNotOnBoard(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(held(twelve, "development", "lfg", core.ClaimRunning))))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "ideas bugs done Not on board Handled 0" {
		t.Errorf("columns = %q, want ideas bugs done Not on board Handled 0:\n%s", got, board)
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
	pr := crew.Issue{Key: "90", Ref: "#90", Title: "Fix the review", Kind: crew.KindPullRequest}
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(held(pr, "fix review", "review", core.ClaimRunning), labeled(pr, "bug"))))
	board := boardOf(t, h.view())

	if got := cardColumns(board, "#90"); !slices.Equal(got, []int{3}) {
		t.Errorf("#90's cards are in columns %v, want one in Not on board (3):\n%s", got, board)
	}
}

// Covers KTD13 of #151: a board that shows every held issue never draws
// Not on board, even with room for every column.
func TestNotOnBoardIsNotDrawnWhileEveryHeldIssueHasACard(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, crewBoard)
	h.send(updateMsg(inTriage()))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "triage development fix Handled 0" {
		t.Errorf("columns = %q, want triage development fix Handled 0:\n%s", got, board)
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
