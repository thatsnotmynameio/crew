package engine_test

import (
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

func TestTheSnapshotCarriesTheFirstPollsTimeAndTheRunTimeLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowPreparer{slowTracker: &slowTracker{Tracker: fake.NewTracker()}, delay: 10 * time.Minute}
		cfg := config(t, tr, develop)
		cfg.RunTimeLimit = time.Hour
		t0 := time.Now()
		r := start(t, cfg)

		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got, want := final.Snapshot.Started, t0.Add(10*time.Minute); !got.Equal(want) {
			t.Errorf("started = %v, want %v: the first poll, after the checks", got, want)
		}
		if got := final.Snapshot.RunTimeLimit; got != time.Hour {
			t.Errorf("run time limit = %v, want 1h0m0s", got)
		}
	})
}

func TestWithoutARunTimeLimitTheSnapshotStillCarriesTheStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		r := start(t, config(t, fake.NewTracker(), develop))

		time.Sleep(time.Hour)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := final.Snapshot.Started; !got.Equal(t0) {
			t.Errorf("started = %v, want %v", got, t0)
		}
		if got := final.Snapshot.RunTimeLimit; got != 0 {
			t.Errorf("run time limit = %v, want none", got)
		}
	})
}

func TestTheLastSnapshotListsAFailedIssueAsHandledWithItsReason(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.sessions(1)["issue-1-development"].End(crew.Outcome{Reason: "tests fail"})
		synctest.Wait()

		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		handled := final.Snapshot.Handled
		if len(handled) != 1 {
			t.Fatalf("handled = %#v, want #1 alone", handled)
		}
		e := handled[0]
		if e.Issue.Ref != "#1" || e.Stage != "implement" || e.To != needsAttention || !e.NeedsAttention() {
			t.Errorf("entry = %#v, want #1 in needs attention, needing attention", e)
		}
		var reasons []string
		for _, f := range e.Failures {
			reasons = append(reasons, f.Action+": "+f.Reason)
		}
		if want := []string{"development: tests fail"}; !reflect.DeepEqual(reasons, want) {
			t.Errorf("failures = %v, want %v", reasons, want)
		}
	})
}
