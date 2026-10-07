package tui

import (
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The popup's size (KTD7 of #151).
const (
	// popupMaxWidth is the widest the popup gets, popupMargin the cells
	// it leaves beside it in a narrower window.
	popupMaxWidth = 100
	popupMargin   = 4
	// popupRoom is the rows it leaves above and below it; popupChrome the
	// rows of its border and title, which do not scroll.
	popupRoom   = 2
	popupChrome = 3
)

// selected is the highlighted card, if the board still has it.
func (m Model) selected() (card, bool) {
	for _, c := range m.cards() {
		if m.sel.is(c) {
			return c, true
		}
	}
	return card{}, false
}

// opened returns m with the highlighted card's popup open, while the
// board has focus and a card is highlighted (R10, KTD12 of #151).
func (m Model) opened() Model {
	if m.focus == focusBoard && !m.sel.empty() {
		m.popup, m.popupOffset = true, 0
	}
	return m
}

// popupKey returns m after a key while the popup is open: ←→ move it to
// the previous or next card, the scroll keys scroll it, and every other
// key does nothing (R13, R14, KTD12 of #151).
func (m Model) popupKey(msg tea.KeyPressMsg) Model {
	switch {
	case key.Matches(msg, m.keys.left):
		return m.walk(-1)
	case key.Matches(msg, m.keys.right):
		return m.walk(1)
	}
	return m.scrollPopup(msg)
}

// place is a card's column and its row in that column.
type place struct{ column, row int }

// walk returns m with the highlight, and the popup with it, moved delta
// cards in board order: down each column, then on to the next column
// holding cards. Past the first or last card it does nothing (KTD12 of
// #151).
func (m Model) walk(delta int) Model {
	cards := m.cards()
	columns := byColumn(cards)
	order := slices.Sorted(maps.Keys(columns))
	var all []place
	for _, c := range order {
		for r := range columns[c] {
			all = append(all, place{c, r})
		}
	}
	i := slices.Index(all, place{m.sel.column, m.sel.row})
	if i < 0 || i+delta < 0 || i+delta >= len(all) {
		return m
	}
	next := all[i+delta]
	cs := columns[next.column]
	top := 0
	if next.column == m.sel.column {
		top = m.sel.top
	}
	m.sel = selection{
		id: cs[next.row].issue.ID, column: next.column, row: next.row,
		top: shownFrom(top, next.row, len(cs), m.budget().cards),
	}
	m.popupOffset = 0
	return m.reveal(cards, order, slices.Index(order, next.column))
}

// scrollPopup returns m with the popup's rows moved by a row, a page or
// to an end, as far as they go past the room it has (KTD7 of #151).
func (m Model) scrollPopup(msg tea.KeyPressMsg) Model {
	c, ok := m.selected()
	if !ok {
		return m
	}
	offset := m.popupOffset
	switch {
	case key.Matches(msg, m.keys.up):
		offset--
	case key.Matches(msg, m.keys.down):
		offset++
	case key.Matches(msg, m.keys.pageUp):
		offset -= scrollPage
	case key.Matches(msg, m.keys.pageDown):
		offset += scrollPage
	case key.Matches(msg, m.keys.top):
		offset = 0
	case key.Matches(msg, m.keys.bottom):
		offset = scrollEnd
	}
	m.popupOffset = m.popupScroll(len(m.popupBody(c, m.popupWidth()-cardFrame)), offset)
	return m
}

// popupWidth is the popup's width in cells, its border included.
func (m Model) popupWidth() int {
	return max(min(m.width-popupMargin, popupMaxWidth), cardFrame+1)
}

// popupBodyRows is how many of the popup's rows under its title the
// window has room for; every one while its height is unknown.
func (m Model) popupBodyRows(n int) int {
	if m.height <= 0 {
		return n
	}
	return max(m.height-popupRoom-popupChrome, 1)
}

// popupScroll is offset kept within what n rows of the popup scroll.
func (m Model) popupScroll(n, offset int) int {
	return min(max(offset, 0), max(n-m.popupBodyRows(n), 0))
}

// popupOverlay draws the highlighted card's popup over the middle of
// view, view drawn dimmed but for its key-help line (R13, KTD7 of #151).
func (m Model) popupOverlay(view string) string {
	c, ok := m.selected()
	if !ok {
		return view
	}
	box := strings.Join(m.popupBox(c), "\n")
	dim := m.dimmed(view)
	x := max((lipgloss.Width(dim)-lipgloss.Width(box))/halves, 0)
	y := max((lipgloss.Height(dim)-lipgloss.Height(box))/halves, 0)
	return lipgloss.NewCompositor(lipgloss.NewLayer(dim), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
}

// dimmed is view without its colours, in the subtle colour, but for its
// last line, the key help, which says what the popup's keys do.
func (m Model) dimmed(view string) string {
	rows := strings.Split(view, "\n")
	for i, r := range rows[:len(rows)-1] {
		rows[i] = m.styles.subtle.Render(ansi.Strip(r))
	}
	return strings.Join(rows, "\n")
}

// popupBox is the popup's rows: its title, then as many of its rows as
// fit from its scroll offset, in a rounded border in the highlight
// colour whose bottom row says it scrolls when its rows do not all fit
// (KTD7 of #151).
func (m Model) popupBox(c card) []string {
	width := m.popupWidth()
	body := m.popupBody(c, width-cardFrame)
	room := m.popupBodyRows(len(body))
	from := m.popupScroll(len(body), m.popupOffset)
	shown := body[from:min(from+room, len(body))]
	out := framed(append([]string{m.popupTitle(c)}, shown...), width, m.styles.highlight)
	if len(body) > room {
		out[len(out)-1] = m.scrollBorder(width)
	}
	return out
}

// scrollBorder is the popup's bottom border, width cells wide, saying
// that ↑↓ scroll it.
func (m Model) scrollBorder(width int) string {
	b := lipgloss.RoundedBorder()
	label := " ↑↓ scroll "
	rest := max(width-cardCorners-1-lipgloss.Width(label), 0)
	hl := m.styles.highlight.Render
	return hl(b.BottomLeft+b.Bottom) + m.styles.muted.Render(label) + hl(strings.Repeat(b.Bottom, rest)+b.BottomRight)
}

// popupTitle is the popup's first row: the issue's reference, linked,
// and its title (R13).
func (m Model) popupTitle(c card) string {
	return m.styles.link(c.issue.Ref, c.issue.URL) + " " + m.styles.title.Render(clean(c.issue.Title))
}

// popupBody is the popup's rows under its title, inner cells wide: its
// header, its actions with their messages, then its events (KTD7, KTD8
// of #151).
func (m Model) popupBody(c card, inner int) []string {
	rows := append(m.popupHeader(c), "")
	rows = append(rows, m.popupActions(c, inner)...)
	rows = append(rows, "", m.styles.title.Render("Events"))
	return append(rows, m.popupEvents(c)...)
}
