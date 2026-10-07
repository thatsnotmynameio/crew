package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// recentEvents is how many of the latest events a Snapshot carries (KTD6).
const recentEvents = 100

// Update is what the engine publishes after each step of the core: the
// step's events and a snapshot of everything the engine holds (KTD6). A step
// may have no events; its snapshot is still new. The latest-wins subscribers
// also get an update without events whenever what the running sessions last
// said changes (KTD5).
type Update struct {
	// Events are the events the core published in this step, in order: the
	// run events the views word and the core's own.
	Events []core.Published
	// Snapshot is the engine's view after this step.
	Snapshot Snapshot
}

// Snapshot is the engine's view after a step: the issues the core holds, by
// rule and claim, their actions with start times, the owed calls, the issues
// handled this run, the latest events, when the run started, and what the
// running sessions last said. Its View shares no memory with the core.
type Snapshot struct {
	core.View

	// Recent are the last recentEvents events, oldest first.
	Recent []core.Published
	// Started is when the first poll ran, where the run time limit counts
	// from; zero before it.
	Started time.Time
	// RunTimeLimit is the run time limit; zero when there is none.
	RunTimeLimit time.Duration
	// Said is what each running session last said, as of the latest said
	// refresh, scrubbed and cut as for the status comment, in issue and
	// action order. A session that never said anything, or cannot narrate,
	// has no entry.
	Said []core.Said
}

// Queue is an ordered subscription for renderers that print every event,
// such as the line renderer. It holds a bounded number of updates; while it
// is full, new updates are dropped and their events counted, so a slow
// reader never blocks the engine.
type Queue struct {
	ch      chan Update
	dropped atomic.Int64
}

// Updates delivers the queued updates in publish order. It is closed once
// the engine has stopped, after the last update.
func (q *Queue) Updates() <-chan Update { return q.ch }

// Dropped returns how many events were dropped so far because the queue was
// full. A renderer reports it once it has drained the queue.
func (q *Queue) Dropped() int { return int(q.dropped.Load()) }

// stream fans each update out to the subscribers, never blocking the
// publisher. publish, publishLatest and close are called from the engine's
// loop only; subscribing is safe from any goroutine.
type stream struct {
	mu     sync.Mutex
	latest []chan Update
	queues []*Queue
}

func newStream() *stream { return &stream{} }

// subscribeLatest returns a channel holding at most the newest update: each
// publish replaces an update not yet received.
func (s *stream) subscribeLatest() <-chan Update {
	ch := make(chan Update, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = append(s.latest, ch)
	return ch
}

// subscribeQueue returns a queue holding up to capacity updates.
func (s *stream) subscribeQueue(capacity int) *Queue {
	q := &Queue{ch: make(chan Update, capacity)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queues = append(s.queues, q)
	return q
}

// publish sends u to every subscriber.
func (s *stream) publish(u Update) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceLatest(u)
	for _, q := range s.queues {
		select {
		case q.ch <- u:
		default:
			q.dropped.Add(int64(len(u.Events)))
		}
	}
}

// publishLatest sends u to the latest-wins subscribers only.
func (s *stream) publishLatest(u Update) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceLatest(u)
}

// replaceLatest puts u in every latest-wins subscription, in place of an
// update not yet received. The caller holds s.mu.
func (s *stream) replaceLatest(u Update) {
	for _, ch := range s.latest {
		// The publisher is the only sender, so once the stale update is
		// taken out, the send finds the slot free.
		select {
		case <-ch:
		default:
		}
		ch <- u
	}
}

// close ends every subscription, after the updates they still hold.
func (s *stream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.latest {
		close(ch)
	}
	for _, q := range s.queues {
		close(q.ch)
	}
}
