package engine_test

import (
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
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

func TestTheLastSnapshotListsAFailedIssueAsHandledWithItsFailedAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.sessions(1)["issue-1-development"].End(port.Verdict{Reason: "tests fail"})
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
		if e.Issue.Ref != "#1" || e.Rule != "implement" || e.To != needsAttention || !e.NeedsAttention() {
			t.Errorf("entry = %#v, want #1 in needs attention, needing attention", e)
		}
		actions := make([]string, 0, len(e.Failures))
		for _, f := range e.Failures {
			actions = append(actions, f.Action)
		}
		if want := []string{"development"}; !reflect.DeepEqual(actions, want) {
			t.Errorf("failed actions = %v, want %v", actions, want)
		}
		if got := r.lastReason(); got != "tests fail" {
			t.Errorf("development's reason = %q, want %q", got, "tests fail")
		}
	})
}

func TestASnapshotKeepsTheLast100Events(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := start(t, config(t, fake.NewTracker(), develop))
		time.Sleep(150 * poll)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		var all []core.Event
		for u := range r.queue.Updates() {
			all = append(all, u.Events...)
		}
		if len(all) <= 100 {
			t.Fatalf("the run had %d events, want more than 100", len(all))
		}
		if got, want := final.Snapshot.Recent, all[len(all)-100:]; !reflect.DeepEqual(got, want) {
			t.Errorf("recent holds %d events, want the last 100 of %d", len(got), len(all))
		}
	})
}
