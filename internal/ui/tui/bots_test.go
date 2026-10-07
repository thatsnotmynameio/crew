package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// reviewerNoKey is the startup warning of a bot reviewer without a key.
const reviewerNoKey = "bot reviewer has no key on this machine for thatsnotmynameio; " +
	"run `crew bots create reviewer` in this repository"

// threeActions are three triage actions that ended: $0.42 and 310K tokens.
func threeActions() crew.Spend {
	return spent(0.1, 100_000).Add(spent(0.12, 110_000)).Add(spent(0.2, 100_000))
}

// aeOneBots are AE1's entries: the default bot clerk for triage, with
// three triage actions ended, developer running development on #1,
// reviewer without a key, and you as octocat.
func aeOneBots() []core.BotView {
	return []core.BotView{
		{Name: "clerk", Acting: true, State: "acting", Writes: true, Pairs: []string{"triage/triage"}, Spend: threeActions()},
		{
			Name: "developer", Acting: true, State: "acting", Pairs: []string{"development/lfg"},
			Running: []core.RunningAction{{IssueRef: "#1", Rule: "development", Action: "lfg"}},
		},
		{Name: "reviewer", State: "cannot act: no key", ActsAsYou: true, Pairs: []string{"review/review"}},
		{Name: "you", You: true, Login: "octocat"},
	}
}

// withBots is runningSnapshot with entries as its bots.
func withBots(entries ...core.BotView) engine.Update {
	u := runningSnapshot()
	u.Snapshot.Bots = entries
	return u
}

// botsOf returns the rows of view's Bots section under its title, down
// to the blank row above Board.
func botsOf(t *testing.T, view string) []string {
	t.Helper()
	all, i, j := rowsOf(view), titleRow(view, "Bots"), titleRow(view, "Board")
	if i < 0 || j < i {
		t.Fatalf("view lacks the Bots section above Board:\n%s", view)
	}
	return all[i+1 : j-1]
}

// botsRule returns the rule that opens view's Bots section.
func botsRule(t *testing.T, view string) string {
	t.Helper()
	i := titleRow(view, "Bots")
	if i < 0 {
		t.Fatalf("view lacks the Bots section:\n%s", view)
	}
	return rowsOf(view)[i]
}

// cardsOf splits view's Bots section into its cards, each a list of its
// rows from border to border.
func cardsOf(t *testing.T, view string) [][]string {
	t.Helper()
	rows := botsOf(t, view)
	if len(rows) != botCardRows {
		t.Fatalf("Bots has %d rows, want %d:\n%s", len(rows), botCardRows, view)
	}
	top := []rune(rows[0])
	var cards [][]string
	for start := slices.Index(top, '╭'); start >= 0; {
		end := start + slices.Index(top[start:], '╮')
		card := make([]string, 0, botCardRows)
		for _, r := range rows {
			card = append(card, string([]rune(r)[start:end+1]))
		}
		cards = append(cards, card)
		next := slices.Index(top[end:], '╭')
		if next < 0 {
			break
		}
		start = end + next
	}
	return cards
}

// cardHas fails t unless card holds each of wants.
func cardHas(t *testing.T, card []string, wants ...string) {
	t.Helper()
	text := strings.Join(card, "\n")
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("card lacks %q:\n%s", want, text)
		}
	}
}

// cardsAreWhole fails t unless view's Bots rows hold cards of width cells,
// one cell apart, each row ending in its card's border.
func cardsAreWhole(t *testing.T, view string, cards [][]string, width int) {
	t.Helper()
	for _, row := range botsOf(t, view) {
		if w := lipgloss.Width(row); w != 1+len(cards)*width+(len(cards)-1)*cardGap {
			t.Errorf("Bots row %q is %d cells, want %d cards of %d cells one cell apart", row, w, len(cards), width)
		}
	}
	for _, card := range cards {
		for _, row := range card {
			if !strings.HasSuffix(row, "╮") && !strings.HasSuffix(row, "│") && !strings.HasSuffix(row, "╯") {
				t.Errorf("card row %q does not end in its border", row)
			}
		}
	}
}

