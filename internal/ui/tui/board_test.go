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

// crewNotify is which of this repository's rules notify: triage,
// development and fix; both promote rules are muted, as rules without
// actions are by default.
var crewNotify = map[crew.RuleName]bool{
	"promote brainstorm": false, "triage": true, "promote triage": false, "development": true, "fix": true,
}

// crewBoard is the default board of this repository's rules, as config
// builds it: a column per rule with actions, triage, development and fix, each with
// its ready and running labels (R22).
var crewBoard = []crew.BoardColumn{
	{Name: "triage", Labels: []crew.State{"crew:triage:ready", "crew:triage:in progress"}},
	{Name: "development", Labels: []crew.State{"crew:development:ready", "crew:development:in progress"}},
	{Name: "fix", Labels: []crew.State{"crew:fix:ready", "crew:fix:in progress"}},
}

// ideasBugsDone is a written board.
var ideasBugsDone = []crew.BoardColumn{
	{Name: "ideas", Labels: []crew.State{"crew:brainstorm:ready"}},
	{Name: "bugs", Labels: []crew.State{"bug"}},
	{Name: "done", Labels: []crew.State{"crew:brainstorm:done", "crew:triage:done"}},
}

var (
	twelve = crew.NewIssue(crew.IssueData{
		ID: issueID("12"), Ref: "#12", Title: "Rule labels", URL: "https://github.com/o/r/issues/12",
	})
	twenty    = crew.NewIssue(crew.IssueData{ID: issueID("20"), Ref: "#20", Title: "Crash on start"})
	twentyOne = crew.NewIssue(crew.IssueData{ID: issueID("21"), Ref: "#21", Title: "Retry the poll"})
	twentyTwo = crew.NewIssue(crew.IssueData{ID: issueID("22"), Ref: "#22", Title: "Typo in help"})
)

// held is a snapshot of issue held by rule in claim, with one running
// action.
func held(issue crew.Issue, rule crew.RuleName, action crew.ActionName, claim core.Claim) engine.Update {
	return engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: []core.IssueView{{
		Issue: issue, Rule: rule, Queue: "clerk", Claim: claim,
		Actions: []core.ActionView{{Name: action, Phase: core.PhaseRunning, Started: start.Add(-time.Minute)}},
	}}}}}
}

