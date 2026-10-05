package tui

import (
	"maps"
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// selection is the highlighted card: its issue's key, its column, its row
// in that column, and the first card that column shows. An empty key is
// no selection (KTD5, KTD6 of #151).
type selection struct {
	key              string
	column, row, top int
}

// is reports whether c is the highlighted card.
func (s selection) is(c card) bool { return c.issue.Key == s.key && c.column == s.column }

// byColumn groups cards by their column, each in board order.
func byColumn(cards []card) map[int][]card {
	out := map[int][]card{}
	for _, c := range cards {
		out[c.column] = append(out[c.column], c)
	}
	return out
}

// rowOf is the row of key's card in cs, or -1.
func rowOf(cs []card, key string) int {
	return slices.IndexFunc(cs, func(c card) bool { return c.issue.Key == key })
}

// repaired returns s for cards (KTD5 of #151): the card of its key in its
// column, at its row now; else the issue's Handled card, or its first
// card in board order; else the nearest card to where it was. With no
// selection, the first card of the first column holding cards.
func (s selection) repaired(cards []card) selection {
	columns := byColumn(cards)
	if row := rowOf(columns[s.column], s.key); row >= 0 {
		s.row = row
		return s
	}
	i := slices.IndexFunc(cards, func(c card) bool { return c.issue.Key == s.key && c.entry != nil })
	if i < 0 {
		i = slices.IndexFunc(cards, func(c card) bool { return c.issue.Key == s.key })
	}
	if i >= 0 {
		c := cards[i]
		return selection{key: s.key, column: c.column, row: rowOf(columns[c.column], s.key)}
	}
	return s.nearest(columns)
}

// nearest is the card at s's row, or the last when the column is
// shorter, in the column nearest s's that holds cards, the left one on a
// tie; no selection when no column holds cards (KTD5 of #151). The zero
// selection's nearest is the first card of the first column holding cards.
func (s selection) nearest(columns map[int][]card) selection {
	best := -1
	for _, c := range slices.Sorted(maps.Keys(columns)) {
		if best < 0 || distance(c, s.column) < distance(best, s.column) {
			best = c
		}
	}
	if best < 0 {
		return selection{}
	}
	cs := columns[best]
	row := min(s.row, len(cs)-1)
	top := 0
	if best == s.column {
		top = s.top
	}
	return selection{key: cs[row].issue.Key, column: best, row: row, top: top}
}

// distance is how many columns apart a and b are.
func distance(a, b int) int { return max(a-b, b-a) }

// shownFrom is the first card a column of n cards shows, limit at a time,
// with its row-th card highlighted: from top, moved just enough to show
// that card and as many cards as fit (KTD6 of #151).
func shownFrom(top, row, n, limit int) int {
	from := min(max(top, row-limit+1), row)
	return max(min(from, n-limit), 0)
}

// navigated returns m after a key that moves focus or the highlight, or
// scrolls the focused section (KTD4, KTD6 of #151).
func (m Model) navigated(msg tea.KeyPressMsg) Model {
	switch {
	case key.Matches(msg, m.keys.focus):
		m.focus = (m.focus + 1) % (focusEvents + 1)
	case key.Matches(msg, m.keys.back):
		m.focus = (m.focus + focusEvents) % (focusEvents + 1)
	case key.Matches(msg, m.keys.bots):
		m.focus = focusBots
	case key.Matches(msg, m.keys.events):
		m.focus = focusEvents
	case key.Matches(msg, m.keys.left):
		return m.sideways(-1)
	case key.Matches(msg, m.keys.right):
		return m.sideways(1)
	case m.focus == focusBoard:
		return m.vertical(msg)
	default:
		return m.scrolled(msg)
	}
	return m
}

// escaped returns m with the help closed when it shows, else with the
// popup closed when it is open, else with the board focused (KTD12 of
// #151).
func (m Model) escaped() Model {
	switch {
	case m.help:
		m.help = false
	case m.popup:
		m.popup = false
	default:
		m.focus = focusBoard
	}
	return m
}

// sideways returns m with the Bots cards moved delta cards while Bots has
// focus, else with the highlight moved delta columns (R9, KTD6 of #151).
func (m Model) sideways(delta int) Model {
	if m.focus == focusBots {
		return m.scrollBots(delta)
	}
	return m.moveColumn(delta)
}

// vertical returns m with the highlight moved a card up or down its
// column; the other keys do nothing on the board (KTD6 of #151).
func (m Model) vertical(msg tea.KeyPressMsg) Model {
	switch {
	case key.Matches(msg, m.keys.up):
		return m.moveRow(-1)
	case key.Matches(msg, m.keys.down):
		return m.moveRow(1)
	}
	return m
}

// moveRow returns m with the highlight moved delta cards within its
// column, stopping at its ends, and the column scrolled to show it (KTD6
// of #151).
func (m Model) moveRow(delta int) Model {
	cs := byColumn(m.cards())[m.sel.column]
	if m.sel.key == "" || len(cs) == 0 {
		return m
	}
	row := min(max(m.sel.row+delta, 0), len(cs)-1)
	m.sel.key, m.sel.row = cs[row].issue.Key, row
	m.sel.top = shownFrom(m.sel.top, row, len(cs), m.budget().cards)
	return m
}

// moveColumn returns m with the highlight moved to the card at the same
// shown slot, or the last, in the column holding cards delta columns
// away, that column showing from its first card, and the board scrolled
// to draw it. Past the first or last such column it does nothing (KTD6 of
// #151).
func (m Model) moveColumn(delta int) Model {
	cards := m.cards()
	columns := byColumn(cards)
	order := slices.Sorted(maps.Keys(columns))
	i := slices.Index(order, m.sel.column) + delta
	if m.sel.key == "" || i < 0 || i >= len(order) {
		return m
	}
	slot := m.sel.row - shownFrom(m.sel.top, m.sel.row, len(columns[m.sel.column]), m.budget().cards)
	cs := columns[order[i]]
	row := min(slot, len(cs)-1)
	m.sel = selection{key: cs[row].issue.Key, column: order[i], row: row}
	return m.reveal(cards, order, i)
}

// reveal returns m with the board scrolled just enough to draw the i-th
// of the columns holding cards, held (KTD6 of #151). The board scrolls
// only through the columns holding cards (KTD9).
func (m Model) reveal(cards []card, held []int, i int) Model {
	l := m.boardLayout(cards)
	m.boardOffset = l.offset
	switch {
	case len(l.columns) == 0 || slices.Contains(l.columns, held[i]):
	case i < l.offset:
		m.boardOffset = i
	default:
		m.boardOffset = i - len(l.columns) + 1
	}
	return m
}