// edgeOf is the style of a card's top border, as the terminal gets it.
func edgeOf(card string) string {
	edge, _, _ := strings.Cut(card, "╭")
	return edge
}

// Covers AE1: one card per entry, in order, each with its state, totals,
// what acts as it and what runs as it now, and a strong border while
// something runs as it.
func TestAE1EachBotShowsACardWithItsStateTotalsPairsAndRunningActions(t *testing.T) {
	view := fitted(t, 120, 0, withBots(aeOneBots()...))

	cards := cardsOf(t, view)
	if len(cards) != 4 {
		t.Fatalf("Bots has %d cards, want 4:\n%s", len(cards), view)
	}
	cardsAreWhole(t, view, cards, 29)
	for i, name := range []string{"clerk", "developer", "reviewer", "you"} {
		if !strings.Contains(cards[i][1], name) {
			t.Errorf("card %d is not %s:\n%s", i, name, strings.Join(cards[i], "\n"))
		}
	}
	cardHas(t, cards[0], "● acting", "3 actions · $0.42", "│ crew's writes +1", "│ idle")
	cardHas(t, cards[1], "● acting", "no actions yet", "│ development/lfg", "⠋ #1 development/lfg")
	cardHas(t, cards[2], "▲ no key", "no actions yet", "│ → you review/review", "│ idle")
	cardHas(t, cards[3], "@octocat", "no actions yet")
	if rule := botsRule(t, view); !strings.HasSuffix(rule, "─ 2 acting · 1 cannot act") {
		t.Errorf("Bots rule is %q, want it to count 2 acting and 1 that cannot act", rule)
	}

	m := newHarness(t, 120).current()
	entries := aeOneBots()
	if strong, subtle := edgeOf(m.botCard(entries[1], 29)[0]), edgeOf(m.botCard(entries[0], 29)[0]); strong == subtle ||
		strong != edgeOf(m.styles.strongAccent.Render("╭")) || subtle != edgeOf(m.styles.subtle.Render("╭")) {
		t.Errorf("developer's border is %q and clerk's %q, want the strong accent and the subtle colour", strong, subtle)
	}
}

// Covers AE2: with no bot configured only your card shows, and the rule
// says so.
func TestAE2WithoutBotsOnlyYourCardShows(t *testing.T) {
	view := fitted(t, 80, 0, withBots(core.BotView{Name: "you", You: true, Writes: true}))

	if cards := cardsOf(t, view); len(cards) != 1 || !strings.Contains(cards[0][1], "you") {
		t.Errorf("Bots shows %d cards, want only yours:\n%s", len(cards), view)
	}
	if rule := botsRule(t, view); !strings.HasSuffix(rule, "─ only you") {
		t.Errorf("Bots rule is %q, want it to end in only you", rule)
	}
}

// Covers R11 and R3: a bot that cannot act shows its short reason on its
// card, and its startup warning stays under the header.
func TestABotThatCannotActKeepsItsStartupWarning(t *testing.T) {
	h := newHarness(t, 120, reviewerNoKey)
	h.send(updateMsg(withBots(aeOneBots()...)))
	view := h.view()

	contains(t, rowsOf(view)[1], "warning: bot reviewer has no key")
	cardHas(t, cardsOf(t, view)[2], "▲ no key")
}

// The card's short state follows the state the core gives a bot that
// cannot act at startup.
func TestTheShortStateDropsTheCoresCannotActPrefix(t *testing.T) {
	v := core.New(nil, 1, core.WithBots(core.BotsConfig{
		Names: []crew.BotName{"reviewer"}, Unable: map[crew.BotName]string{"reviewer": "no key"},
	})).View()
	m := newHarness(t, 80).current()

	if got := ansi.Strip(m.botState(v.Bots[0])); got != "▲ no key" {
		t.Errorf("reviewer's state reads %q, want ▲ no key", got)
	}
}

