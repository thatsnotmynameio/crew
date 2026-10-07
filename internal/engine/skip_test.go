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

// skipped counts the skipped polls among the updates r published. Call it
// once Run has returned.
func (r *rig) skipped() int {
	r.t.Helper()
	n := 0
	for u := range r.queue.Updates() {
		for _, e := range u.Events {
			if _, ok := e.(core.PollSkipped); ok {
				n++
			}
		}
	}
	return n
}

// Covers AE1, AE2.
func TestBusyTicksDoNotListAndAFreedSlotListsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowTracker{Tracker: fake.NewTracker(issue(1, ready), issue(2, ready), issue(3, ready))}
		t0 := time.Now()
		r := start(t, config(t, tr, develop))
		sessions := r.sessions(2)

		// The ticks at 300s and 600s find both slots busy.
		time.Sleep(650 * time.Second)
		sessions["issue-1-implement"].End(port.SessionEnd{Succeeded: true, Reason: "done"})
		r.sessions(1) // #3, taken by the listing the freed slot started
		synctest.Wait()

		if got, want := offsets(t0, tr.spans()), []time.Duration{0, 650 * time.Second}; !reflect.DeepEqual(got, want) {
			t.Errorf("listings started at %v, want %v", got, want)
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := r.skipped(); got != 2 {
			t.Errorf("skipped polls: %d, want 2", got)
		}
	})
}

// Covers AE3.
func TestAFreedSlotWithNoSkippedTickWaitsForTheNextTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowTracker{Tracker: fake.NewTracker(issue(1, ready))}
		t0 := time.Now()
		r := start(t, config(t, tr, develop))
		first := r.sessions(1)["issue-1-implement"]

		// The tick at 300s has a free slot: it lists and takes #2.
		tr.Add(issue(2, ready))
		r.sessions(1)
		time.Sleep(350*time.Second - time.Since(t0))
		first.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		time.Sleep(650*time.Second - time.Since(t0))

		want := []time.Duration{0, 300 * time.Second, 600 * time.Second}
		if got := offsets(t0, tr.spans()); !reflect.DeepEqual(got, want) {
			t.Errorf("listings started at %v, want %v", got, want)
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := r.skipped(); got != 0 {
			t.Errorf("skipped polls: %d, want 0", got)
		}
	})
}
