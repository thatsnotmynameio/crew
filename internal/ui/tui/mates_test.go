package tui

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// reviewerNoKey is the startup warning of a mate reviewer without a key.
const reviewerNoKey = "mate reviewer has no key on this machine for thatsnotmynameio; " +
	"run `crew mates create reviewer` in this repository"

// aeOneMates are AE1's entries: the default mate clerk for triage, with
// three triage actions ended, developer running development on #1, and the
// boss as octocat.
func aeOneMates() []core.MateView {
	return []core.MateView{
		{
			Name: "clerk", Acting: true, State: "acting", Writes: true, Pairs: []string{"triage/triage"},
			Spend: spent(0.1, 100_000).Add(spent(0.12, 110_000)).Add(spent(0.2, 100_000)),
		},
		{
			Name: "developer", Acting: true, State: "acting", Pairs: []string{"development/lfg"},
			Running: []core.RunningAction{{IssueRef: "#1", Stage: "development", Action: "lfg"}},
		},
		{Name: "you", You: true, Login: "octocat"},
	}
}

// withMates is runningSnapshot with entries as its mates.
func withMates(entries ...core.MateView) engine.Update {
	u := runningSnapshot()
	u.Snapshot.Mates = entries
	return u
}

// matesOf returns the rows of view's Mates section under its title, down
// to the blank row above Workflow.
func matesOf(t *testing.T, view string) []string {
	t.Helper()
	all, i, j := rowsOf(view), titleRow(view, "Mates"), titleRow(view, "Workflow")
	if i < 0 || j < i {
		t.Fatalf("view lacks the Mates section above Workflow:\n%s", view)
	}
	return all[i+1 : j-1]
}

// rowsAre fails t unless rows are want, one for one.
func rowsAre(t *testing.T, view string, rows, want []string) {
	t.Helper()
	if !slices.Equal(rows, want) {
		t.Errorf("Mates rows are\n%s\nwant\n%s\nin:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"), view)
	}
}

// Covers AE1: each mate's state and totals, then its pairs, crew's writes
// on the default mate and what runs as it now.
func TestAE1EachMateShowsItsStateTotalsPairsAndRunningActions(t *testing.T) {
	view := fitted(t, 80, 0, withMates(aeOneMates()...))

	rowsAre(t, view, matesOf(t, view), []string{
		" clerk      acting   3 actions · $0.42 · 310K tokens",
		"   crew's writes · triage/triage",
		" developer  acting",
		"   development/lfg  ▸ #1 development/lfg",
		" you        octocat",
		"   none",
	})
	contains(t, rowsOf(view)[titleRow(view, "Mates")], "2 acting")
}

// Covers AE3: a mate that cannot act shows its short reason and that its
// pairs act as you, and its startup warning stays under the header.
func TestAE3AMateThatCannotActShowsItsReasonAndActsAsYou(t *testing.T) {
	h := newHarness(t, 120, reviewerNoKey)
	h.send(updateMsg(withMates(
		core.MateView{Name: "clerk", Acting: true, State: "acting", Writes: true, Pairs: []string{"triage/triage"}},
		core.MateView{
			Name: "reviewer", State: "cannot act: no key", ActsAsYou: true, Pairs: []string{"review/review"},
		},
		core.MateView{Name: "you", You: true, Pairs: []string{"review/review"}, Spend: spent(0.8, 600_000)},
	)))
	view := h.view()

	rowsAre(t, view, matesOf(t, view), []string{
		" clerk     acting",
		"   crew's writes · triage/triage",
		" reviewer  cannot act: no key",
		"   → you: review/review",
		" you                           1 action · $0.80 · 600K tokens",
		"   review/review",
	})
	contains(t, rowsOf(view)[1], "warning: mate reviewer has no key")
	contains(t, view, "crew mates create reviewer", "1 acting · 1 cannot act")
}

// Covers AE4: the default mate whose writes went back to the boss says so,
// crew's writes move to you, and its live warning follows the startup ones.
func TestAE4ADefaultMateWhoseWritesFellBackWarnsUnderTheHeader(t *testing.T) {
	lost := "crew's writes as mate clerk went back to you: GitHub refused its credentials; " +
		"crew writes as you until it restarts"
	h := newHarness(t, 200, reviewerNoKey)
	h.send(updateMsg(withMates(
		core.MateView{
			Name: "clerk", State: "writes as you", Warnings: []string{lost + "\x1b[31m"}, Pairs: []string{"triage/triage"},
		},
		core.MateView{Name: "you", You: true, Writes: true},
	)))
	view := h.view()

	rows := rowsOf(view)
	contains(t, rows[1], "warning: mate reviewer has no key")
	if rows[2] != "warning: "+lost {
		t.Errorf("row 2 is %q, want the live warning after the startup one:\n%s", rows[2], view)
	}
	rowsAre(t, view, matesOf(t, view), []string{
		" clerk  writes as you",
		"   triage/triage",
		" you",
		"   crew's writes",
	})
	contains(t, rows[titleRow(view, "Mates")], "1 cannot act")
}

