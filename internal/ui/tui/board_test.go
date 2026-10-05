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

// crewRules is this repository's rules, with both promote rules
// hidden (AE1).
var crewRules = []crew.Rule{
	{
		Name:     "promote brainstorm",
		Labels:   crew.Labels{Ready: "crew:brainstorm:done", Success: "crew:triage:ready"},
		OffBoard: true,
	},
	{
		Name:   "triage",
		Labels: crew.Labels{Ready: "crew:triage:ready", Success: "crew:triage:done", Failure: "crew:triage:failed"},
	},
	{
		Name:     "promote triage",
		Labels:   crew.Labels{Ready: "crew:triage:done", Success: "crew:development:ready"},
		OffBoard: true,
	},
	{Name: "development", Labels: crew.Labels{Ready: "crew:development:ready", Failure: "crew:development:failed"}},
	{Name: "fix", Labels: crew.Labels{Ready: "crew:fix:ready", Failure: "crew:fix:failed"}},
}

var twelve = crew.Issue{Key: "12", Ref: "#12", Title: "Stage labels", URL: "https://github.com/o/r/issues/12"}

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

// boardOf returns the Workflow section of view: its rule up to the blank
// line before Actions.
func boardOf(t *testing.T, view string) string {
	t.Helper()
	i := strings.Index(view, "Workflow ")
	j := strings.Index(view, "\n\nActions ")
	if i < 0 || j < i {
		t.Fatalf("view lacks the Workflow section:\n%s", view)
	}
	return view[i:j]
}

// Covers AE1.
func TestAE1HiddenRulesHaveNoColumnAndTheirIssuesNoCard(t *testing.T) {
	h := newRulesHarness(t, 120, crewRules)

	h.send(updateMsg(held(twelve, "promote triage", "promote", core.ClaimRunning)))
	board := boardOf(t, h.view())

	names := strings.Fields(strings.Split(board, "\n")[1])
	if got, want := strings.Join(names, " "), "triage development fix"; got != want {
		t.Errorf("columns = %q, want %q:\n%s", got, want, board)
	}
	if strings.Contains(board, "#12") {
		t.Errorf("the board shows #12 while a hidden stage holds it:\n%s", board)
	}
	contains(t, h.view(), "promote triage/promote")
}

// Covers AE2.
func TestAE2ACardWaitsInItsColumnThenSlidesToTheNextRule(t *testing.T) {
	h := newRulesHarness(t, 120, crewRules)
	h.send(updateMsg(held(twelve, "triage", "triage", core.ClaimRunning)))

	h.send(updateMsg(handledBy(twelve, "triage", "crew:triage:done")))
	board := boardOf(t, h.view())
	contains(t, board, "▌ #12 Stage labels", "▌ → crew:triage:done")
	if col := cardColumn(t, board, "#12"); col != 0 {
		t.Errorf("waiting card is in column %d, want triage's 0:\n%s", col, board)
	}

	cmd := h.send(updateMsg(held(twelve, "development", "lfg", core.ClaimRunning)))
	board = boardOf(t, h.view())
	if col := cardColumn(t, board, "#12"); col != 1 {
		t.Errorf("card is in column %d, want development's 1 at once:\n%s", col, board)
	}
	if got := h.current().memory.slides; len(got) != 1 || got[0].from != 1 || got[0].to != 3 {
		t.Errorf("slides = %+v, want one from triage (1) to development (3)", got)
	}
	if !schedulesSlideTick(cmd) {
		t.Error("the move scheduled no slide frame")
	}
}

