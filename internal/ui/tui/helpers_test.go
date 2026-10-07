package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// titledIssue is #key with the title title.
func titledIssue(key, title string) crew.Issue {
	return crew.NewIssue(crew.IssueData{ID: issueID(key), Ref: "#" + key, Title: title})
}

// edited returns i with edit applied to its fields.
func edited(i crew.Issue, edit func(*crew.IssueData)) crew.Issue {
	d := i.Data()
	edit(&d)
	return crew.NewIssue(d)
}

// titled returns i with the title title.
func titled(i crew.Issue, title string) crew.Issue {
	return edited(i, func(d *crew.IssueData) { d.Title = title })
}

// titledOnBoard returns b with its issue titled title.
func titledOnBoard(b crew.BoardIssue, title string) crew.BoardIssue {
	return crew.NewBoardIssue(titled(b.Issue(), title), b.Labels())
}

// entry is #key handled by rule into to, taken and ended the given minutes
// before start.
func entry(key, title string, rule crew.RuleName, to crew.State, taken, ended int) core.HandledView {
	return core.HandledView{
		Issue: titledIssue(key, title), Rule: rule, To: to, Move: crew.MoveDone,
		Taken: start.Add(-time.Duration(taken) * time.Minute), Ended: start.Add(-time.Duration(ended) * time.Minute),
	}
}

// failedEntry is entry with actions failed.
func failedEntry(key, title string, taken, ended int, actions ...string) core.HandledView {
	e := entry(key, title, "implement", "needs attention", taken, ended)
	for _, a := range actions {
		e.Failures = append(e.Failures, crew.ActionFailure{Action: crew.ActionName(a)})
	}
	return e
}

// givenUpEntry is entry whose ending move crew gave up.
func givenUpEntry(e core.HandledView, reason string) core.HandledView {
	e.Move, e.DropReason = crew.MoveDropped, reason
	return e
}

// handledSnapshot is runningSnapshot 12 minutes into a one-hour run, with
// four issues handled: a failure whose code action never had a session, a
// given-up move and two successes, one of two actions, #7 now idle in
// review's column. An earlier rule of #8 spent $2.00 this run. Every action
// acted as you.
func handledSnapshot() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Board = append(u.Snapshot.Board,
		crew.NewBoardIssue(titledIssue("7", "Log the poll interval"), []crew.State{"ready to review"}))
	u.Snapshot.Handled = []core.HandledView{
		acted(
			failedEntry("5", "Parse the config once", 40, 30, "tests", "code"),
			core.HandledAction{Name: "tests", Spend: spent(0.84, 1_200_000), PullRequest: noPullRequest},
			core.HandledAction{Name: "code"}),
		acted(givenUpEntry(entry("6", "Drop the old flag", "implement", "ready to review", 25, 20), "issue closed"),
			core.HandledAction{Name: "code", Spend: spent(3.1, 4_500_000), PullRequest: found("#44")}),
		acted(entry("8", "Trim the README", "review", "ready to merge", 9, 3),
			core.HandledAction{Name: "review", Spend: spent(0.42, 48_200), PullRequest: found("#46")}),
		acted(entry("7", "Log the poll interval", "implement", "ready to review", 15, 6),
			core.HandledAction{Name: "code", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")},
			core.HandledAction{Name: "tests", Spend: spent(1.1, 900_000), PullRequest: noPullRequest}),
	}
	u.Snapshot.Spent = spent(2, 300_000)
	for _, e := range u.Snapshot.Handled {
		u.Snapshot.Spent = u.Snapshot.Spent.Add(e.Spend())
	}
	u.Snapshot.Bots[0].Spend = u.Snapshot.Spent
	return u
}

