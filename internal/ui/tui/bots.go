package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// entryRows are the rows a Bots entry takes with its details.
const entryRows = 2

// botsSection is the Bots section: its summary and, per entry, a row with
// its name, short state and totals, then, with details, a row with what
// acts as it and what runs as it now (R1 to R8, KTD6, KTD7, KTD11).
func (m Model) botsSection(details bool) (string, []string) {
	entries := m.snap.Bots
	summary := botsSummary(entries)
	if len(entries) == 0 {
		return summary, []string{" " + m.styles.muted.Render("none")}
	}
	names, states := make([]string, 0, len(entries)), make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, clean(e.Name))
		states = append(states, m.botState(e))
	}
	nameW, stateW := widest(names), widest(states)
	out := make([]string, 0, entryRows*len(entries))
	for i, e := range entries {
		first := m.styles.text.Render(names[i])
		switch totals := m.botTotals(e.Spend); {
		case totals != "" && stateW == 0:
			first = pad(first, nameW) + cellGap + totals
		case totals != "":
			first = pad(first, nameW) + cellGap + pad(states[i], stateW) + cellGap + totals
		case states[i] != "":
			first = pad(first, nameW) + cellGap + states[i]
		}
		out = append(out, fit(" "+first, m.width))
		if details {
			out = append(out, fit("   "+m.botDetails(e, m.width-len("   ")), m.width))
		}
	}
	return summary, out
}

// botsSummary counts the configured bots that act and those that do not,
// or says there are none (KTD6).
func botsSummary(entries []core.BotView) string {
	acting, unable := 0, 0
	for _, e := range entries {
		switch {
		case e.You:
		case e.Acting:
			acting++
		default:
			unable++
		}
	}
	var parts []string
	if acting > 0 {
		parts = append(parts, fmt.Sprintf("%d acting", acting))
	}
	if unable > 0 {
		parts = append(parts, fmt.Sprintf("%d cannot act", unable))
	}
	if len(parts) == 0 {
		return "no bots"
	}
	return strings.Join(parts, " · ")
}

// botState is e's short state, calm while it acts and a warning otherwise,
// or your gh login on the "you" entry (R4).
func (m Model) botState(e core.BotView) string {
	switch {
	case e.You && e.Login == "":
		return ""
	case e.You:
		return m.styles.muted.Render(clean(e.Login))
	case e.Acting:
		return m.styles.muted.Render(clean(e.State))
	}
	return m.styles.warning.Render(clean(e.State))
}

// botTotals is how many actions ended as an entry and their cost and
// tokens, in the header's words; nothing before the first one ends (R7).
func (m Model) botTotals(s crew.Spend) string {
	if s.Sessions == 0 {
		return ""
	}
	count := "1 action"
	if s.Sessions != 1 {
		count = fmt.Sprintf("%d actions", s.Sessions)
	}
	parts := []string{m.styles.text.Render(count)}
	for _, part := range spendParts(s) {
		parts = append(parts, m.styles.text.Render(part))
	}
	return strings.Join(parts, m.styles.muted.Render(" · "))
}

// botDetails is e's second row, in width cells: crew's writes when they go
// as it, the rule/action pairs that act as it, after "→ you:" on a bot
// that cannot act, then the actions running as it now, or "none". A row too
// wide cuts its pairs first, so the running actions stay whole (R5, KTD7).
func (m Model) botDetails(e core.BotView, width int) string {
	s := m.styles
	var parts []string
	if e.Writes {
		parts = append(parts, s.text.Render("crew's writes"))
	}
	for i, pair := range e.Pairs {
		p := s.text.Render(clean(pair))
		if i == 0 && e.ActsAsYou {
			p = s.warning.Render("→ you: ") + p
		}
		parts = append(parts, p)
	}
	line := strings.Join(parts, s.muted.Render(" · "))
	running := m.botRunning(e.Running)
	switch {
	case running == "" && line == "":
		return s.muted.Render("none")
	case running == "":
		return fit(line, width)
	case line == "":
		return fit(running, width)
	}
	return fit(line, max(width-lipgloss.Width(cellGap+running), 1)) + cellGap + running
}

// botRunning is the actions running as an entry, after "▸ ", or "" when
// none runs.
func (m Model) botRunning(actions []core.RunningAction) string {
	if len(actions) == 0 {
		return ""
	}
	s := m.styles
	running := make([]string, 0, len(actions))
	for _, r := range actions {
		running = append(running, s.link(r.IssueRef, m.issueURL(r.IssueRef))+" "+
			s.text.Render(clean(r.Rule+"/"+r.Action)))
	}
	return s.accent.Render("▸ ") + strings.Join(running, s.muted.Render(" · "))
}

// issueURL is the URL of the held issue whose reference is ref, or "".
func (m Model) issueURL(ref string) string {
	for _, iv := range m.snap.Issues {
		if iv.Issue.Ref == ref {
			return iv.Issue.URL
		}
	}
	return ""
}
