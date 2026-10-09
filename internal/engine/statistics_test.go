package engine_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
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

// widgets is the repository a repositoryTracker names in these tests.
var widgets = crew.Repository{ID: "R_kgDOWidgets", Name: "acme/widgets"}

// recordingConfig returns a config over tr for rules that records its
// statistics in a fresh store, as crew 1.2.3 on GitHub.
func recordingConfig(t *testing.T, tr port.Tracker, rules ...crew.Rule) (engine.Config, *fake.Statistics) {
	t.Helper()
	cfg := config(t, tr, rules...)
	store := fake.NewStatistics()
	cfg.Statistics, cfg.Version, cfg.TrackerName = store, "1.2.3", "github"
	return cfg, store
}

// developedRun is what run, which process took at at, records as it
// develops issue id at once: the open of its span, its take's move, its
// route's move, then the end of its span through passed.
func developedRun(id crew.IssueID, run crew.RuleRunID, process crew.ProcessID, at time.Time) []crew.Statistic {
	move := func(from, to crew.State) crew.LabelMove {
		return crew.LabelMove{Tracker: "github", Issue: id, From: from, To: to, Seen: at, Run: crew.Some(run)}
	}
	open := crew.RuleRunSpan{Tracker: "github", Issue: id, Run: run, Process: process, Rule: develop.Name, Start: at}
	end := open
	end.End = crew.Some(crew.RuleRunEnd{At: at, Outcome: crew.OutcomeRouted, Route: crew.Some(crew.PassedRoute)})
	return []crew.Statistic{open, move(ready, inProgress), move(inProgress, readyToReview), end}
}

// Covers R5, R6, R7, R8, R9, F1, AE3: a crew that develops issue 1 records
// the process, the repository on the configured tracker, the sighting of
// issue 1 in ready, the open of the run's span, then the take's move and
// the route's move, both made by the run, then the end of the run's span
// with its outcome and route, in that order.
func TestRunRecordsTheProcessTheRepositoryTheIssueItsMovesAndItsRunsSpanInOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg, store := recordingConfig(t, repositoryTracker{Tracker: tr, repository: widgets}, develop)
		r := start(t, cfg)

		developOnce(t, r, tr)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		recorded := store.Recorded()
		if len(recorded) != 7 {
			t.Fatalf("recorded %#v, want seven records", recorded)
		}
		p, ok := recorded[0].(crew.Process)
		if !ok || p.ID == "" {
			t.Fatalf("recorded %#v first, want a process with an id", recorded[0])
		}
		take, ok := recorded[4].(crew.LabelMove)
		run, byRun := take.Run.Get()
		if !ok || !byRun || run == "" {
			t.Fatalf("recorded %#v fifth, want a move made by a run", recorded[4])
		}
		started := final.Snapshot.Started
		id := crew.IssueID{Repository: widgets.ID, Key: "1"}
		want := append([]crew.Statistic{
			crew.Process{ID: p.ID, Version: "1.2.3", Folder: cfg.Root, Start: started},
			crew.RepositoryRecord{Tracker: "github", Repository: widgets},
			crew.IssueSighting{
				Tracker: "github", Issue: id, Ref: "#1", Created: crew.Some(issue(1).Created()), Seen: started,
				State: crew.Some(ready),
			},
		}, developedRun(id, run, p.ID, started)...)
		if !reflect.DeepEqual(recorded, want) {
			t.Errorf("recorded\n %#v\nwant\n %#v", recorded, want)
		}
		if n := r.notRecorded(); len(n) != 0 {
			t.Errorf("StatisticNotRecorded = %+v, want none", n)
		}
	})
}

// Covers AE2, R8: a person's move of a blocked issue 1 from ready to ready
// to review, made on the tracker between two ticks, is recorded at the
// second tick's listing, made outside crew.
func TestAMoveMadeOutsideCrewIsRecordedAtTheNextListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked := issue(1, ready).Data()
		blocked.Blocked = true
		tr := fake.NewTracker(crew.NewIssue(blocked))
		review := crew.Rule{
			Name: "review", Labels: crew.Labels{Ready: readyToReview, Running: "in review"},
			Actions: develop.Actions, Routes: routes(needsAttention),
		}
		cfg, store := recordingConfig(t, tr, develop, review)
		r := start(t, cfg)
		synctest.Wait()

		tr.SetStates("1", readyToReview)
		time.Sleep(poll)
		synctest.Wait()
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		recorded := store.Recorded()
		want := crew.LabelMove{
			Tracker: "github", Issue: crew.IssueID{Repository: "repo", Key: "1"}, From: ready, To: readyToReview,
			Seen: final.Snapshot.Started.Add(poll),
		}
		if len(recorded) != 4 || !reflect.DeepEqual(recorded[3], want) {
			t.Errorf("recorded %#v, want the process, the repository, the sighting, then %#v", recorded, want)
		}
	})
}

// wantLostSpan fails the test unless lost is the warning of a lost record
// of a run's span, its end when ended, else its open.
func wantLostSpan(t *testing.T, lost core.StatisticNotRecorded, ended bool) {
	t.Helper()
	sp, ok := lost.Statistic.(crew.RuleRunSpan)
	if _, hasEnd := sp.End.Get(); !ok || hasEnd != ended {
		t.Errorf("lost %#v, want the span of the run, ended %v", lost.Statistic, ended)
	}
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

		// The process, the repository, issue 1's sighting, the open of its
		// run's span, its take's move, its route's move and the end of its
		// run's span.
		n := r.notRecorded()
		if len(n) != 7 {
			t.Fatalf("StatisticNotRecorded = %+v, want seven", n)
		}
		if _, ok := n[0].Statistic.(crew.Process); !ok {
			t.Errorf("the first lost record is %T, want the process", n[0].Statistic)
		}
		wantLostSpan(t, n[3], false)
		wantLostSpan(t, n[6], true)
		want := "open ~/.local/share/crew/statistics.db: database or disk is full"
		for _, lost := range n {
			if lost.Reason != want {
				t.Errorf("reason of the lost %T = %q, want %q", lost.Statistic, lost.Reason, want)
			}
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
		// The process, the repository, issue 1's sighting, the span and
		// moves of its first run, the move back to ready made outside crew,
		// and the span and moves of its second run.
		if got := store.Recorded(); len(got) != 12 {
			t.Errorf("recorded %#v, want twelve records", got)
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