// handledBy is a snapshot of issue handled by rule, moved to to.
func handledBy(issue crew.Issue, rule crew.RuleName, to crew.State) engine.Update {
	e := entry(issue.ID().Key, issue.Title(), rule, to, 10, 1)
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
func labeled(issue crew.Issue, labels ...crew.State) crew.BoardIssue {
	return crew.NewBoardIssue(issue, labels)
}

// boardOf returns the Board section of view: its rule up to the blank
// line before the band of Queues and Events.
func boardOf(t *testing.T, view string) string {
	t.Helper()
	i := strings.Index(view, "Board ")
	j := strings.Index(view, "\n\nQueues ")
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

// columnWidth returns the width of the board's columns: that of its first
// card, from border to border.
func columnWidth(board string) int {
	for l := range strings.SplitSeq(board, "\n") {
		if _, rest, found := strings.Cut(l, "╭"); found {
			top, _, _ := strings.Cut(rest, "╮")
			return len([]rune(top)) + 2
		}
	}
	return maxColumn
}

// cardColumns returns every drawn column holding a card for ref, by its x
// on the board's rows.
func cardColumns(board, ref string) []int {
	var out []int
	step := columnWidth(board) + columnGap
	for l := range strings.SplitSeq(board, "\n") {
		rest, x := l, 0
		for {
			i := nextCard(rest, ref+" ")
			if i < 0 {
				break
			}
			x += len([]rune(rest[:i]))
			out = append(out, x/step)
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
// rule without actions holds has no card, not even in Not on board.
func TestAE5TheDefaultBoardHasAColumnPerRuleWithActionsInRuleOrder(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard[:2])
	u := held(twelve, "promote triage", "promote", core.ClaimRunning)
	u.Snapshot.Issues[0].Actions = nil

	h.send(updateMsg(onBoard(u, labeled(twenty, "crew:development:ready"))))
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
	h := newBoardHarness(t, 120, crewNotify, crewBoard)

	h.send(updateMsg(onBoard(engine.Update{}, labeled(twelve, "crew:development:in progress"))))
	board := boardOf(t, h.view())

	if col := cardColumn(t, board, "#12"); col != 1 {
		t.Errorf("#12's card is in column %d, want development's 1:\n%s", col, board)
	}
	contains(t, board, "run  ○ idle")
}

// Covers R23 and R28, and R7 of #230: an issue whose rule ended in a label
// no column names has no card, and no card waits for the next rule.
func TestAnIssueMovedToALabelNoColumnNamesHasNoCard(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(updateMsg(onBoard(held(twelve, "triage", "triage", core.ClaimRunning),
		labeled(twelve, "crew:triage:in progress"))))

	h.send(updateMsg(handledBy(twelve, "triage", "crew:triage:done")))
	board := boardOf(t, h.view())

	if got := cardColumns(board, "#12"); len(got) != 0 || strings.Contains(board, "→") {
		t.Errorf("#12's cards are in columns %v, want none, or a card waits:\n%s", got, board)
	}
	contains(t, board, " 0 issues")
}

// Covers R22: a pull request shows only in a column of pull requests, and
// never in an issue column whose label its issue mirrored onto it.
func TestAnItemShowsOnlyInTheColumnsOfItsKind(t *testing.T) {
	board := append(slices.Clone(crewBoard[1:2]),
		crew.BoardColumn{Name: "fix review", Labels: []crew.State{"crew:fix-review:ready"}, Takes: crew.KindPullRequest})
	pr := crew.NewIssue(crew.IssueData{ID: issueID("90"), Ref: "#90", Title: "Fix the review", Kind: crew.KindPullRequest})
	h := newBoardHarness(t, 120, crewNotify, board)

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
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)

	h.send(updateMsg(onBoard(held(twenty, "fix", "lfg", core.ClaimRunning), labeled(twenty, "bug"))))
	board := boardOf(t, h.view())

	if got := columnNamesOf(t, board); got != "ideas bugs done" {
		t.Errorf("columns = %q, want ideas bugs done:\n%s", got, board)
	}
	if got := cardColumns(board, "#20"); len(got) != 1 || got[0] != 1 {
		t.Errorf("#20's cards are in columns %v, want one in bugs (1):\n%s", got, board)
	}
	contains(t, board, "run  ⠋ lfg 1m")
}

// Covers AE2 and R23: an issue with two columns' labels has a card in each.
func TestAE2AnIssueWithTwoColumnsLabelsHasACardInEach(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)

	h.send(updateMsg(onBoard(engine.Update{}, labeled(twentyOne, "crew:brainstorm:ready", "bug"))))
	board := boardOf(t, h.view())

	if got := cardColumns(board, "#21"); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("#21's cards are in columns %v, want ideas (0) and bugs (1):\n%s", got, board)
	}
}

// The columns show in board order, empty ones included, whatever the
// rules.
func TestTheColumnsShowInBoardOrder(t *testing.T) {
	board := []crew.BoardColumn{
		{Name: "done", Labels: []crew.State{"crew:triage:done"}},
		{Name: "ideas", Labels: []crew.State{"crew:brainstorm:ready"}},
		{Name: "bugs", Labels: []crew.State{"bug"}},
	}
	h := newBoardHarness(t, 120, crewNotify, board)
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
		issues = append(issues, labeled(crew.NewIssue(crew.IssueData{ID: issueID(strconv.Itoa(n)), Ref: fmt.Sprintf("#%d", n),
			Title: "Bug"}), "bug"))
	}
	h := newBoardHarness(t, 80, crewNotify, ideasBugsDone)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(updateMsg(onBoard(engine.Update{}, issues...)))

	view := checkFits(t, h, 80, 24)
	board := boardOf(t, view)
	contains(t, board, "│ ▸ #1 Bug", "more")
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

// holding is a snapshot of crew holding the issues keys, each titled Bug,
// in claim, with one running action.
func holding(claim core.Claim, keys ...string) engine.Update {
	var u engine.Update
	for _, k := range keys {
		u.Snapshot.Issues = append(u.Snapshot.Issues,
			held(titledIssue(k, "Bug"), "fix", "lfg", claim).Snapshot.Issues...)
	}
	return u
}

// cardOrder returns the refs with a card on board, by the board row of
// their first card, top first; a ref with no card is left out.
func cardOrder(board string, refs ...string) []string {
	row := map[string]int{}
	for i, l := range strings.Split(board, "\n") {
		for _, ref := range refs {
			if _, found := row[ref]; !found && nextCard(l, ref+" ") >= 0 {
				row[ref] = i
			}
		}
	}
	out := slices.DeleteFunc(slices.Clone(refs), func(ref string) bool { _, found := row[ref]; return !found })
	slices.SortStableFunc(out, func(a, b string) int { return row[a] - row[b] })
	return out
}

// Covers AE1, R1 and R2 of #231: a column lists the cards of the items
// crew holds first, then the others, each group in board order.
func TestAE1HeldCardsComeFirstInTheirColumn(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)

	h.send(updateMsg(onBoard(holding(core.ClaimRunning, "15", "12"),
		item("10", "bug"), item("12", "bug"), item("15", "bug"))))
	board := boardOf(t, h.view())

	if got, want := cardOrder(board, "#10", "#12", "#15"), []string{"#12", "#15", "#10"}; !slices.Equal(got, want) {
		t.Errorf("the column shows %v, want %v:\n%s", got, want, board)
	}
}

