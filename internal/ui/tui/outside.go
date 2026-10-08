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

// noteKey identifies a rule's end: one notification each (KTD6).
type noteKey struct {
	issue crew.IssueID
	rule  crew.RuleName
	ended time.Time
}

// outsideState is what the model knows outside the screen: whether the
// terminal has focus, and the rule ends it has seen this run. The program
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
// rule whose notify is off sends none, whatever the board draws (R9), and
// none does once a stop was asked for. Every entry counts as seen, so focus
// coming back sends nothing late.
func (m Model) notifications() []tea.Cmd {
	var out []tea.Cmd
	for _, e := range m.snap.Handled {
		k := noteKey{issue: e.Issue.ID(), rule: e.Rule, ended: e.Ended}
		if m.outside.seen[k] {
			continue
		}
		m.outside.seen[k] = true
		if m.outside.focused || m.stopping || m.snap.Stopping || m.muted(e.Rule) {
			continue
		}
		out = append(out, tea.Raw(ansi.Notify(noteText(e))))
	}
	return out
}

// muted reports whether the rule named name sends no notification: it is
// not in the rules, or its notify is off (R9). It looks the name up among
// the rules' notify, never among the board's columns.
func (m Model) muted(name crew.RuleName) bool {
	return !m.cfg.Notify[name]
}

// noteText says which rule ended on which issue, through which route and
// where that left the issue (R25), cleaned and capped, since the title
// comes from outside crew (KTD14).
func noteText(e core.HandledView) string {
	how := "moved to " + string(e.To)
	switch {
	case e.To == "" && e.Move == crew.MoveDropped:
		how = "its close was given up"
	case e.To == "":
		how = "closed it"
	case e.Move == crew.MoveDropped:
		how = fmt.Sprintf("its move to %s was given up", e.To)
	}
	return capped(fmt.Sprintf("crew: %s ended through %s on %s %s; %s", e.Rule, e.Route, e.Issue.Ref(),
		e.Issue.Title(), how))
}

// attention counts the Handled entries that need you, muted rules
// included.
func (m Model) attention() int {
	n := 0
	for _, e := range m.snap.Handled {
		if needsAttention(e) {
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
	case m.snap.Paused:
		return "crew · paused"
	}
	running, taking := m.actionCounts()
	var parts []string
	for _, p := range []struct {
		n    int
		what string
	}{{running, "running"}, {taking, "taking"}, {m.attention(), "needs attention"}} {
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