// manySnapshot is runningSnapshot with ten issues handled, each now in
// review's column after #2: #11 and #12 failed, and the successes #13 to
// #20, the higher the number the more recent, every action as you.
func manySnapshot() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{
		acted(failedEntry("11", "Parse the config once", 90, 50, "lfg"),
			core.HandledAction{Name: "lfg", Spend: spent(4.05, 6_100_000), PullRequest: noPullRequest}),
		acted(failedEntry("12", "Drop the old --dry flag", 60, 59, "lfg"),
			core.HandledAction{Name: "lfg"}),
	}
	for n := 13; n <= 20; n++ {
		u.Snapshot.Handled = append(u.Snapshot.Handled,
			acted(entry(strconv.Itoa(n), fmt.Sprintf("Success number %d", n), "development", "ready to review", 60, 40-n),
				core.HandledAction{
					Name: "lfg", Spend: spent(float64(n)/2, int64(n)*1_000_000), PullRequest: found("#" + strconv.Itoa(n+30)),
				}))
	}
	for _, e := range u.Snapshot.Handled {
		u.Snapshot.Spent = u.Snapshot.Spent.Add(e.Spend())
		u.Snapshot.Board = append(u.Snapshot.Board, crew.NewBoardIssue(e.Issue, []crew.State{"ready to review"}))
	}
	u.Snapshot.Bots[0].Spend = u.Snapshot.Spent
	return u
}

// firstLine returns the view's first line, the header.
func firstLine(view string) string {
	first, _, _ := strings.Cut(view, "\n")
	return first
}

// fitted renders u in a window of width by height, and checks the view
// fits it.
func fitted(t *testing.T, width, height int, u engine.Update) string {
	t.Helper()
	h := newHarness(t, width)
	h.send(tea.WindowSizeMsg{Width: width, Height: height})
	h.send(updateMsg(u))
	return checkFits(t, h, width, height)
}

// current is the model the harness drives.
func (h *harness) current() Model {
	m, _ := h.model.(Model)
	return m
}

// checkFits returns h's view after checking it fits width by height.
func checkFits(t *testing.T, h *harness, width, height int) string {
	t.Helper()
	view := h.view()
	if n := strings.Count(view, "\n") + 1; height > 0 && n > height {
		t.Errorf("view has %d lines, over the window's %d:\n%s", n, height, view)
	}
	for l := range strings.SplitSeq(view, "\n") {
		if n := lipgloss.Width(l); n > width {
			t.Errorf("line is %d columns wide, over %d: %q", n, width, l)
		}
	}
	return view
}

// spent is one session that reported cost dollars and tokens tokens.
func spent(cost float64, tokens int64) crew.Spend {
	return crew.Usage{Cost: crew.Some(cost), Tokens: crew.Some(crew.Tokens{Output: tokens})}.Spend()
}

// found is the pull request ref, as a lookup found it.
func found(ref string) crew.PullRequest {
	return crew.PullRequestFound{Ref: ref, URL: "https://github.com/o/r/pull/" + ref[1:]}
}

var noPullRequest = crew.PullRequestNone{}

// acted is e with its rule's actions.
func acted(e core.HandledView, actions ...core.HandledAction) core.HandledView {
	e.Actions = actions
	return e
}

// contains fails t unless view holds each of wants.
func contains(t *testing.T, view string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

// nextCard returns the byte index in l of the left border of the first
// card whose first row starts with prefix, after the highlight's marker
// when it has one; -1 when l has none.
func nextCard(l, prefix string) int {
	i := strings.Index(l, "│ "+prefix)
	if j := strings.Index(l, "│ "+focusMark+prefix); j >= 0 && (i < 0 || j < i) {
		i = j
	}
	return i
}

// issueID returns the id of the issue keyed key, in no repository.
func issueID(key string) crew.IssueID { return crew.IssueID{Key: key} }

// taken is the event of rule taking issue at at, moving it from one state to
// another.
func taken(at time.Time, issue crew.Issue, rule crew.RuleName, from, to crew.State) crew.RunTaken {
	return crew.RunTaken{
		At: at, IssueID: issue.ID(), IssueRef: issue.Ref(), Rule: rule, Issue: issue.Data(), From: from, To: to,
	}
}
