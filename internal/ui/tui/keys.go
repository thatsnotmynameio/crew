package tui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// halves centres the help overlay.
const halves = 2 // the overlay's offset is half the room left around it

// keyMap holds the view's keys (R19 to R22, KTD11). None of them acts
// outside crew's own process.
type keyMap struct {
	stop, focus, back, up, down, pageUp, pageDown, top, bottom, left, right, help key.Binding
}

// newKeyMap returns the view's key bindings.
func newKeyMap() keyMap {
	return keyMap{
		stop:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "stop")),
		focus:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus")),
		back:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "focus back")),
		up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
		down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
		pageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		pageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "page down")),
		top:      key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
		bottom:   key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
		left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "board or bots left")),
		right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "board or bots right")),
		help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

// key handles a key press: the stop keys as before (KTD7), the help
// overlay, focus and scrolling.
func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.stop):
		if m.stopping {
			m.cfg.Force()
			return m, tea.Quit
		}
		m.stopping = true
		m.cfg.Stop()
	case key.Matches(msg, m.keys.help):
		m.help = !m.help
	case key.Matches(msg, m.keys.focus):
		m.focus = (m.focus + 1) % (focusEvents + 1)
	case key.Matches(msg, m.keys.back):
		m.focus = (m.focus + focusEvents) % (focusEvents + 1)
	case key.Matches(msg, m.keys.left):
		m = m.scrollSideways(-1)
	case key.Matches(msg, m.keys.right):
		m = m.scrollSideways(1)
	default:
		m = m.scrolled(msg)
	}
	return m, nil
}

// scrollSideways returns m with the Bots cards moved delta cards while
// Bots has focus, else the board moved delta columns (R9, KTD11).
func (m Model) scrollSideways(delta int) Model {
	if m.focus == focusBots {
		return m.scrollBots(delta)
	}
	return m.scrollBoard(delta)
}

// scrollBots returns m with the Bots cards moved delta cards sideways, as
// far as the cards allow: the layout clamps the offset (KTD3). The first
// layout clamps an offset a resize left past the cards drawn, so delta
// moves from what shows rather than from the stale offset.
func (m Model) scrollBots(delta int) Model {
	m.botsOffset = m.botsLayout().offset + delta
	m.botsOffset = m.botsLayout().offset
	return m
}

// scrollBoard returns m with the board moved delta columns sideways, as
// far as its columns allow: the layout clamps the offset (KTD9).
func (m Model) scrollBoard(delta int) Model {
	cards := m.cards()
	m.boardOffset = m.boardLayout(cards).offset + delta
	m.boardOffset = m.boardLayout(cards).offset
	return m
}

// scrolled returns m with Events moved by a row, a page or to an end
// while it has focus, as far as its rows allow. Events counts back from
// the newest row, so up moves it back.
func (m Model) scrolled(msg tea.KeyPressMsg) Model {
	if m.focus != focusEvents {
		return m
	}
	const page, end = 10, 1 << 20
	offset := m.eventsOffset
	switch {
	case key.Matches(msg, m.keys.up):
		offset++
	case key.Matches(msg, m.keys.down):
		offset--
	case key.Matches(msg, m.keys.pageUp):
		offset += page
	case key.Matches(msg, m.keys.pageDown):
		offset -= page
	case key.Matches(msg, m.keys.top):
		offset = end
	case key.Matches(msg, m.keys.bottom):
		offset = 0
	}
	m.eventsOffset = min(max(offset, 0), m.scrollLimit())
	return m
}

// scrollLimit is the furthest Events scrolls: its rows past those the
// window has room for (KTD8).
func (m Model) scrollLimit() int {
	return max(len(m.snap.Recent)-m.budget().events, 0)
}

// helper returns the help bubble styled for the view.
func (m Model) helper() help.Model {
	h := help.New()
	h.ShortSeparator = " · "
	s := m.styles
	h.Styles = help.Styles{
		ShortKey: s.muted, ShortDesc: s.muted, ShortSeparator: s.subtle, Ellipsis: s.subtle,
		FullKey: s.helpKey, FullDesc: s.helpAction, FullSeparator: s.subtle,
	}
	return h
}

// keyHelp is the key-help line (R21), or, once you asked to stop, how
// to force the exit (KTD16).
func (m Model) keyHelp() string {
	if m.stopping {
		return m.styles.warning.Render("q or ctrl+c again forces the exit")
	}
	h := m.helper()
	h.SetWidth(m.width)
	scroll := key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "scroll"))
	board := key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "board/bots"))
	return h.ShortHelpView([]key.Binding{m.keys.stop, m.keys.focus, scroll, board, m.keys.help})
}

// helpOverlay draws every key in a box over the middle of view (R20).
func (m Model) helpOverlay(view string) string {
	groups := [][]key.Binding{
		{m.keys.stop, m.keys.help},
		{m.keys.focus, m.keys.back},
		{m.keys.up, m.keys.down, m.keys.pageUp, m.keys.pageDown, m.keys.top, m.keys.bottom},
		{m.keys.left, m.keys.right},
	}
	box := m.styles.helpBox.Render(m.styles.title.Render("Keys") + "\n\n" + m.helper().FullHelpView(groups))
	x := max((lipgloss.Width(view)-lipgloss.Width(box))/halves, 0)
	y := max((lipgloss.Height(view)-lipgloss.Height(box))/halves, 0)
	return lipgloss.NewCompositor(lipgloss.NewLayer(view), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
}
