package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// pullRequestLane is the outbox's lane of one issue's pull request reports
// not settled yet (KTD3, KTD8). It is kept apart from the held issues, so a
// report never holds the issue nor changes its claim. At most one report is
// in flight.
type pullRequestLane struct {
	// reports are oldest first, in the order their moves landed. The first
	// is in flight when sending is set, or owed; the others wait behind it.
	reports []*pendingReport
	sending bool
}

// pendingReport is one report of a lane.
type pendingReport struct {
	report crew.PullRequestReport
	owed   bool // failed transiently; retried at the next tick
	final  bool // its current or last attempt is its one try after stop
}

// reportPullRequests queues report, which follows a move of its issue that
// landed, unless pull request reports are off (KTD2). Its ID comes from the
// rule run and the move, so it is fixed for its life (KTD7).
func (s *step) reportPullRequests(report crew.PullRequestReport) {
	m := s.m
	if m.outbox.pullRequests == nil {
		return
	}
	sl := m.outbox.pullRequests[report.IssueID()]
	if sl == nil {
		sl = &pullRequestLane{}
		m.outbox.pullRequests[report.IssueID()] = sl
	}
	sl.reports = append(sl.reports, &pendingReport{report: report})
	s.pumpPullRequests(sl)
}

// reportVerdict queues the report that follows the verdict move of h's run,
// which landed. It carries how the rule ended, unless the rule has no
// actions: nobody stopped watching anything, so there is nothing to tell
// (KTD5).
func (s *step) reportVerdict(h *heldIssue) {
	if report, ok := h.run.VerdictReport(s.m.statusUsage); ok {
		s.reportPullRequests(report)
	}
}

// pumpPullRequests sends the lane's oldest report, unless a report is in
// flight or owed.
func (s *step) pumpPullRequests(sl *pullRequestLane) {
	if sl.sending || len(sl.reports) == 0 || sl.reports[0].owed {
		return
	}
	s.sendPullRequests(sl)
}

// sendPullRequests issues the lane's oldest report.
func (s *step) sendPullRequests(sl *pullRequestLane) {
	sl.sending = true
	s.command(ReportPullRequests{Report: sl.reports[0].report})
}

// pullRequestsResult settles the report in flight for r's issue and sends the
// next one (R7). A report that failed transiently is owed and holds back the
// issue's later reports; after a stop it gets one final try. A report that
// cannot work, or failed its final try, is dropped.
func (s *step) pullRequestsResult(r PullRequestsResult) {
	m := s.m
	sl := m.outbox.pullRequests[r.IssueID]
	if sl == nil || !sl.sending {
		return
	}
	sl.sending = false
	p := sl.reports[0]
	switch {
	case r.Result == ResultDone:
	case r.Result == ResultFailed && (!m.stopping || !p.final):
		p.owed = true
		s.emit(CallOwed{At: s.at, Call: p.describe(), Reason: r.Reason})
		if m.stopping {
			p.final = true
			s.sendPullRequests(sl)
		}
		return
	default:
		s.emit(CallDropped{At: s.at, Call: p.describe(), Result: r.Result, Reason: r.Reason})
	}
	sl.reports = sl.reports[1:]
	if len(sl.reports) == 0 {
		delete(m.outbox.pullRequests, r.IssueID)
		return
	}
	s.pumpPullRequests(sl)
}

// retryPullRequests resends each owed report not in flight, in issue id
// order; after a stop, as its one final try.
func (s *step) retryPullRequests() {
	for _, key := range sortedIssueIDs(s.m.outbox.pullRequests) {
		sl := s.m.outbox.pullRequests[key]
		if sl.sending || !sl.reports[0].owed {
			continue
		}
		if s.m.stopping {
			sl.reports[0].final = true
		}
		s.sendPullRequests(sl)
	}
}

// owedPullRequests returns the owed reports, in issue id order.
func (o *outbox) owedPullRequests() []Call {
	var out []Call
	for _, key := range sortedIssueIDs(o.pullRequests) {
		if p := o.pullRequests[key].reports[0]; p.owed {
			out = append(out, p.describe())
		}
	}
	return out
}

// describe returns p as a Call.
func (p *pendingReport) describe() Call {
	return Call{Kind: CallPullRequests, IssueID: p.report.IssueID(), IssueRef: p.report.IssueRef(), To: p.report.State()}
}