// Covers AE2 and R3 of #231: a card is held whatever its issue's claim.
func TestAE2EveryClaimKeepsACardHeld(t *testing.T) {
	for _, claim := range []core.Claim{
		core.ClaimTaking, core.ClaimRunning, core.ClaimStopping, core.ClaimJudging, core.ClaimOwed,
	} {
		h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)

		h.send(updateMsg(onBoard(holding(claim, "15"),
			item("10", "bug"), item("12", "bug"), item("15", "bug"))))
		board := boardOf(t, h.view())

		if got, want := cardOrder(board, "#10", "#12", "#15"), []string{"#15", "#10", "#12"}; !slices.Equal(got, want) {
			t.Errorf("%s: the column shows %v, want %v:\n%s", claim, got, want, board)
		}
	}
}

// Covers R1 of #231: a held issue with cards in two columns comes first
// in both.
func TestAHeldIssueComesFirstInEachOfItsColumns(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)

	h.send(updateMsg(onBoard(holding(core.ClaimRunning, "21"),
		labeled(twenty, "crew:brainstorm:ready", "bug"), labeled(twentyOne, "crew:brainstorm:ready", "bug"))))
	board := boardOf(t, h.view())

	for _, ref := range []string{"#20", "#21"} {
		if got := cardColumns(board, ref); !slices.Equal(got, []int{0, 1}) {
			t.Fatalf("%s's cards are in columns %v, want ideas (0) and bugs (1):\n%s", ref, got, board)
		}
	}
	if got, want := cardOrder(board, "#20", "#21"), []string{"#21", "#20"}; !slices.Equal(got, want) {
		t.Errorf("the columns show %v, want %v:\n%s", got, want, board)
	}
}

// Covers R4 of #231: the Not on board column keeps the order crew holds
// its issues in, after the configured columns.
func TestNotOnBoardKeepsItsOrder(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)

	h.send(updateMsg(onBoard(holding(core.ClaimRunning, "31", "30"), item("10", "bug"))))
	board := boardOf(t, h.view())

	if got := cardColumn(t, board, "#31"); got != 3 {
		t.Errorf("#31's card is in column %d, want Not on board (3):\n%s", got, board)
	}
	if got, want := cardOrder(board, "#30", "#31"), []string{"#31", "#30"}; !slices.Equal(got, want) {
		t.Errorf("Not on board shows %v, want %v:\n%s", got, want, board)
	}
}

// elevenBugs are eleven issues, #1 to #11, each labeled bug.
func elevenBugs() []crew.BoardIssue {
	issues := make([]crew.BoardIssue, 0, 11)
	for n := 1; n <= 11; n++ {
		issues = append(issues, labeled(crew.NewIssue(crew.IssueData{ID: issueID(strconv.Itoa(n)), Ref: fmt.Sprintf("#%d", n),
			Title: "Bug"}), "bug"))
	}
	return issues
}