// Covers AE5 and R11: the default bot whose writes went back to you turns
// grey and says so, its live warning follows the startup ones, and a bot
// acting again regains its colour.
func TestAE5ABotThatStopsActingGoesGreyAndComesBack(t *testing.T) {
	lost := "crew's writes as bot clerk went back to you: GitHub refused its credentials; " +
		"crew writes as you until it restarts"
	clerk := core.BotView{
		Name: "clerk", State: "writes as you", Warnings: []string{lost + "\x1b[31m"}, Pairs: []string{"triage/triage"},
	}
	you := core.BotView{Name: "you", You: true, Writes: true}
	h := newHarness(t, 200, reviewerNoKey)
	h.send(updateMsg(withBots(clerk, you)))
	view := h.view()

	rows := rowsOf(view)
	contains(t, rows[1], "warning: bot reviewer has no key")
	if rows[2] != "warning: "+lost {
		t.Errorf("row 2 is %q, want the live warning after the startup one:\n%s", rows[2], view)
	}
	cards := cardsOf(t, view)
	cardHas(t, cards[0], "▲ writes as you")
	cardHas(t, cards[1], "crew's writes")
	m := h.current()
	if c := m.styles.avatarColour(clerk); c != m.styles.offline {
		t.Error("clerk's avatar is not grey while its writes go as you")
	}

	clerk.Acting, clerk.State, clerk.Warnings = true, "acting", nil
	h.send(updateMsg(withBots(clerk, you)))
	cardHas(t, cardsOf(t, h.view())[0], "● acting")
	if c := h.current().styles.avatarColour(clerk); c == m.styles.offline {
		t.Error("clerk's avatar stayed grey once it acted again")
	}
}

// Covers R3 and KTD7: the totals take the longest form that fits.
func TestTheTotalsTakeTheLongestFormThatFits(t *testing.T) {
	m := newHarness(t, 80).current()
	for _, tt := range []struct {
		width int
		want  string
	}{
		{40, "3 actions · $0.42 · 310K tokens"},
		{31, "3 actions · $0.42 · 310K tokens"},
		{30, "3 actions · $0.42"},
		{19, "3 actions · $0.42"},
		{16, "$0.42"},
		{14, "$0.42"},
		{3, "$0…"},
	} {
		if got := ansi.Strip(m.botTotals(threeActions(), tt.width)); got != tt.want {
			t.Errorf("totals in %d cells read %q, want %q", tt.width, got, tt.want)
		}
	}
	if got := ansi.Strip(m.botTotals(spent(1, 1_000).Add(spent(2, 2_000)), 19)); got != "2 actions · $3.00" {
		t.Errorf("totals read %q, want 2 actions · $3.00", got)
	}
	if got := ansi.Strip(m.botTotals(crew.Spend{}, 30)); got != "no actions yet" {
		t.Errorf("totals before an action ends read %q, want no actions yet", got)
	}
	if got := ansi.Strip(m.botTotals(spent(0.5, 1_000), 30)); got != "1 action · $0.50 · 1K tokens" {
		t.Errorf("totals of one action read %q", got)
	}
}

// Covers R3 and KTD6: on the narrowest card, a long name and a long state
// are cut with an ellipsis, and the border stays whole.
func TestANarrowCardCutsALongNameAndState(t *testing.T) {
	m := newHarness(t, 80).current()
	name := strings.Repeat("n", 30)
	card := m.botCard(core.BotView{Name: crew.BotName(name), State: "token not renewed"}, minCard)

	for _, row := range card {
		if w := lipgloss.Width(row); w != minCard {
			t.Errorf("row %q is %d cells, want %d", ansi.Strip(row), w, minCard)
		}
		if line := ansi.Strip(row); !strings.HasSuffix(line, "╮") && !strings.HasSuffix(line, "│") &&
			!strings.HasSuffix(line, "╯") {
			t.Errorf("row %q does not end in the border", line)
		}
	}
	for _, i := range []int{1, 2} {
		if line := ansi.Strip(card[i]); !strings.Contains(line, "…") {
			t.Errorf("row %q is not cut with an ellipsis", line)
		}
	}
}

