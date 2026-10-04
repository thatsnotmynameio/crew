package tui

import (
	"fmt"
	"time"

	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// eventCount is the rows Events takes when it has room for all: one per
// recent event, or one saying there is none.
func (m Model) eventCount() int { return max(len(m.snap.Recent), 1) }

// eventsSection is the Events section with at most n rows, the newest at
// the bottom, scrolled back eventsOffset rows (R21): each event's time, then
// the sentence the line renderer prints for it, on one row (R16). Only the
// rows shown are rendered.
func (m Model) eventsSection(n int) (string, []string) {
	recent := m.snap.Recent
	summary := fmt.Sprintf("%d %s", len(recent), lines.Plural(len(recent), "event", "events"))
	if len(recent) == 0 {
		return summary, []string{" " + m.styles.muted.Render("none")}
	}
	if len(recent) > n {
		off := min(max(m.eventsOffset, 0), len(recent)-n)
		recent = recent[len(recent)-n-off : len(recent)-off]
		summary += " · ↑↓ scroll"
	}
	out := make([]string, 0, len(recent))
	for _, e := range recent {
		out = append(out, " "+m.styles.muted.Render(e.Time().In(m.cfg.Location).Format(time.TimeOnly))+" "+
			m.styles.text.Render(clean(lines.Text(e))))
	}
	return summary, out
}
