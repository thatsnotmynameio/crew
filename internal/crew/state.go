// Package crew holds crew's domain vocabulary: the states a set of rules moves
// an issue through, issues, rules and actions, and what an action
// run produces. It imports nothing of crew's, so every other package can
// share it.
package crew

import "slices"

// State is where an issue stands in the rules, written as the config
// writes it and the tracker shows it: a label's text on GitHub, such as
// "in progress". Any non-empty text is a state. The rules' states are
// the ones they name (RuleStates); every other label or status an
// issue carries is not crew's.
type State string

// RuleStates returns every state the rules name, rule by
// rule in rule order (ready, running, success, failure), each
// once, leaving out a rule's empty failure. These are crew's states: the ones it takes issues from, moves them
// to, and counts when an issue carries two of them. The slice is new on
// every call.
func RuleStates(rules []Rule) []State {
	states := []State{}
	for _, s := range rules {
		l := s.Labels
		for _, state := range []State{l.Ready, l.Running, l.Success, l.Failure} {
			if state != "" && !slices.Contains(states, state) {
				states = append(states, state)
			}
		}
	}
	return states
}
