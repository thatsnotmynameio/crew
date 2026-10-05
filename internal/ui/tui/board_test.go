package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// crewRules is this repository's rules, both promote rules muted, as rules
// without actions are by default.
var crewRules = []crew.Rule{
	{
		Name: "promote brainstorm",
		Labels: crew.Labels{
			Ready: "crew:brainstorm:done", Running: "crew:brainstorm:promoting", Success: "crew:triage:ready",
		},
	},
	{
		Name: "triage",
		Labels: crew.Labels{
			Ready: "crew:triage:ready", Running: "crew:triage:in progress", Success: "crew:triage:done",
			Failure: "crew:triage:failed",
		},
		Notify: true,
	},
	{
		Name: "promote triage",
		Labels: crew.Labels{
			Ready: "crew:triage:done", Running: "crew:triage:promoting", Success: "crew:development:ready",
		},
	},
	{
		Name: "development",
		Labels: crew.Labels{
			Ready: "crew:development:ready", Running: "crew:development:in progress",
			Failure: "crew:development:failed",
		},
		Notify: true,
	},
	{
		Name:   "fix",
		Labels: crew.Labels{Ready: "crew:fix:ready", Running: "crew:fix:in progress", Failure: "crew:fix:failed"},
		Notify: true,
	},
}

// crewBoard is the default board of crewRules, as config builds it: a
// column per rule with actions, triage, development and fix, each with
// its ready and running labels (R22).
var crewBoard = []crew.BoardColumn{
	{Name: "triage", Labels: []string{"crew:triage:ready", "crew:triage:in progress"}},
	{Name: "development", Labels: []string{"crew:development:ready", "crew:development:in progress"}},
	{Name: "fix", Labels: []string{"crew:fix:ready", "crew:fix:in progress"}},
}

// ideasBugsDone is a written board.
var ideasBugsDone = []crew.BoardColumn{
	{Name: "ideas", Labels: []string{"crew:brainstorm:ready"}},
	{Name: "bugs", Labels: []string{"bug"}},
	{Name: "done", Labels: []string{"crew:brainstorm:done", "crew:triage:done"}},
}

var (
	twelve    = crew.Issue{Key: "12", Ref: "#12", Title: "Rule labels", URL: "https://github.com/o/r/issues/12"}
	twenty    = crew.Issue{Key: "20", Ref: "#20", Title: "Crash on start"}
	twentyOne = crew.Issue{Key: "21", Ref: "#21", Title: "Retry the poll"}
	twentyTwo = crew.Issue{Key: "22", Ref: "#22", Title: "Typo in help"}
)

// held is a snapshot of issue held by rule in claim, with one running
// action.
func held(issue crew.Issue, rule, action string, claim core.Claim) engine.Update {
	return engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: []core.IssueView{{
		Issue: issue, Rule: rule, Queue: "clerk", Claim: claim,
		Actions: []core.ActionView{{Name: action, Phase: core.PhaseRunning, Started: start.Add(-time.Minute)}},
	}}}}}
}

// handledBy is a snapshot of issue handled by rule, moved to to.
func handledBy(issue crew.Issue, rule string, to crew.State) engine.Update {
	e := entry(issue.Key, issue.Title, rule, to, 10, 1)
	e.Issue = issue
	return engine.Update{Snapshot: engine.Snapshot{View: core.View{Handled: []core.HandledView{e}}}}
}

// onBoard is u with the board finding each issue with its labels, in the
// order given.
func onBoard(u engine.Update, issues ...crew.BoardIssue) engine.Update {
	u.Snapshot.Board = issues
	return u
}

// labeled is issue carrying the board labels labels.
func labeled(issue crew.Issue, labels ...string) crew.BoardIssue {
	return crew.BoardIssue{Issue: issue, Labels: labels}
}

