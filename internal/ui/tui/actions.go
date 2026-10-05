package tui

import (
	"github.com/thatsnotmynameio/crew/internal/core"
)

// actions returns the actions not yet ended, in the order of their issues
// and of each issue's actions, as the window title and the tab progress
// count them.
func (m Model) actions() []core.ActionView {
	var out []core.ActionView
	for _, iv := range m.snap.Issues {
		for _, a := range iv.Actions {
			if a.Phase != core.PhaseEnded {
				out = append(out, a)
			}
		}
	}
	return out
}

// actionCounts returns how many actions run, then how many wait for their
// issue's take.
func (m Model) actionCounts() (int, int) {
	waiting := 0
	acts := m.actions()
	for _, a := range acts {
		if a.Phase == core.PhaseWaiting {
			waiting++
		}
	}
	return len(acts) - waiting, waiting
}
