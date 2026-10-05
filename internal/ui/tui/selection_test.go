package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

var (
	downKey  = tea.KeyPressMsg{Code: tea.KeyDown}
	leftKey  = tea.KeyPressMsg{Code: tea.KeyLeft}
	rightKey = tea.KeyPressMsg{Code: tea.KeyRight}
	escKey   = tea.KeyPressMsg{Code: tea.KeyEscape}
	bKey     = tea.KeyPressMsg{Code: 'b', Text: "b"}
	eKey     = tea.KeyPressMsg{Code: 'e', Text: "e"}
)

// lit returns the reference on board's highlighted card and the drawn
// column holding it, or "" and -1 when no card is highlighted. It fails
// t when more than one card is.
func lit(t *testing.T, board string) (string, int) {
	t.Helper()
	marker := "│ " + focusMark
	if n := strings.Count(board, marker); n > 1 {
		t.Fatalf("%d cards are highlighted, want one:\n%s", n, board)
	}
	step := columnWidth(board) + columnGap
	for l := range strings.SplitSeq(board, "\n") {
		if before, after, found := strings.Cut(l, marker); found {
			ref, _, _ := strings.Cut(after, " ")
			return ref, len([]rune(before)) / step
		}
	}
	return "", -1
}

// wantLit fails t unless h's board highlights ref's card in drawn column
// col.
func wantLit(t *testing.T, h *harness, ref string, col int) {
	t.Helper()
	board := boardOf(t, h.view())
	if gotRef, gotCol := lit(t, board); gotRef != ref || gotCol != col {
		t.Errorf("highlight is on %q in column %d, want %q in column %d:\n%s", gotRef, gotCol, ref, col, board)
	}
}

// item is a board item for issue n, titled Bug, labeled lab.
func item(n, lab string) crew.BoardIssue {
	return labeled(crew.Issue{Key: n, Ref: "#" + n, Title: "Bug"}, lab)
}

// Covers R10 and KTD5 of #151: the board has focus when the view opens,
// and the first snapshot highlights the first card of the first column
// holding cards.
func TestTheFirstSnapshotHighlightsTheFirstCardOfTheFirstColumnHoldingCards(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	if row := rowsOf(h.view())[titleRow(h.view(), "Board")]; !strings.HasPrefix(row, focusMark+"Board ") {
		t.Errorf("the board's rule is %q before any snapshot, want it focused", row)
	}

	h.send(updateMsg(onBoard(engine.Update{}, item("20", "bug"), item("21", "bug"), item("22", "crew:triage:done"))))

	wantLit(t, h, "#20", 1)
}

// Covers R10 and KTD6 of #151: ↑ on a column's first card and ↓ on its
// last change nothing.
func TestUpAndDownStopAtTheColumnsEnds(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, item("20", "bug"), item("21", "bug"))))
	first := h.view()

	h.send(upKey)
	if h.view() != first {
		t.Errorf("↑ on the first card changed the view:\n%s", h.view())
	}
	h.send(downKey)
	wantLit(t, h, "#21", 1)
	last := h.view()
	h.send(downKey)
	if h.view() != last {
		t.Errorf("↓ on the last card changed the view:\n%s", h.view())
	}
}

// Covers R10 and KTD6 of #151: → skips an empty column, lands on the
// card at the same shown slot, or on the last card of a shorter column,
// and does nothing past the last column holding cards.
func TestLeftAndRightMoveBetweenColumnsHoldingCards(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, eightColumns()[:4])
	h.send(updateMsg(onBoard(engine.Update{},
		item("1", "l1"), item("2", "l1"), item("3", "l3"), item("4", "l3"), item("5", "l4"))))

	h.send(rightKey)
	wantLit(t, h, "#3", 2)
	h.send(rightKey)
	wantLit(t, h, "#5", 3)
	last := h.view()
	h.send(rightKey)
	if h.view() != last {
		t.Errorf("→ past the last column holding cards changed the view:\n%s", h.view())
	}

	h.send(leftKey)
	h.send(leftKey)
	h.send(downKey)
	wantLit(t, h, "#2", 0)
	h.send(rightKey)
	wantLit(t, h, "#4", 2)
	h.send(rightKey)
	wantLit(t, h, "#5", 3)
}