// boardOf returns the Board section of view: its rule up to the blank
// line before Actions.
func boardOf(t *testing.T, view string) string {
	t.Helper()
	i := strings.Index(view, "Board ")
	j := strings.Index(view, "\n\nActions ")
	if i < 0 || j < i {
		t.Fatalf("view lacks the Board section:\n%s", view)
	}
	return view[i:j]
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

// cardColumn returns which drawn column holds ref's card, by its x on the
// board's rows.
func cardColumn(t *testing.T, board, ref string) int {
	t.Helper()
	got := cardColumns(board, ref)
	if len(got) == 0 {
		t.Fatalf("board has no card for %s:\n%s", ref, board)
	}
	return got[0]
}

// schedulesSlideTick reports whether cmd, or a command it batches, sends a
// slide frame.
func schedulesSlideTick(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		switch msg := msg.(type) {
		case slideTickMsg:
			return true
		case tea.BatchMsg:
			return slices.ContainsFunc(msg, schedulesSlideTick)
		}
	case <-time.After(200 * time.Millisecond):
	}
	return false
}

// Covers AE5 of #134: the default board of promote triage, triage and
// development has two columns, triage then development, and an issue the
// rule without actions holds has no card.
func TestAE5TheDefaultBoardHasAColumnPerRuleWithActionsInRuleOrder(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, crewBoard[:2])

	h.send(updateMsg(onBoard(held(twelve, "promote triage", "promote", core.ClaimRunning),
		labeled(twenty, "crew:development:ready"))))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "triage development" {
		t.Errorf("columns = %q, want triage development:\n%s", got, board)
	}
	if got := cardColumns(board, "#20"); len(got) != 1 || got[0] != 1 {
		t.Errorf("#20's cards are in columns %v, want one in development (1):\n%s", got, board)
	}
	if strings.Contains(board, "#12") {
		t.Errorf("the board shows #12, which carries no column's label:\n%s", board)
	}
}

// Covers KTD10: an item left in a rule's running label that crew does not
// hold shows in that rule's column as idle.
func TestAnItemInARunningLabelCrewDoesNotHoldHasAnIdleCard(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, crewBoard)

	h.send(updateMsg(onBoard(engine.Update{}, labeled(twelve, "crew:development:in progress"))))
	board := boardOf(t, h.view())

	if col := cardColumn(t, board, "#12"); col != 1 {
		t.Errorf("#12's card is in column %d, want development's 1:\n%s", col, board)
	}
	contains(t, board, "▌ ○ idle")
}

// Covers R23 and R28: an issue whose rule ended in a label no column names
// has no card, and no card waits for the next rule.
func TestAnIssueMovedToALabelNoColumnNamesHasNoCard(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, crewBoard)
	h.send(updateMsg(onBoard(held(twelve, "triage", "triage", core.ClaimRunning),
		labeled(twelve, "crew:triage:in progress"))))

	h.send(updateMsg(handledBy(twelve, "triage", "crew:triage:done")))
	board := boardOf(t, h.view())

	if strings.Contains(board, "#12") || strings.Contains(board, "→") {
		t.Errorf("the board shows #12 or a waiting card:\n%s", board)
	}
	contains(t, board, " 0 issues")
}

// Covers R22: a pull request shows only in a column of pull requests, and
// never in an issue column whose label its issue mirrored onto it.
func TestAnItemShowsOnlyInTheColumnsOfItsKind(t *testing.T) {
	board := append(slices.Clone(crewBoard[1:2]),
		crew.BoardColumn{Name: "fix review", Labels: []string{"crew:fix-review:ready"}, Takes: crew.KindPullRequest})
	pr := crew.Issue{Key: "90", Ref: "#90", Title: "Fix the review", Kind: crew.KindPullRequest}
	h := newBoardHarness(t, 120, crewRules, board)

	h.send(updateMsg(onBoard(engine.Update{},
		labeled(twelve, "crew:development:in progress"),
		labeled(pr, "crew:development:in progress", "crew:fix-review:ready"))))
	got := boardOf(t, h.view())

	if cols := cardColumns(got, "#90"); len(cols) != 1 || cols[0] != 1 {
		t.Errorf("#90's cards are in columns %v, want one in fix review (1):\n%s", cols, got)
	}
	if cols := cardColumns(got, "#12"); len(cols) != 1 || cols[0] != 0 {
		t.Errorf("#12's cards are in columns %v, want one in development (0):\n%s", cols, got)
	}
}

// Covers AE1: a held issue has one card, in the column of its board label.
func TestAE1AHeldIssueHasOneCardInTheColumnOfItsBoardLabel(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)

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

