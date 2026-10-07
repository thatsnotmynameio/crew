// Package crew holds crew's domain: the states a set of rules moves an issue
// through, issues, rules and actions, and the rule run, which holds the
// rules of its own lifecycle. A rule run decides each change, from what
// happened to it, as events (Decide), and applies them (Apply); its status
// and reports are computed from it. It imports nothing of crew's, so every
// other package can share it.
package crew

import "slices"

// State is where an issue stands in the rules, written as the config writes
// it and the tracker shows it: a label's text on GitHub, such as
// "in progress". Any non-empty text is a state. The rules' states are the
// ones they name (RuleStates); every other label or status an issue carries
// is not crew's.
type State string

// RuleStates returns every state the rules name, rule by rule in rule order
// (ready, running, then the state each of its routes moves to in route
// order), each once, leaving out a rule's empty states. These are
// crew's states: the ones it takes issues from, moves them to, and counts
// when an issue carries two of them. The slice is new on every call.
func RuleStates(rules []Rule) []State {
	states := []State{}
	add := func(state State) {
		if state != "" && !slices.Contains(states, state) {
			states = append(states, state)
		}
	}
	for _, r := range rules {
		add(r.Labels.Ready)
		add(r.Labels.Running)
		for _, route := range r.Routes {
			for _, step := range route.Steps {
				if m, ok := step.(MoveStep); ok {
					add(m.To)
				}
			}
		}
	}
	return states
}
