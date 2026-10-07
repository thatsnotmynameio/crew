// Package tui is crew's terminal UI (R17): a Bubble Tea model that only
// displays the engine's latest snapshot, tells the terminal crew's state, and
// asks the engine to stop. It is the only package that imports Bubble Tea,
// Lip Gloss and Bubbles (KTD17).
package tui

import (
	"maps"
	"slices"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// tickInterval refreshes elapsed times between engine updates, which arrive
// only a few times per poll (KTD7).
const tickInterval = time.Second

// defaultWidth is the window's width in columns before its first size.
const defaultWidth = 80

// updateMsg is an update the engine published.
type updateMsg engine.Update

// engineStoppedMsg means the update channel was closed: the engine returned.
type engineStoppedMsg struct{}

// tickMsg asks for elapsed times to be recomputed.
type tickMsg struct{}

// armWindow is how long a first q or Ctrl-C keeps the stop armed (R1 of
// #266).
const armWindow = 3 * time.Second

// armExpiredMsg ends the stop armed until until, unless a later press armed
// it again (KTD1 of #266).
type armExpiredMsg struct{ until time.Time }

// Config is what the TUI needs for a run (KTD1).
type Config struct {
	// Updates is a latest-wins subscription (Engine.SubscribeLatest).
	Updates <-chan engine.Update
	// Stop asks the engine to stop; Force kills what must die and returns.
	Stop, Force func()
	// Now is the clock elapsed times are measured with; Location is where
	// event times are shown.
	Now      func() time.Time
	Location *time.Location
	// Notify tells, for each rule by name, whether its ends send a desktop
	// notification (KTD6).
	Notify map[crew.RuleName]bool
	// Board is the board's columns, in board order: the ones the config
	// writes, or its default ones (R21, R22, KTD10).
	Board []crew.BoardColumn
	// Repository is the repository's name, for the header (R3).
	Repository string
	// Warnings are crew's startup warnings, each shown under the header for
	// as long as crew runs (R2).
	Warnings []string
}

// focus is the section the arrow keys move (R21; KTD4 of #151).
type focus int

// The sections that take focus, in tab order. The board is the zero
// value, so the view opens on it (R10 of #151).
const (
	focusBoard focus = iota
	focusBots
	focusEvents
)

// Model is the TUI's state. Use New; the program owns it after that.
type Model struct {
	cfg  Config
	snap engine.Snapshot
	// at is the clock's time at the last update or tick; elapsed times are
	// measured to it, so View stays a function of the model.
	at time.Time
	// width and height are the window's size in columns and rows; a zero
	// height, before the first size, fits nothing to it.
	width, height int
	// stopping is true once you asked to stop.
	stopping bool
	// armedUntil is when the stop a first q or Ctrl-C armed ends; zero
	// when none is armed (KTD1 of #266).
	armedUntil time.Time

	styles  styles
	keys    keyMap
	spinner spinner.Model
	// help is set while the keys show over the view (R20).
	help  bool
	focus focus
	// boardOffset is the first board column shown when the columns that
	// hold cards do not fit (KTD9); botsOffset the first Bots card shown
	// when the cards do not fit (KTD3).
	boardOffset, botsOffset int
	// eventsOffset counts the Events rows scrolled back from the newest
	// (KTD11).
	eventsOffset int
	// sel is the highlighted card (KTD5 of #151).
	sel selection
	// popup is set while the highlighted card's popup shows, popupOffset
	// the rows its content is scrolled (KTD7 of #151).
	popup       bool
	popupOffset int

	// memory remembers each issue's last columns this run and the slides
	// running (KTD10).
	memory *boardMemory
	// messages remembers each action's last message and branch while its
	// issue has a card (KTD9 of #151).
	messages *messageMemory
	// outside tracks focus reports and the rule ends already notified
	// (KTD6).
	outside *outsideState
}

// New returns a model that renders the updates of cfg.Updates.
//
// The first Ctrl-C or q only arms the stop for armWindow and says so; a
// second within it calls Stop and shows that crew is stopping, and the
// model keeps running until Updates is closed, then quits. Once crew is
// stopping, by a key or by the engine, one more Ctrl-C or q calls Force and
// quits at once (KTD7; KTD1, KTD2 of #266). Force is called from Update,
// while the terminal is still in raw mode, so it should kill what must die
// and return, leaving the exit to the caller after Program.Run returns.
// Bubble Tea's own signal handler should be disabled
// (tea.WithoutSignalHandler), so crew's handler is the only one.
func New(cfg Config) Model {
	return Model{
		cfg: cfg, at: cfg.Now(), width: defaultWidth,
		styles:   newStyles(true),
		keys:     newKeyMap(),
		spinner:  spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		memory:   newBoardMemory(),
		messages: newMessageMemory(),
		outside:  newOutsideState(),
	}
}

// Init starts waiting for updates, ticking and spinning, and asks the
// terminal for its background colour (KTD12).
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.wait(), tick(), m.spinner.Tick, tea.RequestBackgroundColor)
}

// Update handles engine updates, ticks, window sizes, focus reports, the
// background colour and keys.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateMsg:
		return m.updated(engine.Update(msg))
	case engineStoppedMsg:
		return m, tea.Quit
	case tickMsg:
		m.at = m.cfg.Now()
		return m, tick()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case slideTickMsg:
		return m, m.memory.advance()
	case armExpiredMsg:
		if msg.until.Equal(m.armedUntil) {
			m.armedUntil = time.Time{}
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		m.styles = newStyles(msg.IsDark())
	case tea.FocusMsg:
		m.outside.focused = true
	case tea.BlurMsg:
		m.outside.focused = false
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// updated takes in a new snapshot: it remembers what the actions said,
// repairs the highlight, starts the slides of the cards that moved and
// sends the notifications of the rules that ended, before it waits for the
// next update, so the read that finds the channel closed, and with it the
// quit, comes after them (KTD6, KTD10; KTD5, KTD9 of #151).
func (m Model) updated(u engine.Update) (tea.Model, tea.Cmd) {
	m.snap = u.Snapshot
	m.at = m.cfg.Now()
	cards := m.cards()
	m.messages.record(m.snap, cards)
	was := m.sel.id
	m.sel = m.sel.repaired(cards)
	// The popup follows its issue while it has a card, and closes when
	// it has none (R21 of #151).
	if m.sel.id != was {
		m.popup = false
	}
	// The highlighted card's column scrolls to its row now, which moves
	// when crew takes or lets go of its issue (R5 of #231).
	if !m.sel.empty() {
		m.sel.top = shownFrom(m.sel.top, m.sel.row, len(byColumn(cards)[m.sel.column]), m.budget().cards)
	}
	// The board scrolls to keep the highlight drawn, as ←→ do (R10 of
	// #151).
	order := slices.Sorted(maps.Keys(byColumn(cards)))
	if i := slices.Index(order, m.sel.column); !m.sel.empty() && i >= 0 {
		m = m.reveal(cards, order, i)
	}
	slide := m.memory.moved(cards)
	notes := m.notifications()
	if len(notes) == 0 {
		return m, tea.Batch(slide, m.wait())
	}
	return m, tea.Batch(slide, tea.Sequence(tea.Batch(notes...), m.wait()))
}

// wait returns a command that receives the next update, or reports the
// engine stopped once the channel is closed.
func (m Model) wait() tea.Cmd {
	updates := m.cfg.Updates
	return func() tea.Msg {
		u, ok := <-updates
		if !ok {
			return engineStoppedMsg{}
		}
		return updateMsg(u)
	}
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}
