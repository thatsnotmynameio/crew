package crew

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// RuleRun is one run of a rule on an issue, from its take until it is
// released: an aggregate that decides its changes as events (Decide) and
// applies them (Apply). It holds the issue as taken, the rule's name, its
// phase, its one workspace, which its actions share, the cursor on its
// action runs, which are in the rule's action order, and the lookup of its
// pull requests. It refers to the rule only by name. It cannot be changed
// once built: accessors return copies, and Apply returns a new run.
//
// The zero RuleRun is no run: Apply of a RunTaken event to it starts one.
type RuleRun struct {
	id        RuleRunID
	continues Optional[RuleRunID]
	issue     Issue
	rule      RuleName
	taken     time.Time
	stopping  bool
	timeUp    bool
	phase     RunPhase
	actions   []ActionRun
	workspace WorkspaceState
	start     Start
	cursor    int
	session   Optional[LatestSession]
	questions []Question
	lookup    Lookup
}

// ID returns the run's id.
func (r RuleRun) ID() RuleRunID { return r.id }

// Continues returns the id of the run of the same issue and rule this run
// continues, when there was one.
func (r RuleRun) Continues() Optional[RuleRunID] { return r.continues }

// Issue returns the issue as the rule took it.
func (r RuleRun) Issue() Issue { return r.issue }

// Rule returns the name of the rule that runs.
func (r RuleRun) Rule() RuleName { return r.rule }

// Taken returns when the rule took the issue.
func (r RuleRun) Taken() time.Time { return r.taken }

// Stopping reports whether a stop reached the run before it was released.
func (r RuleRun) Stopping() bool { return r.stopping }

// TimeUp reports whether crew's run time was up while the run was taking or
// running its actions: no action starts after the one that runs.
func (r RuleRun) TimeUp() bool { return r.timeUp }

// Phase returns where the run stands.
func (r RuleRun) Phase() RunPhase { return clonePhase(r.phase) }

// Actions returns a copy of the run's action runs, in the rule's action
// order.
func (r RuleRun) Actions() []ActionRun { return slices.Clone(r.actions) }

// Action returns the run of the action named name, and whether the run has
// one.
func (r RuleRun) Action(name ActionName) (ActionRun, bool) {
	i := r.actionIndex(name)
	if i < 0 {
		return ActionRun{}, false
	}
	return r.actions[i], true
}

// Cursor returns the action run at the run's cursor, and whether the run
// has actions: the action that runs, or the next to run, and once the run
// chose a route the action whose verdict led to it.
func (r RuleRun) Cursor() (ActionRun, bool) {
	if r.cursor >= len(r.actions) {
		return ActionRun{}, false
	}
	return r.actions[r.cursor], true
}

// WorkspaceState returns where the run's workspace stands.
func (r RuleRun) WorkspaceState() WorkspaceState { return r.workspace }

// Workspace returns the run's workspace, once it was ready.
func (r RuleRun) Workspace() Optional[OpenedWorkspace] {
	if w, ok := r.workspace.(InWorkspace); ok {
		return Some(w.Opened)
	}
	return Optional[OpenedWorkspace]{}
}

// Start returns how the run starts, as its take decided from the run it
// continues, without the worktree it reopens once that proved gone
// (WorkspaceMissing).
func (r RuleRun) Start() Start {
	if r.start == nil {
		return StartFresh{}
	}
	return r.start
}

// LatestSession returns the run's latest session: the latest of its own
// sessions that started, or, before any did, the one a resumed run
// inherited from the run it continues.
func (r RuleRun) LatestSession() Optional[LatestSession] { return r.session }

// Bot returns the bot the run's actions that are not sessions act as: the
// bot of its latest session, or the zero Bot, the tracker's identity, when
// it has none.
func (r RuleRun) Bot() Bot {
	s, _ := r.session.Get()
	return s.Bot
}

// Lookup returns how the lookup of the run's pull requests stands.
func (r RuleRun) Lookup() Lookup { return r.lookup }

// PullRequest returns what the lookup of the run's pull requests found, or
// nil while it was not asked or is pending.
func (r RuleRun) PullRequest() PullRequest {
	if done, ok := r.lookup.(LookupDone); ok {
		return done.PullRequest
	}
	return nil
}

