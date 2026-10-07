package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// handledEntry is the released rule run of a handled entry, with what the
// core folded over it: the listing generation when its route's final step
// settled, as only a later listing marks it Gone (KTD4), whether it
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
// earlier one, when it ended through a route. A rule without actions that ended
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

// ending returns the route e's run ended through, with how its steps
// settled, and false when the run was released without one.
func (e handledEntry) ending() (crew.RoutingPhase, bool) {
	released, _ := e.run.Phase().(crew.ReleasedPhase)
	return released.Route.Get()
}

// view returns e as the view shows it, built from its run, and false when
// the run was released without a route. A run that ended through a route
// other than passed carries the action that ended its sequence as its
// failure; a final move or close that did not land is given up.
func (e handledEntry) view() (HandledView, bool) {
	route, ok := e.ending()
	if !ok {
		return HandledView{}, false
	}
	run := e.run
	end, _ := route.End()
	view := HandledView{
		Issue: run.Issue(), Rule: run.Rule(), To: end.To, Move: crew.MoveDone, Gone: e.gone, Taken: run.Taken(),
		Ended: route.Chosen, Earlier: e.earlier,
	}
	if report, ok := run.FailureReport(); ok && route.Route != crew.PassedRoute {
		view.Failures = report.Failures
	}
	switch final, _ := route.Final(); final := final.(type) {
	case crew.StepGivenUp:
		view.Move, view.DropReason = crew.MoveDropped, final.Reason
	case crew.StepDropped:
		view.Move, view.DropReason = crew.MoveDropped, final.Reason
	case crew.StepLanded, crew.StepRan, crew.StepFailed, crew.StepSkipped, crew.StepStopped, nil:
	}
	for _, a := range run.Actions() {
		action := HandledAction{Name: a.Name(), Spend: a.Spend()}
		if _, started := a.SessionStarted().Get(); started {
			action.PullRequest = run.PullRequest()
		}
		view.Actions = append(view.Actions, action)
	}
	return view, true
}
