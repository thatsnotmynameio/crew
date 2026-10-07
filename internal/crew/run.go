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
// phase and its action runs in the rule's action order, and it refers to the
// rule only by name. It cannot be changed once built: accessors return
// copies, and Apply returns a new run.
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

// ActionsEnded reports whether every action run ended; true for a run
// without actions.
func (r RuleRun) ActionsEnded() bool {
	for _, a := range r.actions {
		if !a.Ended() {
			return false
		}
	}
	return true
}

// Snapshot returns the run as plain data, sharing no memory with it.
func (r RuleRun) Snapshot() RuleRunSnapshot {
	s := RuleRunSnapshot{
		ID: r.id, Continues: r.continues, Issue: r.issue.Data(), Rule: r.rule, Taken: r.taken,
		Stopping: r.stopping, Phase: clonePhase(r.phase),
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
	Actions []ActionRunSnapshot
}

// errBadSnapshot is the error of a snapshot RestoreRuleRun rejects.
var errBadSnapshot = errors.New("not a rule run")

// RestoreRuleRun returns the run s describes, sharing no memory with it. It
// rejects a snapshot without an id, phase, action state or lookup, one that
// names an action twice, and one judging or released with a verdict while
// an action has not ended.
func RestoreRuleRun(s RuleRunSnapshot) (RuleRun, error) {
	if err := validate(s); err != nil {
		return RuleRun{}, fmt.Errorf("restore rule run %q: %w", s.ID, err)
	}
	r := RuleRun{
		id: s.ID, continues: s.Continues, issue: NewIssue(s.Issue), rule: s.Rule, taken: s.Taken,
		stopping: s.Stopping, phase: clonePhase(s.Phase),
	}
	for _, a := range s.Actions {
		r.actions = append(r.actions, restoreAction(a))
	}
	return r, nil
}

// validate returns why s is not a rule run, or nil.
func validate(s RuleRunSnapshot) error {
	if s.ID == "" || s.Phase == nil {
		return fmt.Errorf("%w: it has no id or no phase", errBadSnapshot)
	}
	judged := false
	switch p := s.Phase.(type) {
	case JudgingPhase:
		judged = true
	case ReleasedPhase:
		_, judged = p.Verdict.Get()
	case TakingPhase, RunningPhase:
	}
	var names []ActionName
	for _, a := range s.Actions {
		if a.State == nil || a.Lookup == nil {
			return fmt.Errorf("%w: action %q has no state or no lookup", errBadSnapshot, a.Name)
		}
		if slices.Contains(names, a.Name) {
			return fmt.Errorf("%w: it names action %q twice", errBadSnapshot, a.Name)
		}
		names = append(names, a.Name)
		if _, ended := a.State.(Finished); judged && !ended {
			return fmt.Errorf("%w: it was judged while action %q had not ended", errBadSnapshot, a.Name)
		}
	}
	return nil
}

// RunPhase is where a rule run stands: TakingPhase, RunningPhase,
// JudgingPhase or ReleasedPhase.
//
//sumtype:decl
type RunPhase interface {
	runPhase()
}

// TakingPhase is a run whose take move is in flight or owed.
type TakingPhase struct{}

// RunningPhase is a run whose take landed and some of whose actions run.
type RunningPhase struct{}

// JudgingPhase is a run whose every action ended: its verdict move, and its
// failure report when an action failed, are delivered.
type JudgingPhase struct {
	Verdict Verdict
	// Judged is when the run's last action ended and its verdict was
	// decided.
	Judged time.Time
	// Move is how the verdict move settled; none while it is in flight or
	// owed.
	Move Optional[VerdictMove]
	// ReportSettled is set once the failure report landed or was given up,
	// and from the start for a verdict without failures, which posts none.
	ReportSettled bool
}

// ReleasedPhase is a run crew let go: its verdict settled, or its take was
// given up.
type ReleasedPhase struct {
	// Verdict is the run's settled verdict; none when its take was given up.
	Verdict Optional[SettledVerdict]
}

func (TakingPhase) runPhase()   {}
func (RunningPhase) runPhase()  {}
func (JudgingPhase) runPhase()  {}
func (ReleasedPhase) runPhase() {}

// Verdict is how a rule run ended: the state its issue moves to, and its
// failed actions.
type Verdict struct {
	// To is the rule's success state, or its failure state when an action
	// failed.
	To State
	// Failures are the failed actions, in the rule's action order; empty
	// when every action succeeded.
	Failures []ActionFailure
}

// Failed reports whether an action failed.
func (v Verdict) Failed() bool { return len(v.Failures) > 0 }

// SettledVerdict is the verdict of a released run, with how its move
// settled.
type SettledVerdict struct {
	Verdict Verdict
	// Judged is when the run's last action ended.
	Judged time.Time
	Move   VerdictMove
}

// VerdictMove is how a verdict move settled: VerdictLanded or
// VerdictGivenUp.
//
//sumtype:decl
type VerdictMove interface {
	verdictMove()
}

// VerdictLanded is a verdict move that landed: the issue is in the
// verdict's state.
type VerdictLanded struct{}

// VerdictGivenUp is a verdict move crew gave up, as the issue was closed or
// moved meanwhile, the tracker refused it, or its last try after a stop
// failed.
type VerdictGivenUp struct {
	Reason string
}

func (VerdictLanded) verdictMove()  {}
func (VerdictGivenUp) verdictMove() {}

// clonePhase returns a copy of p that shares no failures with it.
func clonePhase(p RunPhase) RunPhase {
	switch p := p.(type) {
	case JudgingPhase:
		p.Verdict = p.Verdict.clone()
		return p
	case ReleasedPhase:
		if v, ok := p.Verdict.Get(); ok {
			v.Verdict = v.Verdict.clone()
			p.Verdict = Some(v)
		}
		return p
	case TakingPhase, RunningPhase:
	}
	return p
}

// clone returns a copy of v with its own failures.
func (v Verdict) clone() Verdict {
	v.Failures = slices.Clone(v.Failures)
	return v
}