// cardColumn returns which drawn column holds ref's card, by its x on the
// board's rows.
func cardColumn(t *testing.T, board, ref string) int {
	t.Helper()
	for l := range strings.SplitSeq(board, "\n") {
		if i := strings.Index(l, "▌ "+ref+" "); i >= 0 {
			return len([]rune(l[:i])) / (maxColumn + columnGap)
		}
	}
	t.Fatalf("board has no card for %s:\n%s", ref, board)
	return -1
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

// Covers AE3.
func TestAE3AFailedRuleTakesItsCardOffTheBoard(t *testing.T) {
	h := newRulesHarness(t, 120, crewRules)
	five := crew.Issue{Key: "5", Ref: "#5", Title: "Parse the config"}
	h.send(updateMsg(held(five, "development", "lfg", core.ClaimRunning)))

	u := handledBy(five, "development", "crew:development:failed")
	u.Snapshot.Handled[0].Failures = []crew.ActionFailure{{Action: "lfg", Reason: "tests failed"}}
	h.send(updateMsg(u))

	view := h.view()
	if strings.Contains(boardOf(t, view), "#5") {
		t.Errorf("the board still shows #5:\n%s", view)
	}
	contains(t, view, "NEEDS ATTENTION   #5")
}

// eightRules are eight shown rules, s1 to s8, each taking "sN".
func eightRules() []crew.Rule {
	var out []crew.Rule
	for i := 1; i <= 8; i++ {
		name := fmt.Sprintf("s%d", i)
		out = append(out, crew.Rule{Name: name, Labels: crew.Labels{Ready: crew.State(name)}})
	}
	return out
}

// Covers AE4.
func TestAE4EmptyColumnsDropThenTheBoardScrollsSideways(t *testing.T) {
	u := engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: []core.IssueView{
		{Issue: crew.Issue{Key: "1", Ref: "#1", Title: "One"}, Rule: "s2", Claim: core.ClaimRunning},
		{Issue: crew.Issue{Key: "2", Ref: "#2", Title: "Two"}, Rule: "s6", Claim: core.ClaimRunning},
	}}}}

	h := newRulesHarness(t, 80, eightRules())
	h.send(updateMsg(u))
	board := boardOf(t, h.view())
	contains(t, board, "6 empty stages not shown")
	if got := strings.Join(strings.Fields(strings.Split(board, "\n")[1]), " "); got != "s2 s6" {
		t.Errorf("columns = %q, want s2 s6:\n%s", got, board)
	}

	h = newRulesHarness(t, 30, eightRules())
	h.send(updateMsg(u))
	board = boardOf(t, h.view())
	contains(t, board, "s2", "1 ▸", "#1")
	if strings.Contains(board, "#2") {
		t.Errorf("a 30-column board shows both columns:\n%s", board)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	board = boardOf(t, h.view())
	contains(t, board, "◂ 1", "s6", "#2")
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	contains(t, boardOf(t, h.view()), "◂ 1", "#2")
	h.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	contains(t, boardOf(t, h.view()), "#1", "1 ▸")
}

func TestAWaitingCardWhoseIssueIsGoneLeavesTheBoard(t *testing.T) {
	u := handledBy(twelve, "triage", "crew:triage:done")
	u.Snapshot.Handled[0].Gone = true

	h := newRulesHarness(t, 120, crewRules)
	h.send(updateMsg(u))

	if board := boardOf(t, h.view()); strings.Contains(board, "#12") {
		t.Errorf("the board shows a gone issue:\n%s", board)
	}
}

func TestAGivenUpMoveLeavesNoCard(t *testing.T) {
	u := handledBy(twelve, "triage", "crew:triage:done")
	u.Snapshot.Handled[0].Move = crew.MoveDropped

	h := newRulesHarness(t, 120, crewRules)
	h.send(updateMsg(u))

	if board := boardOf(t, h.view()); strings.Contains(board, "#12") {
		t.Errorf("the board shows an issue whose move was given up:\n%s", board)
	}
}

func TestAnItemTheNextRuleWouldNotTakeHasNoWaitingCard(t *testing.T) {
	pr := twelve
	pr.Kind = crew.KindPullRequest

	h := newRulesHarness(t, 120, crewRules)
	h.send(updateMsg(handledBy(pr, "triage", "crew:triage:done")))

	if board := boardOf(t, h.view()); strings.Contains(board, "#12") {
		t.Errorf("the board shows a pull request no stage of its kind takes:\n%s", board)
	}
}

