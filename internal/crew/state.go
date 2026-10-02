// Package crew holds crew's domain vocabulary: the states a workflow moves
// an issue through, issues, workflow stages and actions, and what an action
// run produces. It imports nothing of crew's, so every other package can
// share it.
package crew

import "slices"

// State is where an issue stands in the workflow, written as the workflow
// writes it and the tracker shows it: a label's text on GitHub, such as
// "in progress". Any non-empty text is a state. The workflow's states are
// the ones its stages name (WorkflowStates); every other label or status an
// issue carries is not crew's.
type State string

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
