package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// The cards' frame and the board cards' rows, in cells (KTD1, KTD2).
const (
	// cardFrame is the cells a card's border and padding take, cardCorners
	// those of its corners on a border row.
	cardFrame   = 4
	cardCorners = 2
	// cardLabel is the cells a board card row's label takes, before the
	// space that parts it from its value (R5).
	cardLabel = 4
)

// framed draws rows in a rounded border width cells wide, in edge, each
// row padded or cut to fit inside it (KTD1).
func framed(rows []string, width int, edge lipgloss.Style) []string {
	b := lipgloss.RoundedBorder()
	inner := width - cardFrame
	line := max(width-cardCorners, 0)
	out := make([]string, 0, len(rows)+cardCorners)
	out = append(out, edge.Render(b.TopLeft+strings.Repeat(b.Top, line)+b.TopRight))
	for _, r := range rows {
		out = append(out, edge.Render(b.Left)+" "+pad(r, inner)+" "+edge.Render(b.Right))
	}
	return append(out, edge.Render(b.BottomLeft+strings.Repeat(b.Bottom, line)+b.BottomRight))
}

// cardFace is c's card, width cells wide, in the highlight's border with
// ▸ before its reference while lit (KTD2, KTD5 of #151).
func (m Model) cardFace(c card, width int, lit bool) []string {
	rows, edge := m.liveCard(c, width)
	if lit {
		rows[0] = m.styles.highlight.Render(focusMark) + rows[0]
		edge = m.styles.highlight
	}
	return framed(rows, width, edge)
}

// liveCard is the rows of c's card, width cells wide: its reference and
// title, then its run, bots and via rows, and its border, strong while
// crew runs its issue (R1 to R6, KTD1, KTD2).
func (m Model) liveCard(c card, width int) ([]string, lipgloss.Style) {
	s := m.styles
	value := width - cardFrame - cardLabel - 1
	rows := []string{
		s.link(c.issue.Ref, c.issue.URL) + " " + s.text.Render(clean(c.issue.Title)),
		m.labelled("run", s.items(m.runItems(c), s.muted.Render(" · "), value)),
		m.labelled("bots", m.cardBots(c, value)),
		m.labelled("via", m.cardQueue(c)),
	}
	edge := s.subtle
	if c.held && c.view.Claim == core.ClaimRunning {
		edge = s.strongAccent
	}
	return rows, edge
}

// labelled is value after its row's label, muted and padded so the values
// of a card line up (R5).
func (m Model) labelled(label, value string) string {
	return pad(m.styles.muted.Render(label), cardLabel) + " " + value
}

// runItems are c's actions not yet ended, in their rule's order, after its
// claim when it is owed or stopping, or its claim alone when none is left
// (R2, KTD2).
func (m Model) runItems(c card) []string {
	var items []string
	for _, a := range c.view.Actions {
		if a.Phase != core.PhaseEnded {
			items = append(items, m.runItem(a))
		}
	}
	claim := c.view.Claim
	if len(items) == 0 || c.held && (claim == core.ClaimOwed || claim == core.ClaimStopping) {
		items = append([]string{m.claimState(c)}, items...)
	}
	return items
}

// runItem is a's item on its card: ○, its name and waiting while its
// issue's take is not done, else the spinner, its name and how long it has
// run, or its phase before its session starts (R2, KTD2).
func (m Model) runItem(a core.ActionView) string {
	s := m.styles
	name := s.text.Render(clean(string(a.Name)))
	switch {
	case a.Phase == core.PhaseWaiting:
		return s.warning.Render("○") + " " + name + " " + s.muted.Render(a.Phase.String())
	case a.Started.IsZero():
		return m.spin() + " " + name + " " + s.muted.Render(a.Phase.String())
	}
	return m.spin() + " " + name + " " + s.muted.Render(short(m.at.Sub(a.Started)))
}

// claimState is c's claim through its icon (R11, KTD13); when crew does
// not hold its issue, ⊘ blocked if an open issue blocks it (R1 of #229),
// else ○ idle (#126).
func (m Model) claimState(c card) string {
	s := m.styles
	claim := c.view.Claim
	switch {
	case !c.held && c.issue.Blocked:
		return s.warning.Render("⊘ blocked")
	case !c.held:
		return s.muted.Render("○ idle")
	case claim == core.ClaimRunning || claim == core.ClaimJudging:
		return m.spin() + " " + s.muted.Render(claim.String())
	case claim == core.ClaimStopping:
		return s.muted.Render("■ stopping")
	case claim == core.ClaimOwed:
		return s.warning.Render("! owed")
	}
	return s.warning.Render("◌ " + claim.String())
}

// cardBots are the bots c's running actions act as, in the order of Bots,
// each once, in width cells, or none (R3, KTD2).
func (m Model) cardBots(c card, width int) string {
	s := m.styles
	var items []string
	for _, e := range m.snap.Bots {
		if slices.ContainsFunc(e.Running, func(r core.RunningAction) bool { return r.IssueRef == c.issue.Ref }) {
			items = append(items, s.botName(e))
		}
	}
	if len(items) == 0 {
		return s.subtle.Render("none")
	}
	return s.items(items, " ", width)
}

// cardQueue is the queue of c's issue while crew holds it, or none (R4,
// KTD2).
func (m Model) cardQueue(c card) string {
	if !c.held {
		return m.styles.subtle.Render("none")
	}
	return m.styles.text.Render(clean(string(c.view.Queue)))
}
