package engine_test

import (
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// promote is a rule without actions: it moves ready to ready to review.
var promote = crew.Rule{
	Name:   "promote",
	Labels: crew.Labels{Ready: ready, Running: inProgress, Success: readyToReview},
}

// gatedTake returns a tracker holding #1 in ready, whose take move waits
// for its release.
func gatedTake() *gatedTracker {
	return &gatedTracker{
		Tracker: fake.NewTracker(issue(1, ready)),
		gate:    inProgress, entered: make(chan struct{}, 1), release: make(chan struct{}),
	}
}

// promoted waits for Run to return and checks that #1 moved to ready to
// review with no session started.
func promoted(t *testing.T, r *rig, tr *gatedTracker) {
	t.Helper()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
		t.Errorf("#1 is in %v, want ready to review", got)
	}
	if got := len(r.harness.Sessions()); got != 0 {
		t.Errorf("%d sessions started, want none", got)
	}
}

func TestARuleWithoutActionsTakenWhileCrewStopsMovesToSuccessAndTheEngineStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := gatedTake()
		r := start(t, config(t, tr, promote))
		<-tr.entered
		r.engine.Stop()
		synctest.Wait()
		close(tr.release)

		promoted(t, r, tr)
	})
}

func TestTheRunTimeLimitEndingDuringATakeWithoutActionsStopsTheEngineAfterTheMove(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := gatedTake()
		cfg := config(t, tr, promote)
		cfg.RunTimeLimit = time.Hour
		r := start(t, cfg)
		<-tr.entered
		time.Sleep(time.Hour + time.Second)
		close(tr.release)

		promoted(t, r, tr)
	})
}