// ActionsEnded reports whether the run's sequence is over: it chose a
// route or was released, so no action of it runs or will start.
func (r RuleRun) ActionsEnded() bool {
	switch r.phase.(type) {
	case RoutingPhase, ReleasedPhase:
		return true
	case TakingPhase, RunningPhase:
	}
	return false
}

// Snapshot returns the run as plain data, sharing no memory with it.
func (r RuleRun) Snapshot() RuleRunSnapshot {
	s := RuleRunSnapshot{
		ID: r.id, Continues: r.continues, Issue: r.issue.Data(), Rule: r.rule, Taken: r.taken,
		Stopping: r.stopping, TimeUp: r.timeUp, Phase: clonePhase(r.phase), Workspace: r.workspace, Start: r.Start(),
		Cursor: r.cursor, Session: r.session, Questions: slices.Clone(r.questions), Lookup: r.lookup,
	}
	for _, a := range r.actions {
		s.Actions = append(s.Actions, a.snapshot())
	}
	return s
}

// actionIndex returns the index of the run of the action named name, or -1.
func (r RuleRun) actionIndex(name ActionName) int {
	return slices.IndexFunc(r.actions, func(a ActionRun) bool { return a.name == name })
}

// RuleRunSnapshot is a rule run as plain data, for a store to keep: values
// only, no pointer, function or channel. RestoreRuleRun turns one back into
// a run.
type RuleRunSnapshot struct {
	ID        RuleRunID
	Continues Optional[RuleRunID]
	Issue     IssueData
	Rule      RuleName
	Taken     time.Time
	Stopping  bool
	TimeUp    bool
	Phase     RunPhase
	// Actions are the action runs, in the rule's action order, each named
	// once.
	Actions   []ActionRunSnapshot
	Workspace WorkspaceState
	// Start is how the run starts; nil counts as StartFresh.
	Start Start
	// Cursor is the index in Actions of the action run at the cursor; 0
	// for a run without actions.
	Cursor  int
	Session Optional[LatestSession]
	// Questions are the run's open questions, oldest first.
	Questions []Question
	Lookup    Lookup
}

// errBadSnapshot is the error of a snapshot RestoreRuleRun rejects.
var errBadSnapshot = errors.New("not a rule run")

// RestoreRuleRun returns the run s describes, sharing no memory with it. It
// rejects a snapshot without an id, phase, workspace state, lookup or
// action state, one that names an action twice, one whose cursor is not on
// one of its actions, one whose route has a step past its last, and one
// whose sequence is over while an action runs.
func RestoreRuleRun(s RuleRunSnapshot) (RuleRun, error) {
	if err := validate(s); err != nil {
		return RuleRun{}, fmt.Errorf("restore rule run %q: %w", s.ID, err)
	}
	r := RuleRun{
		id: s.ID, continues: s.Continues, issue: NewIssue(s.Issue), rule: s.Rule, taken: s.Taken,
		stopping: s.Stopping, timeUp: s.TimeUp, phase: clonePhase(s.Phase), workspace: s.Workspace, start: s.Start,
		cursor: s.Cursor, session: s.Session, questions: slices.Clone(s.Questions), lookup: s.Lookup,
	}
	for _, a := range s.Actions {
		r.actions = append(r.actions, restoreAction(a))
	}
	return r, nil
}

// validate returns why s is not a rule run, or nil.
func validate(s RuleRunSnapshot) error {
	switch {
	case s.ID == "" || s.Phase == nil:
		return fmt.Errorf("%w: it has no id or no phase", errBadSnapshot)
	case s.Workspace == nil || s.Lookup == nil:
		return fmt.Errorf("%w: it has no workspace state or no lookup", errBadSnapshot)
	case s.Cursor < 0 || s.Cursor >= max(len(s.Actions), 1):
		return fmt.Errorf("%w: its cursor %d is not on one of its actions", errBadSnapshot, s.Cursor)
	case !stepsFit(s.Phase):
		return fmt.Errorf("%w: its route has a step past its last", errBadSnapshot)
	}
	return validateActions(s)
}

