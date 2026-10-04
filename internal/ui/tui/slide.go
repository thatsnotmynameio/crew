package tui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// A slide's frames (KTD10).
const (
	slideFrames = 12
	slideFrame  = 50 * time.Millisecond
)

// slideTickMsg advances the slides one frame.
type slideTickMsg struct{}

// slide is a card's move from one column to another (R14): a marker that
// crosses the underline row while the card already shows in its new
// column.
type slide struct {
	key, ref string
	// from and to are workflow indexes.
	from, to int
	frame    int
}

// boardMemory is what the board remembers across snapshots: each issue's
// last column this run, and the slides running. The program owns one Model
// at a time, so the copies of a Model share it safely.
type boardMemory struct {
	last    map[string]int
	slides  []slide
	ticking bool
}

func newBoardMemory() *boardMemory { return &boardMemory{last: map[string]int{}} }

// moved starts a slide for each card that shows in another column than the
// last one it had this run, remembers each card's column, and returns the
// frame tick when a slide needs one.
func (b *boardMemory) moved(cards []card) tea.Cmd {
	for _, c := range cards {
		if prev, ok := b.last[c.issue.Key]; ok && prev != c.column {
			b.slides = slices.DeleteFunc(b.slides, func(s slide) bool { return s.key == c.issue.Key })
			b.slides = append(b.slides, slide{key: c.issue.Key, ref: c.issue.Ref, from: prev, to: c.column})
		}
		b.last[c.issue.Key] = c.column
	}
	return b.schedule()
}

// advance moves every slide one frame, ends the finished ones, and returns
// the next frame tick while any runs.
func (b *boardMemory) advance() tea.Cmd {
	b.ticking = false
	for i := range b.slides {
		b.slides[i].frame++
	}
	b.slides = slices.DeleteFunc(b.slides, func(s slide) bool { return s.frame >= slideFrames })
	return b.schedule()
}

func (b *boardMemory) schedule() tea.Cmd {
	if len(b.slides) == 0 || b.ticking {
		return nil
	}
	b.ticking = true
	return tea.Tick(slideFrame, func(time.Time) tea.Msg { return slideTickMsg{} })
}

// underline is the row under the column names: a rule under each column,
// with each slide's marker drawn where its frame has got to (KTD10).
func (m Model) underline(l boardLayout) string {
	prefix := 1
	if l.before > 0 || l.after > 0 {
		prefix += edgeMarker
	}
	row := []rune(strings.Repeat(" ", prefix))
	for i := range l.columns {
		if i > 0 {
			row = append(row, []rune(strings.Repeat(" ", columnGap))...)
		}
		row = append(row, []rune(strings.Repeat("─", l.width))...)
	}
	marks := make([]bool, len(row))
	for _, s := range m.memory.slides {
		marker := []rune(s.ref + " ▸")
		from, to := m.columnX(l, prefix, s.from), m.columnX(l, prefix, s.to)
		if to < from {
			marker = []rune("◂ " + s.ref)
		}
		t := float64(s.frame) / slideFrames
		eased := 1 - (1-t)*(1-t)*(1-t)
		x := min(max(from+int(float64(to-from)*eased), 0), max(len(row)-len(marker), 0))
		for j, r := range marker {
			if x+j < len(row) {
				row[x+j], marks[x+j] = r, true
			}
		}
	}
	return m.marked(row, marks)
}

// marked renders row, its marked runes in the accent colour and the rest
// in the subtle one.
func (m Model) marked(row []rune, marks []bool) string {
	var b strings.Builder
	for start := 0; start < len(row); {
		end := start
		for end < len(row) && marks[end] == marks[start] {
			end++
		}
		st := m.styles.subtle
		if marks[start] {
			st = m.styles.accent
		}
		b.WriteString(st.Render(string(row[start:end])))
		start = end
	}
	return b.String()
}

// columnX is the x of workflow column c on the underline row: its own x
// when drawn; the left edge for a hidden stage; else the edge on its side
// of the drawn columns (KTD10).
func (m Model) columnX(l boardLayout, prefix, c int) int {
	step := l.width + columnGap
	if i := slices.Index(l.columns, c); i >= 0 {
		return prefix + i*step
	}
	if !m.shown(c) || len(l.columns) == 0 || c < l.columns[0] {
		return 0
	}
	return prefix + len(l.columns)*step
}
