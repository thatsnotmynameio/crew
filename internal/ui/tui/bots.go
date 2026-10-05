package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The Bots cards' sizes, in cells (KTD1, KTD2).
const (
	minCard = 24
	maxCard = 34
	cardGap = 1
	// botCardRows are the rows a card takes: its border and five rows.
	botCardRows = 7
	// botsTitle is the section's title.
	botsTitle = "Bots"
)

// cannotAct opens the state of a bot that cannot act at startup; the card
// shows only the reason after it (KTD6).
const cannotAct = "cannot act: "

// cellGap is the space between the strip's entries.
const cellGap = "  "

// botsLayout is which Bots cards show and how wide (KTD2, KTD3).
type botsLayout struct {
	// offset is the first card shown.
	offset int
	// shown is how many cards show.
	shown int
	// before and after count the cards hidden on each side.
	before, after int
	// width is each shown card's width.
	width int
}

// layBots lays out n cards in avail cells: every one when they fit at the
// minimum width, else as many as fit from offset, clamped (KTD2, KTD3).
func layBots(n, avail, offset int) botsLayout {
	fits := func(k int) bool { return k*minCard+(k-1)*cardGap <= avail }
	l := botsLayout{shown: n}
	if !fits(n) {
		l.shown = min(max((avail+cardGap)/(minCard+cardGap), 1), n)
		l.offset = min(max(offset, 0), n-l.shown)
		l.before, l.after = l.offset, n-l.offset-l.shown
	}
	if l.shown > 0 {
		l.width = max(min(maxCard, (avail-(l.shown-1)*cardGap)/l.shown), cardFrame)
	}
	return l
}

// botsLayout lays out the Bots cards for the current snapshot and window.
func (m Model) botsLayout() botsLayout {
	return layBots(len(m.snap.Bots), m.width-1, m.botsOffset)
}

// botsSection is the Bots section: its summary, then a row of cards, one
// per entry, or, without cards, a one-row strip (R1, R7, R10, KTD10).
func (m Model) botsSection(cards bool) (string, []string) {
	entries := m.snap.Bots
	if len(entries) == 0 {
		none := []string{" " + m.styles.muted.Render("none")}
		if cards {
			none = filled(none, botCardRows)
		}
		return "", none
	}
	summary := botsSummary(entries)
	if !cards {
		return summary, []string{" " + m.botsStrip(entries)}
	}
	l := m.botsLayout()
	summary = m.withMarkers(summary, l)
	rows := make([]string, botCardRows)
	for k, e := range entries[l.offset : l.offset+l.shown] {
		for i, line := range m.botCard(e, l.width) {
			if k > 0 {
				rows[i] += strings.Repeat(" ", cardGap)
			}
			rows[i] += line
		}
	}
	for i := range rows {
		rows[i] = " " + rows[i]
	}
	return summary, rows
}

// withMarkers is summary followed by how many cards l hides on each side,
// or the markers alone when both do not fit the rule beside its title, so
// the hidden cards stay announced (R8, KTD3).
func (m Model) withMarkers(summary string, l botsLayout) string {
	var markers []string
	if l.before > 0 {
		markers = append(markers, fmt.Sprintf("◂ %d", l.before))
	}
	if l.after > 0 {
		markers = append(markers, fmt.Sprintf("%d ▸", l.after))
	}
	if len(markers) == 0 {
		return summary
	}
	head := lipgloss.Width(botsTitle)
	if m.focus == focusBots {
		head += lipgloss.Width(focusMark)
	}
	full := strings.Join(append([]string{summary}, markers...), " · ")
	// The rule keeps its summary while a space, a dash and a space fit
	// between the title and it.
	if lipgloss.Width(full) <= m.width-head-ruleGaps {
		return full
	}
	return strings.Join(markers, " · ")
}

// botsSummary counts the configured bots that act and those that do not,
// or says only you act when the config names none (R7).
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
		return "only you"
	}
	return strings.Join(parts, " · ")
}

// botCard is e's card, width cells wide: the avatar beside the name, state
// and totals, then what acts as e, then what runs as it now, in a rounded
// border, strong while something runs as e (R1 to R6, KTD1).
func (m Model) botCard(e core.BotView, width int) []string {
	s := m.styles
	inner := width - cardFrame
	text := max(inner-avatarWidth-1, 0)
	av := s.avatar(avatarSeed(e), s.avatarColour(e))
	content := []string{
		av[0] + " " + fit(s.title.Render(clean(e.Name)), text),
		av[1] + " " + fit(m.botState(e), text),
		av[2] + " " + m.botTotals(e.Spend, text),
		m.botMapping(e, inner),
		m.botRunning(e.Running, inner),
	}
	edge := s.subtle
	if len(e.Running) > 0 {
		edge = s.strongAccent
	}
	return framed(content, width, edge)
}

