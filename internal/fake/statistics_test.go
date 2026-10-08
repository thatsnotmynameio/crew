package fake_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestStatisticsRecordsInOrderUntilItsRecordsFail(t *testing.T) {
	s := fake.NewStatistics()
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	first := crew.Process{ID: "process-1", Version: "v0.1.1", Folder: "/repo", Start: start}
	second := crew.Process{ID: "process-2", Version: "dev", Folder: "/other", Start: start.Add(time.Hour)}
	for _, p := range []crew.Statistic{first, second} {
		if err := s.Record(t.Context(), p); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	full := errors.New("disk full")
	s.FailRecords(full)
	if err := s.Record(t.Context(), crew.Process{ID: "process-3"}); !errors.Is(err, full) {
		t.Fatalf("Record = %v, want the failure set", err)
	}

	if got, want := s.Recorded(), []crew.Statistic{first, second}; !reflect.DeepEqual(got, want) {
		t.Errorf("Recorded = %#v, want the two records that did not fail, in order", got)
	}
}

func TestStatisticsHoldsRecordsUntilReleased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fake.NewStatistics()
		s.Block()
		p := crew.Process{ID: "process-1"}
		done := make(chan error, 1)
		go func() { done <- s.Record(t.Context(), p) }()

		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Record returned %v while blocked", err)
		default:
		}
		if got := s.Recorded(); len(got) != 0 {
			t.Errorf("Recorded = %#v while blocked, want none", got)
		}

		s.Release()
		if err := <-done; err != nil {
			t.Fatalf("Record: %v", err)
		}
		if got := s.Recorded(); !reflect.DeepEqual(got, []crew.Statistic{p}) {
			t.Errorf("Recorded = %#v, want the released record", got)
		}
	})
}

func TestStatisticsBlockedRecordEndsWithItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fake.NewStatistics()
		s.Block()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- s.Record(ctx, crew.Process{ID: "process-1"}) }()

		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("Record = %v, want the context's error", err)
		}
		if got := s.Recorded(); len(got) != 0 {
			t.Errorf("Recorded = %#v, want none", got)
		}
	})
}

func TestStatisticsCountsItsCloses(t *testing.T) {
	s := fake.NewStatistics()
	for range 2 {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if got := s.Closes(); got != 2 {
		t.Errorf("Closes = %d, want 2", got)
	}
}

func TestStatisticsFactoryReturnsTheStoreForAnEmptySection(t *testing.T) {
	s := fake.NewStatistics()
	got, err := fake.StatisticsFactory(s)(func(any) error { return nil }, "/data/crew")
	if err != nil || got != port.Statistics(s) {
		t.Errorf("factory = %v, %v, want the store", got, err)
	}
	invalid := errors.New("unknown key")
	got, err = fake.StatisticsFactory(s)(func(any) error { return invalid }, "/data/crew")
	if !errors.Is(err, invalid) || got != nil {
		t.Errorf("factory = %v, %v, want the decode's error", got, err)
	}
}
