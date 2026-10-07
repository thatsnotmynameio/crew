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
	"strings"
	"sync"
	"time"

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

	_ port.PullRequestFinder   = (*PullRequests)(nil)
	_ port.Tracker             = FindingTracker{}
	_ port.Preparer            = FindingTracker{}
	_ port.StatusReporter      = FindingTracker{}
	_ port.PullRequestFinder   = FindingTracker{}
	_ port.PullRequestReporter = (*PullRequestBoard)(nil)
	_ port.Tracker             = PullRequestTracker{}
	_ port.StatusReporter      = PullRequestTracker{}
	_ port.PullRequestReporter = PullRequestTracker{}

	_ port.Acting          = (*Acting)(nil)
	_ port.CodeOwnerFinder = (*Acting)(nil)
	_ port.LoginFinder     = (*Acting)(nil)
	_ port.WriterReporter  = (*Acting)(nil)
	_ port.Tracker         = ActingTracker{}
	_ port.Preparer        = ActingTracker{}
	_ port.StatusReporter  = ActingTracker{}
	_ port.Acting          = ActingTracker{}
	_ port.CodeOwnerFinder = ActingTracker{}
	_ port.LoginFinder     = ActingTracker{}
	_ port.WriterReporter  = ActingTracker{}

	_ port.Tracker     = BoardTracker{}
	_ port.BoardLister = BoardTracker{}
)

// TrackerSettings is the fake tracker's config section. It has no key, as
// the github adapter's has none, so a config written for github runs with
// the fake by changing tracker.name; any key is an error.
type TrackerSettings struct{}

// TrackerFactory returns a factory that validates its section into
// TrackerSettings and, when it is valid, returns t itself, so the test keeps
// a handle on the tracker the engine uses. It ignores the rules' states.
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
// An issue's other labels, set with SetLabels, are not crew's states: List
// never reports them and a Move leaves them. Its zero value is not usable; use
// NewTracker.
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
	labels []crew.State // the labels that are not crew's
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

