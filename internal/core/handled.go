package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// handledEntry is the released rule run of a handled entry, with what the
// core folded over it: the listing generation when its ending move landed
// or was given up, as only a later listing marks it Gone (KTD4), whether it
// is gone, and what the rules that ended on the issue before it spent. The
// view builds the entry's HandledView afresh from the run each time
// (KTD16).
type handledEntry struct {
	run     crew.RuleRun
	landed  int
	gone    bool
	earlier crew.Spend
}

// handle keeps the handled entry of h's run, which replaces the issue's
// earlier one, when its ending settled. A rule without actions that ended
// well keeps an earlier entry that ended well too, marked Gone: its move
// took the issue out of the entry's To (#109, R10, KTD6).
func (m *Model) handle(h *heldRun) {
	entry := handledEntry{run: h.run, landed: h.landed}
	view, ok := entry.view()
	if !ok {
		return
	}
	i := slices.IndexFunc(m.handled, func(e handledEntry) bool { return e.run.Issue().ID() == h.id() })
	if i >= 0 {
		old, _ := m.handled[i].view()
		if len(m.rules[h.rule].Actions) == 0 && !view.NeedsAttention() && !old.NeedsAttention() {
			m.handled[i].gone = true
			return
		}
		entry.earlier = old.Spend().Add(old.Earlier)
		m.handled = slices.Delete(m.handled, i, i+1)
	}
	m.handled = append(m.handled, entry)
}

// ending returns the settled ending of e's run, and false when the run
// was released without one.
func (e handledEntry) ending() (crew.SettledEnding, bool) {
	released, _ := e.run.Phase().(crew.ReleasedPhase)
	return released.Ending.Get()
}

// view returns e as the view shows it, built from its run, and false when
// the run was released without an ending.
func (e handledEntry) view() (HandledView, bool) {
	ending, ok := e.ending()
	if !ok {
		return HandledView{}, false
	}
	run := e.run
	view := HandledView{
		Issue: run.Issue(), Rule: run.Rule(), To: ending.Ending.To, Failures: ending.Ending.Failures,
		Move: crew.MoveDone, Gone: e.gone, Taken: run.Taken(), Ended: ending.Ended, Earlier: e.earlier,
	}
	if givenUp, ok := ending.Move.(crew.EndingGivenUp); ok {
		view.Move, view.DropReason = crew.MoveDropped, givenUp.Reason
	}
	for _, a := range run.Actions() {
		view.Actions = append(view.Actions, HandledAction{Name: a.Name(), Spend: a.Spend(), PullRequest: a.PullRequest()})
	}
	return view, true
}