// botState is e's state: ● acting in the success colour, or ▲ and its
// short state as a warning, or your login, muted, on the "you" entry (R3,
// KTD6).
func (m Model) botState(e core.BotView) string {
	s := m.styles
	switch {
	case e.You && e.Login == "":
		return ""
	case e.You:
		return s.muted.Render("@" + clean(e.Login))
	case e.Acting:
		return s.success.Render("● " + clean(e.State))
	}
	return s.warning.Render("▲ " + strings.TrimPrefix(clean(e.State), cannotAct))
}

// botTotals is the longest form of an entry's totals that fits width, or
// "no actions yet" before the first one ends (R3, KTD7).
func (m Model) botTotals(sp crew.Spend, width int) string {
	s := m.styles
	parts := spendParts(sp)
	if sp.Sessions == 0 || len(parts) == 0 {
		return s.subtle.Render(fit("no actions yet", width))
	}
	count := "1 action"
	if sp.Sessions != 1 {
		count = fmt.Sprintf("%d actions", sp.Sessions)
	}
	forms := [][]string{append([]string{count}, parts...), {count, parts[0]}, parts, parts[:1]}
	line := ""
	for _, form := range forms {
		styled := make([]string, len(form))
		for i, part := range form {
			styled[i] = s.text.Render(part)
		}
		if line = strings.Join(styled, s.muted.Render(" · ")); lipgloss.Width(line) <= width {
			return line
		}
	}
	return fit(line, width)
}

// botMapping is what acts as e, in width cells: crew's writes when they go
// as it, then its rule/action pairs, after "→ you" on a bot that cannot
// act (R4).
func (m Model) botMapping(e core.BotView, width int) string {
	s := m.styles
	var items []string
	if e.Writes {
		items = append(items, s.accent.Render("crew's writes"))
	}
	for _, pair := range e.Pairs {
		items = append(items, s.text.Render(clean(pair)))
	}
	lead := ""
	if e.ActsAsYou {
		lead = s.warning.Render("→ you ")
	}
	return lead + s.items(items, s.muted.Render(" · "), width-lipgloss.Width(lead))
}

// botRunning is the actions running as an entry, in width cells, or idle
// (R5).
func (m Model) botRunning(actions []core.RunningAction, width int) string {
	s := m.styles
	if len(actions) == 0 {
		return s.subtle.Render("idle")
	}
	items := make([]string, 0, len(actions))
	for _, r := range actions {
		items = append(items, m.spin()+" "+s.link(r.IssueRef, m.issueURL(r.IssueRef))+" "+
			s.text.Render(clean(r.Rule+"/"+r.Action)))
	}
	return s.items(items, s.muted.Render(" · "), width)
}

// botsStrip is the section in one row: each entry's mark in its avatar's
// colour, its name and its glyph, as many whole entries as fit (R10,
// KTD9).
func (m Model) botsStrip(entries []core.BotView) string {
	s := m.styles
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		items = append(items, s.botName(e)+m.stripGlyph(e))
	}
	return s.items(items, cellGap, m.width-1)
}

// mark is e's mark in its avatar's colour, as the strip and the board
// cards draw it (R3 of #151, KTD9).
func (s styles) mark(e core.BotView) string {
	return lipgloss.NewStyle().Foreground(s.avatarColour(e)).Render("■")
}

// botName is e's mark, then its name, as the strip, the board cards and
// the popup show a bot (R3, KTD8 of #151).
func (s styles) botName(e core.BotView) string {
	return s.mark(e) + " " + s.text.Render(clean(e.Name))
}

// stripGlyph is e's glyph in the strip, after a space: ▲ while a bot cannot
// act, then the spinner and a count while actions run as e, or ● for an
// acting bot with nothing running (KTD9).
func (m Model) stripGlyph(e core.BotView) string {
	s := m.styles
	var glyphs []string
	if !e.You && !e.Acting {
		glyphs = append(glyphs, s.warning.Render("▲"))
	}
	switch {
	case len(e.Running) > 0:
		glyphs = append(glyphs, m.spin()+" "+s.text.Render(strconv.Itoa(len(e.Running))))
	case !e.You && e.Acting:
		glyphs = append(glyphs, s.success.Render("●"))
	}
	if len(glyphs) == 0 {
		return ""
	}
	return " " + strings.Join(glyphs, " ")
}

// items joins the leading items that fit width, with sep, and a muted "+N"
// for those left out; when not even one fits beside its "+N", it cuts the
// first (KTD8).
func (s styles) items(items []string, sep string, width int) string {
	more := func(n int) string { return " " + s.muted.Render(fmt.Sprintf("+%d", n)) }
	for keep := len(items); keep > 0; keep-- {
		line := strings.Join(items[:keep], sep)
		if keep < len(items) {
			line += more(len(items) - keep)
		}
		if lipgloss.Width(line) <= width {
			return line
		}
	}
	switch len(items) {
	case 0:
		return ""
	case 1:
		return fit(items[0], width)
	}
	rest := more(len(items) - 1)
	return fit(items[0], width-lipgloss.Width(rest)) + rest
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
