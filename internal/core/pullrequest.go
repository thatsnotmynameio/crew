package core

import (
	"maps"
	"slices"
	"strconv"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// pullRequestSlot holds one issue's pull request reports not settled yet
// (KTD3). It is kept apart from the held issues, so a report never holds the
// issue nor changes its claim. At most one report is in flight.
type pullRequestSlot struct {
	// reports are oldest first, in the order their moves landed. The first
	// is in flight when sending is set, or owed; the others wait behind it.
	reports []*pendingReport
	sending bool
}

// pendingReport is one report of a slot.
type pendingReport struct {
	report crew.PullRequestReport
	owed   bool // failed transiently; retried at the next tick
	final  bool // its current or last attempt is its one try after stop
}

// reportPullRequests queues the report that follows h's move to to, which
// landed, unless pull request reports are off (KTD2). ended is set when the
// move ended h's stage, so the report carries how it ended. The report's ID
// is fixed for its life (KTD7).
func (s *step) reportPullRequests(h *heldIssue, to crew.State, ended bool) {
	m := s.m
	if m.pullRequests == nil {
		return
	}
	m.lastID++
	r := crew.PullRequestReport{
		ID: strconv.FormatUint(uint64(m.lastID), 10), IssueKey: h.issue.Key, IssueRef: h.issue.Ref, State: to,
	}
	if ended {
		r.End = s.stageEnd(h)
	}
	sl := m.pullRequests[r.IssueKey]
	if sl == nil {
		sl = &pullRequestSlot{}
		m.pullRequests[r.IssueKey] = sl
	}
	sl.reports = append(sl.reports, &pendingReport{report: r})
	s.pumpPullRequests(sl)
}

// stageEnd returns how h's stage ended, with each action as its ended status
// shows it.
func (s *step) stageEnd(h *heldIssue) *crew.StageEnd {
	return &crew.StageEnd{Stage: s.m.stages[h.stage].Name, Actions: s.status(h, crew.StatusEnded).Actions}
}

// pumpPullRequests sends the slot's oldest report, unless a report is in
// flight or owed.
func (s *step) pumpPullRequests(sl *pullRequestSlot) {
	if sl.sending || len(sl.reports) == 0 || sl.reports[0].owed {
		return
	}
	s.sendPullRequests(sl)
}

// sendPullRequests issues the slot's oldest report.
func (s *step) sendPullRequests(sl *pullRequestSlot) {
	sl.sending = true
	s.command(ReportPullRequests{Report: sl.reports[0].report.Clone()})
}

// pullRequestsResult settles the report in flight for r's issue and sends the
// next one (R7). A report that failed transiently is owed and holds back the
// issue's later reports; after a stop it gets one final try. A report that
// cannot work, or failed its final try, is dropped.
func (s *step) pullRequestsResult(r PullRequestsResult) {
	m := s.m
	sl := m.pullRequests[r.IssueKey]
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
		delete(m.pullRequests, r.IssueKey)
		return
	}
	s.pumpPullRequests(sl)
}

// retryPullRequests resends each owed report not in flight, in issue-key
// order; after a stop, as its one final try.
func (s *step) retryPullRequests() {
	for _, key := range slices.Sorted(maps.Keys(s.m.pullRequests)) {
		sl := s.m.pullRequests[key]
		if sl.sending || !sl.reports[0].owed {
			continue
		}
		if s.m.stopping {
			sl.reports[0].final = true
		}
		s.sendPullRequests(sl)
	}
}

// owedPullRequests returns the owed reports, in issue-key order.
func (m *Model) owedPullRequests() []Call {
	var out []Call
	for _, key := range slices.Sorted(maps.Keys(m.pullRequests)) {
		if p := m.pullRequests[key].reports[0]; p.owed {
			out = append(out, p.describe())
		}
	}
	return out
}

// describe returns p as a Call.
func (p *pendingReport) describe() Call {
	return Call{Kind: CallPullRequests, IssueKey: p.report.IssueKey, IssueRef: p.report.IssueRef, To: p.report.State}
}
