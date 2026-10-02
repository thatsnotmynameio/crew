// Package crew holds crew's domain vocabulary: the states an issue moves
// through, issues, workflow stages and actions, and what an action run
// produces. It imports nothing of crew's, so every other package can share it.
package crew

import "slices"

// State is where an issue stands in the workflow, written as the workflow
// writes it and the tracker shows it: a label's text on GitHub, such as
// "in progress". Any non-empty text is a state. The workflow's states are
// the ones its stages name (WorkflowStates); every other label or status an
// issue carries is not crew's.
type State string

// The eight crew states, as written under tracker.labels in .crew/config.yaml.
const (
	Ready          State = "ready"
	InProgress     State = "in_progress"
	ReadyToReview  State = "ready_to_review"
	InReview       State = "in_review"
	NeedsAttention State = "needs_attention"
	Paused         State = "paused"
	ReadyToMerge   State = "ready_to_merge"
	Done           State = "done"
)

// States returns the eight crew states in their canonical order. The slice is
// new on every call, so callers may keep or change it.
func States() []State {
	return []State{Ready, InProgress, ReadyToReview, InReview, NeedsAttention, Paused, ReadyToMerge, Done}
}

// Valid reports whether s is one of the eight crew states.
func (s State) Valid() bool {
	return slices.Contains(States(), s)
}

// WorkflowStates returns every state the workflow's stages name, stage by
// stage in workflow order (label, moves_to, on_success, on_failure), each
// once. These are crew's states: the ones it takes issues from, moves them
// to, and counts when an issue carries two of them. The slice is new on
// every call.
func WorkflowStates(workflow []Stage) []State {
	states := []State{}
	for _, s := range workflow {
		for _, state := range []State{s.Label, s.MovesTo, s.OnSuccess, s.OnFailure} {
			if !slices.Contains(states, state) {
				states = append(states, state)
			}
		}
	}
	return states
}
