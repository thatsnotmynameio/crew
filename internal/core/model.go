// Package core is crew's rules as a pure reducer: Model.Update takes an
// Input and returns the Commands to run and the domain Events to publish.
// It performs no I/O, reads no clock, starts no goroutine and builds no path;
// times and paths arrive as input data (R11, KTD2, KTD12). The engine's loop
// is its only caller, from one goroutine.
//
// Each issue the core holds moves through claim states kept apart from the
// tracker's states: Taking, then Running (or Stopping), then Judging. The
// core's outbox delivers the tracker writes a held issue's rule decides on,
// and the view shows the issue Owed while one of them waits for a retry
// (KTD8). An issue is released when its verdict calls are settled, or when
// its take is given up.
package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Model is the core's state. Its zero value is not usable; use New. A Model
// is not safe for concurrent use: one goroutine owns it (KTD2).
type Model struct {
	rules       []crew.Rule
	maxParallel int
	issues      []*heldRun // in the order they were taken
	listing     bool       // a ListIssues is outstanding
	listings    int        // the ListIssues asked for: the generation of the last one (KTD4)
	skipped     int        // ticks that skipped their listing since the last one
	timeUp      bool       // the run time is up: take nothing new
	requested   bool       // a stop was requested
	stopping    bool       // the stop sequence runs: requested, or ending a wind-down
	stopped     bool       // the Stopped event was emitted
	// outbox delivers the tracker writes the rules decide on (KTD8).
	outbox outbox
	// queueOf holds the queue each rule runs in, by rule index, as an
	// index into queues (KTD2). maxParallel caps every queue together.
	queueOf []int
	// queues holds each queue some rule runs in, with the slots it has.
	queues []crew.Queue
	// slots is what the rules can use: the queues' slots summed, at most
	// maxParallel (KTD4).
	slots int
	// handled holds one entry per issue whose rule ended this run, in the
	// order the issues were released.
	handled []handledEntry
	// journal is the rule runs' past; nil when the model journals no runs
	// (KTD12).
	journal *journal
	// reopening is set when the workspace can reopen a failed run's
	// workspace (KTD4).
	reopening bool
	// finding is set when the tracker can find the pull request an action
	// opened (KTD3).
	finding bool
	// statusUsage is set when statuses show each ended action's spend and
	// pull request (KTD11).
	statusUsage bool
	// spent sums what every session that ended this run used (R14).
	spent crew.Spend
	// otherKinds holds, by item id, the rule label of each item the last
	// listing found in the label of a rule of the other kind, which was
	// reported then or before (#92).
	otherKinds map[crew.IssueID]crew.State
	// board is the board the model reads; nil when it reads none (KTD4).
	board *board
	// bots is what the model knows of the identities crew acts as (KTD3).
	bots bots
}

// New returns a model for rules, whose rules are in config order and
// already validated, taking at most maxParallelIssues issues at once (R6),
// and for each rule at most its queue's slots (R6, KTD2).
func New(rules []crew.Rule, maxParallelIssues int, opts ...Option) *Model {
	own := make([]crew.Rule, len(rules))
	for i, r := range rules {
		r.Actions = slices.Clone(r.Actions)
		own[i] = r
	}
	m := &Model{rules: own, maxParallel: maxParallelIssues, outbox: outbox{runs: map[crew.IssueID]*runLane{}}}
	m.queueOf, m.queues, m.slots = queues(own, maxParallelIssues)
	for _, o := range opts {
		o(m)
	}
	return m
}

// Option changes a new Model.
type Option func(*Model)

// FindingPullRequests has the model look up the pull request each action
// opened, through FindPullRequest commands, once its session ended (KTD3).
func FindingPullRequests() Option {
	return func(m *Model) { m.finding = true }
}

// ReportingUsage has the statuses the model reports show each ended
// action's spend and pull request (KTD11). It takes effect only with
// ReportingStatus.
func ReportingUsage() Option {
	return func(m *Model) { m.statusUsage = true }
}

// ReportingStatus has the model report each issue's status through
// ReportStatus commands, for a tracker that keeps status comments (KTD1).
func ReportingStatus() Option {
	return func(m *Model) { m.outbox.statuses = map[crew.IssueID]*statusLane{} }
}

// ReportingPullRequests has the model follow each move that landed with a
// ReportPullRequests command, for a tracker that reports on pull requests
// (KTD1, KTD2).
func ReportingPullRequests() Option {
	return func(m *Model) { m.outbox.pullRequests = map[crew.IssueID]*pullRequestLane{} }
}

// unknownName is what String gives for a value outside its enumeration.
const unknownName = "unknown"
