package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// scrollPage is how far pgup and pgdown scroll Events and the popup, and
// scrollEnd an offset past the end of either, which home or end clamp.
const scrollPage, scrollEnd = 10, 1 << 20

// halves centres the help overlay.
const halves = 2 // the overlay's offset is half the room left around it

// keyMap holds the view's keys (R19 to R22, KTD11; KTD12 of #151). None
// of them acts outside crew's own process, and none is a mouse event
// (R9 of #151).
type keyMap struct {
	stop, pause, resume, focus, back, bots, events, esc, enter key.Binding
	up, down, pageUp, pageDown, top, bottom, left, right, help key.Binding
}

// newKeyMap returns the view's key bindings.
func newKeyMap() keyMap {
	return keyMap{
		stop:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q q", "stop")),
		pause:    key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "pause")),
		resume:   key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "resume")),
		focus:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus")),
		back:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "focus back")),
		bots:     key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bots")),
		events:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "events")),
		esc:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close or board")),
		enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open card")),
		up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "card or events up")),
		down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "card or events down")),
		pageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		pageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "page down")),
		top:      key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
		bottom:   key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
		left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "card or bots left")),
		right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "card or bots right")),
		help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

// key handles a key press: the stop keys (KTD7; KTD1, KTD2 of #266), the
// pause key (KTD6 of #282), the help overlay, then the popup's keys while it is open, else Enter, focus,
// the highlight and scrolling (KTD12 of #151).
func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.stop):
		return m.stopKey()
	case key.Matches(msg, m.keys.pause):
		m.pauseKey()
	case key.Matches(msg, m.keys.help):
		m.help = !m.help
	case key.Matches(msg, m.keys.esc):
		m = m.escaped()
	case m.popup:
		m = m.popupKey(msg)
	case key.Matches(msg, m.keys.enter):
		m = m.opened()
	default:
		m = m.navigated(msg)
	}
	return m, nil
}

// stopKey handles q or Ctrl-C: once crew is stopping it forces the exit;
// within the window a first press armed it confirms the stop; otherwise it
// arms the stop until the window ends (R1 to R7 of #266).
func (m Model) stopKey() (tea.Model, tea.Cmd) {
	now := m.cfg.Now()
	switch {
	case m.stopping || m.snap.Stopping:
		m.cfg.Force()
		return m, tea.Quit
	case now.Before(m.armedUntil):
		m.armedUntil = time.Time{}
		m.stopping = true
		m.cfg.Stop()
		return m, nil
	}
	until := now.Add(armWindow)
	m.armedUntil = until
	return m, tea.Tick(armWindow, func(time.Time) tea.Msg { return armExpiredMsg{until: until} })
}

// pauseKey pauses crew's taking of new issues, or resumes it, unless crew
// is stopping or winding down, which ended any pause (R10 of #282). It
// keeps no state: the header follows the engine's snapshot (KTD6 of #282).
func (m Model) pauseKey() {
	if m.stopping || m.snap.Stopping || m.snap.TimeUp {
		return
	}
	m.cfg.Pause()
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

// scrolled returns m with Events moved by a row, a page or to an end
// while it has focus, as far as its rows allow. Events counts back from
// the newest row, so up moves it back.
func (m Model) scrolled(msg tea.KeyPressMsg) Model {
	if m.focus != focusEvents {
		return m
	}
	offset := m.eventsOffset
	switch {
	case key.Matches(msg, m.keys.up):
		offset++
	case key.Matches(msg, m.keys.down):
		offset--
	case key.Matches(msg, m.keys.pageUp):
		offset += scrollPage
	case key.Matches(msg, m.keys.pageDown):
		offset -= scrollPage
	case key.Matches(msg, m.keys.top):
		offset = scrollEnd
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

// keyHelp is the key-help line (R21), the popup's while it is open
// (KTD12 of #151), or, once crew is stopping, how to force the exit
// (KTD16), else while the stop is armed, how to confirm it (KTD5 of #266).
// The board's line names Ctrl-P as pause, or resume while paused, and
// leaves it out while crew winds down (KTD6 of #282).
func (m Model) keyHelp() string {
	switch {
	case m.stopping || m.snap.Stopping:
		return m.styles.warning.Render("q or ctrl+c forces the exit")
	case !m.armedUntil.IsZero():
		return m.styles.warning.Render(fmt.Sprintf("q or ctrl+c again within %s stops crew", armWindow))
	}
	h := m.helper()
	h.SetWidth(m.width)
	if m.popup {
		return h.ShortHelpView([]key.Binding{
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
			key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "card")),
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "scroll")),
			m.keys.stop,
		})
	}
	move := key.NewBinding(key.WithKeys("left", "right", "up", "down"), key.WithHelp("←→↑↓", "move"))
	open := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	bindings := []key.Binding{m.keys.stop}
	switch {
	case m.snap.TimeUp:
	case m.snap.Paused:
		bindings = append(bindings, m.keys.resume)
	default:
		bindings = append(bindings, m.keys.pause)
	}
	return h.ShortHelpView(append(bindings, m.keys.focus, move, open, m.keys.help))
}

// helpOverlay draws every key in a box over the middle of view (R20), with
// the stop's two presses and the press that forces (R9 of #266), the pause
// (R8 of #282), and what a
// card's labelled rows mean (R12, KTD12 of #151).
func (m Model) helpOverlay(view string) string {
	stop := key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q q", fmt.Sprintf("stop, within %s", armWindow)))
	force := key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "force if stopping"))
	pause := key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "pause or resume"))
	groups := [][]key.Binding{
		{stop, force, pause, m.keys.help, m.keys.focus, m.keys.back, m.keys.bots, m.keys.events, m.keys.esc},
		{m.keys.up, m.keys.down, m.keys.left, m.keys.right, m.keys.enter},
		{m.keys.pageUp, m.keys.pageDown, m.keys.top, m.keys.bottom},
	}
	title := m.styles.title.Render
	box := m.styles.helpBox.Render(title("Keys") + "\n\n" + m.helper().FullHelpView(groups) +
		"\n\n" + title("Cards") + "\n\n" + m.cardsHelp())
	x := max((lipgloss.Width(view)-lipgloss.Width(box))/halves, 0)
	y := max((lipgloss.Height(view)-lipgloss.Height(box))/halves, 0)
	return lipgloss.NewCompositor(lipgloss.NewLayer(view), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
}

// cardsHelp says what each labelled row of a board card means (R12, KTD12
// of #151).
func (m Model) cardsHelp() string {
	labels := []string{"run", "bots", "via"}
	meanings := []string{
		"the issue's actions and how long each has run",
		"the bots its running actions act as",
		"the queue its actions run in",
	}
	s := m.styles
	return lipgloss.JoinHorizontal(lipgloss.Top,
		s.helpKey.Render(strings.Join(labels, "\n")), " ", s.helpAction.Render(strings.Join(meanings, "\n")))
}
