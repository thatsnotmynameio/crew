package tui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// givenUp stands where an entry's state would be when crew gave its verdict
// move up, so the issue is not in that state.
const givenUp = "move given up"

// recentHeader is the rows of the Recent events region's header: a blank
// line and its title.
const recentHeader = 2

// View renders the latest snapshot: a summary of the run and of the issues
// handled so far, then four regions: the held issues by stage and claim, the
// actions not yet ended with their elapsed time, the issues handled this run,
// and the recent events. It fits the window's height: Recent events gives up
// its rows first, then the oldest handled issues that need no attention
// collapse into one line per state, and only then is the view cut at the
// bottom. Every line is cut to the window's width.
func (m Model) View() tea.View {
	out := m.fitted()
	for i, l := range out {
		out[i] = fit(l, m.width)
	}
	return tea.NewView(strings.Join(out, "\n"))
}

// fitted returns the view's lines, at most the window's height of them when
// the height is known.
func (m Model) fitted() []string {
	fixed := []string{m.top()}
	for _, w := range m.warnings {
		fixed = append(fixed, "warning: "+w)
	}
	fixed = append(fixed, m.counts())
	fixed = append(fixed, m.issues()...)
	fixed = append(fixed, m.actions()...)
	fixed = append(fixed, "", "Handled")
	entries := byAttention(m.snap.Handled)
	cols := columnsOf(entries)
	// byAttention puts the entries that need attention first.
	split := slices.IndexFunc(entries, func(e core.HandledView) bool { return !e.NeedsAttention() })
	if split < 0 {
		split = len(entries)
	}
	attention, rest := entries[:split], entries[split:]
	for _, e := range attention {
		fixed = append(fixed, cols.rows(e)...)
	}
	if len(entries) == 0 {
		fixed = append(fixed, "  none")
	}
	recent := m.recent()
	h := m.height
	expanded := withRest(fixed, rest, cols, 0)

	// Everything fits, or there is no height to fit.
	if h <= 0 || len(expanded)+recentHeader+len(recent) <= h {
		return append(expanded, recentRegion(recent)...)
	}
	// Recent events keeps its newest rows that fit, with its header.
	if left := h - len(expanded) - recentHeader; left > 0 {
		return append(expanded, recentRegion(recent[len(recent)-left:])...)
	}
	// The oldest entries that need no attention collapse, as few as fit.
	for k := 0; k <= len(rest); k++ {
		if out := withRest(fixed, rest, cols, k); len(out) <= h {
			return out
		}
	}
	// Even the entries that need attention do not fit: cut at the bottom.
	all := withRest(fixed, rest, cols, len(rest))
	keep := h - 1
	return append(all[:keep:keep], fmt.Sprintf("… %d lines cut", len(all)-keep))
}

// withRest returns fixed, then the rows of the entries of rest but its last
// k, then one line per state of those k.
func withRest(fixed []string, rest []core.HandledView, cols columns, k int) []string {
	out := slices.Clone(fixed)
	for _, e := range rest[:len(rest)-k] {
		out = append(out, cols.rows(e)...)
	}
	return append(out, collapsed(rest[len(rest)-k:])...)
}

// collapsed sums up entries in one line per state, in the order of each
// state's first entry.
func collapsed(entries []core.HandledView) []string {
	var order []crew.State
	n := map[crew.State]int{}
	for _, e := range entries {
		if n[e.To] == 0 {
			order = append(order, e.To)
		}
		n[e.To]++
	}
	out := make([]string, 0, len(order))
	for _, s := range order {
		out = append(out, fmt.Sprintf("  … and %d more in %s", n[s], s))
	}
	return out
}

// top is the first line: why crew runs or stops, how many issues it holds,
// its uptime and, under a run time limit, the time left.
func (m Model) top() string {
	held := fmt.Sprintf("%d %s held", len(m.snap.Issues), lines.Plural(len(m.snap.Issues), "issue", "issues"))
	if started := m.snap.Started; !started.IsZero() {
		held += ", up " + short(m.at.Sub(started))
		if limit := m.snap.RunTimeLimit; limit > 0 && !m.snap.TimeUp {
			held += ", " + short(started.Add(limit).Sub(m.at)) + " left"
		}
	}
	const keys = " (q or ctrl+c stops)"
	switch {
	case m.stopping:
		return "crew: stopping… (q or ctrl+c again forces the exit)"
	case m.snap.Stopping:
		return "crew: stopping…"
	case m.snap.TimeUp:
		return "crew: run time is up, winding down: " + held + keys
	default:
		return "crew: " + held + keys
	}
}

