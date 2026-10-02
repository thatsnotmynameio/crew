package github

import (
	"fmt"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// settings is the adapter's config section: every key under tracker: except
// name.
type settings struct {
	Labels labelSettings `yaml:"labels"`
}

// labelSettings is tracker.labels: one field per crew state, so the strict
// decoder rejects a key that is not a state with its path and line.
type labelSettings struct {
	Ready          string `yaml:"ready"`
	InProgress     string `yaml:"in_progress"`
	ReadyToReview  string `yaml:"ready_to_review"`
	InReview       string `yaml:"in_review"`
	NeedsAttention string `yaml:"needs_attention"`
	Paused         string `yaml:"paused"`
	ReadyToMerge   string `yaml:"ready_to_merge"`
	Done           string `yaml:"done"`
}

// fields returns a pointer to each state's field.
func (l *labelSettings) fields() map[crew.State]*string {
	return map[crew.State]*string{
		crew.Ready:          &l.Ready,
		crew.InProgress:     &l.InProgress,
		crew.ReadyToReview:  &l.ReadyToReview,
		crew.InReview:       &l.InReview,
		crew.NeedsAttention: &l.NeedsAttention,
		crew.Paused:         &l.Paused,
		crew.ReadyToMerge:   &l.ReadyToMerge,
		crew.Done:           &l.Done,
	}
}

// labels is the mapping between crew states and GitHub labels. GitHub
// compares label names case-insensitively, so lookups by name do too.
type labels struct {
	name  map[crew.State]string
	state map[string]crew.State // keyed by the lowercased name
}

// decodeLabels decodes the section and maps each state to its label: the
// configured one, or the state's key with "_" replaced by a space. Two states
// mapping to the same name are an error naming both.
func decodeLabels(decode port.Decode) (labels, error) {
	var s settings
	fields := s.Labels.fields()
	for state, f := range fields {
		*f = strings.ReplaceAll(string(state), "_", " ")
	}
	if err := decode(&s); err != nil {
		return labels{}, err
	}
	l := labels{name: map[crew.State]string{}, state: map[string]crew.State{}}
	for _, state := range crew.States() {
		name := *fields[state]
		if other, taken := l.state[strings.ToLower(name)]; taken {
			return labels{}, fmt.Errorf("tracker.labels: %s and %s both map to the label %q, and GitHub does not tell labels apart by case; give each state its own label",
				other, state, name)
		}
		l.name[state] = name
		l.state[strings.ToLower(name)] = state
	}
	return l, nil
}

// stateOf returns the crew state label maps to, if any.
func (l labels) stateOf(label string) (crew.State, bool) {
	s, ok := l.state[strings.ToLower(label)]
	return s, ok
}
