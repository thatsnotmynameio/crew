package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	// focusMark opens a focused section's title.
	focusMark = "▸ "
	// ruleGaps are the cells a rule keeps between its title and its
	// summary: a space, at least one dash and a space.
	ruleGaps = 3
	// minRun is the shortest run of ╱ the header keeps before it drops
	// details.
	minRun = 3
	// minGap is the least space before the header's last item.
	minGap = 2
)

// header is the first line (R3): crew's name and a gradient run of ╱, then
// the repository, the time up, the time left, the spend and tokens, and the
// key for help, or the state crew is stopping in (KTD16). On a narrow window
// the details drop from the right.
func (m Model) header() string {
	s := m.styles
	items := []string{s.muted.Render(m.cfg.Repository)}
	if started := m.snap.Started; !started.IsZero() {
		items = append(items, s.muted.Render("up "+short(m.at.Sub(started))))
		if limit := m.snap.RunTimeLimit; limit > 0 && !m.snap.TimeUp {
			items = append(items, s.warning.Render(short(started.Add(limit).Sub(m.at))+" left"))
		}
	}
	for _, part := range spendParts(m.snap.Spent) {
		items = append(items, s.text.Render(part))
	}
	right := s.muted.Render("? help")
	switch {
	case m.stopping || m.snap.Stopping:
		right = s.warningPill.Render("STOPPING")
	case m.snap.TimeUp:
		right = s.warningPill.Render("WINDING DOWN")
	}
	name := s.title.Render("crew")
	for k := len(items); k >= 0; k-- {
		details := strings.Join(items[:k], s.muted.Render(" • "))
		used := lipgloss.Width(name+"  "+details) + minGap + lipgloss.Width(right)
		if run := m.width - used; run >= minRun || k == 0 {
			run = max(run, minRun)
			line := name + " " + s.gradient("╱", run) + " " + details
			gap := max(m.width-lipgloss.Width(line)-lipgloss.Width(right), minGap)
			return line + strings.Repeat(" ", gap) + right
		}
	}
	return name
}

// rule opens a section (R4): its title, a rule to the edge of width, and a
// muted summary. A focused section's title has a ▸ before it, so focus
// reads without colour (KTD11).
func (m Model) rule(title, summary string, width int, focused bool) string {
	s := m.styles
	head := s.title.Render(title)
	if focused {
		head = s.accent.Render(focusMark) + head
	}
	tail := ""
	if summary != "" {
		tail = " " + s.muted.Render(summary)
	}
	dashes := width - lipgloss.Width(head) - 1 - lipgloss.Width(tail)
	if dashes < 1 {
		tail = ""
		dashes = width - lipgloss.Width(head) - 1
	}
	return head + " " + s.subtle.Render(strings.Repeat("─", max(dashes, 1))) + tail
}
