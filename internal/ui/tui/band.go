package tui

import (
	"fmt"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// bandGap is the space between Queues and Events.
const bandGap = 3

// queuesSection is the Queues section: its summary and one row per queue
// with its busy and free slots (R16).
func (m Model) queuesSection() (string, []string) {
	busy, slots := 0, 0
	names, counts := make([]string, 0, len(m.snap.Queues)), make([]string, 0, len(m.snap.Queues))
	for _, q := range m.snap.Queues {
		busy, slots = busy+min(q.Busy, q.Slots), slots+q.Slots
		names = append(names, q.Name)
		counts = append(counts, fmt.Sprintf("%d/%d", q.Busy, q.Slots))
	}
	summary := fmt.Sprintf("%d of %d busy", busy, slots)
	if len(m.snap.Queues) == 0 {
		return summary, []string{" " + m.styles.muted.Render("none")}
	}
	nameW, boxW := widest(names), 0
	for _, q := range m.snap.Queues {
		boxW = max(boxW, q.Slots)
	}
	out := make([]string, 0, len(m.snap.Queues))
	for i, q := range m.snap.Queues {
		boxes := m.styles.accent.Render(strings.Repeat("■", min(q.Busy, q.Slots))) +
			m.styles.subtle.Render(strings.Repeat("□", q.Free()))
		out = append(out, " "+pad(m.styles.text.Render(names[i]), nameW)+"  "+pad(boxes, boxW)+"  "+
			m.styles.muted.Render(counts[i]))
	}
	return summary, out
}

// needsAttention reports whether e needs you: it needs attention and no
// rule holds its issue again (#109).
func needsAttention(e core.HandledView) bool { return e.NeedsAttention() && e.HeldBy == "" }

// spendParts splits a spend into its cost and its tokens; nothing when no
// session ended.
func spendParts(s crew.Spend) []string {
	if text := s.String(); text != "" {
		return strings.Split(text, ", ")
	}
	return nil
}
