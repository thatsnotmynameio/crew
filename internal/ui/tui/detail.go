package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// tableGap is the space between the columns of the popup's actions table.
const tableGap = 2

// The states of an action that ended: done, or failed.
const (
	doneState   = "done"
	failedState = "failed"
)

// popupHeader is the popup's header rows, each value after its muted
// label: the rule, the labels as chips, the kind, the priority and its
// URL (R15, KTD8 of #151). Whether the issue is blocked shows among the
// chips, not as a row (R4 of #229).
func (m Model) popupHeader(c card) []string {
	s := m.styles
	none := s.muted.Render("none")
	rule := none
	if c.held {
		rule = s.text.Render(clean(string(c.view.Rule)))
	}
	priority := none
	if c.issue.Priority > 0 {
		priority = s.text.Render(fmt.Sprintf("P%d", c.issue.Priority))
	}
	url := none
	if c.issue.URL != "" {
		url = s.link(clean(c.issue.URL), c.issue.URL)
	}
	labels := []string{"rule", "labels", "kind", "priority", "url"}
	values := []string{rule, m.chips(c), s.text.Render(lines.KindName(c.issue.Kind)), priority, url}
	width := widest(labels)
	out := make([]string, 0, len(labels))
	for i, l := range labels {
		out = append(out, pad(s.muted.Render(l), width)+" "+values[i])
	}
	return out
}

// chips are c's labels as chips: its issue's crew states, then the board
// labels its board item carries, each once (R15, KTD8 of #151). A blocked
// chip follows them when the issue is blocked, read from its board item,
// fresh on every poll, and from c's issue only off the board: core keeps a
// held issue as it took it, unblocked (R3, KTD2, KTD3 of #229).
func (m Model) chips(c card) string {
	i := slices.IndexFunc(m.snap.Board, func(b crew.BoardIssue) bool { return b.Issue.ID == c.issue.ID })
	var labels []string
	switch {
	case c.held:
		labels = states(c.view.Issue)
	case i >= 0:
		labels = states(m.snap.Board[i].Issue)
	}
	if i >= 0 {
		for _, l := range m.snap.Board[i].Labels {
			labels = append(labels, string(l))
		}
	}
	blocked := c.issue.Blocked
	if i >= 0 {
		blocked = m.snap.Board[i].Issue.Blocked
	}
	out := make([]string, 0, len(labels)+1)
	// A label named blocked on a blocked issue gives way to the blocked
	// chip, so the popup does not show blocked twice.
	seen := map[string]bool{"blocked": blocked}
	for _, l := range labels {
		if l = clean(l); l != "" && !seen[l] {
			seen[l] = true
			out = append(out, m.styles.chip.Render(l))
		}
	}
	if blocked {
		out = append(out, m.styles.blockedChip.Render("blocked"))
	}
	if len(out) == 0 {
		return m.styles.muted.Render("none")
	}
	return strings.Join(out, " ")
}

// states are issue's crew states as text.
func states(issue crew.Issue) []string {
	out := make([]string, 0, len(issue.States))
	for _, st := range issue.States {
		out = append(out, string(st))
	}
	return out
}

// actionRow is a row of the popup's actions table, and what shows under
// it: the action's last message, or why it failed.
type actionRow struct {
	cells  []string
	note   string
	failed bool
}

// popupActions is the popup's actions table, inner cells wide, each
// action's note under its row: a held issue's actions, or a muted no
// actions (R16, R17, KTD8 of #151).
func (m Model) popupActions(c card, inner int) []string {
	var rows []actionRow
	if c.held {
		for _, a := range c.view.Actions {
			rows = append(rows, m.liveActionRow(c, a))
		}
	}
	if len(rows) == 0 {
		return []string{m.styles.muted.Render("no actions")}
	}
	return m.table([]string{"action", "bot", "queue", "state", "branch"}, rows, inner)
}

// liveActionRow is held action a's row: its bot, queue, state and branch,
// then why it failed or what it last said.
func (m Model) liveActionRow(c card, a core.ActionView) actionRow {
	message, branch := m.messages.last(c.issue.ID, a.Name)
	if b := clean(a.Branch); b != "" {
		branch = b
	}
	row := actionRow{note: message}
	state := m.actionState(a)
	if a.Phase == core.PhaseEnded {
		state = doneState
		if !a.Outcome.Succeeded {
			state, row.note, row.failed = failedState, clean(a.Outcome.Reason), true
		}
	}
	row.cells = []string{
		m.styles.text.Render(clean(string(a.Name))), m.actionBot(c.issue.Ref, c.view.Rule, a.Name),
		m.styles.text.Render(clean(string(c.view.Queue))), m.styles.text.Render(state), m.styles.text.Render(branch),
	}
	return row
}

