package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// The Actions section's sizes, in cells.
const (
	// chipPadding is the space either side of a queue's name in its chip.
	chipPadding = 2
	// actionCells is the cells of an action's row, cellGap apart.
	actionCells = 6
	// cellGap is the space between a row's cells.
	cellGap = "  "
	// minActionTitle is the narrowest an action's title gets.
	minActionTitle = 12
)

// action is one action not yet ended, as the Actions section lists it.
type action struct {
	core.ActionView

	issue core.IssueView
}

// actions returns the actions not yet ended, in the order of their issues
// and of each issue's actions.
func (m Model) actions() []action {
	var out []action
	for _, iv := range m.snap.Issues {
		for _, a := range iv.Actions {
			if a.Phase != core.PhaseEnded {
				out = append(out, action{issue: iv, ActionView: a})
			}
		}
	}
	return out
}

// actionCounts returns how many actions run, then how many wait for their
// issue's take.
func (m Model) actionCounts() (int, int) {
	waiting := 0
	acts := m.actions()
	for _, a := range acts {
		if a.Phase == core.PhaseWaiting {
			waiting++
		}
	}
	return len(acts) - waiting, waiting
}

// actionsSection is the Actions section: its summary and its rows, each
// action with its icon, reference and title, rule and action, queue,
// state and branch, and under each running one the last thing its session
// said when said is set (R17, R18).
func (m Model) actionsSection(said bool) (string, []string) {
	running, waiting := m.actionCounts()
	summary := fmt.Sprintf("%d running", running)
	if waiting > 0 {
		summary += fmt.Sprintf(" · %d waiting", waiting)
	}
	acts := m.actions()
	if len(acts) == 0 {
		return summary, []string{" " + m.styles.muted.Render("none")}
	}
	s := m.styles
	var refs, titles, names, queues, states []string
	for _, a := range acts {
		refs = append(refs, a.issue.Issue.Ref)
		titles = append(titles, clean(a.issue.Issue.Title))
		names = append(names, a.issue.Rule+"/"+a.Name)
		queues = append(queues, a.issue.Queue)
		states = append(states, m.actionState(a.ActionView))
	}
	refW, nameW, queueW, stateW := widest(refs), widest(names), widest(queues)+chipPadding, widest(states)
	// The title takes what the other columns and their gaps leave.
	others := lipgloss.Width(" ○ ") + refW + nameW + queueW + stateW + widest(branches(acts)) +
		len(cellGap)*(actionCells-1)
	titleW := min(max(m.width-others, minActionTitle), widest(titles))
	var out []string
	for i, a := range acts {
		cells := []string{
			pad(s.link(refs[i], a.issue.Issue.URL), refW), pad(s.text.Render(titles[i]), titleW),
			pad(s.muted.Render(names[i]), nameW), pad(s.chip.Render(queues[i]), queueW),
			pad(s.text.Render(states[i]), stateW), s.muted.Render(clean(a.Branch)),
		}
		out = append(out, " "+m.actionIcon(a.Phase)+" "+strings.Join(cells, cellGap))
		if text := m.said(a); said && text != "" {
			out = append(out, "   "+s.muted.Render("└ "+text))
		}
	}
	return summary, out
}

// branches returns each action's branch.
func branches(acts []action) []string {
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.Branch
	}
	return out
}

// actionIcon is the spinner for an action crew works on, or a waiting icon
// for one whose issue's take is not done (R5, KTD13).
func (m Model) actionIcon(p core.Phase) string {
	if p == core.PhaseWaiting {
		return m.styles.warning.Render("○")
	}
	return m.spin()
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

// said returns what a's session last said, cleaned, while it runs (R18).
func (m Model) said(a action) string {
	if a.Phase != core.PhaseRunning {
		return ""
	}
	for _, x := range m.snap.Said {
		if x.IssueKey == a.issue.Issue.Key && x.Action == a.Name {
			return clean(x.Text)
		}
	}
	return ""
}
