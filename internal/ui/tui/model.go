// Package tui is crew's terminal UI (R17): a Bubble Tea model that only
// displays the engine's latest snapshot and asks the engine to stop. It is
// the only package that imports bubbletea (KTD17).
package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

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

// Model is the TUI's state. Use New; the program owns it after that.
type Model struct {
	updates <-chan engine.Update
	stop    func()
	force   func()
	now     func() time.Time
	loc     *time.Location

	snap engine.Snapshot
	// at is the clock's time at the last update or tick; elapsed times are
	// measured to it, so View stays a function of the model.
	at time.Time
	// width and height are the window's size in columns and rows; a zero
	// height, before the first size, fits nothing to it.
	width, height int
	// stopping is true once the boss asked to stop.
	stopping bool
	// warnings are the startup warnings, shown above everything but the top
	// line.
	warnings []string
}

// New returns a model that renders the updates of a latest-wins
// subscription (Engine.SubscribeLatest), measuring elapsed times with now
// and showing event times in loc. Each of warnings, crew's startup
// warnings, shows on its own line under the top line for as long as crew
// runs.
//
// The first Ctrl-C or q calls stop and shows "stopping…"; the model keeps
// running until updates is closed, then quits. A second Ctrl-C or q calls
// force and quits at once (KTD7). force is called from Update, while the
// terminal is still in raw mode, so it should kill what must die and return,
// leaving the exit to the caller after Program.Run returns. Bubble Tea's own signal handler should be disabled
// (tea.WithoutSignalHandler), so crew's handler is the only one.
func New(
	updates <-chan engine.Update, stop, force func(), now func() time.Time, loc *time.Location, warnings ...string,
) Model {
	return Model{
		updates: updates, stop: stop, force: force, now: now, loc: loc, at: now(), width: defaultWidth,
		warnings: warnings,
	}
}

// Init starts waiting for updates and ticking.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.wait(), tick())
}

// Update handles engine updates, ticks, window sizes and the stop keys.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateMsg:
		m.snap = msg.Snapshot
		m.at = m.now()
		return m, m.wait()
	case engineStoppedMsg:
		return m, tea.Quit
	case tickMsg:
		m.at = m.now()
		return m, tick()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.stopping {
				m.force()
				return m, tea.Quit
			}
			m.stopping = true
			m.stop()
		}
	}
	return m, nil
}

// wait returns a command that receives the next update, or reports the
// engine stopped once the channel is closed.
func (m Model) wait() tea.Cmd {
	updates := m.updates
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
