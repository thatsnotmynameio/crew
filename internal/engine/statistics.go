package engine

import (
	"context"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// statisticsWriter writes the records the core asks for to the statistics
// store, in the order it asked, on one goroutine of its own (KTD5). The loop
// only queues them, so a slow or locked store never delays a tick, a session
// or a tracker call.
type statisticsWriter struct {
	store port.Statistics
	mu    sync.Mutex
	queue []crew.Statistic
	// wake holds a value once the queue may hold records; close closes it.
	wake chan struct{}
}

// statisticsSource returns the writer of cfg's statistics store and the
// core's option to record statistics; neither without a store (AE12).
func statisticsSource(cfg Config) (*statisticsWriter, []core.Option) {
	if cfg.Statistics == nil {
		return nil, nil
	}
	return newStatisticsWriter(cfg.Statistics), []core.Option{core.RecordingStatistics(cfg.Version, cfg.Root)}
}

// newStatisticsWriter returns a writer to store that writes nothing until
// run.
func newStatisticsWriter(store port.Statistics) *statisticsWriter {
	return &statisticsWriter{store: store, wake: make(chan struct{}, 1)}
}

// add queues s after the records queued before it. It never waits.
func (w *statisticsWriter) add(s crew.Statistic) {
	w.mu.Lock()
	w.queue = append(w.queue, s)
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// close ends run once it wrote every record queued. No add follows it.
func (w *statisticsWriter) close() {
	close(w.wake)
}

// run writes the queued records to the store on ctx, one at a time and in
// order, and calls written with each record and its error, until close.
func (w *statisticsWriter) run(ctx context.Context, written func(crew.Statistic, error)) {
	for range w.wake {
		for s, ok := w.next(); ok; s, ok = w.next() {
			written(s, w.store.Record(ctx, s))
		}
	}
}

// next takes the first queued record; false when none is queued.
func (w *statisticsWriter) next() (crew.Statistic, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.queue) == 0 {
		return nil, false
	}
	s := w.queue[0]
	w.queue = w.queue[1:]
	return s, true
}

// recorded is the final message of a record the writer wrote: nothing more
// on success, and a StatisticFailed with the scrubbed reason otherwise.
func (e *Engine) recorded(s crew.Statistic, err error) {
	if err == nil {
		e.inbox <- message{final: true}
		return
	}
	e.post(core.StatisticFailed{Statistic: s, Reason: e.scrub(err.Error())})
}
