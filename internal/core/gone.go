package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// gone marks Gone each handled entry whose issue crew does not hold and
// whose To is a rule's label, when this listing was asked for after the
// entry's verdict move landed or was given up, unless it found the issue
// alone in To (KTD4). Only one listing is outstanding at a time, so this one
// is generation m.listings; one asked for before the move landed predates it
// and marks nothing. Each later listing decides anew, so an issue found in
// To again is no longer gone. A held issue's entry is left alone: its
// rule's new entry replaces it, or keeps it when both ended well and that
// rule has no actions.
func (m *Model) gone(issues []crew.Issue) {
	alone := map[crew.IssueID]crew.State{}
	for _, issue := range issues {
		if len(issue.States()) == 1 {
			alone[issue.ID()] = issue.States()[0]
		}
	}
	for i := range m.handled {
		e := &m.handled[i]
		if e.landed >= m.listings || m.held(e.view.Issue.ID()) != nil {
			continue
		}
		if _, ok := m.ruleLabeled(e.view.To); !ok {
			continue
		}
		state, found := alone[e.view.Issue.ID()]
		e.view.Gone = !found || state != e.view.To
	}
}