func TestTheMatesSummaryCountsActingAndNotActingMates(t *testing.T) {
	for _, tt := range []struct {
		name    string
		entries []core.MateView
		want    string
	}{
		{"both", []core.MateView{
			{Name: "clerk", Acting: true, State: "acting"},
			{Name: "developer", Acting: true, State: "acting"},
			{Name: "reviewer", State: "token not renewed"},
			{Name: "you", You: true},
		}, "2 acting · 1 cannot act"},
		{"only you", []core.MateView{{Name: "you", You: true, Writes: true}}, "no mates"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			view := fitted(t, 80, 0, withMates(tt.entries...))

			i := titleRow(view, "Mates")
			if i < 0 {
				t.Fatalf("view lacks the Mates section:\n%s", view)
			}
			if rule := rowsOf(view)[i]; !strings.HasSuffix(rule, "─ "+tt.want) {
				t.Errorf("Mates rule is %q, want it to end in %q", rule, tt.want)
			}
		})
	}
}

// Covers R5: a second line wider than the window ends in an ellipsis, and
// the entry keeps its two rows.
func TestASecondLineWiderThanTheWindowEndsInAnEllipsis(t *testing.T) {
	pairs := []string{"implement/code", "implement/tests", "implement/docs", "review/review", "review/security"}
	view := fitted(t, 60, 0, withMates(core.MateView{Name: "you", You: true, Writes: true, Pairs: pairs}))

	rows := matesOf(t, view)
	if len(rows) != 2 {
		t.Fatalf("Mates has %d rows, want 2:\n%s", len(rows), view)
	}
	if !strings.HasSuffix(rows[1], "…") || lipgloss.Width(rows[1]) != 60 {
		t.Errorf("second line %q is not cut to 60 cells with an ellipsis", rows[1])
	}
}

// Covers R5: a second line too wide for the window cuts its stages and
// actions first, so the actions running now stay whole.
func TestASecondLineTooWideCutsItsPairsAndKeepsTheRunningActions(t *testing.T) {
	pairs := []string{"implement/code", "implement/tests", "implement/docs", "review/review", "review/security"}
	running := core.RunningAction{IssueRef: "#1", Stage: "implement", Action: "code"}
	view := fitted(t, 60, 0, withMates(core.MateView{
		Name: "you", You: true, Writes: true, Pairs: pairs, Running: []core.RunningAction{running},
	}))

	rows := matesOf(t, view)
	if len(rows) != 2 {
		t.Fatalf("Mates has %d rows, want 2:\n%s", len(rows), view)
	}
	if !strings.HasSuffix(rows[1], "▸ #1 implement/code") || !strings.Contains(rows[1], "…") {
		t.Errorf("second line %q does not end with the running action after cut pairs", rows[1])
	}
	if lipgloss.Width(rows[1]) > 60 {
		t.Errorf("second line %q is wider than 60 cells", rows[1])
	}
}

// shortWindow is AE1's mates with a said line, ten handled issues, 30
// events and three cards in implement.
func shortWindow() engine.Update {
	u := withSaid("Running the tests")
	u.Snapshot.Mates = aeOneMates()
	u.Snapshot.Handled = manySnapshot().Snapshot.Handled
	u.Snapshot.Recent = eventful().Snapshot.Recent
	for _, key := range []string{"3", "4"} {
		u.Snapshot.Issues = append(u.Snapshot.Issues, core.IssueView{
			Issue: crew.Issue{Key: key, Ref: "#" + key, Title: "Card"}, Stage: "implement", Claim: core.ClaimTaking,
		})
	}
	return u
}

// Covers AE6 and R13: once Events, Handled, the said lines and the cards
// have shrunk, the mates drop their second lines, and only then is the view
// cut.
func TestAE6TheMatesDropTheirSecondLinesLastBeforeTheCut(t *testing.T) {
	u := shortWindow()
	h := newHarness(t, 80)
	h.send(updateMsg(u))
	least := budget{events: minScroll, handled: minScroll, cards: 1, said: false, mateDetails: true}
	height := len(h.current().rows(least))
	entries := len(u.Snapshot.Mates)

	view := fitted(t, 80, height, u)
	if len(matesOf(t, view)) != 2*entries || strings.Contains(view, "lines cut") {
		t.Fatalf("at %d rows the mates lost their second lines or the view was cut:\n%s", height, view)
	}
	if strings.Contains(view, "└") || !strings.Contains(view, "+2 more") || len(eventsRows(t, view)) != minScroll {
		t.Errorf("at %d rows Events, the said lines or the cards did not shrink first:\n%s", height, view)
	}

	view = fitted(t, 80, height-1, u)
	if rows := matesOf(t, view); len(rows) != entries || strings.Contains(view, "lines cut") {
		t.Errorf("one row short, the mates kept their second lines or the view was cut:\n%s", view)
	}
	if view := fitted(t, 80, height-entries, u); strings.Contains(view, "lines cut") {
		t.Errorf("%d rows short, the view was cut although one row per mate fits:\n%s", entries, view)
	}
	if view := fitted(t, 80, height-entries-1, u); !strings.Contains(view, "lines cut") {
		t.Errorf("%d rows short, the view was not cut:\n%s", entries+1, view)
	}
}
