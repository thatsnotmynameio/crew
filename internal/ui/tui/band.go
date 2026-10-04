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

const (
	// bandGap is the space between Queues and Handled.
	bandGap = 3
	// minHandledTitle is the narrowest a Handled title gets: the title
	// says which issue it is, so the details give way first.
	minHandledTitle = 20
)

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

// handledSection is the Handled section: its summary and its rows, the
// entries that need attention first, each with its pill, reference, title,
// stage, time, spend and pull request, then its reasons and its actions'
// pull requests (R5, R16), in width cells.
func (m Model) handledSection(width int) (string, []string) {
	summary := strconv.Itoa(len(m.snap.Handled))
	if cost := spendParts(m.snap.Spent); len(cost) > 0 {
		summary += " · " + cost[0]
	}
	entries := byAttention(m.snap.Handled)
	if len(entries) == 0 {
		return summary, []string{" " + m.styles.muted.Render("none")}
	}
	var pills, refs, titles, details []string
	for _, e := range entries {
		pills = append(pills, m.pill(e))
		refs = append(refs, e.Issue.Ref)
		titles = append(titles, clean(e.Issue.Title))
		details = append(details, m.handledDetails(e))
	}
	pillW, refW := widest(pills), widest(refs)
	titleW := min(max(width-(len(" ")+pillW+len("  ")+refW+len(" ")+len("  ")+widest(details)), minHandledTitle),
		widest(titles))
	var out []string
	for i, e := range entries {
		out = append(out, " "+pad(pills[i], pillW)+"  "+pad(m.styles.link(refs[i], e.Issue.URL), refW)+" "+
			pad(m.styles.text.Render(titles[i]), titleW)+"  "+details[i])
		out = append(out, m.reasons(e)...)
		out = append(out, m.pullRequests(e)...)
	}
	return summary, out
}

// pill says how e ended: given up, needing attention, or the last part of
// the state its stage moved it to (R5, KTD13).
func (m Model) pill(e core.HandledView) string {
	switch {
	case e.Move == crew.MoveDropped:
		return m.styles.warningPill.Render("GIVEN UP")
	case e.NeedsAttention():
		return m.styles.errorPill.Render("NEEDS ATTENTION")
	}
	state := string(e.To)
	if i := strings.LastIndex(state, ":"); i >= 0 {
		state = state[i+1:]
	}
	return m.styles.successPill.Render(strings.ToUpper(state))
}

// handledDetails is e's stage and time, then its spend and pull request
// when it has them.
func (m Model) handledDetails(e core.HandledView) string {
	parts := []string{m.styles.muted.Render(e.Stage + " " + elapsed(e.Duration()))}
	if cost := spendParts(e.Spend()); len(cost) > 0 {
		parts = append(parts, m.styles.text.Render(strings.Join(cost, " · ")))
	}
	if pr := m.pullRequestOf(e); pr != "" {
		parts = append(parts, pr)
	}
	return strings.Join(parts, m.styles.muted.Render(" · "))
}

// spendParts splits a spend into its cost and its tokens; nothing when no
// session ended.
func spendParts(s crew.Spend) []string {
	if text := s.String(); text != "" {
		return strings.Split(text, ", ")
	}
	return nil
}

// reasons are the rows under an entry that needs attention: one per failed
// action, then one for a given-up move, each after an × (KTD13).
func (m Model) reasons(e core.HandledView) []string {
	var out []string
	cross := "   " + m.styles.error.Render("×") + " "
	for _, f := range e.Failures {
		out = append(out, cross+m.styles.text.Render(fmt.Sprintf("%s failed: %s", f.Action, clean(f.Reason))))
	}
	if e.Move == crew.MoveDropped {
		out = append(out, cross+m.styles.text.Render(fmt.Sprintf("move to %s given up: %s", e.To, clean(e.DropReason))))
	}
	return out
}

// pullRequestOf is the pull request on e's row: that of its one action,
// when it had a session; a stage of several actions gives each its own row.
func (m Model) pullRequestOf(e core.HandledView) string {
	if len(e.Actions) != 1 || e.Actions[0].Spend.Sessions == 0 {
		return ""
	}
	return m.pullRequest(e.Actions[0].PullRequest)
}

// pullRequest is pr as a link when it was found, or what its lookup said.
func (m Model) pullRequest(pr crew.PullRequest) string {
	if pr.Lookup == crew.PullRequestFound {
		return m.styles.link(pr.Ref, pr.URL)
	}
	return m.styles.muted.Render(pr.String())
}

// pullRequests are the rows under an entry of several actions: one per
// action that had a session, with its pull request.
func (m Model) pullRequests(e core.HandledView) []string {
	if len(e.Actions) <= 1 {
		return nil
	}
	var out []string
	for _, a := range e.Actions {
		if a.Spend.Sessions > 0 {
			out = append(out, "    "+m.styles.muted.Render(a.Name+": ")+m.pullRequest(a.PullRequest))
		}
	}
	return out
}

// byAttention orders entries for Handled: those that need attention first,
// then the rest, each the most recently ended first, then the most recently
// released first.
func byAttention(handled []core.HandledView) []core.HandledView {
	out := slices.Clone(handled)
	slices.Reverse(out)
	slices.SortStableFunc(out, func(a, b core.HandledView) int {
		return cmp.Or(trueFirst(a.NeedsAttention(), b.NeedsAttention()), b.Ended.Compare(a.Ended))
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
