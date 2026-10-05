package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// ideasBugsDone is AE1's board.
var ideasBugsDone = []crew.BoardColumn{
	{Name: "ideas", Labels: []string{"crew:brainstorm:ready"}},
	{Name: "bugs", Labels: []string{"bug"}},
	{Name: "done", Labels: []string{"crew:brainstorm:done", "crew:triage:done"}},
}

var (
	twenty    = crew.Issue{Key: "20", Ref: "#20", Title: "Crash on start"}
	twentyOne = crew.Issue{Key: "21", Ref: "#21", Title: "Retry the poll"}
	twentyTwo = crew.Issue{Key: "22", Ref: "#22", Title: "Typo in help"}
)

// onBoard is u with the board read finding each issue with its labels, in
// the order given.
func onBoard(u engine.Update, issues ...crew.BoardIssue) engine.Update {
	u.Snapshot.Board = issues
	return u
}

// labeled is issue carrying the board labels labels.
func labeled(issue crew.Issue, labels ...string) crew.BoardIssue {
	return crew.BoardIssue{Issue: issue, Labels: labels}
}

// columnNamesOf returns the board's column names, as its names row shows
// them.
func columnNamesOf(t *testing.T, board string) string {
	t.Helper()
	return strings.Join(strings.Fields(strings.Split(board, "\n")[1]), " ")
}

// cardColumns returns every drawn column holding a card for ref, by its x
// on the board's rows.
func cardColumns(board, ref string) []int {
	var out []int
	for l := range strings.SplitSeq(board, "\n") {
		rest, x := l, 0
		for {
			i := strings.Index(rest, "▌ "+ref+" ")
			if i < 0 {
				break
			}
			x += len([]rune(rest[:i]))
			out = append(out, x/(maxColumn+columnGap))
			rest, x = rest[i+1:], x+1
		}
	}
	return out
}

// Covers AE1.
func TestAE1AHeldIssueHasOneCardInTheColumnOfItsBoardLabel(t *testing.T) {
	h := newConfiguredHarness(t, 120, crewRules, ideasBugsDone)

	h.send(updateMsg(onBoard(held(twenty, "fix", "lfg", core.ClaimRunning), labeled(twenty, "bug"))))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "ideas bugs done" {
		t.Errorf("columns = %q, want ideas bugs done:\n%s", got, board)
	}
	if got := cardColumns(board, "#20"); len(got) != 1 || got[0] != 1 {
		t.Errorf("#20's cards are in columns %v, want one in bugs (1):\n%s", got, board)
	}
	contains(t, board, "▌ ⠋ running")
}

// Covers AE2.
func TestAE2AnIssueWithTwoColumnsLabelsHasACardInEach(t *testing.T) {
	h := newConfiguredHarness(t, 120, crewRules, ideasBugsDone)

	h.send(updateMsg(onBoard(engine.Update{}, labeled(twentyOne, "crew:brainstorm:ready", "bug"))))
	board := boardOf(t, h.view())

	if got := cardColumns(board, "#21"); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("#21's cards are in columns %v, want ideas (0) and bugs (1):\n%s", got, board)
	}
}

func TestAHeldIssueOnNoColumnsLabelHasNoCardButShowsInActions(t *testing.T) {
	h := newConfiguredHarness(t, 120, crewRules, ideasBugsDone)

	h.send(updateMsg(onBoard(held(twelve, "development", "lfg", core.ClaimRunning))))
	view := h.view()

	if board := boardOf(t, view); strings.Contains(board, "#12") {
		t.Errorf("the board shows #12, which carries no column's label:\n%s", board)
	}
	contains(t, view, "development/lfg")
}

// The configured columns ignore the rules: every rule hidden from the
// board leaves them as they are, empty ones included.
func TestTheConfiguredColumnsShowInConfigOrderWhateverTheRules(t *testing.T) {
	hidden := []crew.Rule{{Name: "only", Label: "ready", OffBoard: true}}
	board := []crew.BoardColumn{
		{Name: "done", Labels: []string{"crew:triage:done"}},
		{Name: "ideas", Labels: []string{"crew:brainstorm:ready"}},
		{Name: "bugs", Labels: []string{"bug"}},
	}
	for _, rules := range [][]crew.Rule{crewRules, hidden} {
		h := newConfiguredHarness(t, 120, rules, board)
		h.send(updateMsg(onBoard(engine.Update{}, labeled(twenty, "bug"))))

		got := boardOf(t, h.view())
		if names := columnNamesOf(t, got); names != "done ideas bugs" {
			t.Errorf("columns = %q, want done ideas bugs:\n%s", names, got)
		}
		if strings.Contains(got, "every stage is hidden") {
			t.Errorf("a configured board says the stages are hidden:\n%s", got)
		}
	}
}

