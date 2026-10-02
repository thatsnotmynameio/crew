package core

import (
	"maps"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// statusSlot is what the core knows of one issue's status comment (KTD3). It
// is kept apart from the held issues, so a queued issue has one too and a
// status write never holds a free slot. At most one write is in flight.
type statusSlot struct {
	ref string
	// shown is what the comment shows, as far as the core knows; nil when
	// unknown, as after a failed write.
	shown *crew.Status
	// sending is the write in flight, nil when none; next is the newest
	// status not sent yet, sent once sending returns.
	sending, next *crew.Status
	// owed is an ended status whose write failed transiently, retried at the
	// next tick or, after a stop, once (KTD5).
	owed *crew.Status
	// final is set once an ended status got its one more try after a stop.
	final bool
	// failing is set while writes fail, so failures in a row are reported
	// once.
	failing bool
}

// busy reports whether the slot has a write in flight, waiting or owed.
func (sl *statusSlot) busy() bool {
	return sl.sending != nil || sl.next != nil || sl.owed != nil
}

// report sends st, unless status reporting is off or, outside a running
// issue, the comment already shows it or will (R5, R6). A write in flight
// makes st wait as the slot's newest status.
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
	if st.Kind != crew.StatusRunning {
		latest := sl.shown
		if sl.sending != nil {
			latest = sl.sending
		}
		if sl.next != nil {
			latest = sl.next
		}
		if latest != nil && sameStatus(*latest, st) {
			return
		}
	}
	sl.owed = nil // st is newer
	if sl.sending != nil {
		sl.next = &st
		return
	}
	s.send(sl, st)
}

// send issues the write of st for its slot.
func (s *step) send(sl *statusSlot, st crew.Status) {
	sl.sending = &st
	s.command(ReportStatus{Status: st.Clone()})
}

// statusResult settles the write in flight for r's issue and sends the
// newest waiting status (KTD5).
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
		if r.Result == ResultFailed && sent.Kind == crew.StatusEnded && sl.next == nil {
			switch {
			case !m.stopping:
				sl.owed = sent
			case !sl.final:
				sl.final = true
				s.send(sl, *sent)
				return
			}
		}
	}
	if sl.next != nil {
		next := *sl.next
		sl.next = nil
		s.send(sl, next)
	}
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

// queued reports issue as queued for stage si, waiting for a free slot (R4).
func (s *step) queued(si int, issue crew.Issue) {
	s.report(crew.Status{
		IssueKey: issue.Key, IssueRef: issue.Ref, Stage: s.m.stages[si].Name,
		Kind: crew.StatusQueued, Slots: s.m.maxParallel, Updated: s.at,
	})
}

// running reports h's stage and its actions as they stand (R6, R7, R8).
func (s *step) running(h *heldIssue) {
	st := s.status(h, crew.StatusRunning)
	for i, a := range h.actions {
		if a.phase == PhaseRunning {
			st.Actions[i].Started, st.Actions[i].Said = a.started, a.said
		}
	}
	s.report(st)
}

// ended reports h's stage as ended, with each action's final state and the
// move to to (R11).
func (s *step) ended(h *heldIssue, to crew.State, move crew.MoveProgress) {
	st := s.status(h, crew.StatusEnded)
	st.To, st.Move = to, move
	s.report(st)
}

// status returns h's status of kind, with each action's state.
func (s *step) status(h *heldIssue, kind crew.StatusKind) crew.Status {
	st := crew.Status{
		IssueKey: h.issue.Key, IssueRef: h.issue.Ref, Stage: s.m.stages[h.stage].Name,
		Kind: kind, Updated: s.at,
	}
	for _, a := range h.actions {
		state := crew.ActionRunning
		if a.phase == PhaseEnded {
			state = crew.ActionFailed
			if a.outcome.Succeeded {
				state = crew.ActionSucceeded
			}
		}
		st.Actions = append(st.Actions, crew.ActionStatus{Name: a.name, State: state})
	}
	return st
}

// sameStatus reports whether a and b show the same, whenever computed (R5).
func sameStatus(a, b crew.Status) bool {
	return a.IssueKey == b.IssueKey && a.IssueRef == b.IssueRef && a.Stage == b.Stage &&
		a.Kind == b.Kind && a.Slots == b.Slots && a.To == b.To && a.Move == b.Move &&
		slices.Equal(a.Actions, b.Actions)
}