// Covers R10 and KTD6 of #151: → onto a column scrolled off the right
// edge scrolls the board just enough to draw it, and ← back past the left
// edge scrolls it back.
func TestRightOntoAColumnOffTheEdgeScrollsTheBoard(t *testing.T) {
	h := newBoardHarness(t, 80, crewRules, eightColumns())
	h.send(updateMsg(threeBotsOnABoard()))

	h.send(rightKey)
	h.send(rightKey)
	wantLit(t, h, "#3", 2)
	contains(t, boardOf(t, h.view()), "2 ▸")

	h.send(rightKey)
	board := boardOf(t, h.view())
	wantLit(t, h, "#4", 2)
	contains(t, board, "◂ 1", "c4", "1 ▸")

	h.send(leftKey)
	h.send(leftKey)
	wantLit(t, h, "#2", 0)
	h.send(leftKey)
	board = boardOf(t, h.view())
	wantLit(t, h, "#1", 0)
	if strings.Contains(board, "◂") {
		t.Errorf("← onto the first column left the board scrolled:\n%s", board)
	}
}

// focused returns the title of the section h's view marks focused.
func focused(t *testing.T, h *harness) string {
	t.Helper()
	var out []string
	for _, l := range rowsOf(h.view()) {
		for _, title := range []string{"Bots", "Board", "Events"} {
			if strings.HasPrefix(l, focusMark+title+" ") || strings.Contains(l, " "+focusMark+title+" ") {
				out = append(out, title)
			}
		}
	}
	if len(out) != 1 {
		t.Fatalf("sections focused: %q, want one:\n%s", out, h.view())
	}
	return out[0]
}

// Covers R11 and KTD4 of #151: tab cycles the board, Bots and Events,
// shift+tab goes back, b and e jump to Bots and Events, and esc returns to
// the board.
func TestTabBAndEMoveFocusAndEscReturnsToTheBoard(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}

	for i, step := range []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{tab, "Bots"}, {tab, "Events"}, {tab, "Board"},
		{shiftTab, "Events"}, {shiftTab, "Bots"}, {shiftTab, "Board"},
		{eKey, "Events"}, {bKey, "Bots"}, {eKey, "Events"}, {escKey, "Board"},
		{bKey, "Bots"}, {escKey, "Board"}, {escKey, "Board"},
	} {
		h.send(step.key)
		if got := focused(t, h); got != step.want {
			t.Fatalf("step %d (%s): %s has focus, want %s", i, step.key, got, step.want)
		}
	}
}

// Covers KTD6 of #151: with Bots focused ←→ scroll the Bots cards, and
// with Events focused ↑↓ scroll Events, while the highlight stays.
func TestTheHighlightStaysWhileBotsOrEventsScroll(t *testing.T) {
	h := newBoardHarness(t, 80, crewRules, eightColumns())
	u := threeBotsOnABoard()
	u.Snapshot.Recent = withEvents(30).Snapshot.Recent
	h.send(updateMsg(u))

	h.send(bKey)
	h.send(rightKey)
	if rule := botsRule(t, h.view()); !strings.HasSuffix(rule, "· ◂ 1") {
		t.Errorf("→ with Bots focused did not scroll the cards: %q", rule)
	}
	wantLit(t, h, "#1", 0)

	h.send(eKey)
	h.send(upKey)
	if strings.Contains(h.view(), "listed 30 issues") {
		t.Errorf("↑ with Events focused did not scroll Events:\n%s", h.view())
	}
	h.send(downKey)
	contains(t, h.view(), "listed 30 issues")
	wantLit(t, h, "#1", 0)
}

// Covers R6, R10 and KTD5, KTD11 of #151: the highlighted card, live or
// Handled, has its border in the highlight colour and ▸ before its
// reference, and no other card has either.
func TestTheHighlightedCardIsDrawnInTheHighlightColourWithAMarker(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handledSnapshot()))
	border := h.current().styles.highlight.Render(topBorder(maxColumn))

	for _, want := range []struct {
		ref string
		col int
	}{{"#1", 0}, {"#6", 2}} {
		board := boardOf(t, h.view())
		wantLit(t, h, want.ref, want.col)
		if card := cardOf(t, board, want.ref); !strings.HasPrefix(card[1], "│ "+focusMark+want.ref+" ") {
			t.Errorf("%s's first row is %q, want ▸ before its reference", want.ref, card[1])
		}
		if n := strings.Count(h.raw(), border); n != 1 {
			t.Errorf("with %s highlighted, %d borders are in the highlight colour, want 1", want.ref, n)
		}
		h.send(rightKey)
		h.send(rightKey)
	}
}

