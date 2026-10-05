package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// The board's column sizes, in cells (KTD9).
const (
	minColumn = 18
	maxColumn = 30
	columnGap = 2
	// edgeMarker is the width kept at an edge for "◂ N" or "N ▸".
	edgeMarker = 4
)

// card is an issue on the board. On the board of the rules (KTD3), it is
// held by a shown rule, or waits in its rule's column for the next rule
// to take it. On a configured board, it is in a column whose labels the
// issue carries, held or not (KTD9).
type card struct {
	issue crew.Issue
	// column is the card's column: an index into the rules, or into the
	// configured board.
	column int
	// held is set while crew holds the issue, with claim its claim; the
	// zero claim is ClaimTaking, so claim alone cannot tell.
	held  bool
	claim core.Claim
	// waiting is set for a card whose rule ended; label is the state the
	// rule moved its issue to.
	waiting bool
	label   crew.State
}

// columnIndex returns the index of the rule named name in the rules, or
// -1.
func (m Model) columnIndex(name string) int {
	return slices.IndexFunc(m.cfg.Rules, func(s crew.Rule) bool { return s.Name == name })
}

// shown reports whether column i is on the board: a rule not hidden from
// it (R12), or any column of a configured board (R5).
func (m Model) shown(i int) bool {
	if m.configured() {
		return i >= 0 && i < len(m.cfg.Board)
	}
	return i >= 0 && !m.cfg.Rules[i].OffBoard
}

// cards returns the cards of the snapshot: a configured board's (KTD9), or
// the rules' (KTD3), each held issue of a shown rule, in the order taken,
// then each waiting card, by when its rule ended.
func (m Model) cards() []card {
	if m.configured() {
		return m.configuredCards()
	}
	var out []card
	for _, iv := range m.snap.Issues {
		if i := m.columnIndex(iv.Rule); m.shown(i) {
			out = append(out, card{issue: iv.Issue, column: i, held: true, claim: iv.Claim})
		}
	}
	var waiting []core.HandledView
	for _, e := range m.snap.Handled {
		if m.waits(e) {
			waiting = append(waiting, e)
		}
	}
	slices.SortStableFunc(waiting, func(a, b core.HandledView) int { return a.Ended.Compare(b.Ended) })
	for _, e := range waiting {
		out = append(out, card{issue: e.Issue, column: m.columnIndex(e.Rule), waiting: true, label: e.To})
	}
	return out
}

// waits reports whether e's issue waits on the board for the next rule:
// no rule holds it again, its move is done, the issue is still where the
// move put it, its rule is shown, and a rule of its kind takes that state
// (R10, KTD3, KTD4, #109).
func (m Model) waits(e core.HandledView) bool {
	if e.HeldBy != "" || e.Move != crew.MoveDone || e.Gone || !m.shown(m.columnIndex(e.Rule)) {
		return false
	}
	return slices.ContainsFunc(m.cfg.Rules, func(s crew.Rule) bool {
		return s.Label == e.To && s.Takes == e.Issue.Kind
	})
}

// boardLayout is which columns the board draws and how wide (KTD9).
type boardLayout struct {
	// columns are the indexes of the columns drawn, left to right.
	columns []int
	width   int
	// before and after count the columns scrolled off each side; dropped
	// counts the empty columns left out.
	before, after, dropped int
	// offset is the first scrollable column drawn.
	offset int
}

// layout lays out the shown columns in avail cells: every one when they
// fit, else the ones holding cards, else as many of those as fit from
// offset (KTD9).
func layout(shown []int, held map[int]bool, avail, offset int) boardLayout {
	fits := func(n int) bool { return n*minColumn+(n-1)*columnGap <= avail }
	l := boardLayout{columns: shown}
	if !fits(len(shown)) {
		l.columns = nil
		for _, c := range shown {
			if held[c] {
				l.columns = append(l.columns, c)
			}
		}
		l.dropped = len(shown) - len(l.columns)
	}
	room := avail
	if n := len(l.columns); n > 0 && !fits(n) {
		room = avail - edgeMarker - edgeMarker
		k := max((room+columnGap)/(minColumn+columnGap), 1)
		l.offset = min(max(offset, 0), n-k)
		l.before, l.after = l.offset, n-l.offset-k
		l.columns = l.columns[l.offset : l.offset+k]
	}
	if n := len(l.columns); n > 0 {
		l.width = max(min(maxColumn, (room-(n-1)*columnGap)/n), 1)
	}
	return l
}

// boardLayout lays out the board for the current snapshot and window.
func (m Model) boardLayout(cards []card) boardLayout {
	var shown []int
	for i := range m.columnCount() {
		if m.shown(i) {
			shown = append(shown, i)
		}
	}
	held := map[int]bool{}
	for _, c := range cards {
		held[c.column] = true
	}
	return layout(shown, held, m.width-1, m.boardOffset)
}

