// Package fake holds in-memory adapters for tests: a Tracker, a scripted
// Harness and a temp-dir Workspace. The tracker and the harness come with
// factories that validate their config section like real adapters, so tests
// build them through a registry and exercise the same path production does.
// Every fake is safe for concurrent use, as the engine calls its ports from
// several goroutines. Only tests import this package.
package fake

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guards: the fakes implement exactly the interfaces they claim.
var (
	_ port.Tracker  = (*Tracker)(nil)
	_ port.Preparer = (*Preparation)(nil)
	_ port.Tracker  = PreparingTracker{}
	_ port.Preparer = PreparingTracker{}
)

// TrackerSettings is the fake tracker's config section. It has no key, as
// the github adapter's has none, so a config written for github runs with
// the fake by changing tracker.name; any key is an error.
type TrackerSettings struct{}

// TrackerFactory returns a factory that validates its section into
// TrackerSettings and, when it is valid, returns t itself, so the test keeps
// a handle on the tracker the engine uses. It ignores the workflow's states.
func TrackerFactory(t port.Tracker) port.TrackerFactory {
	return func(decode port.Decode, _ []crew.State) (port.Tracker, error) {
		var settings TrackerSettings
		if err := decode(&settings); err != nil {
			return nil, err
		}
		return t, nil
	}
}

// Move is a move the fake tracker applied.
type Move struct {
	Key      string
	From, To crew.State
}

// Tracker is an in-memory issue tracker. It lists issues in the order they
// were added, and a Move leaves an issue in exactly the state it moved to.
// Its zero value is not usable; use NewTracker.
type Tracker struct {
	mu         sync.Mutex
	issues     []*trackedIssue
	moveErrs   map[string][]error
	reportErrs map[string][]error
	moves      []Move
	reports    []crew.FailureReport
}

type trackedIssue struct {
	issue  crew.Issue
	closed bool
}

// NewTracker returns a tracker holding issues, all open.
func NewTracker(issues ...crew.Issue) *Tracker {
	t := &Tracker{moveErrs: map[string][]error{}, reportErrs: map[string][]error{}}
	for _, i := range issues {
		t.Add(i)
	}
	return t
}

// Add adds issue as an open issue, replacing any issue with the same key.
func (t *Tracker) Add(issue crew.Issue) {
	t.mu.Lock()
	defer t.mu.Unlock()
	issue = issue.Clone()
	if ti := t.find(issue.Key); ti != nil {
		ti.issue, ti.closed = issue, false
		return
	}
	t.issues = append(t.issues, &trackedIssue{issue: issue})
}

// SetStates sets the states of the issue with key, as a person editing it
// would. It does nothing for an unknown key.
func (t *Tracker) SetStates(key string, states ...crew.State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ti := t.find(key); ti != nil {
		ti.issue.States = slices.Clone(states)
	}
}

// Close closes the issue with key: List no longer returns it, and moving it
// is ErrMovedMeanwhile.
func (t *Tracker) Close(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ti := t.find(key); ti != nil {
		ti.closed = true
	}
}

// Issue returns the issue with key as it is now, open or closed.
func (t *Tracker) Issue(key string) (crew.Issue, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ti := t.find(key)
	if ti == nil {
		return crew.Issue{}, false
	}
	return ti.issue.Clone(), true
}

// FailMoves makes the next moves of the issue with key fail, one error per
// move, in order; later moves succeed again. Wrap port.ErrRefused or
// port.ErrMovedMeanwhile for those classes; any other error is transient.
func (t *Tracker) FailMoves(key string, errs ...error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.moveErrs[key] = append(t.moveErrs[key], errs...)
}

// FailReports makes the next failure reports on the issue with key fail, as
// FailMoves does for moves.
func (t *Tracker) FailReports(key string, errs ...error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reportErrs[key] = append(t.reportErrs[key], errs...)
}

// Moves returns the moves applied so far, in order. Failed moves are not
// among them.
func (t *Tracker) Moves() []Move {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.moves)
}

