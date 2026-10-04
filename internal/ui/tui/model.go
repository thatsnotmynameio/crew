// Package tui is crew's terminal UI (R17): a Bubble Tea model that only
// displays the engine's latest snapshot, tells the terminal crew's state, and
// asks the engine to stop. It is the only package that imports Bubble Tea,
// Lip Gloss and Bubbles (KTD17).
package tui

import (
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
	// Workflow is the configured stages, in config order: the board's
	// columns (R8).
	Workflow []crew.Stage
	// Repository is the repository's name, for the header (R3).
	Repository string
	// Warnings are crew's startup warnings, each shown under the header for
	// as long as crew runs (R2).
	Warnings []string
}

// focus is the section the scroll keys move (R21).
type focus int

// The sections that take focus, in tab order.
const (
	focusNone focus = iota
	focusHandled
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
	// stopping is true once the boss asked to stop.
	stopping bool

	styles  styles
	keys    keyMap
	spinner spinner.Model
	// help is set while the keys show over the view (R20).
	help  bool
	focus focus
	// boardOffset is the first board column shown when the columns that
	// hold cards do not fit (KTD9).
	boardOffset int
	// handledOffset counts the Handled rows scrolled past at the top;
	// eventsOffset the Events rows scrolled back from the newest (KTD11).
	handledOffset, eventsOffset int

	// memory remembers each issue's last column this run and the slides
	// running (KTD10).
	memory *boardMemory
	// outside tracks focus reports and the stage ends already notified
	// (KTD6).
	outside *outsideState
}

// New returns a model that renders the updates of cfg.Updates.
//
// The first Ctrl-C or q calls Stop and shows that crew is stopping; the
// model keeps running until Updates is closed, then quits. A second Ctrl-C
// or q calls Force and quits at once (KTD7). Force is called from Update,
// while the terminal is still in raw mode, so it should kill what must die
// and return, leaving the exit to the caller after Program.Run returns.
// Bubble Tea's own signal handler should be disabled
// (tea.WithoutSignalHandler), so crew's handler is the only one.
func New(cfg Config) Model {
	return Model{
		cfg: cfg, at: cfg.Now(), width: defaultWidth,
		styles:  newStyles(true),
		keys:    newKeyMap(),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		memory:  newBoardMemory(),
		outside: newOutsideState(),
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

// updated takes in a new snapshot: it starts the slides of the cards that
// moved and sends the notifications of the stages that ended, before it
// waits for the next update, so the read that finds the channel closed, and
// with it the quit, comes after them (KTD6, KTD10).
func (m Model) updated(u engine.Update) (tea.Model, tea.Cmd) {
	m.snap = u.Snapshot
	m.at = m.cfg.Now()
	slide := m.memory.moved(m.cards())
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
