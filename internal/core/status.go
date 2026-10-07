package core

import (
	"reflect"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// statusLane is the outbox's lane of one issue's status comment, and what
// the core knows of that comment (KTD3, KTD8). It is kept apart from the
// held issues, for the whole run, so a released issue's last statuses still
// land, a status write never holds a slot, and a later rule run of the issue
// still finds the comment's entry. At most one write is in flight.
type statusLane struct {
	ref string
	// shown is what the comment shows, as far as the core knows; nil when
	// unknown, as after a failed write.
	shown *crew.Status
	// sending is the write in flight, nil when none.
	sending *crew.Status
	// waiting are the statuses not sent yet, oldest first, at most one per
	// rule run: a newer status of a run replaces the run's waiting one.
	// They are sent in order once nothing is in flight or owed, so an earlier
	// run's ended status lands before the next run's statuses.
	waiting []crew.Status
	// owed is an ended status whose write failed transiently, retried at the
	// next tick or, after a stop, once (KTD5).
	owed *crew.Status
	// final is set once an ended status got its one more try after a stop.
	final bool
	// failing is set while writes fail, so failures in a row are reported
	// once.
	failing bool
	// run is the id of the comment's current entry: the id of the rule run
	// whose status opened it. runRule is its rule; runEnded is set once an
	// ended status of it was reported.
	run      crew.RuleRunID
	runRule  crew.RuleName
	runEnded bool
}

// busy reports whether the lane has a write in flight, waiting or owed.
func (sl *statusLane) busy() bool {
	return sl.sending != nil || len(sl.waiting) > 0 || sl.owed != nil
}

// report sends st, unless status reporting is off or, outside a running
// issue, the comment already shows it or will (R5, R6). A write in flight or
// owed makes st wait.
func (s *step) report(st crew.Status) {
	m := s.m
	if m.outbox.statuses == nil {
		return
	}
	sl := m.outbox.statuses[st.IssueID()]
	if sl == nil {
		sl = &statusLane{}
		m.outbox.statuses[st.IssueID()] = sl
	}
	sl.ref = st.IssueRef()
	st = s.assignRun(sl, st)
	if _, running := st.Progress().(crew.StatusRunning); !running {
		latest := sl.shown
		if sl.sending != nil {
			latest = sl.sending
		}
		if n := len(sl.waiting); n > 0 {
			latest = &sl.waiting[n-1]
		}
		if latest != nil && sameStatus(*latest, st) {
			return
		}
	}
	if sl.owed != nil && sl.owed.Run() == st.Run() {
		sl.owed = nil // st is newer
	}
	if n := len(sl.waiting); n > 0 && sl.waiting[n-1].Run() == st.Run() {
		sl.waiting[n-1] = st
	} else {
		sl.waiting = append(sl.waiting, st)
	}
	s.pump(sl)
}

// assignRun returns st with the id of its comment entry (R10): the issue's
// current entry goes on until it ended and a running status comes, or until
// a status of another rule comes. A new entry takes the id of the rule run
// st comes from, which is global (KTD5); every status of the entry carries
// it.
func (*step) assignRun(sl *statusLane, st crew.Status) crew.Status {
	_, ended := st.Progress().(crew.StatusEnded)
	if sl.run == "" || st.Rule() != sl.runRule || (sl.runEnded && !ended) {
		sl.run, sl.runRule = st.Run(), st.Rule()
	}
	sl.runEnded = ended
	d := st.Data()
	d.Run = sl.run
	return crew.NewStatus(d)
}

// pump sends the oldest waiting status, unless a write is in flight or owed.
func (s *step) pump(sl *statusLane) {
	if sl.sending != nil || sl.owed != nil || len(sl.waiting) == 0 {
		return
	}
	next := sl.waiting[0]
	sl.waiting = sl.waiting[1:]
	s.send(sl, next)
}

// send issues the write of st for its lane.
func (s *step) send(sl *statusLane, st crew.Status) {
	sl.sending = &st
	s.command(ReportStatus{Status: st})
}

// statusResult settles the write in flight for r's issue and sends the
// oldest waiting status (KTD5). An ended status that failed transiently is
// owed, unless a newer status of its run waits to replace it, and holds back
// the waiting statuses of later runs until it lands or is given up.
func (s *step) statusResult(r StatusResult) {
	m := s.m
	sl := m.outbox.statuses[r.IssueID]
	if sl == nil || sl.sending == nil {
		return
	}
	sent := sl.sending
	sl.sending = nil
	if r.Result == ResultDone {
		sl.shown, sl.failing = sent, false
	} else {
		sl.shown = nil
		if !sl.failing {
			sl.failing = true
			s.emit(StatusFailed{At: s.at, IssueID: r.IssueID, IssueRef: sl.ref, Result: r.Result, Reason: r.Reason})
		}
		superseded := len(sl.waiting) > 0 && sl.waiting[0].Run() == sent.Run()
		if _, ended := sent.Progress().(crew.StatusEnded); r.Result == ResultFailed && ended && !superseded {
			switch {
			case !m.stopping:
				sl.owed = sent
				return
			case !sl.final:
				sl.final = true
				s.send(sl, *sent)
				return
			}
		}
	}
	s.pump(sl)
}

// retryStatuses resends each owed status, in issue id order; after a stop,
// as its one final try.
func (s *step) retryStatuses() {
	for _, key := range sortedIssueIDs(s.m.outbox.statuses) {
		sl := s.m.outbox.statuses[key]
		if sl.owed == nil || sl.sending != nil {
			continue
		}
		owed := *sl.owed
		sl.owed = nil
		if s.m.stopping {
			sl.final = true
		}
		s.send(sl, owed)
	}
}

// running reports h's rule and its actions as they stand (R6, R7, R8).
func (s *step) running(h *heldIssue) {
	s.report(s.status(h, crew.StatusRunning{}))
}

// ended reports h's rule as ended, with each action's final state and its
// move to the state to (R11).
func (s *step) ended(h *heldIssue, to crew.State, move crew.MoveProgress) {
	s.report(s.status(h, crew.StatusEnded{To: to, Move: move}))
}

// status returns h's status with progress and its actions as they stand.
func (s *step) status(h *heldIssue, progress crew.StatusProgress) crew.Status {
	return crew.NewStatus(crew.StatusData{
		IssueID: h.issue.ID(), IssueRef: h.issue.Ref(), Rule: s.m.rules[h.rule].Name,
		Progress: progress, Actions: s.actionStatuses(h), Updated: s.at, Run: h.run,
	})
}

// actionStatuses returns h's actions as a status shows them: each one's
// state and how its checks that ran so far ended. Only a check's reason goes
// with them: a session's or a tool's own words never do (R12). An action
// that resumed also names its workspace.
func (s *step) actionStatuses(h *heldIssue) []crew.ActionStatus {
	out := make([]crew.ActionStatus, 0, len(h.actions))
	for _, a := range h.actions {
		as := crew.ActionStatus{Name: a.name, State: s.actionState(a), Checks: slices.Clone(a.results)}
		if a.resumed {
			as.Workspace = a.workspace
		}
		out = append(out, as)
	}
	return out
}

// actionState returns how a stands. An action whose check runs is still
// running, since its session started; its session's last words are no
// longer current. A failed action carries its cause and log. With
// ReportingUsage, an ended action whose session started also carries what
// it spent and its pull request.
func (s *step) actionState(a *actionRun) crew.ActionState {
	switch a.phase {
	case PhaseRunning:
		return crew.ActionRunning{Started: a.started, Said: a.said}
	case PhaseChecking:
		return crew.ActionRunning{Started: a.started}
	case PhaseEnded:
		var usage crew.Optional[crew.ShownUsage]
		if s.m.statusUsage && !a.started.IsZero() {
			usage = crew.Some(crew.ShownUsage{Spend: a.spend(), PullRequest: a.pr})
		}
		if a.outcome.Succeeded {
			return crew.ActionSucceeded{Usage: usage}
		}
		return crew.ActionFailed{Cause: a.cause, Log: a.log, Usage: usage}
	case PhaseWaiting, PhaseCreating, PhaseReopening, PhaseStarting, PhaseFinishing:
		// No session runs: the action has no start time to report.
	}
	return crew.ActionPending{}
}

// sameStatus reports whether a and b show the same, whenever computed (R5).
func sameStatus(a, b crew.Status) bool {
	ad, bd := a.Data(), b.Data()
	ad.Updated = bd.Updated
	return reflect.DeepEqual(ad, bd)
}
