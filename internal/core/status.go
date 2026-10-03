package core

import (
	"fmt"
	"maps"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// statusSlot is what the core knows of one issue's status comment (KTD3). It
// is kept apart from the held issues, so a released issue's last statuses
// still land and a status write never holds a slot. At most one write is in
// flight.
type statusSlot struct {
	ref string
	// shown is what the comment shows, as far as the core knows; nil when
	// unknown, as after a failed write.
	shown *crew.Status
	// sending is the write in flight, nil when none.
	sending *crew.Status
	// waiting are the statuses not sent yet, oldest first, at most one per
	// stage run: a newer status of a run replaces the run's waiting one.
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
	// run is the id of the issue's current stage run, and runStage its
	// stage; runEnded is set once an ended status of it was reported.
	run      string
	runStage string
	runEnded bool
}

// busy reports whether the slot has a write in flight, waiting or owed.
func (sl *statusSlot) busy() bool {
	return sl.sending != nil || len(sl.waiting) > 0 || sl.owed != nil
}

// report sends st, unless status reporting is off or, outside a running
// issue, the comment already shows it or will (R5, R6). A write in flight or
// owed makes st wait.
func (s *step) report(st crew.Status) {
	m := s.m
	if m.statuses == nil {
		return
	}
	sl := m.statuses[st.IssueKey]
	if sl == nil {
		sl = &statusSlot{}
		m.statuses[st.IssueKey] = sl
	}
	sl.ref = st.IssueRef
	s.assignRun(sl, &st)
	if st.Kind != crew.StatusRunning {
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
	if sl.owed != nil && sl.owed.Run == st.Run {
		sl.owed = nil // st is newer
	}
	if n := len(sl.waiting); n > 0 && sl.waiting[n-1].Run == st.Run {
		sl.waiting[n-1] = st
	} else {
		sl.waiting = append(sl.waiting, st)
	}
	s.pump(sl)
}

// assignRun gives st the id of its stage run (R10): the issue's current run
// goes on until it ended and a status of another kind comes, or until a
// status of another stage comes. An id is the run's start time and a count,
// so ids differ across crew processes and within one.
func (s *step) assignRun(sl *statusSlot, st *crew.Status) {
	if sl.run == "" || st.Stage != sl.runStage || (sl.runEnded && st.Kind != crew.StatusEnded) {
		s.m.runs++
		sl.run = fmt.Sprintf("%s.%d", s.at.UTC().Format("20060102T150405.000000000Z"), s.m.runs)
		sl.runStage = st.Stage
	}
	sl.runEnded = st.Kind == crew.StatusEnded
	st.Run = sl.run
}

// pump sends the oldest waiting status, unless a write is in flight or owed.
func (s *step) pump(sl *statusSlot) {
	if sl.sending != nil || sl.owed != nil || len(sl.waiting) == 0 {
		return
	}
	next := sl.waiting[0]
	sl.waiting = sl.waiting[1:]
	s.send(sl, next)
}

// send issues the write of st for its slot.
func (s *step) send(sl *statusSlot, st crew.Status) {
	sl.sending = &st
	s.command(ReportStatus{Status: st.Clone()})
}

// statusResult settles the write in flight for r's issue and sends the
// oldest waiting status (KTD5). An ended status that failed transiently is
// owed, unless a newer status of its run waits to replace it, and holds back
// the waiting statuses of later runs until it lands or is given up.
func (s *step) statusResult(r StatusResult) {
	m := s.m
	sl := m.statuses[r.IssueKey]
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
			s.emit(StatusFailed{At: s.at, IssueKey: r.IssueKey, IssueRef: sl.ref, Result: r.Result, Reason: r.Reason})
		}
		superseded := len(sl.waiting) > 0 && sl.waiting[0].Run == sent.Run
		if r.Result == ResultFailed && sent.Kind == crew.StatusEnded && !superseded {
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

// retryStatuses resends each owed status, in issue-key order; after a stop,
// as its one final try.
func (s *step) retryStatuses() {
	for _, key := range slices.Sorted(maps.Keys(s.m.statuses)) {
		sl := s.m.statuses[key]
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

// statusesBusy reports whether any status write is in flight, waiting or
// owed.
func (m *Model) statusesBusy() bool {
	for _, sl := range m.statuses {
		if sl.busy() {
			return true
		}
	}
	return false
}

// running reports h's stage and its actions as they stand (R6, R7, R8). An
// action whose check runs is still running, since its session started; its
// session's last words are no longer current.
func (s *step) running(h *heldIssue) {
	st := s.status(h, crew.StatusRunning)
	for i, a := range h.actions {
		switch a.phase {
		case PhaseRunning:
			st.Actions[i].Started, st.Actions[i].Said = a.started, a.said
		case PhaseChecking:
			st.Actions[i].Started = a.started
		case PhaseWaiting, PhaseCreating, PhaseReopening, PhaseStarting, PhaseEnded:
			// No session runs: the action has no start time to report.
		}
	}
	s.report(st)
}

// ended reports h's stage as ended, with each action's final state and its
// move to the state to (R11).
func (s *step) ended(h *heldIssue, to crew.State, move crew.MoveProgress) {
	st := s.status(h, crew.StatusEnded)
	st.To, st.Move = to, move
	s.report(st)
}

// status returns h's status of kind, with each action's state, and for a
// failed action its cause and log. Only a check's reason goes with it: a
// session's or a tool's own words never do (R12). An action that resumed
// also names its workspace.
func (s *step) status(h *heldIssue, kind crew.StatusKind) crew.Status {
	st := crew.Status{
		IssueKey: h.issue.Key, IssueRef: h.issue.Ref, Stage: s.m.stages[h.stage].Name,
		Kind: kind, Updated: s.at,
	}
	for _, a := range h.actions {
		as := crew.ActionStatus{Name: a.name, State: crew.ActionRunning}
		switch {
		case a.phase != PhaseEnded:
		case a.outcome.Succeeded:
			as.State = crew.ActionSucceeded
		default:
			as.State, as.Cause, as.Log = crew.ActionFailed, a.cause, a.log
			if a.cause == crew.CauseCheck {
				as.Reason = a.outcome.Reason
			}
		}
		if a.resumed {
			as.Workspace = a.workspace
		}
		st.Actions = append(st.Actions, as)
	}
	return st
}

// sameStatus reports whether a and b show the same, whenever computed (R5).
func sameStatus(a, b crew.Status) bool {
	return a.IssueKey == b.IssueKey && a.IssueRef == b.IssueRef && a.Stage == b.Stage &&
		a.Kind == b.Kind && a.To == b.To && a.Move == b.Move && a.Run == b.Run &&
		slices.Equal(a.Actions, b.Actions)
}
