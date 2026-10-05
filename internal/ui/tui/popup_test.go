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
// down each column and on to the next, the Handled column last, and stop
// at the first and last card.
func TestThePopupWalksTheCardsInBoardOrderAndStopsAtTheEnds(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handledSnapshot()))
	h.send(enterKey)

	titles := make([]string, 0, 8)
	for range 8 {
		ref, _, _ := strings.Cut(popupRows(t, h)[0], " ")
		titles = append(titles, ref)
		h.send(rightKey)
	}
	if want := []string{"#1", "#2", "#7", "#6", "#5", "#8", "#7", "#7"}; !slices.Equal(titles, want) {
		t.Errorf("→ walked %q, want %q", titles, want)
	}
	if sel := h.current().sel; sel.column != h.current().handledColumn() {
		t.Errorf("the last card walked to is in column %d, want Handled", sel.column)
	}
	for range 8 {
		h.send(leftKey)
	}
	if got := popupRows(t, h)[0]; got != "#1 Add login form" {
		t.Errorf("← past the first card shows %q, want #1's popup", got)
	}
}

// headerIssue is #3, with everything the popup's header shows.
var headerIssue = crew.Issue{
	Key: "3", Ref: "#3", Title: "Speed up the poll", URL: "https://github.com/o/r/issues/3",
	Priority: 2, Blocked: true, States: []crew.State{"in progress"},
}

