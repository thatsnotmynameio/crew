package engine

import (
	"context"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Repository returns the repository the engine read in Prepare, for the
// black-box tests.
func (e *Engine) Repository() crew.Repository { return e.repository }

// ReadQuestion runs the read of the comments c asks for, as the engine's
// goroutine does, and returns the input it posts, for the black-box tests.
func (e *Engine) ReadQuestion(ctx context.Context, c core.ReadQuestion) core.Input {
	e.readQuestion(ctx, c)
	return (<-e.inbox).input
}

// ReadReturn runs the read of the comments c asks for, as the engine's
// goroutine does, and returns the input it posts, for the black-box tests.
func (e *Engine) ReadReturn(ctx context.Context, c core.ReadReturn) core.Input {
	e.readReturn(ctx, c)
	return (<-e.inbox).input
}

// WriteStatistics queues records on a statistics writer for store while it
// runs on ctx, ends it and returns each record's error in the order it wrote
// them, for the black-box tests.
func WriteStatistics(ctx context.Context, store port.Statistics, records ...crew.Statistic) []error {
	w := newStatisticsWriter(store)
	var errs []error
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(ctx, func(_ crew.Statistic, err error) { errs = append(errs, err) })
	}()
	for _, s := range records {
		w.add(s)
	}
	w.close()
	<-done
	return errs
}