func TestWithEveryRuleHiddenTheBoardSaysSo(t *testing.T) {
	h := newRulesHarness(t, 80, []crew.Rule{{Name: "only", Labels: crew.Labels{Ready: "ready"}, OffBoard: true}})

	contains(t, boardOf(t, h.view()), "every stage is hidden")
}

// Covers R11 and KTD13: each claim reads through its icon.
func TestEachClaimReadsThroughItsIcon(t *testing.T) {
	for claim, want := range map[core.Claim]string{
		core.ClaimRunning: "⠋ running", core.ClaimJudging: "⠋ judging", core.ClaimTaking: "◌ taking",
		core.ClaimOwed: "! owed", core.ClaimStopping: "■ stopping",
	} {
		h := newRulesHarness(t, 120, crewRules)
		h.send(updateMsg(held(twelve, "triage", "triage", claim)))
		contains(t, boardOf(t, h.view()), "▌ "+want)
	}
}

func TestAColumnCapsItsCardsWhenTheWindowIsShort(t *testing.T) {
	issues := make([]core.IssueView, 0, 8)
	for n := range 8 {
		issues = append(issues, core.IssueView{
			Issue: crew.Issue{Key: strconv.Itoa(n), Ref: fmt.Sprintf("#%d", n), Title: "Card"}, Rule: "triage",
			Claim: core.ClaimRunning,
		})
	}
	h := newRulesHarness(t, 80, crewRules)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(updateMsg(engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: issues}}}))

	view := checkFits(t, h, 80, 24)
	contains(t, boardOf(t, view), "more")
	if !strings.HasSuffix(view, "? help") {
		t.Errorf("view does not end with the key-help line:\n%s", view)
	}
}

// The card cap counts only the columns the board draws: a taller column
// scrolled off to the side must not keep the view from fitting (KTD8).
func TestTheCardCapCountsOnlyTheDrawnColumns(t *testing.T) {
	var issues []core.IssueView
	for col, n := range []int{3, 3, 3, 1, 6} {
		for k := range n {
			key := fmt.Sprintf("%d-%d", col, k)
			issues = append(issues, core.IssueView{
				Issue: crew.Issue{Key: key, Ref: "#" + key, Title: "Card"}, Rule: fmt.Sprintf("s%d", col+1),
				Claim: core.ClaimRunning,
			})
		}
	}
	u := engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: issues}}}
	// The two heights just above the lowest that fits: a cap counting the
	// scrolled-off column of 6 would leave both cut.
	for _, height := range []int{25, 26} {
		h := newRulesHarness(t, 80, eightRules()[:5])
		h.send(tea.WindowSizeMsg{Width: 80, Height: height})
		h.send(updateMsg(u))

		if view := checkFits(t, h, 80, height); strings.Contains(view, "lines cut") {
			t.Errorf("at %d rows the view was cut although a lower card cap fits:\n%s", height, view)
		}
	}
}

// heldAgain is a snapshot of #12 held by development with the entry its
// triage left, marked held.
func heldAgain() engine.Update {
	u := held(twelve, "development", "lfg", core.ClaimRunning)
	e := handledBy(twelve, "triage", "crew:triage:done").Snapshot.Handled[0]
	e.HeldBy = "development"
	u.Snapshot.Handled = []core.HandledView{e}
	return u
}

func TestAnIssueHeldAgainHasOnlyTheCardOfTheRuleHoldingIt(t *testing.T) {
	h := newRulesHarness(t, 120, crewRules)

	h.send(updateMsg(heldAgain()))

	board := boardOf(t, h.view())
	if n := strings.Count(board, "#12"); n != 1 {
		t.Fatalf("board shows #12 %d times, want once:\n%s", n, board)
	}
	if col := cardColumn(t, board, "#12"); col != 1 {
		t.Errorf("card is in column %d, want development's 1:\n%s", col, board)
	}
}
