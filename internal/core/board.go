package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// board is the board the model reads (KTD4): columns of labels, each
// showing the items of its kind (KTD10).
type board struct {
	// columns are the board's columns, in board order.
	columns []crew.BoardColumn
	// labels are the board's labels, each spelled once, so they compare
	// exactly.
	labels []crew.State
	// crewLabels are the labels a move removes: the rules' states.
	crewLabels []crew.State
	// listed is set when the model fills the board from its own listings
	// rather than by reading it through ListBoard (KTD10).
	listed bool
	// issues are what the last read found, with crew's moves since applied.
	issues  []crew.BoardIssue
	reading bool   // a ListBoard is outstanding
	reads   int    // the reads asked for: the generation of the last one
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

// ListingBoard has the model read the open issues that carry any label of
// columns, a board the config writes, at each tick, and apply crew's moves
// to them as they land (KTD4), removing the rules' states but to's.
func ListingBoard(columns []crew.BoardColumn) Option {
	return func(m *Model) { m.board = newBoard(columns, crew.RuleStates(m.rules), false) }
}

// BoardFromListings has the model fill the board of columns, the default
// board, from its own listings, which ask for every rule's ready and
// running labels: each listed item with a card in the columns of its kind
// whose labels it carries. It applies crew's moves as ListingBoard does, and
// reads no board through ListBoard (KTD10).
func BoardFromListings(columns []crew.BoardColumn) Option {
	return func(m *Model) { m.board = newBoard(columns, crew.RuleStates(m.rules), true) }
}

// newBoard returns the board of columns, whose moves remove crewLabels.
func newBoard(columns []crew.BoardColumn, crewLabels []crew.State, listed bool) *board {
	columns = slices.Clone(columns)
	for i := range columns {
		columns[i].Labels = slices.Clone(columns[i].Labels)
	}
	return &board{columns: columns, labels: crew.BoardLabels(columns), crewLabels: crewLabels, listed: listed}
}

// readBoard asks for the board's issues, unless the model reads no board or
// a read is outstanding. The tick calls it before its busy check, so the
// board is read even when no issue can be taken (R9).
func (s *step) readBoard() {
	b := s.m.board
	if b == nil || b.listed || b.reading {
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
	b.issues = slices.Clone(issues)
	b.moves = slices.DeleteFunc(b.moves, func(mv boardMove) bool { return mv.read < b.reads })
	for _, mv := range b.moves {
		b.apply(mv)
	}
}

// listingAsked counts a listing as a read of a board filled from the
// listings, so the moves that land while it is outstanding are applied
// again to its answer.
func (m *Model) listingAsked() {
	if b := m.board; b != nil && b.listed {
		b.reads++
	}
}

// boardFromListing fills a board filled from the listings with issues, the
// listing's answer (KTD10).
func (m *Model) boardFromListing(issues []crew.Issue) {
	b := m.board
	if b == nil || !b.listed {
		return
	}
	var found []crew.BoardIssue
	for _, issue := range issues {
		var labels []crew.State
		for _, l := range b.labels {
			if slices.Contains(issue.States(), l) && b.names(issue.Kind(), l) {
				labels = append(labels, l)
			}
		}
		if len(labels) > 0 {
			found = append(found, crew.NewBoardIssue(issue, labels))
		}
	}
	m.boardListed(found)
}

// listingFailed records why the listing failed on a board filled from the
// listings.
func (m *Model) listingFailed(reason string) {
	if b := m.board; b != nil && b.listed {
		m.boardListFailed(reason)
	}
}

// boardListFailed keeps the last board and records why the read failed.
func (m *Model) boardListFailed(reason string) {
	if b := m.board; b != nil {
		b.reading, b.failure = false, reason
	}
}

// boardMoved applies at once the move of held issue to to, or its close
// when to is empty, which landed, and records it for the read that may
// predate it.
func (m *Model) boardMoved(issue crew.Issue, to crew.State) {
	b := m.board
	if b == nil {
		return
	}
	mv := boardMove{issue: issue, to: to, read: b.reads}
	b.moves = append(b.moves, mv)
	b.apply(mv)
}

// names reports whether a column showing items of kind names label.
func (b *board) names(kind crew.Kind, label crew.State) bool {
	return slices.ContainsFunc(b.columns, func(c crew.BoardColumn) bool {
		return c.Takes == kind && slices.Contains(c.Labels, label)
	})
}

// apply moves mv's issue on the board: it loses every crew label and gains
// mv's target when a column of its kind names it. An issue not on the board
// joins it from crew's copy when such a column names the target; an issue
// left with no board label leaves it, and so does an issue mv closes, as no
// read lists a closed issue.
func (b *board) apply(mv boardMove) {
	to := mv.to
	named := to != "" && b.names(mv.issue.Kind(), to)
	i := slices.IndexFunc(b.issues, func(e crew.BoardIssue) bool { return e.Issue().ID() == mv.issue.ID() })
	if i >= 0 && to == "" {
		b.issues = slices.Delete(b.issues, i, i+1)
		return
	}
	if i < 0 {
		if named {
			b.issues = append(b.issues, crew.NewBoardIssue(mv.issue, []crew.State{to}))
		}
		return
	}
	e := b.issues[i]
	labels := slices.DeleteFunc(e.Labels(), func(l crew.State) bool { return slices.Contains(b.crewLabels, l) })
	if named {
		labels = append(labels, to)
	}
	if len(labels) == 0 {
		b.issues = slices.Delete(b.issues, i, i+1)
		return
	}
	b.issues[i] = crew.NewBoardIssue(e.Issue(), labels)
}

// view returns a copy of the board's issues, oldest first and then by id
// (KTD6); nil when there are none.
func (b *board) view() []crew.BoardIssue {
	if len(b.issues) == 0 {
		return nil
	}
	out := slices.Clone(b.issues)
	slices.SortStableFunc(out, func(x, y crew.BoardIssue) int {
		if c := x.Issue().Created().Compare(y.Issue().Created()); c != 0 {
			return c
		}
		return x.Issue().ID().Compare(y.Issue().ID())
	})
	return out
}
