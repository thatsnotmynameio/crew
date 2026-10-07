package crew

import "slices"

// Rule is one of the config's rules. It takes an item of its Takes kind in
// its Labels.Ready state and moves it to Labels.Running while its actions
// run, one at a time in one workspace. Each action's verdict leads to the
// next action or to one of its Routes, which ends the run.
type Rule struct {
	// Name identifies the rule in events and the TUI.
	Name RuleName
	// Labels are the states the rule takes an item from and moves it to.
	Labels Labels
	// Actions run one at a time, in this order, in the run's one
	// workspace. A rule may have none.
	Actions []Action
	// Queue is the queue the rule runs in: the share of the global limit
	// its issues may hold. The zero Queue is no queue: the rule is limited
	// only by the global limit, which it shares with every other such rule.
	Queue Queue
	// Takes is the kind of item the rule takes: it takes only the items of
	// that kind in its Labels.Ready state. The zero Kind takes issues.
	Takes Kind
	// Routes are the rule's ways to end a run, in the config's order. A
	// rule with actions declares PassedRoute and FailedRoute; a rule
	// without actions declares only PassedRoute.
	Routes []Route
}

// Action returns the definition of r's action named name, or the zero
// Action when r has none of that name.
func (r Rule) Action(name ActionName) Action {
	if i := slices.IndexFunc(r.Actions, func(a Action) bool { return a.Name == name }); i >= 0 {
		return r.Actions[i]
	}
	return Action{}
}

// Route returns r's route named name, and whether r has one.
func (r Rule) Route(name RouteName) (Route, bool) {
	if i := slices.IndexFunc(r.Routes, func(route Route) bool { return route.Name == name }); i >= 0 {
		return r.Routes[i], true
	}
	return Route{}, false
}

// WaitingStates returns the states r's waiting routes move the item to, in
// route order, each once: the final move of each route an action's On
// sends Waiting to. An item there waits for an answer, and r never takes
// it from there.
func (r Rule) WaitingStates() []State {
	var out []State
	for _, route := range r.Routes {
		if !r.waitsThrough(route.Name) || len(route.Steps) == 0 {
			continue
		}
		if m, ok := route.Steps[len(route.Steps)-1].(MoveStep); ok && !slices.Contains(out, m.To) {
			out = append(out, m.To)
		}
	}
	return out
}

// waitsThrough reports whether an action of r's On sends Waiting to the
// route named name.
func (r Rule) waitsThrough(name RouteName) bool {
	return slices.ContainsFunc(r.Actions, func(a Action) bool {
		t, ok := a.On[Waiting].(ToRoute)
		return ok && t.Route == name
	})
}

// Labels are a rule's states, one for each point of its run.
type Labels struct {
	// Ready is the state an item must be in for the rule to take it.
	Ready State
	// Running is the state the item is in while the rule's actions run.
	Running State
}

// DefaultQueue gets the slots the other queues leave, and runs every rule
// that names no queue.
const DefaultQueue QueueName = "default"

// Queue is a fixed share of the global limit on the issues crew holds at
// once. Only the rules in a queue use its slots, and it never lends an idle
// one to another queue.
type Queue struct {
	// Name identifies the queue: DefaultQueue or a queue the config
	// declares.
	Name QueueName
	// Slots is how many issues the queue's rules may hold at once, possibly
	// zero.
	Slots int
}

// Action is one action a rule runs for an issue: a session, or a shell
// script when its Kind says so.
type Action struct {
	// Name identifies the action within its rule, in workspace names, logs
	// and failure reports.
	Name ActionName
	// Kind is what the action runs: a session, named after its agent
	// unless the config names it, or one of the config's shell actions,
	// named as the config's actions key it.
	Kind ActionKind
	// On maps the action's verdicts to their targets.
	On On
}

// Outcome is how an action's session ended, as its harness reported it.
type Outcome struct {
	// Succeeded is true when the session ended cleanly.
	Succeeded bool
	// Reason is one line saying why, such as the session's last message.
	Reason SessionText
}

// FailureReport is what a route's report step asks a tracker to post on an
// issue: the action whose verdict ended the rule's sequence, that verdict,
// the route the rule ends through and the log (R17, KTD5). The tracker
// adapter formats it in its own markup. It carries no reason: an outcome's
// reason is a session's or a tool's last words, which a tracker comment
// must not show.
type FailureReport struct {
	// IssueID and IssueRef identify the issue, as ID and Ref in Issue.
	IssueID  IssueID
	IssueRef string
	// Rule is the rule that ran, and Route the route it ends through.
	Rule  RuleName
	Route RouteName
	// Failures holds the action at the run's cursor, whose verdict ended
	// the sequence; none for a rule without actions.
	Failures []ActionFailure
}

// ActionFailure is the action that ended a rule's sequence, in a
// FailureReport: its verdict and where to read why, never the reason
// itself.
type ActionFailure struct {
	// Action is the action's name.
	Action ActionName
	// Verdict is the verdict the action ended with.
	Verdict Verdict
	// Workspace is the workspace the action ran in.
	Workspace WorkspaceName
	// Log is the repository-relative path of the session's log file.
	Log string
}

// hasSession reports whether any of r's actions is a session.
func (r Rule) hasSession() bool {
	return slices.ContainsFunc(r.Actions, func(a Action) bool {
		_, ok := a.Kind.(SessionSpec)
		return ok
	})
}
