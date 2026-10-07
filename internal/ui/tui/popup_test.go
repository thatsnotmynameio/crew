package tui

import (
	"fmt"
	"image/color"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

var enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}

// popupBox returns the rows of the popup in view, its border included,
// the index of its top row and of its left border; nil when view shows
// no popup of width cells.
func popupBox(view string, width int) ([]string, int, int) {
	top := "╭" + strings.Repeat("─", width-2) + "╮"
	rows := rowsOf(view)
	for i, l := range rows {
		before, _, found := strings.Cut(l, top)
		if !found {
			continue
		}
		x := len([]rune(before))
		var out []string
		for _, r := range rows[i:] {
			runes := []rune(r)
			if len(runes) < x+width {
				break
			}
			out = append(out, string(runes[x:x+width]))
			if runes[x] == '╰' {
				return out, i, x
			}
		}
	}
	return nil, -1, -1
}

// popupWidth is the width of the popup in a window width cells wide.
func popupWidthIn(width int) int { return min(width-4, 100) }

// popupRows returns what the rows of the popup in h's view hold inside
// its border, right-trimmed, its title first, and fails t when no popup
// shows.
func popupRows(t *testing.T, h *harness) []string {
	t.Helper()
	box, _, _ := popupBox(h.view(), popupWidthIn(h.current().width))
	if box == nil {
		t.Fatalf("no popup shows:\n%s", h.view())
	}
	out := make([]string, 0, len(box)-2)
	for _, r := range box[1 : len(box)-1] {
		out = append(out, strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(r, "│ "), "│"), " "))
	}
	return out
}

// popupShows reports whether h's view shows a popup.
func popupShows(h *harness) bool {
	box, _, _ := popupBox(h.view(), popupWidthIn(h.current().width))
	return box != nil
}

// field returns the value after label on the popup's header row for it,
// or fails t.
func field(t *testing.T, rows []string, label string) string {
	t.Helper()
	for _, r := range rows {
		if after, ok := strings.CutPrefix(r, label+" "); ok {
			return strings.TrimSpace(after)
		}
	}
	t.Fatalf("the popup has no %q row:\n%s", label, strings.Join(rows, "\n"))
	return ""
}

// words is each row with its runs of spaces made one.
func words(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strings.Join(strings.Fields(r), " "))
	}
	return out
}

// hasRow fails t unless rows hold want, spaces collapsed, and returns its
// index.
func hasRow(t *testing.T, rows []string, want string) int {
	t.Helper()
	i := slices.Index(words(rows), want)
	if i < 0 {
		t.Errorf("the popup lacks the row %q:\n%s", want, strings.Join(rows, "\n"))
	}
	return i
}

// fg is the escape sequence that sets the foreground to c.
func fg(c color.Color) string { return sgr(38, c) }

// sgr is the escape sequence that sets layer, 38 for the foreground or
// 48 for the background, to c.
func sgr(layer int, c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", layer, r>>8, g>>8, b>>8)
}

// Covers AE4, R10 and R13 of #151: right after the first snapshot, → then
// Enter opens the popup of the first card in the second column; → in it
// shows the next card's popup, and Esc closes it with that card
// highlighted.
func TestAE4EnterOpensTheHighlightedCardsPopupAndArrowsWalkTheCards(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handledSnapshot()))

	h.send(rightKey)
	h.send(enterKey)
	if got := popupRows(t, h)[0]; got != "#2 Fix the flaky stream test" {
		t.Fatalf("popup title = %q, want #2's", got)
	}
	h.send(rightKey)
	if got := popupRows(t, h)[0]; got != "#7 Log the poll interval" {
		t.Fatalf("after →, popup title = %q, want #7's, the next card", got)
	}
	h.send(escKey)
	if popupShows(h) {
		t.Fatalf("esc left the popup open:\n%s", h.view())
	}
	wantLit(t, h, "#7", 1)
}