// board is the Workflow section: its summary and its rows, with at most
// limit cards a column; limit < 0 means no limit (KTD8).
func (m Model) board(limit int) (string, []string) {
	if m.configured() {
		cards := m.cards()
		l := m.boardLayout(cards)
		return m.configuredSummary(cards, l), m.boardRows(l, cards, limit)
	}
	if !slices.ContainsFunc(m.cfg.Rules, func(s crew.Rule) bool { return !s.OffBoard }) {
		return "", []string{" " + m.styles.muted.Render("every stage is hidden")}
	}
	cards := m.cards()
	l := m.boardLayout(cards)
	held := 0
	for _, c := range cards {
		if !c.waiting {
			held++
		}
	}
	summary := fmt.Sprintf("%d in play", held)
	if waiting := len(cards) - held; waiting > 0 {
		summary += fmt.Sprintf(" · %d waiting", waiting)
	}
	if l.dropped > 0 {
		summary += fmt.Sprintf(" · %d empty %s not shown", l.dropped, lines.Plural(l.dropped, "stage", "stages"))
	}
	return summary, m.boardRows(l, cards, limit)
}

// boardRows draws the column names, their underlines with the slides, then
// the cards, two rows each, then "+N more" where a column has more than
// limit.
func (m Model) boardRows(l boardLayout, cards []card, limit int) []string {
	byColumn := make([][]card, len(l.columns))
	for _, c := range cards {
		if i := slices.Index(l.columns, c.column); i >= 0 {
			byColumn[i] = append(byColumn[i], c)
		}
	}
	tallest := 0
	for _, cs := range byColumn {
		tallest = max(tallest, len(cs))
	}
	shownCards := tallest
	if limit >= 0 {
		shownCards = min(tallest, limit)
	}
	out := []string{m.boardRow(l, m.columnNames(l, byColumn), true), m.underline(l)}
	for k := range shownCards {
		out = append(out, m.cardRows(l, byColumn, k)...)
	}
	if shownCards < tallest {
		more := make([]string, len(l.columns))
		for i, cs := range byColumn {
			if n := len(cs) - shownCards; n > 0 {
				more[i] = m.styles.muted.Render(fmt.Sprintf("+%d more", n))
			}
		}
		out = append(out, m.boardRow(l, more, false))
	}
	return out
}

// cardRows are the two rows of each column's card k, blank where a column
// has fewer cards.
func (m Model) cardRows(l boardLayout, byColumn [][]card, k int) []string {
	top, bottom := make([]string, len(l.columns)), make([]string, len(l.columns))
	for i, cs := range byColumn {
		if k < len(cs) {
			top[i], bottom[i] = m.cardLines(cs[k], l.width)
		}
	}
	return []string{m.boardRow(l, top, false), m.boardRow(l, bottom, false)}
}

// columnNames are the drawn columns' names, in the accent colour for those
// holding cards (R27).
func (m Model) columnNames(l boardLayout, byColumn [][]card) []string {
	names := make([]string, len(l.columns))
	for i, c := range l.columns {
		st := m.styles.muted
		if len(byColumn[i]) > 0 {
			st = m.styles.accent
		}
		names[i] = st.Render(m.columnName(c))
	}
	return names
}

// boardRow joins one cell per column, padded to the column width, with the
// scroll markers at the edges on the names row.
func (m Model) boardRow(l boardLayout, cells []string, names bool) string {
	var b strings.Builder
	b.WriteString(" ")
	if l.before > 0 || l.after > 0 {
		marker := ""
		if names && l.before > 0 {
			marker = m.styles.muted.Render(fmt.Sprintf("◂ %d", l.before))
		}
		b.WriteString(pad(marker, edgeMarker))
	}
	for i, c := range cells {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", columnGap))
		}
		b.WriteString(pad(c, l.width))
	}
	if names && l.after > 0 {
		b.WriteString(m.styles.muted.Render(fmt.Sprintf(" %d ▸", l.after)))
	}
	return b.String()
}

// cardLines are a card's two rows: its reference and title, then its claim
// or the label it waits in (R11, KTD13), or ○ idle when crew does not hold
// its issue (#126).
func (m Model) cardLines(c card, width int) (string, string) {
	s := m.styles
	bar := s.subtle.Render("▌")
	if c.held && c.claim == core.ClaimRunning {
		bar = s.strongAccent.Render("▌")
	}
	top := fit(bar+" "+s.link(c.issue.Ref, c.issue.URL)+" "+s.text.Render(clean(c.issue.Title)), width)
	var state string
	switch {
	case c.waiting:
		state = s.warning.Render("→ " + fitLeft(string(c.label), width-lipgloss.Width("▌ → ")))
	case !c.held:
		state = s.muted.Render("○ idle")
	case c.claim == core.ClaimRunning || c.claim == core.ClaimJudging:
		state = m.spin() + " " + s.muted.Render(c.claim.String())
	case c.claim == core.ClaimStopping:
		state = s.muted.Render("■ stopping")
	case c.claim == core.ClaimOwed:
		state = s.warning.Render("! owed")
	default:
		state = s.warning.Render("◌ " + c.claim.String())
	}
	return top, fit(bar+" "+state, width)
}

// spin is the current frame of the shared spinner, in the success colour.
func (m Model) spin() string {
	sp := m.spinner
	sp.Style = m.styles.success
	return sp.View()
}