// Covers AE2 and R23: an issue with two columns' labels has a card in each.
func TestAE2AnIssueWithTwoColumnsLabelsHasACardInEach(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)

	h.send(updateMsg(onBoard(engine.Update{}, labeled(twentyOne, "crew:brainstorm:ready", "bug"))))
	board := boardOf(t, h.view())

	if got := cardColumns(board, "#21"); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("#21's cards are in columns %v, want ideas (0) and bugs (1):\n%s", got, board)
	}
}

func TestAHeldIssueOnNoColumnsLabelHasNoCardButShowsInActions(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)

	h.send(updateMsg(onBoard(held(twelve, "development", "lfg", core.ClaimRunning))))
	view := h.view()

	if board := boardOf(t, view); strings.Contains(board, "#12") {
		t.Errorf("the board shows #12, which carries no column's label:\n%s", board)
	}
	contains(t, view, "development/lfg")
}

// The columns show in board order, empty ones included, whatever the
// rules.
func TestTheColumnsShowInBoardOrder(t *testing.T) {
	board := []crew.BoardColumn{
		{Name: "done", Labels: []string{"crew:triage:done"}},
		{Name: "ideas", Labels: []string{"crew:brainstorm:ready"}},
		{Name: "bugs", Labels: []string{"bug"}},
	}
	h := newBoardHarness(t, 120, crewRules, board)
	h.send(updateMsg(onBoard(engine.Update{}, labeled(twenty, "bug"))))

	got := boardOf(t, h.view())
	if names := columnNamesOf(t, got); names != "done ideas bugs" {
		t.Errorf("columns = %q, want done ideas bugs:\n%s", names, got)
	}
}

// Covers KTD6: a column lists its cards as the board orders them, oldest
// first, and "+N more" hides the newest.
func TestAColumnsCardsGoOldestFirstAndTheNewestAreCut(t *testing.T) {
	issues := make([]crew.BoardIssue, 0, 8)
	for n := 1; n <= 8; n++ {
		issues = append(issues, labeled(crew.Issue{Key: strconv.Itoa(n), Ref: fmt.Sprintf("#%d", n), Title: "Bug"}, "bug"))
	}
	h := newBoardHarness(t, 80, crewRules, ideasBugsDone)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(updateMsg(onBoard(engine.Update{}, issues...)))

	view := checkFits(t, h, 80, 24)
	board := boardOf(t, view)
	contains(t, board, "▌ #1 Bug", "more")
	if strings.Contains(board, "#8 ") {
		t.Errorf("the newest card shows while the column is cut:\n%s", board)
	}
	if first, second := strings.Index(board, "#1 "), strings.Index(board, "#2 "); second >= 0 && second < first {
		t.Errorf("#2 is drawn above #1:\n%s", board)
	}
	if !strings.HasSuffix(view, "? help") {
		t.Errorf("view does not end with the key-help line:\n%s", view)
	}
}

// elevenBugs are eleven issues, #1 to #11, each labeled bug.
func elevenBugs() []crew.BoardIssue {
	issues := make([]crew.BoardIssue, 0, 11)
	for n := 1; n <= 11; n++ {
		issues = append(issues, labeled(crew.Issue{Key: strconv.Itoa(n), Ref: fmt.Sprintf("#%d", n), Title: "Bug"}, "bug"))
	}
	return issues
}

// A column shows at most maxCards cards, however tall the window, and
// "+N more" counts the rest.
func TestAColumnShowsAtMostFiveCards(t *testing.T) {
	h := newBoardHarness(t, 80, crewRules, ideasBugsDone)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 200})
	h.send(updateMsg(onBoard(engine.Update{}, elevenBugs()...)))

	board := boardOf(t, checkFits(t, h, 80, 200))
	contains(t, board, "#1 ", "#5 ", "+6 more")
	if strings.Contains(board, "#6 ") {
		t.Errorf("a sixth card shows:\n%s", board)
	}
}