// Covers KTD12 of #151: in the popup ←→ walk every card in board order,
// down each column and on to the next, and stop at the first and last
// card.
func TestThePopupWalksTheCardsInBoardOrderAndStopsAtTheEnds(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handledSnapshot()))
	h.send(enterKey)

	titles := make([]string, 0, 5)
	for range 5 {
		ref, _, _ := strings.Cut(popupRows(t, h)[0], " ")
		titles = append(titles, ref)
		h.send(rightKey)
	}
	if want := []string{"#1", "#2", "#7", "#7", "#7"}; !slices.Equal(titles, want) {
		t.Errorf("→ walked %q, want %q", titles, want)
	}
	for range 5 {
		h.send(leftKey)
	}
	if got := popupRows(t, h)[0]; got != "#1 Add login form" {
		t.Errorf("← past the first card shows %q, want #1's popup", got)
	}
}

// Covers R1 of #231: in the popup → walks a column in the order it shows
// its cards, the held ones first.
func TestThePopupWalksTheHeldCardsFirst(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(holding(core.ClaimRunning, "12"), item("10", "bug"), item("12", "bug"))))
	h.send(enterKey)
	if got := popupRows(t, h)[0]; !strings.HasPrefix(got, "#12 ") {
		t.Fatalf("Enter opens %q, want #12's popup", got)
	}

	h.send(rightKey)

	if got := popupRows(t, h)[0]; !strings.HasPrefix(got, "#10 ") {
		t.Errorf("→ opens %q, want #10's popup", got)
	}
}

// headerIssue is #3, with everything the popup's header shows.
var headerIssue = crew.Issue{
	ID: issueID("3"), Ref: "#3", Title: "Speed up the poll", URL: "https://github.com/o/r/issues/3",
	Priority: 2, Blocked: true, States: []crew.State{"in progress"},
}

// Covers R15 and KTD8 of #151, and AE3, AE4, R3 and R4 of #229: the
// header shows the rule, the crew state and board labels as chips, each
// once, then a blocked chip when the issue is blocked, the kind, the
// priority and the URL, and no blocked row.
func TestThePopupHeaderShowsTheIssuesFields(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(onBoard(held(headerIssue, "implement", "code", core.ClaimRunning),
		labeled(headerIssue, "in progress", "bug"))))
	h.send(enterKey)

	rows := popupRows(t, h)
	for label, want := range map[string]string{
		"rule": "implement", "kind": "issue", "priority": "P2", "url": headerIssue.URL,
	} {
		if got := field(t, rows, label); got != want {
			t.Errorf("%s = %q, want %q", label, got, want)
		}
	}
	if got := strings.Fields(field(t, rows, "labels")); !slices.Equal(got, []string{"in", "progress", "bug", "blocked"}) {
		t.Errorf("labels = %q, want the state in progress once, bug, then blocked", got)
	}
	if !strings.Contains(h.raw(), sgr(48, darkPalette().chipBack)) {
		t.Error("the labels are not drawn as chips")
	}
	if !strings.Contains(h.raw(), blockedChip(darkPalette())) {
		t.Error("the blocked chip is not drawn in the warning colour on the chips' background")
	}
	noBlockedRow(t, rows)

	plain := headerIssue
	plain.Priority, plain.Blocked = 0, false
	h.send(updateMsg(onBoard(held(plain, "implement", "code", core.ClaimRunning), labeled(plain, "in progress"))))
	rows = popupRows(t, h)
	if got := field(t, rows, "priority"); got != "none" {
		t.Errorf("priority 0 reads %q, want none", got)
	}
	if got := field(t, rows, "labels"); got != "in progress" {
		t.Errorf("an unblocked issue's labels = %q, want in progress alone", got)
	}
	noBlockedRow(t, rows)
}

// blockedChip is the blocked chip in palette p as the popup draws it: the
// chips' background, its padding, then its text in the warning colour.
func blockedChip(p palette) string {
	return sgr(48, p.chipBack) + " " + fg(p.warning) + "blocked"
}

// noBlockedRow fails t when one of the popup's rows is a blocked row.
func noBlockedRow(t *testing.T, rows []string) {
	t.Helper()
	for _, r := range rows {
		if strings.HasPrefix(r, "blocked ") {
			t.Errorf("the popup has the row %q, want no blocked row", r)
		}
	}
}

// Covers R3 and KTD2 of #229: a blocked issue with no labels shows the
// blocked chip alone, not none.
func TestABlockedIssueWithNoLabelsShowsTheBlockedChipAlone(t *testing.T) {
	bare := headerIssue
	bare.States = nil
	h := newHarness(t, 120)
	h.send(updateMsg(held(bare, "implement", "code", core.ClaimRunning)))
	h.send(enterKey)

	if got := field(t, popupRows(t, h), "labels"); got != "blocked" {
		t.Errorf("labels = %q, want the blocked chip alone", got)
	}
}