// Add adds issue as an open issue without other labels,
// replacing any issue with the same key.
func (t *Tracker) Add(issue crew.Issue) {
	t.mu.Lock()
	defer t.mu.Unlock()
	issue = issue.Clone()
	if ti := t.find(issue.ID.Key); ti != nil {
		ti.issue, ti.labels, ti.closed = issue, nil, false
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

// SetLabels sets the labels of the issue with key that are not crew's, such
// as bug, as a person editing it would. It does nothing for an unknown key.
func (t *Tracker) SetLabels(key string, labels ...crew.State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ti := t.find(key); ti != nil {
		ti.labels = slices.Clone(labels)
	}
}

// Labels returns the labels that are not crew's the issue with key has now.
func (t *Tracker) Labels(key string) []crew.State {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ti := t.find(key); ti != nil {
		return slices.Clone(ti.labels)
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
// exactly in to, whatever its other labels, is already moved, so Move returns nil
// and records no move, as the github adapter does on a retry. Any other issue
// not in from is ErrMovedMeanwhile. A move leaves the issue's other labels.
func (t *Tracker) Move(_ context.Context, id crew.IssueID, from, to crew.State) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.moveErrs.pop(id.Key); err != nil {
		return fmt.Errorf("move issue %s from %s to %s: %w", id.Key, from, to, err)
	}
	ti := t.find(id.Key)
	if ti == nil || ti.closed {
		return fmt.Errorf("move issue %s from %s to %s: %w", id.Key, from, to, port.ErrMovedMeanwhile)
	}
	if !slices.Contains(ti.issue.States, from) {
		if slices.Equal(ti.issue.States, []crew.State{to}) {
			return nil
		}
		return fmt.Errorf("move issue %s from %s to %s: %w", id.Key, from, to, port.ErrMovedMeanwhile)
	}
	ti.issue.States = []crew.State{to}
	t.moves = append(t.moves, Move{Key: id.Key, From: from, To: to})
	return nil
}

// ReportFailure implements port.Tracker. It records report unless a scripted
// failure comes first.
func (t *Tracker) ReportFailure(_ context.Context, report crew.FailureReport) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.reportErrs.pop(report.IssueID.Key); err != nil {
		return fmt.Errorf("report failure on issue %s: %w", report.IssueID.Key, err)
	}
	report.Failures = slices.Clone(report.Failures)
	t.reports = append(t.reports, report)
	return nil
}

func (t *Tracker) find(key string) *trackedIssue {
	for _, ti := range t.issues {
		if ti.issue.ID.Key == key {
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
// the states of each call, reports the step set by ReportStep and returns the
// error set by Fail. Its zero value reports no step and succeeds.
type Preparation struct {
	mu    sync.Mutex
	err   error
	step  string
	calls [][]crew.State
}

// Prepare implements port.Preparer. It reports its step, if any, through
// port.Step on ctx before it records the call.
func (p *Preparation) Prepare(ctx context.Context, states []crew.State) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.step != "" {
		port.Step(ctx, p.step)
	}
	p.calls = append(p.calls, slices.Clone(states))
	return p.err
}

// ReportStep makes every later Prepare report step, as a real Preparer
// reports each of its checks; "" makes it report none again.
func (p *Preparation) ReportStep(step string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.step = step
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
	if err := b.errs.pop(status.IssueID.Key); err != nil {
		return fmt.Errorf("report status on issue %s: %w", status.IssueID.Key, err)
	}
	if b.statuses == nil {
		b.statuses = map[string][]crew.Status{}
	}
	b.statuses[status.IssueID.Key] = append(b.statuses[status.IssueID.Key], status.Clone())
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

// Lookup is one pull request lookup a PullRequests received.
type Lookup struct {
	Branch string
	Since  time.Time
}

// LookupScript is how a fake pull request lookup goes.
type LookupScript struct {
	// Found is what the lookup returns; its zero value, not looked up, is
	// returned as no pull request, as a tracker that looked finds one or
	// none.
	Found crew.PullRequest
	// Err, when set, makes the lookup fail with it.
	Err error
	// Block makes the lookup wait until its context ends, as one that
	// outlasts the engine's timeout.
	Block bool
}

// PullRequests is a scriptable port.PullRequestFinder, to embed in a fake
// tracker: each lookup goes as scripted for its branch, and an unscripted
// branch has no pull request. It records every lookup. Its zero value is
// ready to use.
type PullRequests struct {
	mu      sync.Mutex
	scripts map[string]LookupScript
	lookups []Lookup
}

// ScriptLookup makes every later lookup of branch go as s.
func (p *PullRequests) ScriptLookup(branch string, s LookupScript) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.scripts == nil {
		p.scripts = map[string]LookupScript{}
	}
	p.scripts[branch] = s
}

// Lookups returns the lookups received so far, in the order they started.
func (p *PullRequests) Lookups() []Lookup {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.lookups)
}

// FindPullRequest implements port.PullRequestFinder.
func (p *PullRequests) FindPullRequest(ctx context.Context, branch string, since time.Time) (crew.PullRequest, error) {
	p.mu.Lock()
	p.lookups = append(p.lookups, Lookup{Branch: branch, Since: since})
	s := p.scripts[branch]
	p.mu.Unlock()
	switch {
	case s.Block:
		<-ctx.Done()
		return crew.PullRequest{}, fmt.Errorf("find the pull request from %s: %w", branch, ctx.Err())
	case s.Err != nil:
		return crew.PullRequest{}, fmt.Errorf("find the pull request from %s: %w", branch, s.Err)
	case s.Found.Lookup == crew.PullRequestNotLookedUp:
		return crew.PullRequest{Lookup: crew.PullRequestNone}, nil
	}
	return s.Found, nil
}

// FindingTracker is a ReportingTracker that also implements
// port.PullRequestFinder, for the tests about the pull request an action
// opened. A plain *Tracker, PreparingTracker or ReportingTracker does not
// implement it.
type FindingTracker struct {
	ReportingTracker
	*PullRequests
}

// NewFindingTracker returns a FindingTracker holding issues, whose Prepare
// and status writes succeed until told otherwise and which finds no pull
// request until a lookup is scripted.
func NewFindingTracker(issues ...crew.Issue) FindingTracker {
	return FindingTracker{ReportingTracker: NewReportingTracker(issues...), PullRequests: &PullRequests{}}
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
	if err := b.errs.pop(report.IssueID.Key); err != nil {
		return fmt.Errorf("report pull requests of issue %s: %w", report.IssueID.Key, err)
	}
	if b.reports == nil {
		b.reports = map[string][]crew.PullRequestReport{}
	}
	b.reports[report.IssueID.Key] = append(b.reports[report.IssueID.Key], report.Clone())
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

// ActAsCall is one call to port.Acting's ActAs an Acting received.
type ActAsCall struct {
	Writer port.Identity
	Bots   []string
}

// Acting is a scriptable port.Acting, port.CodeOwnerFinder, port.LoginFinder
// and port.WriterReporter, to embed in a fake tracker. It records each ActAs
// call and returns the code owners' logins set by SetCodeOwners, the login set
// by SetLogin and the writes warning set by SetWriterLost. Its zero value is
// ready to use: it finds no code owner and no login, and its writes never went
// back to you.
type Acting struct {
	mu         sync.Mutex
	codeOwners []string
	login      string
	lost       string
	calls      []ActAsCall
}

// ActAs implements port.Acting.
func (a *Acting) ActAs(writer port.Identity, bots []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, ActAsCall{Writer: writer, Bots: slices.Clone(bots)})
}

// CodeOwners implements port.CodeOwnerFinder: it returns what
// SetCodeOwners last set.
func (a *Acting) CodeOwners() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.codeOwners)
}

// SetCodeOwners sets the code owners' logins CodeOwners returns.
func (a *Acting) SetCodeOwners(logins ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.codeOwners = slices.Clone(logins)
}

// Login implements port.LoginFinder: it returns what SetLogin last set.
func (a *Acting) Login() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.login
}

