package tui

import (
	"fmt"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// configured reports whether the config draws the board's columns, rather
// than the board of the workflow's stages (KTD9).
func (m Model) configured() bool { return len(m.cfg.Board) > 0 }

// columnCount is how many columns the board has, shown or not.
func (m Model) columnCount() int {
	if m.configured() {
		return len(m.cfg.Board)
	}
	return len(m.cfg.Workflow)
}

// columnName is the name of column c: its stage's, or its configured one.
func (m Model) columnName(c int) string {
	if m.configured() {
		return m.cfg.Board[c].Name
	}
	return m.cfg.Workflow[c].Name
}

// configuredCards returns a card in each configured column whose labels an
// issue of the board read carries, issue by issue in the read's order,
// oldest first (R7, KTD6). A card of an issue crew holds carries its claim
// (R10, KTD9); no card waits (R12).
func (m Model) configuredCards() []card {
	claims := map[string]core.Claim{}
	for _, iv := range m.snap.Issues {
		claims[iv.Issue.Key] = iv.Claim
	}
	var out []card
	for _, bi := range m.snap.Board {
		claim, held := claims[bi.Issue.Key]
		for i, c := range m.cfg.Board {
			if slices.ContainsFunc(c.Labels, func(l string) bool { return slices.Contains(bi.Labels, l) }) {
				out = append(out, card{issue: bi.Issue, column: i, held: held, claim: claim})
			}
		}
	}
	return out
}

// configuredSummary is the configured board's summary (KTD8): the issues
// with a card, the empty columns dropped, and, in the warning style, that
// the last board read failed (KTD5).
func (m Model) configuredSummary(cards []card, l boardLayout) string {
	issues := map[string]bool{}
	for _, c := range cards {
		issues[c.issue.Key] = true
	}
	summary := fmt.Sprintf("%d %s", len(issues), lines.Plural(len(issues), "issue", "issues"))
	if l.dropped > 0 {
		summary += fmt.Sprintf(" · %d empty %s not shown", l.dropped, lines.Plural(l.dropped, "column", "columns"))
	}
	if m.snap.BoardFailure != "" {
		summary += " · " + m.styles.warning.Render("board not read")
	}
	return summary
}