// Covers R3 and R5 of #229: a blocked issue that also carries a label named
// blocked shows blocked once, as the blocked chip; an issue nothing blocks
// keeps that label as a plain chip.
func TestALabelNamedBlockedGivesWayToTheBlockedChip(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(onBoard(held(headerIssue, "implement", "code", core.ClaimRunning),
		labeled(headerIssue, "blocked", "bug"))))
	h.send(enterKey)

	got := strings.Fields(field(t, popupRows(t, h), "labels"))
	if !slices.Equal(got, []string{"in", "progress", "bug", "blocked"}) {
		t.Errorf("labels = %q, want in progress, bug, then blocked once", got)
	}
	if !strings.Contains(h.raw(), blockedChip(darkPalette())) {
		t.Error("the one blocked is not the blocked chip")
	}

	free := headerIssue
	free.Blocked = false
	h.send(updateMsg(onBoard(held(free, "implement", "code", core.ClaimRunning),
		labeled(free, "blocked", "bug"))))
	got = strings.Fields(field(t, popupRows(t, h), "labels"))
	if !slices.Equal(got, []string{"in", "progress", "blocked", "bug"}) {
		t.Errorf("labels = %q, want the label blocked in its place", got)
	}
	if strings.Contains(h.raw(), blockedChip(darkPalette())) {
		t.Error("an issue nothing blocks shows the blocked chip")
	}
}

// Covers AE5, R3 and KTD3 of #229: a held issue's view keeps the issue as
// core took it, unblocked, while its board item is blocked; its popup
// shows the blocked chip.
func TestAE5AHeldIssueBlockedOnTheBoardShowsTheBlockedChip(t *testing.T) {
	taken := headerIssue
	taken.Blocked = false
	h := newHarness(t, 120)
	h.send(updateMsg(onBoard(held(taken, "implement", "code", core.ClaimRunning),
		labeled(headerIssue, "in progress"))))
	h.send(enterKey)

	got := strings.Fields(field(t, popupRows(t, h), "labels"))
	if !slices.Equal(got, []string{"in", "progress", "blocked"}) {
		t.Errorf("labels = %q, want in progress, then blocked", got)
	}
}

// Covers R3 and KTD3 of #229: core keeps an issue whose rule ended as it
// took it, unblocked, while the same issue on the board is blocked; its
// card stays in its label's column (#230) and its popup shows the blocked
// chip.
func TestAnIssueWhoseRuleEndedAndIsBlockedOnTheBoardShowsTheBlockedChip(t *testing.T) {
	h := newBoardHarness(t, 120, crewRules, ideasBugsDone)
	h.send(updateMsg(onBoard(engine.Update{}, item("20", "bug"), item("22", "bug"))))
	h.send(downKey)

	blocked := item("22", "crew:triage:done")
	blocked.Issue.Blocked = true
	u := handledBy(crew.Issue{ID: issueID("22"), Ref: "#22", Title: "Bug"}, "fix", "crew:triage:done")
	h.send(updateMsg(onBoard(u, item("20", "bug"), blocked)))
	wantLit(t, h, "#22", 2)
	h.send(enterKey)

	if got := strings.Fields(field(t, popupRows(t, h), "labels")); !slices.Contains(got, "blocked") {
		t.Errorf("labels = %q, want a blocked chip", got)
	}
}

// Covers R15, R16 and R19 of #151: an issue crew does not hold has no
// rule, no actions and, with none recent, no events.
func TestThePopupOfAnIdleIssueHasNoRuleAndNoActions(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(onBoard(engine.Update{}, labeled(twenty, "ready"))))
	h.send(enterKey)

	rows := popupRows(t, h)
	if got := field(t, rows, "rule"); got != "none" {
		t.Errorf("rule = %q, want none", got)
	}
	hasRow(t, rows, "no actions")
	if rows[len(rows)-1] != "none" {
		t.Errorf("the popup of an issue with no events ends in %q, want none", rows[len(rows)-1])
	}
}

