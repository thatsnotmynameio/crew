package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// outbox delivers the tracker writes the core decides on (KTD8). It owns
// whether each one is in flight, owed or on its final try, and hands a held
// run only the outcome of a delivery that settled. Its lanes are kept by
// issue, each with a life of its own: the run lane of a held issue lives
// while the issue has a delivery not settled, an issue's status lane for the
// whole run, and its pull request lane while it has a report not settled.
// Only the run lane holds a slot: an owed status or report never does.
type outbox struct {
	// lastID is the CallID of the last delivery enqueued.
	lastID CallID
	// runs holds the run lane of each held issue with a delivery not
	// settled, by issue id: its take move or one step of its route, as a
	// run asks a step only once the one before it settled (KTD9).
	runs map[crew.IssueID]*delivery
	// statuses holds each issue's status lane, by issue id; nil when
	// status reporting is off (KTD3).
	statuses map[crew.IssueID]*statusLane
	// pullRequests holds each issue's pull request lane, by issue id, while
	// it has a report not settled; nil when pull request reports are off
	// (KTD3).
	pullRequests map[crew.IssueID]*pullRequestLane
}

// purpose is what a run-lane delivery does for its run.
type purpose int

// The purposes of a run-lane delivery.
const (
	// purposeTake moves the issue to its rule's running label.
	purposeTake purpose = iota
	// purposeStep delivers a tracker step of the run's route: a move, a
	// close, a comment or a report (KTD9).
	purposeStep
)

// delivery is one tracker write of a run lane.
type delivery struct {
	id      CallID
	purpose purpose
	// step is the index in its route of the step a purposeStep delivery
	// delivers.
	step int
	// call is the write as CallOwed, CallDropped and View.Owed show it.
	call Call
	// report is the failure report a report step posts, and body the
	// comment a comment step posts.
	report crew.FailureReport
	body   string
	// waiting is set on a close that waits for the pull request report in
	// flight of its issue, before its first try (KTD-S14).
	waiting  bool
	inFlight bool
	owed     bool // failed transiently; retried at the next tick
	final    bool // its current or last attempt is its one try after stop
}

// deliver enqueues d on the run lane of h's issue under a new CallID and
// makes its first attempt. A close waits until no pull request report of
// the issue is in flight (sendWaiting).
func (s *step) deliver(h *heldRun, d *delivery) {
	o := &s.m.outbox
	o.lastID++
	d.id = o.lastID
	o.runs[h.id()] = d
	if d.call.Kind == CallClose && o.reporting(h.id()) {
		d.waiting = true
		return
	}
	s.attempt(d)
}

// sendWaiting makes the first attempt of the close waiting in the run lane
// of the issue identified by id, once no pull request report of the issue
// is in flight.
func (s *step) sendWaiting(id crew.IssueID) {
	o := &s.m.outbox
	if d := o.runs[id]; d != nil && d.waiting && !o.reporting(id) {
		d.waiting = false
		s.attempt(d)
	}
}

// attempt issues d's command. A close first drops its issue's pull request
// reports not settled, none of which is in flight, so none puts a crew
// label back on a pull request the close took crew's labels off
// (KTD-S14).
func (s *step) attempt(d *delivery) {
	d.inFlight = true
	c := d.call
	switch c.Kind {
	case CallReport:
		s.command(ReportFailure{ID: d.id, Report: cloneReport(d.report)})
	case CallComment:
		s.command(Comment{ID: d.id, IssueID: c.IssueID, Body: d.body})
	case CallClose:
		s.m.outbox.dropPullRequests(c.IssueID)
		s.command(Close{ID: d.id, IssueID: c.IssueID, From: c.From})
	case CallMove, CallPullRequests:
		s.command(Move{ID: d.id, IssueID: c.IssueID, From: c.From, To: c.To})
	}
}

// callResult settles, owes or retries the delivery r answers, and hands the
// run of its issue the outcome of one that settled, as a fact. A take and a
// step are owed alike when they fail transiently: a take may have landed
// although it failed, so releasing its issue could strand it in the running
// label with no session, and the tracker makes the retry idempotent. A
// landed take starts the run's actions, and the core reports the run
// running unless its sequence ended already; the run asks its route's next
// step once one settled, and releases itself once its final step settled,
// or its take was given up. A result for no delivery in flight, or for an
// issue no longer held, changes nothing.
func (s *step) callResult(r CallResult) {
	m := s.m
	id, d := m.outbox.find(r.ID)
	h := m.held(id)
	if d == nil || !d.inFlight || h == nil {
		return
	}
	d.inFlight = false
	fact, settled := s.settleDelivery(h, d, r)
	if !settled {
		return
	}
	s.decide(h, fact)
	if _, running := h.run.Phase().(crew.RunningPhase); running && d.purpose == purposeTake {
		s.reportRun(h)
	}
}

// settleDelivery applies r to d, a delivery of h's issue, and returns the
// fact that tells h's run how it settled, when it did. A transient failure
// makes d owed, and after a stop gives it its final try at once; a failure
// on its final try, or a call that cannot work, gives it up.
func (s *step) settleDelivery(h *heldRun, d *delivery, r CallResult) (crew.Fact, bool) {
	switch {
	case r.Result == ResultDone:
	case r.Result == ResultFailed && (!s.m.stopping || !d.final):
		d.owed = true
		s.emit(CallOwed{At: s.at, Call: d.call, Reason: r.Reason})
		if s.m.stopping {
			d.final = true
			s.attempt(d)
		}
		return nil, false
	default:
		s.emit(CallDropped{At: s.at, Call: d.call, Result: r.Result, Reason: r.Reason})
	}
	delete(s.m.outbox.runs, h.id())
	return s.settled(h, d, r.Result, r.Reason), true
}

// retryRun attempts the owed delivery not in flight of the issue
// identified by id, as its final try when final is set.
func (s *step) retryRun(id crew.IssueID, final bool) {
	d := s.m.outbox.runs[id]
	if d == nil || !d.owed || d.inFlight {
		return
	}
	if final {
		d.final = true
	}
	s.attempt(d)
}

// find returns the unsettled run-lane delivery with id and its issue's id,
// or a nil delivery.
func (o *outbox) find(id CallID) (crew.IssueID, *delivery) {
	for issue, d := range o.runs {
		if d.id == id {
			return issue, d
		}
	}
	return crew.IssueID{}, nil
}

// owing reports whether the delivery of the run lane of the issue
// identified by id failed transiently and has not settled since.
func (o *outbox) owing(id crew.IssueID) bool {
	d := o.runs[id]
	return d != nil && d.owed
}

// owedRun returns the owed delivery of the run lane of the issue
// identified by id, if any.
func (o *outbox) owedRun(id crew.IssueID) []Call {
	if !o.owing(id) {
		return nil
	}
	return []Call{o.runs[id].call}
}

// idle reports whether the outbox has no status write in flight, waiting or
// owed and no pull request report not settled. Run lanes are left out: each
// belongs to a held issue, which the core waits for anyway.
func (o *outbox) idle() bool {
	for _, sl := range o.statuses {
		if sl.busy() {
			return false
		}
	}
	return len(o.pullRequests) == 0
}

// cloneReport copies r, so the copy shares no slice with it.
func cloneReport(r crew.FailureReport) crew.FailureReport {
	r.Failures = slices.Clone(r.Failures)
	return r
}
