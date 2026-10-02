package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// View renders three regions from the latest snapshot: the held issues by
// stage and claim, the actions not yet ended with their elapsed time, and
// the recent events. Every line is cut to the window's width.
func (m Model) View() tea.View {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	switch {
	case m.stopping:
		add("crew: stopping… (q or ctrl+c again forces the exit)")
	case m.snap.Stopping:
		add("crew: stopping…")
	case m.snap.TimeUp:
		add("crew: run time is up, winding down: %d %s held (q or ctrl+c stops)", len(m.snap.Issues), lines.Plural(len(m.snap.Issues), "issue", "issues"))
	default:
		add("crew: %d %s held (q or ctrl+c stops)", len(m.snap.Issues), lines.Plural(len(m.snap.Issues), "issue", "issues"))
	}

	add("")
	add("Issues")
	if len(m.snap.Issues) == 0 {
		add("  none")
	}
	for _, stage := range byStage(m.snap.Issues) {
		add("  %s", stage[0].Stage)
		for _, iv := range stage {
			add("    %-9s %s %s", iv.Claim, iv.Issue.Ref, iv.Issue.Title)
		}
	}

	add("")
	add("Actions")
	var names, states []string
	for _, iv := range m.snap.Issues {
		for _, a := range iv.Actions {
			if a.Phase == core.PhaseEnded {
				continue
			}
			names = append(names, fmt.Sprintf("%s %s/%s", iv.Issue.Ref, iv.Stage, a.Name))
			state := a.Phase.String()
			if a.Phase == core.PhaseRunning {
				state = "running " + elapsed(m.at.Sub(a.Started))
			}
			states = append(states, state)
		}
	}
	if len(names) == 0 {
		add("  none")
	}
	pad := 0
	for _, n := range names {
		pad = max(pad, utf8.RuneCountInString(n))
	}
	for i, n := range names {
		add("  %-*s  %s", pad, n, states[i])
	}

	add("")
	add("Recent events")
	if len(m.snap.Recent) == 0 {
		add("  none")
	}
	for _, e := range m.snap.Recent {
		add("  %s %s", e.Time().In(m.loc).Format(time.TimeOnly), lines.Text(e))
	}

	for i, l := range out {
		out[i] = fit(l, m.width)
	}
	return tea.NewView(strings.Join(out, "\n"))
}

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

// elapsed formats d as 5m03s, or 1h05m03s past an hour.
func elapsed(d time.Duration) string {
	s := int(max(d, 0) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%dh%02dm%02ds", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
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