// fourActions is #1 of runningSnapshot held by implement with code
// running as crew-dev, tests waiting, docs done and lint failed; every
// action but code acts as you.
func fourActions() engine.Update {
	u := runningSnapshot()
	iv := &u.Snapshot.Issues[0]
	iv.Actions = []core.ActionView{
		{Name: "code", Phase: core.PhaseRunning, Branch: "crew/1-code", Started: start.Add(-5*time.Minute - 3*time.Second)},
		{Name: "tests", Phase: core.PhaseWaiting},
		{Name: "docs", Phase: core.PhaseEnded, Branch: "crew/1-docs", Outcome: crew.Outcome{Succeeded: true}},
		{
			Name: "lint", Phase: core.PhaseEnded, Branch: "crew/1-lint",
			Outcome: crew.Outcome{Reason: crew.NewSessionText("exited 1")},
		},
	}
	u.Snapshot.Bots = []core.BotView{
		{Name: "crew-dev", Acting: true, Pairs: []string{"implement/code"},
			Running: []core.RunningAction{{IssueRef: "#1", Rule: "implement", Action: "code"}}},
		you([]string{"implement/tests", "implement/docs", "implement/lint", "review/check"}),
	}
	return u
}

// Covers R16 and KTD8 of #151: the actions table has a header row, then
// a row per action with its bot, queue, state and branch.
func TestThePopupTableShowsEachActionsBotQueueStateAndBranch(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(fourActions()))
	h.send(enterKey)

	rows := popupRows(t, h)
	for _, want := range []string{
		"action bot queue state branch",
		"code ■ crew-dev default running 5m03s crew/1-code",
		"tests ■ you default waiting",
		"docs ■ you default done crew/1-docs",
		"lint ■ you default failed crew/1-lint",
	} {
		hasRow(t, rows, want)
	}
	if i := hasRow(t, rows, "lint ■ you default failed crew/1-lint"); i >= 0 && words(rows)[i+1] != "└ exited 1" {
		t.Errorf("under lint's row is %q, want why it failed", rows[i+1])
	}
}

// Covers KTD8 of #151: an action whose bot cannot act shows the you entry
// as its bot while it is not running.
func TestAnActionWhoseBotCannotActShowsYou(t *testing.T) {
	h := newHarness(t, 120)
	u := held(headerIssue, "implement", "code", core.ClaimTaking)
	u.Snapshot.Issues[0].Actions[0].Phase = core.PhaseWaiting
	u.Snapshot.Bots = []core.BotView{
		{Name: "crew-dev", ActsAsYou: true, Pairs: []string{"implement/code"}},
		you([]string{"implement/code"}),
	}
	h.send(updateMsg(onBoard(u, labeled(headerIssue, "in progress"))))
	h.send(enterKey)

	hasRow(t, popupRows(t, h), "code ■ you clerk waiting")
}

// Covers AE5, R17 and R18 of #151: an action that said something and
// then succeeded still shows its last message under its row, and one its
// check failed shows the check's reason in the error colour.
func TestAE5AnEndedActionShowsItsLastMessageAndAFailedOneWhy(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(saying(core.Said{
		IssueID: issueID("1"), Action: "code", Text: crew.NewSaid("running the tests now"),
	})))
	ended := runningSnapshot()
	actions := &ended.Snapshot.Issues[0].Actions
	(*actions)[0].Phase, (*actions)[0].Outcome = core.PhaseEnded, crew.Outcome{Succeeded: true}
	*actions = append(*actions, core.ActionView{
		Name: "docs", Phase: core.PhaseEnded, Outcome: crew.Outcome{Reason: crew.NewSessionText("check failed: exited 2")},
	})
	h.send(updateMsg(ended))
	h.send(enterKey)

	rows := words(popupRows(t, h))
	if i := hasRow(t, rows, "code ■ you default done crew/1-code"); i >= 0 && rows[i+1] != "└ running the tests now" {
		t.Errorf("under code's row is %q, want its last message", rows[i+1])
	}
	if i := hasRow(t, rows, "docs default failed"); i >= 0 && rows[i+1] != "└ check failed: exited 2" {
		t.Errorf("under docs' row is %q, want its check's reason", rows[i+1])
	}
	if !strings.Contains(h.raw(), fg(darkPalette().error)+"└ check failed: exited 2") {
		t.Error("the check's reason is not in the error colour")
	}
	if !strings.Contains(h.raw(), fg(darkPalette().muted)+"└ running the tests now") {
		t.Error("the last message is not muted")
	}
}