// Covers R4 and R5: pairs and running actions that do not fit show as
// whole items, then +N, and text from outside crew is cleaned.
func TestPairsAndRunningActionsShowWholeItemsThenACount(t *testing.T) {
	pairs := []string{"implement/code", "implement/tests", "implement/docs", "review/review", "review/sec\x1b[31murity"}
	running := []core.RunningAction{
		{IssueRef: "#1", Rule: "implement", Action: "code"},
		{IssueRef: "#2", Rule: "implement", Action: "tests"},
		{IssueRef: "#3", Rule: "review", Action: "review"},
	}
	m := newHarness(t, 80).current()
	card := m.botCard(core.BotView{Name: "you", You: true, Pairs: pairs, Running: running}, minCard)

	mapping, now := ansi.Strip(card[4]), ansi.Strip(card[5])
	if !strings.HasPrefix(mapping, "│ implement/code +4 ") {
		t.Errorf("mapping row is %q, want the first pair whole, then +4", mapping)
	}
	if !strings.HasPrefix(now, "│ ⠋ #1 implement/c… +2") {
		t.Errorf("running row is %q, want the first action cut beside +2", now)
	}
	for _, row := range card {
		if strings.Contains(row, "\x1b[31m") {
			t.Errorf("row %q carries an escape sequence from a pair", row)
		}
	}

	card = m.botCard(core.BotView{Name: "you", You: true, Pairs: pairs[3:], Running: running}, 2*minCard+cardFrame)
	if line := ansi.Strip(card[4]); !strings.Contains(line, "review/review · review/security") {
		t.Errorf("mapping row %q does not show both pairs, the second one clean", line)
	}
	if line := ansi.Strip(card[5]); !strings.Contains(line, "⠋ #1 implement/code · ⠋ #2 implement/tests +1") {
		t.Errorf("running row %q does not show the actions that fit whole, then +1", line)
	}
}

// When even one item does not fit beside its +N, the first is cut.
func TestAnItemTooWideForItsRowIsCut(t *testing.T) {
	s := newStyles(true)
	if got := ansi.Strip(s.items([]string{"implement/code", "review/review"}, " · ", 10)); got != "implem… +1" {
		t.Errorf("items read %q, want implem… +1", got)
	}
	if got := ansi.Strip(s.items([]string{"implement/code"}, " · ", 10)); got != "implement…" {
		t.Errorf("one item reads %q, want implement…", got)
	}
	if got := s.items(nil, " · ", 10); got != "" {
		t.Errorf("no items read %q, want nothing", got)
	}
}

// threeBotsOnABoard is three bots and you, with issues in five of eight
// board columns, too many for 80 columns.
func threeBotsOnABoard() engine.Update {
	var issues []crew.BoardIssue
	for col := 1; col <= 5; col++ {
		key := strconv.Itoa(col)
		label := crew.State(fmt.Sprintf("l%d", col))
		issues = append(issues, labeled(crew.Issue{Key: key, Ref: "#" + key, Title: "Card"}, label))
	}
	u := onBoard(engine.Update{}, issues...)
	u.Snapshot.Bots = aeOneBots()
	return u
}

