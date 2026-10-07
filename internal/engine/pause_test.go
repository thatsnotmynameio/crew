package engine_test

import (
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Covers R1 and R4 of #282 (KTD5): a toggle made before Run pauses the
// first listing's take, and the toggle that resumes takes at once, without
// waiting for the next poll.
func TestAPauseBeforeRunTakesNothingAndTheResumeTakesAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		e := engine.New(cfg)
		e.TogglePause()
		t0 := time.Now()
		r := run(t, cfg, e)

		time.Sleep(10 * time.Second)
		synctest.Wait()
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{ready}) {
			t.Fatalf("paused: #1 is in %v, want it still ready", got)
		}

		e.TogglePause()
		session := r.sessions(1)["issue-1-implement"]
		if got := time.Since(t0); got != 10*time.Second {
			t.Errorf("the resumed crew took #1 after %v, want 10s: at once, not at the next poll", got)
		}

		session.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		e.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		var toggles []core.Published
		for _, ev := range r.events() {
			switch ev.(type) {
			case core.Paused, core.Resumed:
				toggles = append(toggles, ev)
			}
		}
		want := []core.Published{core.Paused{At: t0}, core.Resumed{At: t0.Add(10 * time.Second)}}
		if !reflect.DeepEqual(toggles, want) {
			t.Errorf("pause events:\n got %#v\nwant %#v", toggles, want)
		}
	})
}

// Covers R5 and AE3 of #282: a paused crew with nothing held keeps running
// until it is stopped, and its snapshot says it is paused.
func TestAPausedEngineWithNothingHeldRunsUntilStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker()
		cfg := config(t, tr, develop)
		e := engine.New(cfg)
		latest := e.SubscribeLatest()
		r := run(t, cfg, e)
		e.TogglePause()

		time.Sleep(time.Hour)
		synctest.Wait()
		select {
		case err := <-r.done:
			t.Fatalf("Run returned %v while paused, without a stop", err)
		default:
		}
		var last engine.Update
		for drained := false; !drained; {
			select {
			case last = <-latest:
			default:
				drained = true
			}
		}
		if !last.Snapshot.Paused {
			t.Error("the snapshot does not say crew is paused")
		}

		e.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for range latest {
		}
	})
}

// Covers KTD5 of #282: a toggle after Run returned does not block.
func TestATogglePauseAfterRunReturnedReturnsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config(t, fake.NewTracker(), develop)
		r := start(t, cfg)
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		for range 100 {
			r.engine.TogglePause()
		}
	})
}
