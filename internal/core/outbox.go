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
	// settled, by issue id.
	runs map[crew.IssueID]*runLane
	// statuses holds each issue's status lane, by issue id; nil when
	// status reporting is off (KTD3).
	statuses map[crew.IssueID]*statusLane
	// pullRequests holds each issue's pull request lane, by issue id, while
	// it has a report not settled; nil when pull request reports are off
	// (KTD3).
	pullRequests map[crew.IssueID]*pullRequestLane
}

// runLane holds one held issue's take move, or its verdict move and failure
// report, not settled yet, in the order they were enqueued.
type runLane struct {
	deliveries []*delivery
	// owing is set at the first transient failure of one of its deliveries.
	// The lane is forgotten once its last delivery settles, so the issue
	// shows owed until then, through every retry.
	owing bool
}

// purpose is what a run-lane delivery does for its run.
type purpose int

// The purposes of a run-lane delivery.
const (
	// purposeTake moves the issue to its rule's running label.
	purposeTake purpose = iota
	// purposeVerdict moves the issue to its rule's success or failure
	// label.
	purposeVerdict
	// purposeReport posts the failure report.
	purposeReport
)

// delivery is one tracker write of a run lane.
type delivery struct {
	id      CallID
	purpose purpose
	// call is the write as CallOwed, CallDropped and View.Owed show it.
	call Call
	// report is the failure report a purposeReport delivery posts.
	report   crew.FailureReport
	inFlight bool
	owed     bool // failed transiently; retried at the next tick
	final    bool // its current or last attempt is its one try after stop
}

// outcome is how one of a run's deliveries settled: it landed, or crew gave
// it up. The run receives it as a fact (fact).
type outcome struct {
	purpose purpose
	landed  bool
	call    Call
	reason  string
}

// deliver enqueues d on the run lane of h's issue under a new CallID and
// makes its first attempt.
func (s *step) deliver(h *heldIssue, d *delivery) {
	o := &s.m.outbox
	o.lastID++
	d.id = o.lastID
	lane := o.runs[h.id()]
	if lane == nil {
		lane = &runLane{}
		o.runs[h.id()] = lane
	}
	lane.deliveries = append(lane.deliveries, d)
	s.attempt(d)
}

// attempt issues d's command.
func (s *step) attempt(d *delivery) {
	d.inFlight = true
	if d.call.Kind == CallReport {
		s.command(ReportFailure{ID: d.id, Report: cloneReport(d.report)})
		return
	}
	s.command(Move{ID: d.id, IssueID: d.call.IssueID, From: d.call.From, To: d.call.To})
}

// callResult settles, owes or retries the delivery r answers, and hands the
// run of its issue the outcome of one that settled, as a fact. A take and a
// verdict call are owed alike when they fail transiently: a take may have
// landed although it failed, so releasing its issue could strand it in the
// running label with no session, and the tracker makes the retry idempotent.
// A landed take starts the run's actions, and the core reports the run
// running unless they all ended already; the run releases itself once its
// verdict move and failure report settled, or its take was given up. A
// result for no delivery in flight, or for an issue no longer held, changes
// nothing.
func (s *step) callResult(r CallResult) {
	m := s.m
	id, d := m.outbox.find(r.ID)
	h := m.held(id)
	if d == nil || !d.inFlight || h == nil {
		return
	}
	d.inFlight = false
	out, settled := s.settleDelivery(id, d, r)
	if !settled {
		return
	}
	s.decide(h, out.fact(s.head(h)))
	if _, running := h.run.Phase().(crew.RunningPhase); running && out.purpose == purposeTake {
		s.reportRun(h)
	}
}

// settleDelivery applies r to d, a delivery of the issue identified by id,
// and returns its outcome when it settled. A transient failure makes d owed,
// and after a stop gives it its final try at once; a failure on its final
// try, or a call that cannot work, gives it up.
func (s *step) settleDelivery(id crew.IssueID, d *delivery, r CallResult) (outcome, bool) {
	o := &s.m.outbox
	switch {
	case r.Result == ResultDone:
	case r.Result == ResultFailed && (!s.m.stopping || !d.final):
		d.owed = true
		o.runs[id].owing = true
		s.emit(CallOwed{At: s.at, Call: d.call, Reason: r.Reason})
		if s.m.stopping {
			d.final = true
			s.attempt(d)
		}
		return outcome{}, false
	default:
		s.emit(CallDropped{At: s.at, Call: d.call, Result: r.Result, Reason: r.Reason})
	}
	o.settle(id, d)
	return outcome{purpose: d.purpose, landed: r.Result == ResultDone, call: d.call, reason: r.Reason}, true
}

// retryRun attempts the owed deliveries not in flight of the issue
// identified by id, each as its final try when final is set.
func (s *step) retryRun(id crew.IssueID, final bool) {
	lane := s.m.outbox.runs[id]
	if lane == nil {
		return
	}
	for _, d := range lane.deliveries {
		if d.owed && !d.inFlight {
			if final {
				d.final = true
			}
			s.attempt(d)
		}
	}
}

// find returns the unsettled run-lane delivery with id and its issue's id,
// or a nil delivery.
func (o *outbox) find(id CallID) (crew.IssueID, *delivery) {
	for issue, lane := range o.runs {
		for _, d := range lane.deliveries {
			if d.id == id {
				return issue, d
			}
		}
	}
	return crew.IssueID{}, nil
}

// settle forgets d, a delivery of the issue identified by id, and the
// issue's run lane once it holds none.
func (o *outbox) settle(id crew.IssueID, d *delivery) {
	lane := o.runs[id]
	lane.deliveries = slices.DeleteFunc(lane.deliveries, func(x *delivery) bool { return x == d })
	if len(lane.deliveries) == 0 {
		delete(o.runs, id)
	}
}

// owing reports whether a delivery of the run lane of the issue identified
// by id failed transiently and the lane has not emptied since.
func (o *outbox) owing(id crew.IssueID) bool {
	lane := o.runs[id]
	return lane != nil && lane.owing
}

// owedRun returns the owed deliveries of the run lane of the issue
// identified by id, in the order they were enqueued.
func (o *outbox) owedRun(id crew.IssueID) []Call {
	lane := o.runs[id]
	if lane == nil {
		return nil
	}
	var out []Call
	for _, d := range lane.deliveries {
		if d.owed {
			out = append(out, d.call)
		}
	}
	return out
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
