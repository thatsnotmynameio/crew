package core

import (
	"time"
	"uuid"
)

// Update applies in to the model and returns the commands to run and the
// events to publish, in order. It is deterministic: the same model
// and input always give the same result. Inputs that answer nothing the
// core is waiting for, such as a result for a released run, change
// nothing.
func (m *Model) Update(in Input) ([]Command, []Published) {
	s := &step{m: m, at: in.arrival()}
	switch in := in.(type) {
	case RunInput:
		s.runInput(in)
	case SchedulerInput:
		s.schedulerInput(in)
	}
	s.windDown()
	if m.Stopped() && !m.stopped {
		m.stopped = true
		s.emit(Stopped{At: s.at})
	}
	return s.cmds, s.events
}

// schedulerInput applies an input about what spans rule runs: a tick, a
// stop, the run time, a listing, a board read, the bots, or a tracker
// write's or a record's result.
func (s *step) schedulerInput(in SchedulerInput) {
	switch in := in.(type) {
	case Tick:
		s.tick(in.Said)
	case StopRequested:
		s.m.requested = true
		s.stop()
	case TimeUp:
		s.timeUp(in.Limit)
	case IssuesListed:
		s.seed = in.Seed
		s.listed(in.Issues)
	case ListFailed:
		s.m.listing = false
		s.m.listingFailed(in.Reason)
		s.emit(ListingFailed{At: s.at, Reason: in.Reason})
	case BoardListed:
		s.m.boardListed(in.Issues)
	case BoardListFailed:
		s.m.boardListFailed(in.Reason)
	case BotsChecked:
		s.botsChecked(in)
	case CallResult:
		s.callResult(in)
	case StatusResult:
		s.statusResult(in)
	case PullRequestsResult:
		s.pullRequestsResult(in)
	case RecordFailed:
		if e, ok := notRecorded(in.Event, s.at, in.Reason); ok {
			s.emit(e)
		}
	}
}

// step is one Update in progress: the input's time and what it produced.
type step struct {
	m      *Model
	at     time.Time
	cmds   []Command
	events []Published
	// seed is the input's seed, from which the rule runs it takes get their
	// ids, and runs counts those runs (KTD5).
	seed uuid.UUID
	runs int
}

func (s *step) command(c Command) { s.cmds = append(s.cmds, c) }
func (s *step) emit(e Published)  { s.events = append(s.events, e) }

// windDown starts the stop sequence once the run time is up and no held
// issue has an action left to end, so owed calls get their final try (R5).
func (s *step) windDown() {
	m := s.m
	if !m.timeUp || m.stopping {
		return
	}
	for _, h := range m.issues {
		if !h.run.ActionsEnded() {
			return
		}
	}
	s.stop()
}

// Stopped reports whether a stop, requested or ending a wind-down, has
// completed: the core holds no issue, no owed call, no status write in
// flight or owed and no pull request report not settled. The engine returns
// once Stopped is true and none of its commands is still running.
func (m *Model) Stopped() bool {
	return m.stopping && len(m.issues) == 0 && m.outbox.idle()
}
