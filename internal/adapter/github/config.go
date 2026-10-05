package github

import (
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// settings is the adapter's config section: every key under tracker: except
// name. It has no key, so the strict decoder refuses any, tracker.labels
// included, with its path and line.
type settings struct{}

// labels maps a GitHub label to the rules' state it names. A state's
// text is its label's name. GitHub compares label names case-insensitively,
// so it is keyed by the lowercased name and lookups ignore case.
type labels map[string]crew.State

// newLabels maps the label of each of states to the state, in the state's
// spelling.
func newLabels(states []crew.State) labels {
	l := make(labels, len(states))
	for _, s := range states {
		l[strings.ToLower(string(s))] = s
	}
	return l
}

// stateOf returns the rule state that label names, if any.
func (l labels) stateOf(label string) (crew.State, bool) {
	s, ok := l[strings.ToLower(label)]
	return s, ok
}