// counts is the second line: how many issues crew handled and what the
// sessions that ended this run spent, then the issues by the state each went
// to. Given-up moves come first, then the states of entries that need
// attention, so a narrow window cuts the counts of successes first, and the
// spend last.
func (m Model) counts() string {
	handled := m.snap.Handled
	line := fmt.Sprintf("handled %d", len(handled))
	if spent := m.snap.Spent.String(); spent != "" {
		line += " (" + spent + ")"
	}
	if len(handled) == 0 {
		return line
	}
	var parts []string
	dropped := 0
	n := map[crew.State]int{}
	failing := map[crew.State]bool{}
	for _, e := range handled {
		if e.Move == crew.MoveDropped {
			dropped++
			continue
		}
		n[e.To]++
		failing[e.To] = failing[e.To] || e.NeedsAttention()
	}
	if dropped > 0 {
		parts = append(parts, fmt.Sprintf("%d %s given up", dropped, lines.Plural(dropped, "move", "moves")))
	}
	states := slices.Collect(maps.Keys(n))
	slices.SortFunc(states, func(a, b crew.State) int {
		return cmp.Or(trueFirst(failing[a], failing[b]), cmp.Compare(n[b], n[a]), cmp.Compare(a, b))
	})
	for _, s := range states {
		parts = append(parts, fmt.Sprintf("%d %s", n[s], s))
	}
	return line + ": " + strings.Join(parts, ", ")
}

// issues is the Issues region: the held issues by stage and claim.
func (m Model) issues() []string {
	out := []string{"", "Issues"}
	if len(m.snap.Issues) == 0 {
		out = append(out, "  none")
	}
	for _, stage := range byStage(m.snap.Issues) {
		out = append(out, "  "+stage[0].Stage)
		for _, iv := range stage {
			out = append(out, fmt.Sprintf("    %-9s %s %s", iv.Claim, iv.Issue.Ref, iv.Issue.Title))
		}
	}
	return out
}

// actions is the Actions region: the actions not yet ended, with the
// elapsed time of the running ones, a running check included: its action
// still runs, from its session's start. A resumed one names its workspace.
func (m Model) actions() []string {
	out := []string{"", "Actions"}
	var names, states []string
	for _, iv := range m.snap.Issues {
		for _, a := range iv.Actions {
			if a.Phase == core.PhaseEnded {
				continue
			}
			names = append(names, fmt.Sprintf("%s %s/%s", iv.Issue.Ref, iv.Stage, a.Name))
			states = append(states, m.actionState(a))
		}
	}
	if len(names) == 0 {
		out = append(out, "  none")
	}
	pad := width(names)
	for i, n := range names {
		out = append(out, fmt.Sprintf("  %-*s  %s", pad, n, states[i]))
	}
	return out
}

// actionState is a's phase, with its elapsed time while it runs or is
// checked, and its workspace when it resumed.
func (m Model) actionState(a core.ActionView) string {
	state := a.Phase.String()
	if a.Phase != core.PhaseRunning && a.Phase != core.PhaseChecking {
		return state
	}
	state += " " + elapsed(m.at.Sub(a.Started))
	if a.Resumed {
		state = "resumed in " + a.Workspace + ", " + state
	}
	return state
}

// recent returns one line per recent event, oldest first, or none. Each
// event takes one row, as the fitting counts, even when its reason spans
// several lines, such as git's stderr.
func (m Model) recent() []string {
	if len(m.snap.Recent) == 0 {
		return []string{"  none"}
	}
	var out []string
	for _, e := range m.snap.Recent {
		out = append(out, fmt.Sprintf("  %s %s", e.Time().In(m.loc).Format(time.TimeOnly), oneLine(lines.Text(e))))
	}
	return out
}

// recentRegion is the Recent events region holding rows.
func recentRegion(rows []string) []string {
	return append([]string{"", "Recent events"}, rows...)
}

// byAttention orders entries for the Handled region: those that need
// attention first, then the rest, each the most recently ended first, then
// the most recently released first.
func byAttention(handled []core.HandledView) []core.HandledView {
	out := slices.Clone(handled)
	slices.Reverse(out)
	slices.SortStableFunc(out, func(a, b core.HandledView) int {
		return cmp.Or(trueFirst(a.NeedsAttention(), b.NeedsAttention()), b.Ended.Compare(a.Ended))
	})
	return out
}

// trueFirst orders true before false.
func trueFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// columns are the widths of a Handled line's padded columns. The spend and
// pull request columns are left out when no entry fills them.
type columns struct{ state, ref, stage, spend, pr int }