// SetLogin sets the login Login returns.
func (a *Acting) SetLogin(login string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.login = login
}

// WriterLost implements port.WriterReporter: it returns what SetWriterLost
// last set.
func (a *Acting) WriterLost() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lost
}

// SetWriterLost sets the warning WriterLost returns, as when the tracker's
// writes went back to you; "" makes them go as the writer again.
func (a *Acting) SetWriterLost(warning string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lost = warning
}

// ActAsCalls returns the ActAs calls received so far, in order.
func (a *Acting) ActAsCalls() []ActAsCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ActAsCall, len(a.calls))
	for i, c := range a.calls {
		c.Bots = slices.Clone(c.Bots)
		out[i] = c
	}
	return out
}

// ActingTracker is a ReportingTracker that also implements port.Acting,
// port.CodeOwnerFinder, port.LoginFinder and port.WriterReporter, for the tests
// about crew acting as its bots. A plain *Tracker, PreparingTracker or
// ReportingTracker does not implement them.
type ActingTracker struct {
	ReportingTracker
	*Acting
}

// NewActingTracker returns an ActingTracker holding issues, whose Prepare and
// status writes succeed until told otherwise, which finds no code owner until
// SetCodeOwners and no login until SetLogin, and whose writes never went back
// to you until SetWriterLost.
func NewActingTracker(issues ...crew.Issue) ActingTracker {
	return ActingTracker{ReportingTracker: NewReportingTracker(issues...), Acting: &Acting{}}
}

// BoardTracker is a Tracker that also implements port.BoardLister, for the
// tests about a board the config draws. A plain *Tracker does not implement
// it.
type BoardTracker struct {
	*Tracker
}

// NewBoardTracker returns a BoardTracker holding issues, all open.
func NewBoardTracker(issues ...crew.Issue) BoardTracker {
	return BoardTracker{Tracker: NewTracker(issues...)}
}

// ListBoard implements port.BoardLister: the open issues of kind issue
// whose states or other labels match any of labels ignoring case, as
// GitHub compares them, oldest first and otherwise in the order they were
// added. Each carries the labels of labels it matches, in labels' spelling
// and order.
func (b BoardTracker) ListBoard(_ context.Context, labels []crew.State) ([]crew.BoardIssue, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []crew.BoardIssue
	for _, ti := range b.issues {
		if ti.closed || ti.issue.Kind != crew.KindIssue {
			continue
		}
		carries := slices.Concat(ti.labels, ti.issue.States)
		var matched []crew.State
		for _, l := range labels {
			if slices.ContainsFunc(carries, func(c crew.State) bool { return strings.EqualFold(string(c), string(l)) }) {
				matched = append(matched, l)
			}
		}
		if len(matched) > 0 {
			out = append(out, crew.BoardIssue{Issue: ti.issue.Clone(), Labels: matched})
		}
	}
	slices.SortStableFunc(out, func(x, y crew.BoardIssue) int { return x.Issue.Created.Compare(y.Issue.Created) })
	return out, nil
}