// stepsFit reports whether the steps settled and in flight of p's route,
// when it has one, are steps of that route.
func stepsFit(p RunPhase) bool {
	route, ok := RuleRun{phase: p}.route()
	if !ok {
		return true
	}
	i, asked := route.InFlight()
	if asked {
		i++
	}
	return i <= len(route.Steps)
}

// validateActions returns why the actions of s are not a rule run's, or
// nil.
func validateActions(s RuleRunSnapshot) error {
	over := RuleRun{phase: s.Phase}.ActionsEnded()
	var names []ActionName
	for _, a := range s.Actions {
		if a.State == nil {
			return fmt.Errorf("%w: action %q has no state", errBadSnapshot, a.Name)
		}
		if slices.Contains(names, a.Name) {
			return fmt.Errorf("%w: it names action %q twice", errBadSnapshot, a.Name)
		}
		names = append(names, a.Name)
		if over && (ActionRun{state: a.State}).running() {
			return fmt.Errorf("%w: its sequence is over while action %q runs", errBadSnapshot, a.Name)
		}
	}
	return nil
}

// RunPhase is where a rule run stands: TakingPhase, RunningPhase,
// RoutingPhase or ReleasedPhase.
//
//sumtype:decl
type RunPhase interface {
	runPhase()
}

// TakingPhase is a run whose take move is in flight or owed.
type TakingPhase struct{}

// RunningPhase is a run whose take landed and whose actions run, one at a
// time.
type RunningPhase struct{}

// RoutingPhase is a run whose sequence is over and which ends through
// Route, one step at a time: it asks a step only once the step before it
// settled, and is released once the final step settled.
type RoutingPhase struct {
	Route RouteName
	// Chosen is when the run chose the route.
	Chosen time.Time
	// Steps are the route's steps, in the order they run.
	Steps []StepPlan
	// Settled are how the route's first steps settled, one for each, in
	// the route's order.
	Settled []StepOutcome
	// Asked says whether the step after the settled ones was asked and has
	// not settled: it is in flight.
	Asked bool
}

// InFlight returns the index in Steps of the step that was asked and has
// not settled, and whether one was.
func (p RoutingPhase) InFlight() (int, bool) { return len(p.Settled), p.Asked }

// Final returns how the route's final step, the move or close that ends
// it, settled, once it did.
func (p RoutingPhase) Final() (StepOutcome, bool) {
	if len(p.Steps) == 0 || len(p.Settled) < len(p.Steps) {
		return nil, false
	}
	return p.Settled[len(p.Steps)-1], true
}

// End returns the route's final step: the move or close that ends it.
func (p RoutingPhase) End() (StepPlan, bool) {
	if len(p.Steps) == 0 {
		return StepPlan{}, false
	}
	return p.Steps[len(p.Steps)-1], true
}

// clone returns a copy of p with its own steps and outcomes.
func (p RoutingPhase) clone() RoutingPhase {
	p.Steps, p.Settled = slices.Clone(p.Steps), slices.Clone(p.Settled)
	return p
}

// ReleasedPhase is a run crew let go: the final step of its route settled,
// or its take was given up.
type ReleasedPhase struct {
	// Route is the route the run ended through, with how each of its steps
	// settled; none when its take was given up.
	Route Optional[RoutingPhase]
}

func (TakingPhase) runPhase()   {}
func (RunningPhase) runPhase()  {}
func (RoutingPhase) runPhase()  {}
func (ReleasedPhase) runPhase() {}

// clonePhase returns a copy of p that shares no steps or outcomes with it.
func clonePhase(p RunPhase) RunPhase {
	switch p := p.(type) {
	case RoutingPhase:
		return p.clone()
	case ReleasedPhase:
		if route, ok := p.Route.Get(); ok {
			p.Route = Some(route.clone())
		}
		return p
	case TakingPhase, RunningPhase:
	}
	return p
}

// route returns the route the run chose, with how its steps stand, once it
// chose one.
func (r RuleRun) route() (RoutingPhase, bool) {
	switch p := r.phase.(type) {
	case RoutingPhase:
		return p, true
	case ReleasedPhase:
		return p.Route.Get()
	case TakingPhase, RunningPhase:
	}
	return RoutingPhase{}, false
}
