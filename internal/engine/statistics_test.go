package engine_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// notRecorded returns the StatisticNotRecorded events r's engine published,
// once Run has returned. It drains r's queue, as events does.
func (r *rig) notRecorded() []core.StatisticNotRecorded {
	var out []core.StatisticNotRecorded
	for _, e := range r.events() {
		if n, ok := e.(core.StatisticNotRecorded); ok {
			out = append(out, n)
		}
	}
	return out
}

// developOnce runs issue 1's development session in r to a success and
// checks that the issue moved to ready to review.
func developOnce(t *testing.T, r *rig, tr *fake.Tracker) {
	t.Helper()
	r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})
	synctest.Wait()
	if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
		t.Fatalf("issue 1 is in %v, want ready to review", got)
	}
}

func TestRunRecordsTheProcessOnceWithTheVersionTheRootAndTheStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		store := fake.NewStatistics()
		cfg.Statistics, cfg.Version = store, "1.2.3"
		r := start(t, cfg)

		developOnce(t, r, tr)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		recorded := store.Recorded()
		if len(recorded) != 1 {
			t.Fatalf("recorded %#v, want one process", recorded)
		}
		p, ok := recorded[0].(crew.Process)
		if !ok {
			t.Fatalf("recorded %T, want a process", recorded[0])
		}
		if p.ID == "" {
			t.Error("the process has no id")
		}
		want := crew.Process{ID: p.ID, Version: "1.2.3", Folder: cfg.Root, Start: final.Snapshot.Started}
		if p != want {
			t.Errorf("recorded %+v, want %+v", p, want)
		}
		if n := r.notRecorded(); len(n) != 0 {
			t.Errorf("StatisticNotRecorded = %+v, want none", n)
		}
	})
}

func TestAE8AStoreThatCannotBeWrittenIsWarnedAboutAndTheRunGoesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		db := filepath.Join(cfg.Home, ".local", "share", "crew", "statistics.db")
		store := fake.NewStatistics()
		store.FailRecords(fmt.Errorf("open %s: database or disk is full", db))
		cfg.Statistics = store
		r := start(t, cfg)

		developOnce(t, r, tr)
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		n := r.notRecorded()
		if len(n) != 1 {
			t.Fatalf("StatisticNotRecorded = %+v, want one", n)
		}
		if _, ok := n[0].Statistic.(crew.Process); !ok {
			t.Errorf("the lost record is %T, want the process", n[0].Statistic)
		}
		want := "open ~/.local/share/crew/statistics.db: database or disk is full"
		if n[0].Reason != want {
			t.Errorf("reason = %q, want %q", n[0].Reason, want)
		}
		if got := store.Recorded(); len(got) != 0 {
			t.Errorf("recorded %#v, want nothing", got)
		}
	})
}

func TestABlockedStoreDelaysNoTickAndRunWaitsForItsRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		store := fake.NewStatistics()
		store.Block()
		cfg.Statistics = store
		r := start(t, cfg)

		developOnce(t, r, tr)
		// A later tick lists the issue again and runs it again.
		tr.SetStates("1", ready)
		time.Sleep(poll)
		developOnce(t, r, tr)

		r.engine.Stop()
		synctest.Wait()
		select {
		case err := <-r.done:
			t.Fatalf("Run returned %v while its record was still being written", err)
		default:
		}
		if got := store.Recorded(); len(got) != 0 {
			t.Fatalf("recorded %#v before the release, want nothing", got)
		}

		store.Release()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := store.Recorded(); len(got) != 1 {
			t.Errorf("recorded %#v, want the process", got)
		}
	})
}

func TestAE12WithoutAStoreTheRunGoesOnAndNothingIsRecorded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		developOnce(t, r, tr)
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if n := r.notRecorded(); len(n) != 0 {
			t.Errorf("StatisticNotRecorded = %+v, want none", n)
		}
	})
}

func TestTheWriterWritesItsRecordsInTheOrderTheyCame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := fake.NewStatistics()
		a := crew.Process{ID: "a", Version: "1"}
		b := crew.Process{ID: "b", Version: "1"}

		errs := engine.WriteStatistics(context.Background(), store, a, b)

		if got := store.Recorded(); !slices.Equal(got, []crew.Statistic{a, b}) {
			t.Errorf("recorded %#v, want a then b", got)
		}
		if !slices.Equal(errs, []error{nil, nil}) {
			t.Errorf("errors = %v, want two nils", errs)
		}
	})
}