// Covers R19 and KTD8 of #151: the popup lists its issue's events from
// the recent ones, oldest first, with their times, and no other issue's.
func TestThePopupListsOnlyItsIssuesEventsOldestFirst(t *testing.T) {
	h := newHarness(t, 120)
	u := runningSnapshot()
	one, two := u.Snapshot.Issues[0].Issue, u.Snapshot.Issues[1].Issue
	u.Snapshot.Recent = []core.Event{
		core.IssueTaken{At: start.Add(-7 * time.Minute), Issue: one, Rule: "implement", From: "ready", To: "in progress"},
		core.ActionStarted{At: start.Add(-6 * time.Minute), IssueID: issueID("1"), IssueRef: "#1", Rule: "implement",
			Action: "tests", Branch: "crew/1-tests", Log: ".crew/logs/1-tests.log"},
		core.IssueTaken{At: start.Add(-5 * time.Minute), Issue: two, Rule: "review",
			From: "ready to review", To: "in review"},
		core.CallOwed{At: start.Add(-4 * time.Minute), Call: core.Call{Kind: core.CallMove, IssueID: issueID("1"),
			IssueRef: "#1", From: "ready", To: "done"}, Reason: "rate limited"},
		core.PollDone{At: start.Add(-3 * time.Minute), Listed: 2},
	}
	h.send(updateMsg(u))
	h.send(enterKey)

	rows := popupRows(t, h)
	at := slices.Index(rows, "Events")
	if at < 0 {
		t.Fatalf("the popup has no Events row:\n%s", strings.Join(rows, "\n"))
	}
	var want []string
	for _, e := range []core.Event{u.Snapshot.Recent[0], u.Snapshot.Recent[1], u.Snapshot.Recent[3]} {
		want = append(want, e.Time().In(zone).Format(time.TimeOnly)+" "+lines.Text(e))
	}
	if got := rows[at+1:]; !slices.Equal(got, want) {
		t.Errorf("events = %q, want %q", got, want)
	}

	h.send(rightKey)
	rows = popupRows(t, h)
	taken := u.Snapshot.Recent[2]
	if got, want := rows[len(rows)-1], taken.Time().In(zone).Format(time.TimeOnly)+" "+lines.Text(taken); got != want {
		t.Errorf("#2's popup ends in %q, want its one event %q", got, want)
	}
}

// Covers R10 of #230: the popup of an issue whose rule ended this run
// shows no cost and no pull request, only what any card's popup shows.
func TestThePopupOfAHandledIssueShowsNoCostNorPullRequests(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handledSnapshot()))
	h.send(rightKey)
	h.send(downKey)
	h.send(enterKey)

	rows := popupRows(t, h)
	if rows[0] != "#7 Log the poll interval" {
		t.Fatalf("the popup is %q's, want #7's", rows[0])
	}
	text := strings.Join(rows, "\n")
	if strings.Contains(text, "$") || strings.Contains(text, "pull request") || strings.Contains(text, "#45") {
		t.Errorf("#7's popup shows a cost or a pull request:\n%s", text)
	}
	if i := slices.IndexFunc(words(rows), func(r string) bool { return strings.HasPrefix(r, "cost ") }); i >= 0 {
		t.Errorf("#7's popup has a cost row %q", rows[i])
	}
}

// Covers KTD8 of #151: a message longer than the popup's inner width
// wraps onto indented rows, and no row crosses the border.
func TestALongMessageWrapsInsideThePopup(t *testing.T) {
	h := newHarness(t, 60)
	long := strings.Repeat("the parser now reads every key once ", 4)
	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: crew.NewSaid(long)})))
	h.send(enterKey)

	box, _, _ := popupBox(h.view(), popupWidthIn(60))
	if box == nil {
		t.Fatalf("no popup shows:\n%s", h.view())
	}
	for _, r := range box[1 : len(box)-1] {
		if !strings.HasPrefix(r, "│") || !strings.HasSuffix(r, "│") {
			t.Errorf("row %q crosses the popup's border", r)
		}
	}
	rows := popupRows(t, h)
	i := slices.IndexFunc(rows, func(r string) bool { return strings.HasPrefix(r, "└ the parser") })
	if i < 0 {
		t.Fatalf("no row starts the message:\n%s", strings.Join(rows, "\n"))
	}
	message := []string{strings.TrimPrefix(rows[i], "└ ")}
	for _, r := range rows[i+1:] {
		rest, ok := strings.CutPrefix(r, "  ")
		if !ok || strings.HasPrefix(rest, " ") {
			break
		}
		message = append(message, rest)
	}
	if len(message) < 2 || strings.Join(message, " ") != strings.TrimSpace(long) {
		t.Errorf("the message wraps as %q, want %q on indented rows", message, strings.TrimSpace(long))
	}
}

