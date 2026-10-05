package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// entry is #key handled by rule into to, taken and ended the given minutes
// before start.
func entry(key, title, rule string, to crew.State, taken, ended int) core.HandledView {
	return core.HandledView{
		Issue: crew.Issue{Key: key, Ref: "#" + key, Title: title}, Rule: rule, To: to, Move: crew.MoveDone,
		Taken: start.Add(-time.Duration(taken) * time.Minute), Ended: start.Add(-time.Duration(ended) * time.Minute),
	}
}

// failedEntry is entry with its actions failed for reasons, in pairs of
// action and reason.
func failedEntry(key, title string, taken, ended int, failures ...string) core.HandledView {
	e := entry(key, title, "implement", "needs attention", taken, ended)
	for len(failures) >= 2 {
		e.Failures = append(e.Failures, crew.ActionFailure{Action: failures[0], Reason: failures[1]})
		failures = failures[2:]
	}
	return e
}

// givenUpEntry is entry whose verdict move crew gave up.
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
	u.Snapshot.Board = append(u.Snapshot.Board, crew.BoardIssue{
		Issue: crew.Issue{Key: "7", Ref: "#7", Title: "Log the poll interval"}, Labels: []string{"ready to review"},
	})
	u.Snapshot.Handled = []core.HandledView{
		acted(
			failedEntry("5", "Parse the config once", 40, 30,
				"tests", "exited 1: tests fail", "code", "prompt did not render"),
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

// manySnapshot is runningSnapshot with ten issues handled: #11 and #12
// failed, and the successes #13 to #20 in crew:waiting review, the higher
// the number the more recent, every action as you.
func manySnapshot() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{
		acted(failedEntry("11", "Parse the config once", 90, 50, "lfg", `exited 1: "tests fail on Go 1.27"`),
			core.HandledAction{Name: "lfg", Spend: spent(4.05, 6_100_000), PullRequest: noPullRequest}),
		acted(failedEntry("12", "Drop the old --dry flag", 60, 59, "lfg", `prompt did not render: no field "Body"`),
			core.HandledAction{Name: "lfg"}),
	}
	for n := 13; n <= 20; n++ {
		u.Snapshot.Handled = append(u.Snapshot.Handled,
			acted(entry(strconv.Itoa(n), fmt.Sprintf("Success number %d", n), "development", "crew:waiting review", 60, 40-n),
				core.HandledAction{
					Name: "lfg", Spend: spent(float64(n)/2, int64(n)*1_000_000), PullRequest: found("#" + strconv.Itoa(n+30)),
				}))
	}
	for _, e := range u.Snapshot.Handled {
		u.Snapshot.Spent = u.Snapshot.Spent.Add(e.Spend())
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
	return crew.Usage{Cost: cost, HasCost: true, Tokens: crew.Tokens{Output: tokens}, HasTokens: true}.Spend()
}

// found is the pull request ref, as a lookup found it.
func found(ref string) crew.PullRequest {
	return crew.PullRequest{Lookup: crew.PullRequestFound, Ref: ref, URL: "https://github.com/o/r/pull/" + ref[1:]}
}

var noPullRequest = crew.PullRequest{Lookup: crew.PullRequestNone}

// acted is e with its rule's actions.
func acted(e core.HandledView, actions ...core.HandledAction) core.HandledView {
	e.Actions = actions
	return e
}

// handledView renders a snapshot handling entries, in a window wide enough
// for their whole lines.
func handledView(t *testing.T, entries ...core.HandledView) string {
	t.Helper()
	u := runningSnapshot()
	u.Snapshot.Handled = entries
	return fitted(t, 160, 0, u)
}

// handledText is every Handled row of u in a window width wide, past the
// rows the view has room for, with its styles stripped.
func handledText(t *testing.T, width int, u engine.Update) string {
	t.Helper()
	h := newHarness(t, width)
	h.send(updateMsg(u))
	_, rows := h.current().handledSection(width)
	return ansi.Strip(strings.Join(rows, "\n"))
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