// A window two rows short of five cards takes one card off a column past
// maxCards: its "+N more" row is already drawn.
func TestAShortWindowTakesOneCardOffACappedColumn(t *testing.T) {
	h := newBoardHarness(t, 80, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, elevenBugs()...)))
	least := budget{events: minScroll, handled: minScroll, cards: maxCards, said: false, botDetails: true}
	height := len(h.current().rows(least)) - cardRows
	h.send(tea.WindowSizeMsg{Width: 80, Height: height})

	view := checkFits(t, h, 80, height)
	board := boardOf(t, view)
	contains(t, board, "#4 ", "+7 more")
	if strings.Contains(board, "#5 ") || strings.Contains(view, "lines cut") {
		t.Errorf("at %d rows the column kept five cards or the view was cut:\n%s", height, view)
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

// Covers AE4.
func TestEmptyColumnsDropThenTheBoardScrollsSideways(t *testing.T) {
	u := onBoard(engine.Update{},
		labeled(crew.Issue{Key: "1", Ref: "#1", Title: "One"}, "l2"),
		labeled(crew.Issue{Key: "2", Ref: "#2", Title: "Two"}, "l6"))

	h := newBoardHarness(t, 80, crewRules, eightColumns())
	h.send(updateMsg(u))
	board := boardOf(t, h.view())
	contains(t, board, "6 empty columns not shown")
	if got := columnNamesOf(t, board); got != "c2 c6" {
		t.Errorf("columns = %q, want c2 c6:\n%s", got, board)
	}

	h = newBoardHarness(t, 30, crewRules, eightColumns())
	h.send(updateMsg(u))
	board = boardOf(t, h.view())
	contains(t, board, "c2", "1 ▸", "#1")
	if strings.Contains(board, "#2") {
		t.Errorf("a 30-column board shows both columns:\n%s", board)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	contains(t, boardOf(t, h.view()), "◂ 1", "c6", "#2")
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	contains(t, boardOf(t, h.view()), "◂ 1", "#2")
	h.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	contains(t, boardOf(t, h.view()), "#1", "1 ▸")
}

// The card cap counts only the columns the board draws: a taller column
// scrolled off to the side must not keep the view from fitting (KTD8).
func TestTheCardCapCountsOnlyTheDrawnColumns(t *testing.T) {
	var issues []crew.BoardIssue
	for col, n := range []int{3, 3, 3, 1, 6} {
		for k := range n {
			key := fmt.Sprintf("%d-%d", col, k)
			issues = append(issues, labeled(crew.Issue{Key: key, Ref: "#" + key, Title: "Card"}, fmt.Sprintf("l%d", col+1)))
		}
	}
	u := onBoard(engine.Update{}, issues...)
	// The two heights just above the lowest that fits: a cap counting the
	// scrolled-off column of 6 would leave both cut.
	for _, height := range []int{25, 26} {
		h := newBoardHarness(t, 80, crewRules, eightColumns()[:5])
		h.send(tea.WindowSizeMsg{Width: 80, Height: height})
		h.send(updateMsg(u))

		if view := checkFits(t, h, 80, height); strings.Contains(view, "lines cut") {
			t.Errorf("at %d rows the view was cut although a lower card cap fits:\n%s", height, view)
		}
	}
}

// Covers KTD5 and KTD8.
func TestTheSummaryCountsIssuesAndSaysWhenTheBoardWasNotRead(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	contains(t, boardOf(t, h.view()), "Board ", " 0 issues")

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

// Covers R11 and KTD13: each claim reads through its icon.
func TestEachClaimReadsThroughItsIcon(t *testing.T) {
	for claim, want := range map[core.Claim]string{
		core.ClaimRunning: "⠋ running", core.ClaimJudging: "⠋ judging", core.ClaimTaking: "◌ taking",
		core.ClaimOwed: "! owed", core.ClaimStopping: "■ stopping",
	} {
		h := newBoardHarness(t, 120, crewRules, crewBoard)
		h.send(updateMsg(onBoard(held(twelve, "triage", "triage", claim), labeled(twelve, "crew:triage:in progress"))))
		contains(t, boardOf(t, h.view()), "▌ "+want)
	}
}

// Covers #126: an unheld card keeps its two rows, the second a status of
// its own, ○ idle, and no claim.
func TestAnUnheldCardShowsIdle(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)

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