// Covers AE6 and R21 of #151, and R7 of #230: an open popup follows its
// issue into the column its labels put it in when its rule fails, and
// closes when its issue leaves the board, the highlight moving to the
// nearest card.
func TestAE6ThePopupFollowsItsIssueAndClosesWhenItLeaves(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	h.send(enterKey)

	failed := runningSnapshot()
	failed.Snapshot.Issues = failed.Snapshot.Issues[1:]
	failed.Snapshot.Board[0].Labels = []crew.State{"ready to review"}
	failed.Snapshot.Handled = []core.HandledView{failedEntry("1", "Add login form", 10, 0, "code")}
	h.send(updateMsg(failed))
	if got := popupRows(t, h)[0]; got != "#1 Add login form" {
		t.Fatalf("after #1 failed the popup shows %q, want #1's", got)
	}

	// #1, which crew let go, now sits below #2, which crew holds (R1 of
	// #231).
	h.send(escKey)
	wantLit(t, h, "#1", 1)
	h.send(upKey)
	h.send(enterKey)
	if got := popupRows(t, h)[0]; got != "#2 Fix the flaky stream test" {
		t.Fatalf("the popup shows %q, want #2's", got)
	}
	left := failed
	left.Snapshot.Issues, left.Snapshot.Board = nil, left.Snapshot.Board[:1]
	h.send(updateMsg(left))
	if popupShows(h) {
		t.Errorf("#2 left the board and its popup stayed open:\n%s", h.view())
	}
	wantLit(t, h, "#1", 1)
}

// Covers R13 and KTD7 of #151: the popup is centred over the view drawn
// dimmed, without its colours.
func TestThePopupIsCentredOverTheDimmedView(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	before := h.view()
	h.send(enterKey)

	box, y, x := popupBox(h.view(), 100)
	if box == nil {
		t.Fatalf("no popup shows:\n%s", h.view())
	}
	if lines := len(rowsOf(h.view())); y != (lines-len(box))/2 || x != (120-100)/2 {
		t.Errorf("the popup is at %d,%d, want it centred at %d,%d", x, y, (120-100)/2, (lines-len(box))/2)
	}
	if got := firstLine(h.view()); got != firstLine(before) {
		t.Errorf("the header under the popup reads %q, want %q", got, firstLine(before))
	}
	header := firstLine(h.raw())
	for _, seq := range regexp.MustCompile(`\x1b\[[0-9;]*m`).FindAllString(header, -1) {
		if seq != fg(darkPalette().subtle) && seq != "\x1b[m" {
			t.Errorf("the header under the popup is drawn in %q, want the subtle colour only: %q", seq, header)
			break
		}
	}
	if !strings.Contains(header, fg(darkPalette().subtle)) {
		t.Errorf("the header under the popup is not in the subtle colour: %q", header)
	}
}

// Covers R16: a running action resumed in a failed run's workspace names
// that workspace in its state on the popup's action row.
func TestAResumedActionsRowNamesItsWorkspace(t *testing.T) {
	h := newHarness(t, 120)
	u := held(headerIssue, "implement", "code", core.ClaimRunning)
	a := &u.Snapshot.Issues[0].Actions[0]
	a.Resumed, a.Workspace = true, "issue-1-code"
	h.send(updateMsg(onBoard(u, labeled(headerIssue, "in progress"))))
	h.send(enterKey)

	rows := words(popupRows(t, h))
	if !slices.ContainsFunc(rows, func(r string) bool {
		return strings.HasPrefix(r, "code ") && strings.Contains(r, "resumed in issue-1-code, running")
	}) {
		t.Errorf("no code row names its workspace:\n%s", strings.Join(rows, "\n"))
	}
}