// Covers AE3: three cards fit 80 columns; once tab gives Bots focus, →
// shows your card; with Events focused, → moves the board's highlight
// instead, and the board scrolls once it reaches a column off its edge
// (KTD6 of #151).
func TestAE3TheCardsScrollSidewaysWhileBotsHasFocus(t *testing.T) {
	h := newBoardHarness(t, 80, crewRules, eightColumns())
	h.send(tea.WindowSizeMsg{Width: 80, Height: 0})
	h.send(updateMsg(threeBotsOnABoard()))
	view := h.view()

	if cards := cardsOf(t, view); len(cards) != 3 || !strings.Contains(cards[0][1], "clerk") {
		t.Fatalf("Bots shows %d cards, want clerk's first of three:\n%s", len(cards), view)
	}
	if rule := botsRule(t, view); !strings.HasSuffix(rule, "· 1 ▸") {
		t.Errorf("Bots rule is %q, want it to mark 1 card hidden after", rule)
	}

	h.send(tab)
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	view = h.view()
	cards := cardsOf(t, view)
	if len(cards) != 3 || !strings.Contains(cards[2][1], "you") || strings.Contains(view, "clerk") {
		t.Errorf("→ with Bots focused did not show your card in clerk's place:\n%s", view)
	}
	if rule := botsRule(t, view); !strings.HasSuffix(rule, "· ◂ 1") || !strings.HasPrefix(rule, "▸ Bots") {
		t.Errorf("Bots rule is %q, want it focused and marking 1 card hidden before", rule)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := h.current().botsOffset; got != 1 {
		t.Errorf("→ past the last card left the offset at %d, want 1", got)
	}
	board := boardOf(t, view)

	h.send(tab)
	for range 3 {
		h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	view = h.view()
	if got := boardOf(t, view); got == board || !strings.Contains(got, "◂ 1") {
		t.Errorf("→ with Events focused did not scroll the board:\n%s", got)
	}
	if rule := botsRule(t, view); !strings.HasSuffix(rule, "· ◂ 1") {
		t.Errorf("→ with Events focused moved the cards: %q", rule)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := h.current().botsOffset; got != 1 {
		t.Errorf("← with Events focused moved the cards to %d", got)
	}
}

// Covers R8: a window too narrow for the counts beside the scroll markers
// keeps the markers, so the hidden cards stay announced.
func TestANarrowRuleKeepsTheScrollMarkers(t *testing.T) {
	h := newHarness(t, 40)
	h.send(tea.WindowSizeMsg{Width: 40, Height: 0})
	h.send(updateMsg(withBots(aeOneBots()...)))
	if rule := botsRule(t, h.view()); !strings.HasSuffix(rule, "─ 2 acting · 1 cannot act · 3 ▸") {
		t.Errorf("Bots rule is %q, want the counts and 3 ▸ while they fit", rule)
	}

	h.send(tab)
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	if rule := botsRule(t, h.view()); !strings.HasSuffix(rule, "─ ◂ 1 · 2 ▸") {
		t.Errorf("Bots rule is %q, want only the markers once the counts do not fit", rule)
	}
}

// Covers R9: ↑↓ do nothing while Bots has focus, and shift+tab from Bots
// gives the board its focus back (KTD4 of #151).
func TestBotsTakesFocusFirstAndIgnoresUpAndDown(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(withBots(aeOneBots()...)))
	before := h.view()
	h.send(tab)
	if rule := botsRule(t, h.view()); !strings.HasPrefix(rule, "▸ Bots") {
		t.Fatalf("tab did not focus Bots first: %q", rule)
	}
	focused := h.view()
	h.send(tea.KeyPressMsg{Code: tea.KeyDown})
	h.send(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if h.view() != focused {
		t.Error("↓ or pgdown changed the view while Bots has focus")
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if h.view() != before {
		t.Errorf("shift+tab from Bots did not give the board its focus back:\n%s", h.view())
	}
}

// Covers R8 and KTD3: the card row's layout, every card or as many as
// fit from the offset, clamped.
func TestTheCardRowShowsEveryCardOrAsManyAsFit(t *testing.T) {
	for _, tt := range []struct {
		name             string
		n, avail, offset int
		want             botsLayout
	}{
		{"all fit", 4, 119, 0, botsLayout{shown: 4, width: 29}},
		{"one fits wide", 1, 79, 3, botsLayout{shown: 1, width: maxCard}},
		{"one hidden after", 4, 79, 0, botsLayout{shown: 3, after: 1, width: 25}},
		{"one each side", 5, 79, 1, botsLayout{offset: 1, shown: 3, before: 1, after: 1, width: 25}},
		{"offset clamped", 5, 79, 9, botsLayout{offset: 2, shown: 3, before: 2, width: 25}},
		{"negative offset", 5, 79, -1, botsLayout{shown: 3, after: 2, width: 25}},
		{"narrower than a card", 3, 10, 0, botsLayout{shown: 1, after: 2, width: 10}},
		{"no room", 2, 0, 0, botsLayout{shown: 1, after: 1, width: cardFrame}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := layBots(tt.n, tt.avail, tt.offset); got != tt.want {
				t.Errorf("layBots(%d, %d, %d) = %+v, want %+v", tt.n, tt.avail, tt.offset, got, tt.want)
			}
		})
	}
}

// Before the first snapshot there is no entry: the section says none and
// keeps its height.
func TestBeforeAnySnapshotBotsSaysNoneAndKeepsItsHeight(t *testing.T) {
	h := newHarness(t, 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 0})
	rows := botsOf(t, h.view())
	if len(rows) != botCardRows || rows[0] != " none" {
		t.Errorf("Bots rows are %q, want none over %d rows", rows, botCardRows)
	}
}

// Covers R10 and KTD9: the strip's glyphs and its +N.
func TestTheStripShowsEachEntrysGlyphAndCountsTheRest(t *testing.T) {
	running := []core.RunningAction{{IssueRef: "#1", Rule: "review", Action: "review"}}
	entries := []core.BotView{
		{Name: "clerk", Acting: true, State: "acting"},
		{Name: "reviewer", State: "cannot act: no key", Running: running},
		{Name: "you", You: true},
	}
	h := newHarness(t, 80)
	h.send(updateMsg(withBots(entries...)))
	m := h.current()

	if got := ansi.Strip(m.botsStrip(entries)); got != "■ clerk ●  ■ reviewer ▲ ⠋ 1  ■ you" {
		t.Errorf("strip reads %q", got)
	}
	m.width = 20
	if got := ansi.Strip(m.botsStrip(entries)); got != "■ clerk ● +2" {
		t.Errorf("a narrow strip reads %q, want whole entries then +2", got)
	}
}

// shortWindow is AE1's bots with ten handled issues, 30 events and three
// cards in implement.
func shortWindow() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Bots = aeOneBots()
	u.Snapshot.Handled = manySnapshot().Snapshot.Handled
	u.Snapshot.Recent = eventful().Snapshot.Recent
	for _, key := range []string{"3", "4"} {
		card := crew.Issue{Key: key, Ref: "#" + key, Title: "Card"}
		u.Snapshot.Issues = append(u.Snapshot.Issues, core.IssueView{Issue: card, Rule: "implement", Claim: core.ClaimTaking})
		u.Snapshot.Board = append(u.Snapshot.Board, crew.BoardIssue{Issue: card, Labels: []crew.State{"ready"}})
	}
	return u
}

// Covers AE4 and R10: once Events and the board cards have shrunk, the
// cards collapse to a one-row strip, and only then is the view cut (KTD10
// of #151).
func TestAE4TheCardsCollapseToAStripLastBeforeTheCut(t *testing.T) {
	u := shortWindow()
	h := newHarness(t, 120)
	h.send(updateMsg(u))
	least := budget{events: minScroll, cards: 1, botCards: true}
	height := len(h.current().rows(least))

	view := fitted(t, 120, height, u)
	if len(botsOf(t, view)) != botCardRows || strings.Contains(view, "lines cut") {
		t.Fatalf("at %d rows the cards collapsed or the view was cut:\n%s", height, view)
	}
	if !strings.Contains(view, "+2 more") || len(eventsRows(t, view)) != minScroll {
		t.Errorf("at %d rows Events or the board cards did not shrink first:\n%s", height, view)
	}

	view = fitted(t, 120, height-1, u)
	rows := botsOf(t, view)
	if len(rows) != 1 || strings.Contains(view, "lines cut") {
		t.Fatalf("one row short, the cards did not collapse to one strip row, or the view was cut:\n%s", view)
	}
	contains(t, rows[0], "■ developer ⠋ 1", "■ reviewer ▲", "■ clerk ●")
	stripped := height - botCardRows + 1
	if view := fitted(t, 120, stripped, u); strings.Contains(view, "lines cut") {
		t.Errorf("at %d rows the view was cut although the strip fits:\n%s", stripped, view)
	}
	if view := fitted(t, 120, stripped-1, u); !strings.Contains(view, "lines cut") {
		t.Errorf("at %d rows the view was not cut:\n%s", stripped-1, view)
	}
}
