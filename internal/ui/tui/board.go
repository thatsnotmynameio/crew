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

// The board's column sizes, in cells (KTD9; KTD1 of #151).
const (
	minColumn = 22
	maxColumn = 34
	columnGap = 2
	// edgeMarker is the width kept at an edge for "◂ N" or "N ▸".
	edgeMarker = 4
)

// card is an item on the board, in a column whose labels it carries and
// that shows its kind, held by crew or not (KTD9, KTD10), or an issue in
// the Not on board or Handled column (KTD3, KTD13 of #151).
type card struct {
	issue crew.Issue
	// column is the card's column: an index into the board's columns,
	// then Not on board's, then Handled's.
	column int
	// held is set while crew holds the item, with view its issue's view;
	// the zero view's claim is ClaimTaking, so view alone cannot tell.
	held bool
	view core.IssueView
	// entry is the Handled entry a card of the Handled column shows; nil
	// on a live card.
	entry *core.HandledView
}

// cards returns a card in each column whose labels an item of the board
// carries and that shows its kind, item by item in the board's order,
// oldest first (R7, KTD6, R23), then the Not on board and Handled cards
// (KTD3, KTD13 of #151). A card of an item crew holds carries its issue's
// view (R10, KTD9); no card waits for the next rule (R28).
func (m Model) cards() []card {
	views := map[string]core.IssueView{}
	for _, iv := range m.snap.Issues {
		views[iv.Issue.Key] = iv
	}
	var out []card
	for _, bi := range m.snap.Board {
		view, held := views[bi.Issue.Key]
		for i, c := range m.cfg.Board {
			carries := slices.ContainsFunc(c.Labels, func(l string) bool { return slices.Contains(bi.Labels, l) })
			if carries && c.Takes == bi.Issue.Kind {
				out = append(out, card{issue: bi.Issue, column: i, held: held, view: view})
			}
		}
	}
	out = append(out, m.unboardedCards(out)...)
	return append(out, m.handledCards()...)
}

// notOnBoard and handledColumn are the indexes of the columns the TUI adds
// after the configured ones (KTD3, KTD13 of #151).
func (m Model) notOnBoard() int    { return len(m.cfg.Board) }
func (m Model) handledColumn() int { return len(m.cfg.Board) + 1 }

// unboardedCards are a live card in the Not on board column for each
// issue crew holds that has none in boarded, the cards of the configured
// columns. An issue whose rule has no actions is left out: such a rule
// has no column on the default board, by design (KTD13 of #151, #134).
func (m Model) unboardedCards(boarded []card) []card {
	shown := map[string]bool{}
	for _, c := range boarded {
		shown[c.issue.Key] = true
	}
	var out []card
	for _, iv := range m.snap.Issues {
		if !shown[iv.Issue.Key] && len(iv.Actions) > 0 {
			out = append(out, card{issue: iv.Issue, column: m.notOnBoard(), held: true, view: iv})
		}
	}
	return out
}

// handledCards are a card in the Handled column for each issue crew
// stopped handling, those that need you first (R8, KTD3 of #151).
func (m Model) handledCards() []card {
	entries := byAttention(m.snap.Handled)
	out := make([]card, 0, len(entries))
	for i := range entries {
		out = append(out, card{issue: entries[i].Issue, column: m.handledColumn(), entry: &entries[i]})
	}
	return out
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

// boardLayout lays out the board for the current snapshot and window: the
// configured columns, Not on board while it holds cards, then Handled
// (KTD3, KTD13 of #151).
func (m Model) boardLayout(cards []card) boardLayout {
	held := map[int]bool{}
	for _, c := range cards {
		held[c.column] = true
	}
	columns := make([]int, 0, m.handledColumn()+1)
	for i := range m.cfg.Board {
		columns = append(columns, i)
	}
	if held[m.notOnBoard()] {
		columns = append(columns, m.notOnBoard())
	}
	columns = append(columns, m.handledColumn())
	return layout(columns, held, m.width-1, m.boardOffset)
}

// board is the Board section: its summary and its rows, with at most
// limit cards a column; limit < 0 means no limit (KTD8).
func (m Model) board(limit int) (string, []string) {
	cards := m.cards()
	l := m.boardLayout(cards)
	return m.boardSummary(cards, l), m.boardRows(l, cards, limit)
}

// boardSummary is the board's summary (KTD8): the items with a card in a
// configured column, the empty columns dropped, and, in the warning style,
// that the last board read or listing failed (KTD5).
func (m Model) boardSummary(cards []card, l boardLayout) string {
	issues := map[string]bool{}
	for _, c := range cards {
		if c.column < m.notOnBoard() {
			issues[c.issue.Key] = true
		}
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

// boardRows draws the column names, their underlines with the slides, then
// the cards, cardRows rows each, then "+N more" where a column has more
// than limit.
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
	shownCards := min(tallest, limit)
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

// cardRows are the rows of each column's card k, blank where a column has
// fewer cards (KTD1 of #151).
func (m Model) cardRows(l boardLayout, byColumn [][]card, k int) []string {
	cells := make([][]string, cardRows)
	for r := range cells {
		cells[r] = make([]string, len(l.columns))
	}
	for i, cs := range byColumn {
		if k < len(cs) {
			for r, line := range m.cardFace(cs[k], l.width) {
				cells[r][i] = line
			}
		}
	}
	out := make([]string, 0, cardRows)
	for _, row := range cells {
		out = append(out, m.boardRow(l, row, false))
	}
	return out
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
		names[i] = m.columnName(c, st)
	}
	return names
}

// columnName is column c's name in st: a configured column's, Not on
// board, or Handled with its count and the run's cost after it, muted
// (R8, KTD3, KTD13 of #151).
func (m Model) columnName(c int, st lipgloss.Style) string {
	switch c {
	case m.notOnBoard():
		return st.Render("Not on board")
	case m.handledColumn():
		return st.Render("Handled") + " " + m.styles.muted.Render(m.handledSummary())
	}
	return st.Render(m.cfg.Board[c].Name)
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

// spin is the current frame of the shared spinner, in the success colour.
func (m Model) spin() string {
	sp := m.spinner
	sp.Style = m.styles.success
	return sp.View()
}
