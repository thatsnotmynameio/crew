package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// full is a complete progress bar.
const full = 100

// noteKey identifies a stage's end: one notification each (KTD6).
type noteKey struct {
	issue, stage string
	ended        time.Time
}

// outsideState is what the model knows outside the screen: whether the
// terminal has focus, and the stage ends it has seen this run. The program
// owns one Model at a time, so the copies of a Model share it safely.
type outsideState struct {
	// focused starts true, so a terminal that never reports focus never
	// gets a notification (KTD6).
	focused bool
	seen    map[noteKey]bool
}

func newOutsideState() *outsideState {
	return &outsideState{focused: true, seen: map[noteKey]bool{}}
}

// notifications returns one desktop notification for each Handled entry not
// seen before, while the terminal has no focus (R25, KTD6). An entry of a
// stage with on_board: false sends none, whatever the board draws (R14,
// KTD9), and none does once a stop was asked for. Every entry counts as
// seen, so focus coming back sends nothing late.
func (m Model) notifications() []tea.Cmd {
	var out []tea.Cmd
	for _, e := range m.snap.Handled {
		k := noteKey{issue: e.Issue.Key, stage: e.Stage, ended: e.Ended}
		if m.outside.seen[k] {
			continue
		}
		m.outside.seen[k] = true
		if m.outside.focused || m.stopping || m.snap.Stopping || m.muted(e.Stage) {
			continue
		}
		out = append(out, tea.Raw(ansi.Notify(noteText(e))))
	}
	return out
}

// muted reports whether the stage named name sends no notification: it is
// not in the workflow, or on_board: false hides it from the board of the
// stages (R14).
func (m Model) muted(name string) bool {
	i := m.columnIndex(name)
	return i < 0 || m.cfg.Workflow[i].OffBoard
}

// noteText says which stage ended on which issue and how (R25), cleaned and
// capped, since the title comes from outside crew (KTD14).
func noteText(e core.HandledView) string {
	verb, how := "ended", "moved to "+string(e.To)
	switch {
	case e.Move == crew.MoveDropped:
		how = fmt.Sprintf("its move to %s was given up", e.To)
	case len(e.Failures) > 0:
		verb = "failed"
	}
	return capped(fmt.Sprintf("crew: %s %s on %s %s; %s", e.Stage, verb, e.Issue.Ref, e.Issue.Title, how))
}

// attention counts the Handled entries that need the boss, hidden stages
// included.
func (m Model) attention() int {
	n := 0
	for _, e := range m.snap.Handled {
		if needsBoss(e) {
			n++
		}
	}
	return n
}

// windowTitle is crew's state for the terminal's tab title (R23, KTD7).
func (m Model) windowTitle() string {
	switch {
	case m.stopping || m.snap.Stopping:
		return "crew · stopping"
	case m.snap.TimeUp:
		return "crew · winding down"
	}
	running, waiting := m.actionCounts()
	var parts []string
	for _, p := range []struct {
		n    int
		what string
	}{{running, "running"}, {waiting, "waiting"}, {m.attention(), "needs attention"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.what))
		}
	}
	if len(parts) == 0 {
		return "crew · idle"
	}
	return "crew · " + strings.Join(parts, " · ")
}

// progress is the tab progress indicator (R24, KTD7): an error while an
// entry needs attention, activity while an action has not ended, else none.
func (m Model) progress() *tea.ProgressBar {
	switch {
	case m.attention() > 0:
		return tea.NewProgressBar(tea.ProgressBarError, full)
	case len(m.actions()) > 0:
		return tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	}
	return nil
}
