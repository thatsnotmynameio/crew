package core

import (
	"cmp"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// board is the board the model reads (KTD4).
type board struct {
	// labels are the board's labels, each spelled once, so they compare
	// exactly.
	labels []string
	// crewLabels are the labels a move removes: the rules' states.
	crewLabels []crew.State
	// issues are what the last read found, with crew's moves since applied.
	issues  []crew.BoardIssue
	reading bool   // a ListBoard is outstanding
	reads   int    // the ListBoard asked for: the generation of the last one
	failure string // why the last read failed; empty once one succeeds
	// moves are the moves of held issues that landed since the last
	// answered read was requested, which that answer may predate.
	moves []boardMove
}

// boardMove is a move of a held issue that landed, with the generation of
// the latest board read requested when it did.
type boardMove struct {
	issue crew.Issue
	to    crew.State
	read  int
}

// ListingBoard has the model read the open issues that carry any of labels,
// the board's labels, at each tick, and apply crew's moves to them as they
// land (KTD4), removing the rules' states but to's.
func ListingBoard(labels []string) Option {
	return func(m *Model) {
		m.board = &board{labels: slices.Clone(labels), crewLabels: crew.RuleStates(m.rules)}
	}
}

// readBoard asks for the board's issues, unless the model reads no board or
// a read is outstanding. The tick calls it before its busy check, so the
// board is read even when no issue can be taken (R9).
func (s *step) readBoard() {
	b := s.m.board
	if b == nil || b.reading {
		return
	}
	b.reading = true
	b.reads++
	s.command(ListBoard{Labels: slices.Clone(b.labels)})
}

// boardListed replaces the board with issues, the answer to read
// generation b.reads, since only one read is outstanding at a time. That
// read may predate the moves that landed while it was outstanding, so they
// are applied again; the older moves are forgotten, since it found them.
func (m *Model) boardListed(issues []crew.BoardIssue) {
	b := m.board
	if b == nil {
		return
	}
	b.reading, b.failure = false, ""
	b.issues = make([]crew.BoardIssue, len(issues))
	for i, issue := range issues {
		b.issues[i] = issue.Clone()
	}
	b.moves = slices.DeleteFunc(b.moves, func(mv boardMove) bool { return mv.read < b.reads })
	for _, mv := range b.moves {
		b.apply(mv)
	}
}

// boardListFailed keeps the last board and records why the read failed.
func (m *Model) boardListFailed(reason string) {
	if b := m.board; b != nil {
		b.reading, b.failure = false, reason
	}
}

// boardMoved applies at once the move of held issue to to, which landed,
// and records it for the read that may predate it.
func (m *Model) boardMoved(issue crew.Issue, to crew.State) {
	b := m.board
	if b == nil {
		return
	}
	mv := boardMove{issue: issue.Clone(), to: to, read: b.reads}
	b.moves = append(b.moves, mv)
	b.apply(mv)
}

// apply moves mv's issue on the board: it loses every crew label and gains
// mv's target when the board names it. An issue not on the board joins it
// from crew's copy when the board names the target, unless it is a pull
// request; an issue left with no board label leaves it.
func (b *board) apply(mv boardMove) {
	to := string(mv.to)
	named := slices.Contains(b.labels, to)
	i := slices.IndexFunc(b.issues, func(e crew.BoardIssue) bool { return e.Issue.Key == mv.issue.Key })
	if i < 0 {
		if named && mv.issue.Kind != crew.KindPullRequest {
			b.issues = append(b.issues, crew.BoardIssue{Issue: mv.issue.Clone(), Labels: []string{to}})
		}
		return
	}
	e := &b.issues[i]
	e.Labels = slices.DeleteFunc(e.Labels, func(l string) bool { return slices.Contains(b.crewLabels, crew.State(l)) })
	if named {
		e.Labels = append(e.Labels, to)
	}
	if len(e.Labels) == 0 {
		b.issues = slices.Delete(b.issues, i, i+1)
	}
}

// view returns a copy of the board's issues, oldest first and then by key
// (KTD6); nil when there are none.
func (b *board) view() []crew.BoardIssue {
	if len(b.issues) == 0 {
		return nil
	}
	out := make([]crew.BoardIssue, len(b.issues))
	for i, e := range b.issues {
		out[i] = e.Clone()
	}
	slices.SortStableFunc(out, func(x, y crew.BoardIssue) int {
		if c := x.Issue.Created.Compare(y.Issue.Created); c != 0 {
			return c
		}
		return cmp.Compare(x.Issue.Key, y.Issue.Key)
	})
	return out
}
