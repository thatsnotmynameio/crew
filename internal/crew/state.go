// Package crew holds crew's domain vocabulary: the states an issue moves
// through, issues, workflow stages and actions, and what an action run
// produces. It imports nothing of crew's, so every other package can share it.
package crew

// State is one of crew's eight issue states. It is the engine's vocabulary:
// each tracker adapter maps a State to its own representation (a label, a
// status, a column), and nothing outside an adapter knows that mapping.
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
	switch s {
	case Ready, InProgress, ReadyToReview, InReview, NeedsAttention, Paused, ReadyToMerge, Done:
		return true
	}
	return false
}
