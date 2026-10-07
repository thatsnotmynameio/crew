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
	phase     RunPhase
	actions   []ActionRun
	workspace WorkspaceState
	resume    Optional[ResumePoint]
	cursor    int
	bot       Bot
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

// Stopping reports whether a stop reached the run while it was taking or
// running its actions.
func (r RuleRun) Stopping() bool { return r.stopping }

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

// Resume returns where the run resumes the work of the run it continues,
// when it inherited a resume point.
func (r RuleRun) Resume() Optional[ResumePoint] { return r.resume }

// Bot returns the bot the run's actions that are not sessions act as: the
// bot of its latest session that started, or the zero Bot, the tracker's
// identity, before any did.
func (r RuleRun) Bot() Bot { return r.bot }

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
// route, ended or was released, so no action of it runs or will start.
func (r RuleRun) ActionsEnded() bool {
	switch r.phase.(type) {
	case RoutingPhase, EndingPhase, ReleasedPhase:
		return true
	case TakingPhase, RunningPhase:
	}
	return false
}

// Snapshot returns the run as plain data, sharing no memory with it.
func (r RuleRun) Snapshot() RuleRunSnapshot {
	s := RuleRunSnapshot{
		ID: r.id, Continues: r.continues, Issue: r.issue.Data(), Rule: r.rule, Taken: r.taken,
		Stopping: r.stopping, Phase: clonePhase(r.phase), Workspace: r.workspace, Resume: r.resume,
		Cursor: r.cursor, Bot: r.bot, Lookup: r.lookup,
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
	Phase     RunPhase
	// Actions are the action runs, in the rule's action order, each named
	// once.
	Actions   []ActionRunSnapshot
	Workspace WorkspaceState
	Resume    Optional[ResumePoint]
	// Cursor is the index in Actions of the action run at the cursor; 0
	// for a run without actions.
	Cursor int
	Bot    Bot
	Lookup Lookup
}

// errBadSnapshot is the error of a snapshot RestoreRuleRun rejects.
var errBadSnapshot = errors.New("not a rule run")

// RestoreRuleRun returns the run s describes, sharing no memory with it. It
// rejects a snapshot without an id, phase, workspace state, lookup or
// action state, one that names an action twice, one whose cursor is not on
// one of its actions, and one whose sequence is over while an action runs.
func RestoreRuleRun(s RuleRunSnapshot) (RuleRun, error) {
	if err := validate(s); err != nil {
		return RuleRun{}, fmt.Errorf("restore rule run %q: %w", s.ID, err)
	}
	r := RuleRun{
		id: s.ID, continues: s.Continues, issue: NewIssue(s.Issue), rule: s.Rule, taken: s.Taken,
		stopping: s.Stopping, phase: clonePhase(s.Phase), workspace: s.Workspace, resume: s.Resume,
		cursor: s.Cursor, bot: s.Bot, lookup: s.Lookup,
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
	}
	return validateActions(s)
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
// RoutingPhase, EndingPhase or ReleasedPhase.
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
// Route.
type RoutingPhase struct {
	Route RouteName
	// Chosen is when the run chose the route.
	Chosen time.Time
}

// EndingPhase is a run whose every action ended: its ending move, and its
// failure report when an action failed, are delivered.
type EndingPhase struct {
	Ending RunEnding
	// Ended is when the run's last action ended and its ending was
	// decided.
	Ended time.Time
	// Move is how the ending move settled; none while it is in flight or
	// owed.
	Move Optional[EndingMove]
	// ReportSettled is set once the failure report landed or was given up,
	// and from the start for an ending without failures, which posts none.
	ReportSettled bool
}

// ReleasedPhase is a run crew let go: its ending settled, or its take was
// given up.
type ReleasedPhase struct {
	// Ending is the run's settled ending; none when its take was given up.
	Ending Optional[SettledEnding]
}

func (TakingPhase) runPhase()   {}
func (RunningPhase) runPhase()  {}
func (RoutingPhase) runPhase()  {}
func (EndingPhase) runPhase()   {}
func (ReleasedPhase) runPhase() {}

// RunEnding is how a rule run ended: the state its issue moves to, and its
// failed actions.
type RunEnding struct {
	// To is the rule's success state, or its failure state when an action
	// failed.
	To State
	// Failures are the failed actions, in the rule's action order; empty
	// when every action succeeded.
	Failures []ActionFailure
}

// Failed reports whether an action failed.
func (v RunEnding) Failed() bool { return len(v.Failures) > 0 }

// SettledEnding is the ending of a released run, with how its move
// settled.
type SettledEnding struct {
	Ending RunEnding
	// Ended is when the run's last action ended.
	Ended time.Time
	Move  EndingMove
}

// EndingMove is how an ending move settled: EndingLanded or
// EndingGivenUp.
//
//sumtype:decl
type EndingMove interface {
	endingMove()
}

// EndingLanded is an ending move that landed: the issue is in the
// ending's state.
type EndingLanded struct{}

// EndingGivenUp is an ending move crew gave up, as the issue was closed or
// moved meanwhile, the tracker refused it, or its last try after a stop
// failed.
type EndingGivenUp struct {
	Reason string
}

func (EndingLanded) endingMove()  {}
func (EndingGivenUp) endingMove() {}

// clonePhase returns a copy of p that shares no failures with it.
func clonePhase(p RunPhase) RunPhase {
	switch p := p.(type) {
	case EndingPhase:
		p.Ending = p.Ending.clone()
		return p
	case ReleasedPhase:
		if v, ok := p.Ending.Get(); ok {
			v.Ending = v.Ending.clone()
			p.Ending = Some(v)
		}
		return p
	case TakingPhase, RunningPhase, RoutingPhase:
	}
	return p
}

// clone returns a copy of v with its own failures.
func (v RunEnding) clone() RunEnding {
	v.Failures = slices.Clone(v.Failures)
	return v
}
