package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// gone marks Gone each handled entry whose issue crew does not hold and
// whose To is a rule's label, when this listing was asked for after the
// entry's final move settled, unless it found the issue alone in To (KTD4).
// An entry whose route closed the issue is gone at the first such listing:
// no listing finds a closed issue (KTD23).
// Only one listing is outstanding at a time, so this one is generation
// m.listings; one asked for before the move landed predates it and marks
// nothing. Each later listing decides anew, so an issue found in
// To again is no longer gone. A held issue's entry is left alone: its
// rule's new entry replaces it, or keeps it when both ended well and that
// rule has no actions.
func (m *Model) gone(issues []crew.Issue) {
	alone := map[crew.IssueID]crew.State{}
	for _, issue := range issues {
		if state, ok := issue.OnlyState(); ok {
			alone[issue.ID()] = state
		}
	}
	for i := range m.handled {
		e := &m.handled[i]
		id := e.run.Issue().ID()
		if e.landed >= m.listings || m.held(id) != nil {
			continue
		}
		route, _ := e.ending()
		end, _ := route.End()
		if end.Kind == crew.StepClose {
			final, _ := route.Final()
			_, landed := final.(crew.StepLanded)
			e.gone = landed
			continue
		}
		to := end.To
		if _, ok := m.ruleLabeled(to); !ok {
			continue
		}
		state, found := alone[id]
		e.gone = !found || state != to
	}
}