// Covers R21 and KTD5 of #151: the highlighted issue's card moving to
// another column keeps the highlight.
func TestAnIssueMovingColumnsKeepsItsHighlight(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, item("20", "bug"), item("22", "bug"))))
	h.send(downKey)

	h.send(updateMsg(onBoard(engine.Update{}, item("20", "bug"), item("22", "crew:triage:done"))))

	wantLit(t, h, "#22", 2)
}

// Covers R21 and KTD5 of #151: an issue with cards in a configured column
// and in Handled takes the highlight to its Handled card.
func TestAnIssueMovingIntoHandledHighlightsItsHandledCard(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, item("20", "bug"), item("22", "bug"))))
	h.send(downKey)

	u := handledBy(crew.Issue{Key: "22", Ref: "#22", Title: "Bug"}, "fix", "crew:triage:done")
	h.send(updateMsg(onBoard(u, item("20", "bug"), item("22", "crew:triage:done"))))

	wantLit(t, h, "#22", 3)
}

// Covers R21 and KTD5 of #151: when the highlighted issue leaves the
// board, the card at its row in its column takes the highlight, or the
// column's last card when it is shorter.
func TestAnIssueLeavingTheBoardHighlightsTheCardAtItsRow(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, item("1", "bug"), item("2", "bug"), item("3", "bug"))))
	h.send(downKey)

	h.send(updateMsg(onBoard(engine.Update{}, item("1", "bug"), item("3", "bug"))))
	wantLit(t, h, "#3", 1)

	h.send(updateMsg(onBoard(engine.Update{}, item("1", "bug"))))
	wantLit(t, h, "#1", 1)
}

// Covers KTD5 of #151: when the highlighted card's column empties, the
// nearest column holding cards takes the highlight at the same row, the
// left one on a tie.
func TestAnEmptiedColumnHandsTheHighlightToTheNearestColumn(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	ideas := []crew.BoardIssue{
		item("4", "crew:brainstorm:ready"), item("5", "crew:brainstorm:ready"), item("6", "crew:brainstorm:ready"),
	}
	bugs := []crew.BoardIssue{item("1", "bug"), item("2", "bug")}
	done := item("7", "crew:triage:done")
	h.send(updateMsg(onBoard(engine.Update{}, append(append(ideas, bugs...), done)...)))
	h.send(rightKey)
	h.send(downKey)
	wantLit(t, h, "#2", 1)

	h.send(updateMsg(onBoard(engine.Update{}, append(ideas, done)...)))
	wantLit(t, h, "#5", 0)

	h.send(updateMsg(onBoard(engine.Update{}, ideas[0], bugs[0], done)))
	h.send(rightKey)
	h.send(rightKey)
	wantLit(t, h, "#7", 2)
	h.send(updateMsg(onBoard(engine.Update{}, ideas[0], bugs[0])))
	wantLit(t, h, "#1", 1)
}

// Covers KTD5 of #151: an empty board highlights nothing, and its first
// card takes the highlight once cards arrive.
func TestAnEmptyBoardHighlightsNothing(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, item("20", "bug"))))

	h.send(updateMsg(engine.Update{}))
	wantLit(t, h, "", -1)
	h.send(downKey)
	h.send(rightKey)

	h.send(updateMsg(onBoard(engine.Update{}, item("21", "crew:triage:done"))))
	wantLit(t, h, "#21", 2)
}

// Covers R9 of #151: the view does not ask the terminal for mouse
// reports, so text selection and the issue links keep working.
func TestTheViewDoesNotCaptureTheMouse(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))

	if mode := h.model.View().MouseMode; mode != tea.MouseModeNone {
		t.Errorf("the view asks for mouse mode %d, want none", mode)
	}
}
