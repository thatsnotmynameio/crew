package tui

import (
	"github.com/thatsnotmynameio/crew/internal/core"
)

// actions returns the actions in progress, in the order of their issues and
// of each issue's actions: the one each held issue's run takes the issue
// for, starts or runs, as the window title and the tab progress count them.
func (m Model) actions() []core.ActionView {
	var out []core.ActionView
	for _, iv := range m.snap.Issues {
		for _, a := range iv.Actions {
			if inProgress(a.Phase) {
				out = append(out, a)
			}
		}
	}
	return out
}

// inProgress reports whether an action in phase p is the one its issue's
// run takes the issue for, starts or runs (KTD-S17).
func inProgress(p core.Phase) bool {
	switch p {
	case core.PhaseTaking, core.PhaseCreating, core.PhaseReopening, core.PhaseStarting, core.PhaseRunning:
		return true
	case core.PhaseAwaitingTurn, core.PhaseEnded, core.PhaseNotRun, core.PhaseDoneInEarlierRun:
	}
	return false
}

// actionCounts returns how many actions run, then how many wait for their
// issue's take.
func (m Model) actionCounts() (int, int) {
	taking := 0
	acts := m.actions()
	for _, a := range acts {
		if a.Phase == core.PhaseTaking {
			taking++
		}
	}
	return len(acts) - taking, taking
}