// actionState is what a not yet ended does, with how long it has run
// while its session runs or its check does.
func (m Model) actionState(a core.ActionView) string {
	state := a.Phase.String()
	if a.Phase != core.PhaseRunning && a.Phase != core.PhaseChecking {
		return state
	}
	state += " " + elapsed(m.at.Sub(a.Started))
	if a.Resumed {
		state = "resumed in " + clean(string(a.Workspace)) + ", " + state
	}
	return state
}

// actionBot is the bot rule/action of the issue ref acts as: the entry
// it runs as now, else the first whose pairs hold it and that can act,
// as its mark and name; empty when no entry has it (KTD8 of #151).
func (m Model) actionBot(ref string, rule crew.RuleName, action crew.ActionName) string {
	runs := func(e core.BotView) bool {
		return slices.ContainsFunc(e.Running, func(r core.RunningAction) bool {
			return r.IssueRef == ref && r.Rule == rule && r.Action == action
		})
	}
	i := slices.IndexFunc(m.snap.Bots, runs)
	if i < 0 {
		i = slices.IndexFunc(m.snap.Bots, func(e core.BotView) bool {
			return !e.ActsAsYou && slices.Contains(e.Pairs, string(rule)+"/"+string(action))
		})
	}
	if i < 0 {
		return ""
	}
	e := m.snap.Bots[i]
	return m.styles.botName(e)
}

// table lays rows out under a muted header row, inner cells wide, its
// widest columns cut until the rows fit, each row's note under it.
func (m Model) table(header []string, rows []actionRow, inner int) []string {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = lipgloss.Width(h)
		for _, r := range rows {
			widths[i] = max(widths[i], lipgloss.Width(r.cells[i]))
		}
	}
	for total(widths) > inner && slices.Max(widths) > 1 {
		widths[slices.Index(widths, slices.Max(widths))]--
	}
	line := func(cells []string) string {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = pad(c, widths[i])
		}
		return strings.Join(parts, strings.Repeat(" ", tableGap))
	}
	muted := make([]string, len(header))
	for i, h := range header {
		muted[i] = m.styles.muted.Render(h)
	}
	out := make([]string, 0, 1+len(rows)*2)
	out = append(out, line(muted))
	for _, r := range rows {
		out = append(out, line(r.cells))
		out = append(out, m.note(r, inner)...)
	}
	return out
}

// total is the cells a table row of columns widths takes.
func total(widths []int) int {
	sum := (len(widths) - 1) * tableGap
	for _, w := range widths {
		sum += w
	}
	return sum
}

// note is r's note after └, word-wrapped to inner cells, each row after
// the first indented under its text: muted, or in the error colour when
// it says why the action failed; nothing without one (R17, KTD8 of #151).
func (m Model) note(r actionRow, inner int) []string {
	if r.note == "" {
		return nil
	}
	st := m.styles.muted
	if r.failed {
		st = m.styles.error
	}
	const lead = "└ "
	indent := lipgloss.Width(lead)
	wrapped := strings.Split(ansi.Wrap(r.note, max(inner-indent, 1), ""), "\n")
	out := make([]string, 0, len(wrapped))
	for i, l := range wrapped {
		prefix := strings.Repeat(" ", indent)
		if i == 0 {
			prefix = lead
		}
		out = append(out, st.Render(prefix+strings.TrimRight(l, " ")))
	}
	return out
}

// popupEvents are the recent events of c's issue, oldest first, each with
// its time and the line renderer's sentence, or a muted none (R19, KTD8
// of #151).
func (m Model) popupEvents(c card) []string {
	var out []string
	for _, e := range m.snap.Recent {
		if eventIssue(e) == c.issue.ID {
			out = append(out, m.styles.muted.Render(e.Time().In(m.cfg.Location).Format(time.TimeOnly))+" "+
				m.styles.text.Render(clean(lines.Text(e))))
		}
	}
	if len(out) == 0 {
		return []string{m.styles.muted.Render("none")}
	}
	return out
}

// eventIssue is the id of the issue e is about, or the zero id for an
// event about no one issue (KTD8 of #151).
func eventIssue(e core.Event) crew.IssueID {
	switch e := e.(type) {
	case core.IssueTaken:
		return e.Issue.ID
	case core.ActionStarted:
		return e.IssueID
	case core.WorkspaceMissing:
		return e.IssueID
	case core.RunNotRecorded:
		return e.IssueID
	case core.ActionEnded:
		return e.IssueID
	case core.IssueMoved:
		return e.IssueID
	case core.FailureReported:
		return e.IssueID
	case core.IssueSkipped:
		return e.IssueID
	case core.IssueOfOtherKind:
		return e.IssueID
	case core.StatusFailed:
		return e.IssueID
	case core.CallOwed:
		return e.Call.IssueID
	case core.CallDropped:
		return e.Call.IssueID
	}
	return crew.IssueID{}
}
