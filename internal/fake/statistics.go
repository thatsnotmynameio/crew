package fake

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Statistics = (*Statistics)(nil)

// StatisticsSettings is the fake store's config section. It has no key;
// any key is an error.
type StatisticsSettings struct{}

// StatisticsFactory returns a factory that validates its section into
// StatisticsSettings and, when it is valid, returns s itself, whatever the
// folder, so the test keeps its handle on the store.
func StatisticsFactory(s port.Statistics) port.StatisticsFactory {
	return func(decode port.Decode, _ string) (port.Statistics, error) {
		var settings StatisticsSettings
		if err := decode(&settings); err != nil {
			return nil, err
		}
		return s, nil
	}
}

// Statistics is an in-memory statistics store: it keeps the records it was
// given, fails them once the test says so, and holds them while the test
// blocks it. It is safe for concurrent use. Its zero value records every
// record at once.
type Statistics struct {
	mu       sync.Mutex
	recorded []crew.Statistic
	failure  error
	gate     chan struct{}
	closes   int
}

// NewStatistics returns an empty store.
func NewStatistics() *Statistics {
	return &Statistics{}
}

// FailRecords makes every later Record fail with err, recording nothing;
// a nil err makes them record again.
func (s *Statistics) FailRecords(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = err
}

// Block makes every later Record wait until Release, or until its context
// ends.
func (s *Statistics) Block() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate == nil {
		s.gate = make(chan struct{})
	}
}

// Release lets the records Block holds go on, and later ones record at once.
func (s *Statistics) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate != nil {
		close(s.gate)
		s.gate = nil
	}
}

// Recorded returns the records stored so far, in order.
func (s *Statistics) Recorded() []crew.Statistic {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.recorded)
}

// Closes returns how many times Close was called.
func (s *Statistics) Closes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// Record implements port.Statistics. While the store is blocked it waits
// for Release, and returns the context's error if ctx ends first.
func (s *Statistics) Record(ctx context.Context, st crew.Statistic) error {
	s.mu.Lock()
	gate := s.gate
	s.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return fmt.Errorf("the record was ended: %w", ctx.Err())
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return s.failure
	}
	s.recorded = append(s.recorded, st)
	return nil
}

// Close implements port.Statistics.
func (s *Statistics) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}
