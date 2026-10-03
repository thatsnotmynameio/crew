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
	_ port.Tracker        = (*Tracker)(nil)
	_ port.Preparer       = (*Preparation)(nil)
	_ port.Tracker        = PreparingTracker{}
	_ port.Preparer       = PreparingTracker{}
	_ port.StatusReporter = (*StatusBoard)(nil)
	_ port.Tracker        = ReportingTracker{}
	_ port.Preparer       = ReportingTracker{}
	_ port.StatusReporter = ReportingTracker{}

	_ port.PullRequestReporter = (*PullRequestBoard)(nil)
	_ port.Tracker             = PullRequestTracker{}
	_ port.StatusReporter      = PullRequestTracker{}
	_ port.PullRequestReporter = PullRequestTracker{}
)

// TrackerSettings is the fake tracker's config section. It has no key, as
// the github adapter's has none, so a config written for github runs with
// the fake by changing tracker.name; any key is an error.
type TrackerSettings struct{}

// TrackerFactory returns a factory that validates its section into
// TrackerSettings and, when it is valid, returns t itself, so the test keeps
// a handle on the tracker the engine uses. It ignores the workflow's states
// and the extras; a test sets an issue's extras with SetExtras.
func TrackerFactory(t port.Tracker) port.TrackerFactory {
	return func(decode port.Decode, _, _ []crew.State) (port.Tracker, error) {
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
// An issue's extra labels, set with SetExtras, are kept apart from its
// states: List never reports them and a Move clears them. Its zero value is
// not usable; use NewTracker.
type Tracker struct {
	mu         sync.Mutex
	issues     []*trackedIssue
	moveErrs   failures
	reportErrs failures
	moves      []Move
	reports    []crew.FailureReport
}

type trackedIssue struct {
	issue  crew.Issue
	extras []crew.State
	closed bool
}

// NewTracker returns a tracker holding issues, all open.
func NewTracker(issues ...crew.Issue) *Tracker {
	t := &Tracker{}
	for _, i := range issues {
		t.Add(i)
	}
	return t
}

// Add adds issue as an open issue without extras, replacing any issue with
// the same key.
func (t *Tracker) Add(issue crew.Issue) {
	t.mu.Lock()
	defer t.mu.Unlock()
	issue = issue.Clone()
	if ti := t.find(issue.Key); ti != nil {
		ti.issue, ti.extras, ti.closed = issue, nil, false
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

// SetExtras sets the extra labels of the issue with key, as a person
// editing it would. It does nothing for an unknown key.
func (t *Tracker) SetExtras(key string, extras ...crew.State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ti := t.find(key); ti != nil {
		ti.extras = slices.Clone(extras)
	}
}

// Extras returns the extra labels the issue with key has now.
func (t *Tracker) Extras(key string) []crew.State {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ti := t.find(key); ti != nil {
		return slices.Clone(ti.extras)
	}
	return nil
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
	t.moveErrs.add(key, errs...)
}

// FailReports makes the next failure reports on the issue with key fail, as
// FailMoves does for moves.
func (t *Tracker) FailReports(key string, errs ...error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reportErrs.add(key, errs...)
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
// exactly in to, whatever its extras, is already moved, so Move returns nil
// and records no move, as the github adapter does on a retry. Any other issue
// not in from is ErrMovedMeanwhile. A move clears the issue's extras.
func (t *Tracker) Move(_ context.Context, issueKey string, from, to crew.State) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.moveErrs.pop(issueKey); err != nil {
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
	ti.issue.States, ti.extras = []crew.State{to}, nil
	t.moves = append(t.moves, Move{Key: issueKey, From: from, To: to})
	return nil
}

// ReportFailure implements port.Tracker. It records report unless a scripted
// failure comes first.
func (t *Tracker) ReportFailure(_ context.Context, report crew.FailureReport) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.reportErrs.pop(report.IssueKey); err != nil {
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

// failures are scripted errors by issue key, each returned by one call, in
// order. Its zero value is ready to use; its owner guards it with its mutex.
type failures map[string][]error

// add queues errs for the next calls for key.
func (f *failures) add(key string, errs ...error) {
	if *f == nil {
		*f = failures{}
	}
	(*f)[key] = append((*f)[key], errs...)
}

// pop removes and returns the first scripted error for key, if any.
func (f *failures) pop(key string) error {
	errs := (*f)[key]
	if len(errs) == 0 {
		return nil
	}
	(*f)[key] = errs[1:]
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

// StatusBoard is a scriptable port.StatusReporter, to embed in a fake
// tracker. It records each status written, by issue key, unless a failure
// scripted with FailStatuses comes first. Its zero value is ready to use.
type StatusBoard struct {
	mu       sync.Mutex
	errs     failures
	statuses map[string][]crew.Status
}

// ReportStatus implements port.StatusReporter.
func (b *StatusBoard) ReportStatus(_ context.Context, status crew.Status) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.errs.pop(status.IssueKey); err != nil {
		return fmt.Errorf("report status on issue %s: %w", status.IssueKey, err)
	}
	if b.statuses == nil {
		b.statuses = map[string][]crew.Status{}
	}
	b.statuses[status.IssueKey] = append(b.statuses[status.IssueKey], status.Clone())
	return nil
}

// FailStatuses makes the next len(errs) status writes for key fail, in
// order, with errs.
func (b *StatusBoard) FailStatuses(key string, errs ...error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.errs.add(key, errs...)
}

// Statuses returns the statuses written for key, in order.
func (b *StatusBoard) Statuses(key string) []crew.Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]crew.Status, len(b.statuses[key]))
	for i, s := range b.statuses[key] {
		out[i] = s.Clone()
	}
	return out
}

// ReportingTracker is a PreparingTracker that also implements
// port.StatusReporter, for the tests about status comments. A plain
// *Tracker or PreparingTracker does not implement it.
type ReportingTracker struct {
	PreparingTracker
	*StatusBoard
}

// NewReportingTracker returns a ReportingTracker holding issues, whose
// Prepare and status writes succeed until told otherwise.
func NewReportingTracker(issues ...crew.Issue) ReportingTracker {
	return ReportingTracker{PreparingTracker: NewPreparingTracker(issues...), StatusBoard: &StatusBoard{}}
}

// PullRequestBoard is a scriptable port.PullRequestReporter, to embed in a
// fake tracker. It records each pull request report, by issue key, unless a
// failure scripted with FailPullRequests comes first. Its zero value is
// ready to use.
type PullRequestBoard struct {
	mu      sync.Mutex
	errs    failures
	reports map[string][]crew.PullRequestReport
}

// ReportPullRequests implements port.PullRequestReporter.
func (b *PullRequestBoard) ReportPullRequests(_ context.Context, report crew.PullRequestReport) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.errs.pop(report.IssueKey); err != nil {
		return fmt.Errorf("report pull requests of issue %s: %w", report.IssueKey, err)
	}
	if b.reports == nil {
		b.reports = map[string][]crew.PullRequestReport{}
	}
	b.reports[report.IssueKey] = append(b.reports[report.IssueKey], report.Clone())
	return nil
}

// FailPullRequests makes the next len(errs) pull request reports for key
// fail, in order, with errs. Wrap port.ErrRefused or port.ErrMovedMeanwhile
// for those classes; any other error is transient.
func (b *PullRequestBoard) FailPullRequests(key string, errs ...error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.errs.add(key, errs...)
}

// PullRequestReports returns the pull request reports recorded for key, in
// order. Failed reports are not among them.
func (b *PullRequestBoard) PullRequestReports(key string) []crew.PullRequestReport {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]crew.PullRequestReport, len(b.reports[key]))
	for i, r := range b.reports[key] {
		out[i] = r.Clone()
	}
	return out
}

// PullRequestTracker is a ReportingTracker that also implements
// port.PullRequestReporter, for the tests about pull requests. A
// ReportingTracker does not implement it.
type PullRequestTracker struct {
	ReportingTracker
	*PullRequestBoard
}

// NewPullRequestTracker returns a PullRequestTracker holding issues, whose
// Prepare, status writes and pull request reports succeed until told
// otherwise.
func NewPullRequestTracker(issues ...crew.Issue) PullRequestTracker {
	return PullRequestTracker{ReportingTracker: NewReportingTracker(issues...), PullRequestBoard: &PullRequestBoard{}}
}
