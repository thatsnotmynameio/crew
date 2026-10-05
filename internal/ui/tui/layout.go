package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// scrollRows is the rows Events takes under its title, whatever its
	// count, so the view holds still as it fills (#108).
	scrollRows = 5
	// minScroll is the rows Events keeps when the window is short (KTD8).
	minScroll = 2
	// cutRows are the rows a cut view ends with: the line saying how many
	// were cut, and the key-help line.
	cutRows = 2
	// cardRows are the rows a card takes on the board: its border and four
	// rows (KTD1 of #151).
	cardRows = 6
	// maxCards is the most cards a board column shows, however tall the
	// window; the rest go into its "+N more" row.
	maxCards = 5
)

// budget is how much of each section the view draws: the rows of Events,
// the cards a column, and whether Bots draws its cards or its one-row
// strip (KTD8; KTD10 of #151).
type budget struct {
	events, cards int
	botCards      bool
}

// View renders the dashboard (R1): the header, the warnings, Bots, Board,
// Queues beside Events and the key-help line, fitted to the window, with
// the keys over it while help shows. It also sets the window title, the
// tab progress and focus reports (R23, R24, KTD6).
func (m Model) View() tea.View {
	out := m.fitted()
	for i, l := range out {
		out[i] = fit(l, m.width)
	}
	content := strings.Join(out, "\n")
	if m.help {
		content = m.helpOverlay(content)
	}
	v := tea.NewView(content)
	v.WindowTitle = m.windowTitle()
	v.ProgressBar = m.progress()
	v.ReportFocus = true
	return v
}

// fitted returns the view's rows, at most the window's height of them when
// the height is known, giving rows up in KTD10's order (#151): Events, the
// cards past a column's limit, the Bots cards (R10), then the rows above
// the key-help line.
func (m Model) fitted() []string {
	b := m.budget()
	all := m.rows(b)
	if m.height <= 0 || len(all) <= m.height {
		return all
	}
	if m.height < cutRows {
		return all[len(all)-m.height:]
	}
	keep := m.height - cutRows
	return append(all[:keep:keep], m.styles.muted.Render(fmt.Sprintf("… %d lines cut", len(all)-keep-1)), all[len(all)-1])
}

// budget returns the largest budget whose rows fit the window (KTD8; KTD10
// of #151).
func (m Model) budget() budget {
	b := budget{events: scrollRows, cards: maxCards, botCards: true}
	if m.height <= 0 {
		return b
	}
	over := func() int { return len(m.rows(b)) - m.height }
	if o := over(); o > 0 {
		b.events = max(minScroll, scrollRows-o)
	}
	// Capping a column at c of the s cards it shows saves cardRows*(s-c)
	// rows and adds the "+N more" row, unless maxCards already did.
	if o, t := over(), m.tallestColumn(); o > 0 && t > 1 {
		shown, more := min(t, maxCards), 1
		if t > maxCards {
			more = 0
		}
		b.cards = max(shown-(o+more+cardRows-1)/cardRows, 1)
	}
	// Bots gives way last, its cards collapsing to a strip, just before the
	// cut (R10, KTD10).
	if over() > 0 {
		b.botCards = false
	}
	return b
}

// rows draws every section within b.
func (m Model) rows(b budget) []string {
	out := []string{m.header()}
	for _, w := range m.warnings() {
		out = append(out, m.styles.warning.Render("warning: ")+m.styles.text.Render(clean(w)))
	}
	summary, bots := m.botsSection(b.botCards)
	out = append(out, "", m.rule(botsTitle, summary, m.width, m.focus == focusBots))
	out = append(out, bots...)
	summary, board := m.board(b.cards)
	out = append(out, "", m.rule("Board", summary, m.width, m.focus == focusBoard))
	out = append(out, board...)
	out = append(out, "")
	out = append(out, m.band(b.events)...)
	return append(out, "", m.keyHelp())
}

// warnings are the startup warnings, then each bot's live warnings, in
// the order of the bots (R11, R12, KTD11).
func (m Model) warnings() []string {
	out := slices.Clone(m.cfg.Warnings)
	for _, e := range m.snap.Bots {
		out = append(out, e.Warnings...)
	}
	return out
}

// band draws Queues and Events side by side: Queues at its natural width,
// Events in the rest, with n Events rows (R22, KTD10 of #151; #108).
func (m Model) band(n int) []string {
	qSummary, queues := m.queuesSection()
	left := max(widest(queues), lipgloss.Width("Queues ─── "+qSummary))
	right := max(m.width-left-bandGap, 1)
	eSummary, events := m.eventsSection(n)
	lefts := append([]string{m.rule("Queues", qSummary, left, false)}, queues...)
	rights := append([]string{m.rule("Events", eSummary, right, m.focus == focusEvents)}, events...)
	out := make([]string, max(len(lefts), len(rights)))
	for i := range out {
		l, r := "", ""
		if i < len(lefts) {
			l = lefts[i]
		}
		if i < len(rights) {
			r = rights[i]
		}
		out[i] = pad(l, left) + strings.Repeat(" ", bandGap) + fit(r, right)
	}
	return out
}

// filled returns rows with blank rows after them up to n: a section keeps
// its height while it has fewer rows than that (#108).
func filled(rows []string, n int) []string {
	for len(rows) < n {
		rows = append(rows, "")
	}
	return rows
}

// tallestColumn is the most cards a drawn board column holds: a column
// dropped or scrolled off the board adds no rows.
func (m Model) tallestColumn() int {
	cards := m.cards()
	drawn := m.boardLayout(cards).columns
	n := map[int]int{}
	tallest := 0
	for _, c := range cards {
		if slices.Contains(drawn, c.column) {
			n[c.column]++
			tallest = max(tallest, n[c.column])
		}
	}
	return tallest
}