func columnsOf(entries []core.HandledView) columns {
	var c columns
	for _, e := range entries {
		c.state = max(c.state, utf8.RuneCountInString(stateOf(e)))
		c.ref = max(c.ref, utf8.RuneCountInString(e.Issue.Ref))
		c.stage = max(c.stage, utf8.RuneCountInString(stageOf(e)))
		c.spend = max(c.spend, utf8.RuneCountInString(e.Spend().String()))
		c.pr = max(c.pr, utf8.RuneCountInString(pullRequestOf(e)))
	}
	return c
}

// rows are e's rows in the Handled region: its line, then its reasons, then
// its actions' pull requests.
func (c columns) rows(e core.HandledView) []string {
	return slices.Concat([]string{c.line(e)}, reasons(e), pullRequests(e))
}

// line is e's Handled line: its state, ref, stage and duration, spend and
// pull request, then title, which sits last so a narrow window cuts it
// first.
func (c columns) line(e core.HandledView) string {
	cells := []string{pad(stateOf(e), c.state), pad(e.Issue.Ref, c.ref), pad(stageOf(e), c.stage)}
	if c.spend > 0 {
		cells = append(cells, pad(e.Spend().String(), c.spend))
	}
	if c.pr > 0 {
		cells = append(cells, pad(pullRequestOf(e), c.pr))
	}
	return "  " + strings.Join(append(cells, e.Issue.Title), "  ")
}

// pad pads s with spaces to width runes.
func pad(s string, width int) string { return fmt.Sprintf("%-*s", width, s) }

func stateOf(e core.HandledView) string {
	if e.Move == crew.MoveDropped {
		return givenUp
	}
	return string(e.To)
}

func stageOf(e core.HandledView) string { return e.Stage + " " + elapsed(e.Duration()) }

// reasons are the lines under an entry that needs attention: one per failed
// action, then one for a given-up move, each on one line.
func reasons(e core.HandledView) []string {
	var out []string
	for _, f := range e.Failures {
		out = append(out, fmt.Sprintf("    %s failed: %s", f.Action, oneLine(f.Reason)))
	}
	if e.Move == crew.MoveDropped {
		out = append(out, fmt.Sprintf("    move to %s given up: %s", e.To, oneLine(e.DropReason)))
	}
	return out
}

// pullRequestOf is the pull request on e's line: that of its one action,
// when it had a session; a stage of several actions gives each its own row.
func pullRequestOf(e core.HandledView) string {
	if len(e.Actions) != 1 || e.Actions[0].Spend.Sessions == 0 {
		return ""
	}
	pr := e.Actions[0].PullRequest
	if pr.Lookup == crew.PullRequestFound {
		return pr.Ref
	}
	return pr.String()
}

// pullRequests are the lines under an entry of several actions: one per
// action that had a session, with its pull request.
func pullRequests(e core.HandledView) []string {
	if len(e.Actions) <= 1 {
		return nil
	}
	var out []string
	for _, a := range e.Actions {
		if a.Spend.Sessions > 0 {
			out = append(out, fmt.Sprintf("    %s: %s", a.Name, a.PullRequest))
		}
	}
	return out
}

// oneLine joins s's words with single spaces, so a reason takes one row.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// byStage groups issues by stage, in the order each stage first appears,
// and orders each group by claim, keeping the taken order within a claim.
func byStage(issues []core.IssueView) [][]core.IssueView {
	var groups [][]core.IssueView
	index := map[string]int{}
	for _, iv := range issues {
		i, ok := index[iv.Stage]
		if !ok {
			i = len(groups)
			index[iv.Stage] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], iv)
	}
	for _, g := range groups {
		slices.SortStableFunc(g, func(a, b core.IssueView) int { return cmp.Compare(a.Claim, b.Claim) })
	}
	return groups
}

// width returns the widest of names, in runes.
func width(names []string) int {
	pad := 0
	for _, n := range names {
		pad = max(pad, utf8.RuneCountInString(n))
	}
	return pad
}

// elapsed formats d as 5m03s, or 1h05m03s past an hour.
func elapsed(d time.Duration) string {
	d = max(d, 0)
	minutes, seconds := int(d%time.Hour/time.Minute), int(d%time.Minute/time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm%02ds", int(d/time.Hour), minutes, seconds)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

// short formats d as 3h12m, 48m, or 30s under a minute.
func short(d time.Duration) string {
	d = max(d, 0)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

// fit cuts s to width runes, ending a cut line with an ellipsis. Titles sit
// at the end of their lines, so a narrow window truncates them first.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	r := []rune(s)
	return string(r[:width-1]) + "…"
}
