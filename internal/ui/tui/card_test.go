package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// cardOf returns the rows of ref's first card on board, from border to
// border.
func cardOf(t *testing.T, board, ref string) []string {
	t.Helper()
	rows := strings.Split(board, "\n")
	for i, l := range rows {
		lead, _, found := strings.Cut(l, "│ "+ref+" ")
		if !found || i == 0 || i+cardRows-1 > len(rows) {
			continue
		}
		x := len([]rune(lead))
		top := []rune(rows[i-1])
		end := x + slices.Index(top[x:], '╮')
		card := make([]string, 0, cardRows)
		for _, r := range rows[i-1 : i-1+cardRows] {
			card = append(card, string([]rune(r)[x:end+1]))
		}
		return card
	}
	t.Fatalf("board has no card for %s:\n%s", ref, board)
	return nil
}

// faceOf returns the text inside ref's first card on board, a row per
// line, without its border or trailing spaces.
func faceOf(t *testing.T, board, ref string) []string {
	t.Helper()
	card := cardOf(t, board, ref)
	out := make([]string, 0, len(card)-2)
	for _, r := range card[1 : len(card)-1] {
		inner := strings.TrimSuffix(strings.TrimPrefix(r, "│ "), "│")
		out = append(out, strings.TrimRight(inner, " "))
	}
	return out
}

// aeOneCards is runningSnapshot with #1's actions running as crew-dev and
// crew-qa.
func aeOneCards() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Bots = []core.BotView{
		{Name: "crew-dev", Acting: true, State: "acting",
			Running: []core.RunningAction{{IssueRef: "#1", Rule: "implement", Action: "code"}}},
		{Name: "crew-qa", Acting: true, State: "acting",
			Running: []core.RunningAction{{IssueRef: "#1", Rule: "implement", Action: "tests"}}},
	}
	return u
}

// Covers AE1 and AE2 of #151: a live card reads its reference and title,
// then its actions, its bots and its queue, on a card as wide as its
// 34-cell column.
func TestAE1AndAE2ALiveCardReadsItsActionsBotsAndQueue(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(aeOneCards()))
	board := boardOf(t, h.view())

	for ref, want := range map[string][]string{
		"#1": {"#1 Add login form", "run  ⠋ code 5m · ⠋ tests 7m", "bots ■ crew-dev ■ crew-qa", "via  default"},
		"#2": {"#2 Fix the flaky stream test", "run  ○ check waiting", "bots none", "via  clerk"},
	} {
		if got := faceOf(t, board, ref); !slices.Equal(got, want) {
			t.Errorf("%s's card = %q, want %q:\n%s", ref, got, want, board)
		}
		for _, r := range cardOf(t, board, ref) {
			if w := lipgloss.Width(r); w != maxColumn {
				t.Errorf("%s's card row %q is %d cells, want %d", ref, r, w, maxColumn)
			}
		}
	}
}

// Covers R2 of #151: an action whose session has not started reads its
// phase, and an owed claim comes before the actions waiting on it.
func TestARunRowReadsAStartingActionAndAnOwedClaim(t *testing.T) {
	for _, tt := range []struct {
		claim core.Claim
		phase core.Phase
		want  string
	}{
		{core.ClaimRunning, core.PhaseStarting, "run  ⠋ triage starting"},
		{core.ClaimOwed, core.PhaseWaiting, "run  ! owed · ○ triage waiting"},
	} {
		h := newBoardHarness(t, 120, crewRules, crewBoard)
		u := held(twelve, "triage", "triage", tt.claim)
		u.Snapshot.Issues[0].Actions[0].Phase, u.Snapshot.Issues[0].Actions[0].Started = tt.phase, time.Time{}
		h.send(updateMsg(onBoard(u, labeled(twelve, "crew:triage:in progress"))))
		if got := faceOf(t, boardOf(t, h.view()), "#12")[1]; got != tt.want {
			t.Errorf("run row = %q, want %q", got, tt.want)
		}
	}
}

// Covers R2 of #151: on a 22-cell column, the run row keeps the whole
// items that fit and counts the rest, and every row keeps the card's
// width.
func TestANarrowCardKeepsWholeItemsAndItsWidth(t *testing.T) {
	h := newBoardHarness(t, 71, crewRules, ideasBugsDone)
	u := held(twenty, "fix", "code", core.ClaimRunning)
	iv := &u.Snapshot.Issues[0]
	iv.Actions[0].Started = start.Add(-5 * time.Minute)
	for _, name := range []string{"tests", "docs"} {
		iv.Actions = append(iv.Actions, core.ActionView{Name: name, Phase: core.PhaseRunning, Started: start})
	}
	h.send(updateMsg(onBoard(u, labeled(twenty, "bug"))))
	board := boardOf(t, h.view())

	if got := faceOf(t, board, "#20")[1]; got != "run  ⠋ code 5m +2" {
		t.Errorf("run row = %q, want run  ⠋ code 5m +2:\n%s", got, board)
	}
	for _, r := range cardOf(t, board, "#20") {
		if w := lipgloss.Width(r); w != 22 {
			t.Errorf("card row %q is %d cells, want 22", r, w)
		}
	}
}

// Covers R3 of #151: a bot's mark on a card is in the colour of its card
// in Bots.
func TestABotsMarkTakesItsAvatarColour(t *testing.T) {
	h := newHarness(t, 120)
	u := aeOneCards()
	h.send(updateMsg(u))

	s := h.current().styles
	mark := lipgloss.NewStyle().Foreground(s.avatarColour(u.Snapshot.Bots[0])).Render("■")
	if !strings.Contains(h.raw(), mark+" "+s.text.Render("crew-dev")) {
		t.Error("crew-dev's mark is not in its avatar colour")
	}
}

// Covers R6 of #151: a card's border is strong while crew runs its issue,
// and subtle while it waits or is idle.
func TestACardsBorderIsStrongOnlyWhileItsIssueRuns(t *testing.T) {
	h := newHarness(t, 120)
	u := runningSnapshot()
	u.Snapshot.Bots = nil
	h.send(updateMsg(u))

	s := h.current().styles
	top := "╭" + strings.Repeat("─", maxColumn-2) + "╮"
	if want := s.strongAccent.Render(top) + "  " + s.subtle.Render(top); !strings.Contains(h.raw(), want) {
		t.Errorf("#1's border is not strong beside #2's subtle one:\n%s", h.view())
	}

	h = newHarness(t, 120)
	h.send(updateMsg(onBoard(engine.Update{}, labeled(twelve, "ready"))))
	if raw := h.raw(); !strings.Contains(raw, s.subtle.Render(top)) || strings.Contains(raw, s.strongAccent.Render(top)) {
		t.Errorf("an idle card's border is not subtle:\n%s", h.view())
	}
}

// Covers R1 of #151: a title holding an escape sequence is drawn clean and
// cut to the card.
func TestACardsTitleIsCleanAndCut(t *testing.T) {
	h := newHarness(t, 120)
	u := runningSnapshot()
	title := "Fix \x1b[31mred\x1b[0m output in the parser of every config file"
	u.Snapshot.Issues[0].Issue.Title, u.Snapshot.Board[0].Issue.Title = title, title
	h.send(updateMsg(u))

	if strings.Contains(h.raw(), "\x1b[31m") {
		t.Error("the title's escape sequence reaches the terminal")
	}
	if got := faceOf(t, boardOf(t, h.view()), "#1")[0]; got != "#1 Fix red output in the pars…" {
		t.Errorf("first row = %q, want the clean title cut to the card", got)
	}
}
