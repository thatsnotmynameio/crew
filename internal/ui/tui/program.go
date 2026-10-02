package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
)

// Program runs a Model in the terminal. It exists so that crew's wiring can
// start the TUI without importing bubbletea, which only this package does
// (KTD17).
type Program struct {
	p *tea.Program
}

// NewProgram returns a program that runs m, drawing to out and reading keys
// from in; a nil in means the standard input, or the terminal when the
// standard input is not one. Bubble Tea's own signal handler is disabled, so
// crew's handler is the only one (KTD7).
func NewProgram(m Model, in io.Reader, out io.Writer) *Program {
	opts := []tea.ProgramOption{tea.WithOutput(out), tea.WithoutSignalHandler()}
	if in != nil {
		opts = append(opts, tea.WithInput(in))
	}
	return &Program{p: tea.NewProgram(m, opts...)}
}

// Run runs the program until the model quits, or Quit is called, and
// restores the terminal before it returns.
func (p *Program) Run() error {
	if _, err := p.p.Run(); err != nil {
		return fmt.Errorf("run the TUI: %w", err)
	}
	return nil
}

// Quit makes Run return. It is safe from any goroutine; before Run starts it
// waits for Run, and once Run has returned it does nothing.
func (p *Program) Quit() {
	p.p.Quit()
}