// Covers KTD6: a column lists its cards as the board read orders them,
// oldest first, and "+N more" hides the newest.
func TestAColumnsCardsGoOldestFirstAndTheNewestAreCut(t *testing.T) {
	issues := make([]crew.BoardIssue, 0, 8)
	for n := 1; n <= 8; n++ {
		issues = append(issues, labeled(crew.Issue{Key: strconv.Itoa(n), Ref: fmt.Sprintf("#%d", n), Title: "Bug"}, "bug"))
	}
	h := newConfiguredHarness(t, 80, crewRules, ideasBugsDone)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(updateMsg(onBoard(engine.Update{}, issues...)))

	board := boardOf(t, checkFits(t, h, 80, 24))
	contains(t, board, "▌ #1 Bug", "more")
	if strings.Contains(board, "#8 ") {
		t.Errorf("the newest card shows while the column is cut:\n%s", board)
	}
	if first, second := strings.Index(board, "#1 "), strings.Index(board, "#2 "); second >= 0 && second < first {
		t.Errorf("#2 is drawn above #1:\n%s", board)
	}
}

// eightColumns are eight columns, c1 to c8, each showing the label "lN".
func eightColumns() []crew.BoardColumn {
	var out []crew.BoardColumn
	for i := 1; i <= 8; i++ {
		out = append(out, crew.BoardColumn{Name: fmt.Sprintf("c%d", i), Labels: []string{fmt.Sprintf("l%d", i)}})
	}
	return out
}

func TestEmptyConfiguredColumnsDropThenTheBoardScrollsSideways(t *testing.T) {
	u := onBoard(engine.Update{},
		labeled(crew.Issue{Key: "1", Ref: "#1", Title: "One"}, "l2"),
		labeled(crew.Issue{Key: "2", Ref: "#2", Title: "Two"}, "l6"))

	h := newConfiguredHarness(t, 80, crewRules, eightColumns())
	h.send(updateMsg(u))
	board := boardOf(t, h.view())
	contains(t, board, "6 empty columns not shown")
	if got := columnNamesOf(t, board); got != "c2 c6" {
		t.Errorf("columns = %q, want c2 c6:\n%s", got, board)
	}

	h = newConfiguredHarness(t, 30, crewRules, eightColumns())
	h.send(updateMsg(u))
	contains(t, boardOf(t, h.view()), "c2", "1 ▸", "#1")
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	contains(t, boardOf(t, h.view()), "◂ 1", "c6", "#2")
}

// Covers KTD5 and KTD8.
func TestTheSummaryCountsIssuesAndSaysWhenTheBoardWasNotRead(t *testing.T) {
	h := newConfiguredHarness(t, 120, crewRules, ideasBugsDone)
	contains(t, boardOf(t, h.view()), "Workflow ", " 0 issues")

	u := onBoard(engine.Update{}, labeled(twentyOne, "crew:brainstorm:ready", "bug"), labeled(twentyTwo, "bug"))
	h.send(updateMsg(u))
	rule, _, _ := strings.Cut(boardOf(t, h.view()), "\n")
	if !strings.HasSuffix(rule, " 2 issues") {
		t.Errorf("summary of two issues with three cards: %q", rule)
	}

	u.Snapshot.BoardFailure = "gh: rate limited"
	h.send(updateMsg(u))
	rule, _, _ = strings.Cut(boardOf(t, h.view()), "\n")
	if !strings.HasSuffix(rule, " 2 issues · board not read") {
		t.Errorf("summary while the board read fails: %q", rule)
	}
	if want := h.current().styles.warning.Render("board not read"); !strings.Contains(h.raw(), want) {
		t.Errorf("board not read is not in the warning style:\n%q", h.raw())
	}

	u.Snapshot.BoardFailure = ""
	h.send(updateMsg(u))
	if board := boardOf(t, h.view()); strings.Contains(board, "board not read") {
		t.Errorf("the summary keeps the failure after a read succeeded:\n%s", board)
	}
}

// Covers #126: an unheld card keeps its two rows, the second a status of
// its own, ○ idle, and no claim.
func TestAnUnheldCardShowsIdle(t *testing.T) {
	h := newConfiguredHarness(t, 120, crewRules, ideasBugsDone)

	h.send(updateMsg(onBoard(held(twenty, "fix", "lfg", core.ClaimRunning),
		labeled(twenty, "bug"), labeled(twentyTwo, "bug"))))
	board := boardOf(t, h.view())

	contains(t, board, "▌ #22 Typo in help", "▌ ⠋ running")
	if strings.Contains(board, "◌") {
		t.Errorf("an unheld card draws a claim:\n%s", board)
	}
	rows := strings.Split(board, "\n")
	for i, l := range rows {
		if lead, _, found := strings.Cut(l, "▌ #22"); found {
			x := len([]rune(lead))
			if got := strings.TrimRight(string([]rune(rows[i+1])[x:]), " "); got != "▌ ○ idle" {
				t.Errorf("#22's second row = %q, want ▌ ○ idle:\n%s", got, board)
			}
		}
	}
	if strings.Count(board, "○ idle") != 1 {
		t.Errorf("want one idle card, #22's; the running #20 is not idle:\n%s", board)
	}
}
