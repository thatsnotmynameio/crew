package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
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

// handledSummary is how many issues crew handled, then the run's cost
// when it has one (R8).
func (m Model) handledSummary() string {
	summary := strconv.Itoa(len(m.snap.Handled))
	if cost := spendParts(m.snap.Spent); len(cost) > 0 {
		summary += " · " + cost[0]
	}
	return summary
}

// stateName is the part of to after its last colon, or all of it.
func stateName(to crew.State) string {
	state := string(to)
	if i := strings.LastIndex(state, ":"); i >= 0 {
		state = state[i+1:]
	}
	return state
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

// reasonTexts are why e needs attention: one per failed action, then one
// for a given-up move (KTD13).
func reasonTexts(e core.HandledView) []string {
	var out []string
	for _, f := range e.Failures {
		out = append(out, fmt.Sprintf("%s failed: %s", f.Action, clean(f.Reason)))
	}
	if e.Move == crew.MoveDropped {
		out = append(out, fmt.Sprintf("move to %s given up: %s", e.To, clean(e.DropReason)))
	}
	return out
}

// byAttention orders entries for Handled: those that need you first,
// then the rest, each the most recently ended first, then the most recently
// released first.
func byAttention(handled []core.HandledView) []core.HandledView {
	out := slices.Clone(handled)
	slices.Reverse(out)
	slices.SortStableFunc(out, func(a, b core.HandledView) int {
		return cmp.Or(trueFirst(needsAttention(a), needsAttention(b)), b.Ended.Compare(a.Ended))
	})
	return out
}

// trueFirst orders true before false.
func trueFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}
