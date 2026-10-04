package tui

import (
	"fmt"
	"time"

	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// eventsSection is the Events section in n rows, filled from the top, the
// newest event last, scrolled back eventsOffset rows (R21, #108): each
// event's time, then the sentence the line renderer prints for it, on one
// row (R16). Only the rows shown are rendered.
func (m Model) eventsSection(n int) (string, []string) {
	recent := m.snap.Recent
	summary := fmt.Sprintf("%d %s", len(recent), lines.Plural(len(recent), "event", "events"))
	if len(recent) == 0 {
		return summary, filled([]string{" " + m.styles.muted.Render("none")}, n)
	}
	if len(recent) > n {
		off := min(max(m.eventsOffset, 0), len(recent)-n)
		recent = recent[len(recent)-n-off : len(recent)-off]
		summary += " · ↑↓ scroll"
	}
	out := make([]string, 0, n)
	for _, e := range recent {
		out = append(out, " "+m.styles.muted.Render(e.Time().In(m.cfg.Location).Format(time.TimeOnly))+" "+
			m.styles.text.Render(clean(lines.Text(e))))
	}
	return summary, filled(out, n)
}