// Covers R15 and KTD8 of #151: the header shows the rule, the crew state
// and board labels as chips, each once, the kind, the priority, whether
// it is blocked and the URL.
func TestThePopupHeaderShowsTheIssuesFields(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(onBoard(held(headerIssue, "implement", "code", core.ClaimRunning),
		labeled(headerIssue, "in progress", "bug"))))
	h.send(enterKey)

	rows := popupRows(t, h)
	for label, want := range map[string]string{
		"rule": "implement", "kind": "issue", "priority": "P2", "blocked": "yes", "url": headerIssue.URL,
	} {
		if got := field(t, rows, label); got != want {
			t.Errorf("%s = %q, want %q", label, got, want)
		}
	}
	if got := strings.Fields(field(t, rows, "labels")); !slices.Equal(got, []string{"in", "progress", "bug"}) {
		t.Errorf("labels = %q, want the state in progress once, then bug", got)
	}
	if !strings.Contains(h.raw(), sgr(48, darkPalette().chipBack)) {
		t.Error("the labels are not drawn as chips")
	}

	plain := headerIssue
	plain.Priority, plain.Blocked = 0, false
	h.send(updateMsg(onBoard(held(plain, "implement", "code", core.ClaimRunning), labeled(plain, "in progress"))))
	rows = popupRows(t, h)
	if got := field(t, rows, "priority"); got != "none" {
		t.Errorf("priority 0 reads %q, want none", got)
	}
	if got := field(t, rows, "blocked"); got != "no" {
		t.Errorf("an unblocked issue reads blocked %q, want no", got)
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
		{Name: "lint", Phase: core.PhaseEnded, Branch: "crew/1-lint", Outcome: crew.Outcome{Reason: "exited 1"}},
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
	h.send(updateMsg(saying(core.Said{IssueKey: "1", Action: "code", Text: "running the tests now"})))
	ended := runningSnapshot()
	actions := &ended.Snapshot.Issues[0].Actions
	(*actions)[0].Phase, (*actions)[0].Outcome = core.PhaseEnded, crew.Outcome{Succeeded: true}
	*actions = append(*actions, core.ActionView{
		Name: "docs", Phase: core.PhaseEnded, Outcome: crew.Outcome{Reason: "check failed: exited 2"},
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
		core.ActionStarted{At: start.Add(-6 * time.Minute), IssueKey: "1", IssueRef: "#1", Rule: "implement",
			Action: "tests", Branch: "crew/1-tests", Log: ".crew/logs/1-tests.log"},
		core.IssueTaken{At: start.Add(-5 * time.Minute), Issue: two, Rule: "review",
			From: "ready to review", To: "in review"},
		core.CallOwed{At: start.Add(-4 * time.Minute), Call: core.Call{Kind: core.CallMove, IssueKey: "1", IssueRef: "#1",
			From: "ready", To: "done"}, Reason: "rate limited"},
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

// handledFive is #5 needing attention: tests failed, after a session that
// opened #47, code failed with no session, and docs had no session. An
// earlier rule spent $2.00 on it this run.
func handledFive() core.HandledView {
	e := acted(failedEntry("5", "Parse the config once", 40, 30,
		"tests", "exited 1: tests fail", "code", "prompt did not render"),
		core.HandledAction{Name: "tests", Spend: spent(0.84, 1_200_000), PullRequest: found("#47")},
		core.HandledAction{Name: "code"},
		core.HandledAction{Name: "docs"})
	e.Earlier = spent(2, 300_000)
	return e
}

// Covers AE7, R20 and KTD8, KTD14 of #151: a Handled card's popup lists
// each failed action's reason, the issue's cost this run once, its
// rules' together, and the pull request each action opened.
func TestAE7AHandledPopupListsItsReasonsCostAndPullRequests(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(handling(handledFive())))
	h.send(enterKey)

	rows := popupRows(t, h)
	if got := field(t, rows, "rule"); got != "implement" {
		t.Errorf("rule = %q, want implement", got)
	}
	if got := field(t, rows, "labels"); got != "needs attention" {
		t.Errorf("labels = %q, want the state it moved to", got)
	}
	if got := field(t, rows, "cost"); !strings.HasPrefix(got, "$2.84") {
		t.Errorf("cost = %q, want both rules' $2.84", got)
	}
	if n := strings.Count(strings.Join(rows, "\n"), "$"); n != 1 {
		t.Errorf("the popup shows %d costs, want the issue's once", n)
	}
	hasRow(t, rows, "action bot queue state branch pull request")
	for _, want := range []struct{ row, under string }{
		{"tests default failed #47", "└ exited 1: tests fail"},
		{"code default failed", "└ prompt did not render"},
	} {
		if i := hasRow(t, rows, want.row); i >= 0 && words(rows)[i+1] != want.under {
			t.Errorf("under %q is %q, want %q", want.row, rows[i+1], want.under)
		}
	}
	hasRow(t, rows, "docs default no session")
}

// Covers KTD8 of #151: a message longer than the popup's inner width
// wraps onto indented rows, and no row crosses the border.
func TestALongMessageWrapsInsideThePopup(t *testing.T) {
	h := newHarness(t, 60)
	long := strings.Repeat("the parser now reads every key once ", 4)
	h.send(updateMsg(saying(core.Said{IssueKey: "1", Action: "code", Text: long})))
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

// Covers AE6 and R21 of #151: an open popup follows its issue into the
// Handled column when its rule fails, and closes when its issue leaves
// the board, the highlight moving to the nearest card.
func TestAE6ThePopupFollowsItsIssueAndClosesWhenItLeaves(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))
	h.send(enterKey)

	failed := runningSnapshot()
	failed.Snapshot.Issues, failed.Snapshot.Board = failed.Snapshot.Issues[1:], failed.Snapshot.Board[1:]
	failed.Snapshot.Handled = []core.HandledView{failedEntry("1", "Add login form", 10, 0, "code", "exited 1")}
	h.send(updateMsg(failed))
	if got := popupRows(t, h)[0]; got != "#1 Add login form" {
		t.Fatalf("after #1 failed the popup shows %q, want #1's", got)
	}
	if sel := h.current().sel; sel.key != "1" || sel.column != h.current().handledColumn() {
		t.Errorf("the highlight is on %q in column %d, want #1's Handled card", sel.key, sel.column)
	}
	if got := field(t, popupRows(t, h), "cost"); got != "none" {
		t.Errorf("the popup now shows #1's Handled cost %q, want none", got)
	}

	h.send(escKey)
	wantLit(t, h, "#1", 2)
	h.send(leftKey)
	h.send(enterKey)
	if got := popupRows(t, h)[0]; got != "#2 Fix the flaky stream test" {
		t.Fatalf("the popup shows %q, want #2's", got)
	}
	left := failed
	left.Snapshot.Issues, left.Snapshot.Board = nil, nil
	h.send(updateMsg(left))
	if popupShows(h) {
		t.Errorf("#2 left the board and its popup stayed open:\n%s", h.view())
	}
	wantLit(t, h, "#1", 2)
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
