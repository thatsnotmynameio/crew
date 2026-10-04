package tui

import (
	"fmt"
	"time"

	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// eventRows returns one row per recent event, oldest first: its time, then
// the sentence the line renderer prints for it, on one row (R16).
func (m Model) eventRows() []string {
	if len(m.snap.Recent) == 0 {
		return []string{" " + m.styles.muted.Render("none")}
	}
	out := make([]string, 0, len(m.snap.Recent))
	for _, e := range m.snap.Recent {
		out = append(out, " "+m.styles.muted.Render(e.Time().In(m.cfg.Location).Format(time.TimeOnly))+" "+
			m.styles.text.Render(clean(lines.Text(e))))
	}
	return out
}

// eventsSection is the Events section with at most n rows, the newest at
// the bottom, scrolled back eventsOffset rows (R21).
func (m Model) eventsSection(n int) (string, []string) {
	rows := m.eventRows()
	summary := fmt.Sprintf("%d %s", len(m.snap.Recent), lines.Plural(len(m.snap.Recent), "event", "events"))
	if len(rows) <= n {
		return summary, rows
	}
	off := min(max(m.eventsOffset, 0), len(rows)-n)
	return summary + " · ↑↓ scroll", rows[len(rows)-n-off : len(rows)-off]
}
