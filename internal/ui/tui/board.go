package tui

import (
	"fmt"
	"slices"
	"strings"

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

// card is an item on the board, in a column whose labels it carries and
// that shows its kind, held by crew or not (KTD9, KTD10).
type card struct {
	issue crew.Issue
	// column is the card's column: an index into the board's columns.
	column int
	// held is set while crew holds the item, with claim its claim; the
	// zero claim is ClaimTaking, so claim alone cannot tell.
	held  bool
	claim core.Claim
}

// cards returns a card in each column whose labels an item of the board
// carries and that shows its kind, item by item in the board's order,
// oldest first (R7, KTD6, R23). A card of an item crew holds carries its
// claim (R10, KTD9); no card waits for the next rule (R28).
func (m Model) cards() []card {
	claims := map[string]core.Claim{}
	for _, iv := range m.snap.Issues {
		claims[iv.Issue.Key] = iv.Claim
	}
	var out []card
	for _, bi := range m.snap.Board {
		claim, held := claims[bi.Issue.Key]
		for i, c := range m.cfg.Board {
			carries := slices.ContainsFunc(c.Labels, func(l string) bool { return slices.Contains(bi.Labels, l) })
			if carries && c.Takes == bi.Issue.Kind {
				out = append(out, card{issue: bi.Issue, column: i, held: held, claim: claim})
			}
		}
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

// boardLayout lays out the board for the current snapshot and window.
func (m Model) boardLayout(cards []card) boardLayout {
	columns := make([]int, len(m.cfg.Board))
	for i := range columns {
		columns[i] = i
	}
	held := map[int]bool{}
	for _, c := range cards {
		held[c.column] = true
	}
	return layout(columns, held, m.width-1, m.boardOffset)
}

// board is the Workflow section: its summary and its rows, with at most
// limit cards a column; limit < 0 means no limit (KTD8).
func (m Model) board(limit int) (string, []string) {
	cards := m.cards()
	l := m.boardLayout(cards)
	return m.boardSummary(cards, l), m.boardRows(l, cards, limit)
}

// boardSummary is the board's summary (KTD8): the items with a card, the
// empty columns dropped, and, in the warning style, that the last board
// read or listing failed (KTD5).
func (m Model) boardSummary(cards []card, l boardLayout) string {
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
		names[i] = st.Render(m.cfg.Board[c].Name)
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
// (R11, KTD13), or ○ idle when crew does not hold its issue (#126).
func (m Model) cardLines(c card, width int) (string, string) {
	s := m.styles
	bar := s.subtle.Render("▌")
	if c.held && c.claim == core.ClaimRunning {
		bar = s.strongAccent.Render("▌")
	}
	top := fit(bar+" "+s.link(c.issue.Ref, c.issue.URL)+" "+s.text.Render(clean(c.issue.Title)), width)
	var state string
	switch {
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