// Reports returns the failure reports posted so far, in order. Failed
// reports are not among them.
func (t *Tracker) Reports() []crew.FailureReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]crew.FailureReport, len(t.reports))
	for i, r := range t.reports {
		r.Failures = slices.Clone(r.Failures)
		out[i] = r
	}
	return out
}

// List implements port.Tracker.
func (t *Tracker) List(_ context.Context, states []crew.State) ([]crew.Issue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []crew.Issue
	for _, ti := range t.issues {
		if ti.closed {
			continue
		}
		if slices.ContainsFunc(ti.issue.States, func(s crew.State) bool { return slices.Contains(states, s) }) {
			out = append(out, ti.issue.Clone())
		}
	}
	return out, nil
}

// Move implements port.Tracker. A scripted failure comes first; then an
// unknown or closed issue is ErrMovedMeanwhile. An open issue not in from but
// exactly in to is already moved, so Move returns nil and records no move, as
// the github adapter does on a retry. Any other issue not in from is
// ErrMovedMeanwhile.
func (t *Tracker) Move(_ context.Context, issueKey string, from, to crew.State) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := pop(t.moveErrs, issueKey); err != nil {
		return fmt.Errorf("move issue %s from %s to %s: %w", issueKey, from, to, err)
	}
	ti := t.find(issueKey)
	if ti == nil || ti.closed {
		return fmt.Errorf("move issue %s from %s to %s: %w", issueKey, from, to, port.ErrMovedMeanwhile)
	}
	if !slices.Contains(ti.issue.States, from) {
		if slices.Equal(ti.issue.States, []crew.State{to}) {
			return nil
		}
		return fmt.Errorf("move issue %s from %s to %s: %w", issueKey, from, to, port.ErrMovedMeanwhile)
	}
	ti.issue.States = []crew.State{to}
	t.moves = append(t.moves, Move{Key: issueKey, From: from, To: to})
	return nil
}

// ReportFailure implements port.Tracker. It records report unless a scripted
// failure comes first.
func (t *Tracker) ReportFailure(_ context.Context, report crew.FailureReport) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := pop(t.reportErrs, report.IssueKey); err != nil {
		return fmt.Errorf("report failure on issue %s: %w", report.IssueKey, err)
	}
	report.Failures = slices.Clone(report.Failures)
	t.reports = append(t.reports, report)
	return nil
}

func (t *Tracker) find(key string) *trackedIssue {
	for _, ti := range t.issues {
		if ti.issue.Key == key {
			return ti
		}
	}
	return nil
}

// pop removes and returns the first scripted error for key, if any.
func pop(scripted map[string][]error, key string) error {
	errs := scripted[key]
	if len(errs) == 0 {
		return nil
	}
	scripted[key] = errs[1:]
	return errs[0]
}

// Preparation is a scriptable port.Preparer, to embed in a fake. It records
// the states of each call and returns the error set by Fail. Its zero value
// succeeds.
type Preparation struct {
	mu    sync.Mutex
	err   error
	calls [][]crew.State
}

// Prepare implements port.Preparer.
func (p *Preparation) Prepare(_ context.Context, states []crew.State) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, slices.Clone(states))
	return p.err
}

// Fail makes every later Prepare return err; nil makes it succeed again.
func (p *Preparation) Fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

// Calls returns the states each Prepare call received, in order.
func (p *Preparation) Calls() [][]crew.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]crew.State, len(p.calls))
	for i, c := range p.calls {
		out[i] = slices.Clone(c)
	}
	return out
}

// PreparingTracker is a Tracker that also implements port.Preparer, for the
// tests where the tracker adapter has tools to check. A plain *Tracker does
// not implement it.
type PreparingTracker struct {
	*Tracker
	*Preparation
}

// NewPreparingTracker returns a PreparingTracker holding issues whose
// Prepare succeeds until told to Fail.
func NewPreparingTracker(issues ...crew.Issue) PreparingTracker {
	return PreparingTracker{Tracker: NewTracker(issues...), Preparation: &Preparation{}}
}
