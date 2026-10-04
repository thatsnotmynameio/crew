package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// otherKind reports each listed item crew does not hold whose one crew state
// is the label of a stage that takes the other kind of item, blocked or not
// (#92). It reports an item once while it stays in that state: it remembers
// the label it reported for each item, and forgets every item this listing
// did not find in such a state, so a label put back afterwards is reported
// again. An item in two or more states is left to skipped.
func (s *step) otherKind(issues []crew.Issue) {
	m := s.m
	found := map[string]crew.State{}
	for _, issue := range issues {
		if len(issue.States) != 1 || m.held(issue.Key) != nil {
			continue
		}
		stage, ok := m.stageLabeled(issue.States[0])
		if !ok || stage.Takes == issue.Kind {
			continue
		}
		found[issue.Key] = stage.Label
		if m.otherKinds[issue.Key] == stage.Label {
			continue
		}
		s.emit(IssueOfOtherKind{
			At: s.at, IssueKey: issue.Key, IssueRef: issue.Ref, Kind: issue.Kind,
			Label: stage.Label, Stage: stage.Name, Takes: stage.Takes,
		})
	}
	m.otherKinds = found
}

// stageLabeled returns the stage whose label is state, if any.
func (m *Model) stageLabeled(state crew.State) (crew.Stage, bool) {
	for _, st := range m.stages {
		if st.Label == state {
			return st, true
		}
	}
	return crew.Stage{}, false
}