// A column shows at most maxCards cards, however tall the window, and
// "+N more" counts the rest.
func TestAColumnShowsAtMostFiveCards(t *testing.T) {
	h := newBoardHarness(t, 80, crewNotify, ideasBugsDone)
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
	h := newBoardHarness(t, 80, crewNotify, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, elevenBugs()...)))
	least := budget{events: minScroll, cards: maxCards, botCards: true}
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
		label := crew.State(fmt.Sprintf("l%d", i))
		out = append(out, crew.BoardColumn{Name: fmt.Sprintf("c%d", i), Labels: []crew.State{label}})
	}
	return out
}

// Covers AE4.
func TestEmptyColumnsDropThenTheBoardScrollsSideways(t *testing.T) {
	u := onBoard(engine.Update{},
		labeled(crew.NewIssue(crew.IssueData{ID: issueID("1"), Ref: "#1", Title: "One"}), "l2"),
		labeled(crew.NewIssue(crew.IssueData{ID: issueID("2"), Ref: "#2", Title: "Two"}), "l6"))

	h := newBoardHarness(t, 80, crewNotify, eightColumns())
	h.send(updateMsg(u))
	board := boardOf(t, h.view())
	contains(t, board, "6 empty columns not shown")
	if got := columnNamesOf(t, board); got != "c2 c6" {
		t.Errorf("columns = %q, want c2 c6:\n%s", got, board)
	}

	h = newBoardHarness(t, 30, crewNotify, eightColumns())
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
			label := crew.State(fmt.Sprintf("l%d", col+1))
			issues = append(issues, labeled(titledIssue(key, "Card"), label))
		}
	}
	u := onBoard(engine.Update{}, issues...)
	// The two lowest heights that fit: a cap counting the scrolled-off
	// column of 6 would leave both cut.
	for _, height := range []int{21, 22} {
		h := newBoardHarness(t, 80, crewNotify, eightColumns()[:5])
		h.send(tea.WindowSizeMsg{Width: 80, Height: height})
		h.send(updateMsg(u))

		if view := checkFits(t, h, 80, height); strings.Contains(view, "lines cut") {
			t.Errorf("at %d rows the view was cut although a lower card cap fits:\n%s", height, view)
		}
	}
}

// Covers KTD5 and KTD8.
func TestTheSummaryCountsIssuesAndSaysWhenTheBoardWasNotRead(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)
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

// Covers R2 of #151: a held issue whose actions all ended reads its claim
// through its icon.
func TestEachClaimReadsThroughItsIcon(t *testing.T) {
	for claim, want := range map[core.Claim]string{
		core.ClaimRunning: "run  ⠋ running", core.ClaimJudging: "run  ⠋ judging", core.ClaimTaking: "run  ◌ taking",
		core.ClaimOwed: "run  ! owed", core.ClaimStopping: "run  ■ stopping",
	} {
		h := newBoardHarness(t, 120, crewNotify, crewBoard)
		u := held(twelve, "triage", "triage", claim)
		u.Snapshot.Issues[0].Actions[0].Phase = core.PhaseEnded
		h.send(updateMsg(onBoard(u, labeled(twelve, "crew:triage:in progress"))))
		if got := faceOf(t, boardOf(t, h.view()), "#12")[1]; got != want {
			t.Errorf("%s: run row = %q, want %q", claim, got, want)
		}
	}
}

// Covers #126: an unheld card reads ○ idle, no bots and no queue.
func TestAnUnheldCardShowsIdle(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)

	h.send(updateMsg(onBoard(held(twenty, "fix", "lfg", core.ClaimRunning),
		labeled(twenty, "bug"), labeled(twentyTwo, "bug"))))
	board := boardOf(t, h.view())

	want := []string{"#22 Typo in help", "run  ○ idle", "bots none", "via  none"}
	if got := faceOf(t, board, "#22"); !slices.Equal(got, want) {
		t.Errorf("#22's card = %q, want %q:\n%s", got, want, board)
	}
	if strings.Count(board, "○ idle") != 1 {
		t.Errorf("want one idle card, #22's; the running #20 is not idle:\n%s", board)
	}
}
