package engine_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// gatedTracker holds every move to gate until release is closed, and fails
// such a move when its context ended meanwhile.
type gatedTracker struct {
	*fake.Tracker

	gate    crew.State
	entered chan struct{}
	release chan struct{}
}

func (g *gatedTracker) Move(ctx context.Context, id crew.IssueID, from, to crew.State) error {
	if to == g.gate {
		g.entered <- struct{}{}
		<-g.release
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("move issue %s: %w", id.Key, err)
		}
	}
	return g.Tracker.Move(ctx, id, from, to)
}

// Covers AE9 through the loop.
func TestStopLetsAVerdictMoveInFlightFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &gatedTracker{
			Tracker: fake.NewTracker(issue(1, ready), issue(2, ready)),
			gate:    readyToReview, entered: make(chan struct{}, 1), release: make(chan struct{}),
		}
		r := start(t, config(t, tr, develop))
		sessions := r.sessions(2)

		sessions["issue-1-development"].End(crew.Outcome{Succeeded: true, Reason: "done"})
		<-tr.entered
		r.cancel() // Run's context ending is a stop request, not an abort.
		synctest.Wait()

		select {
		case err := <-r.done:
			t.Fatalf("Run returned %v while a verdict move was in flight", err)
		default:
		}
		if !sessions["issue-2-development"].Stopped() {
			t.Error("issue 2's running session was not stopped")
		}
		close(tr.release)
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
		if got := states(t, tr, "2"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("issue 2 is in %v, want needs attention", got)
		}
	})
}

func TestStopKillsASessionIgnoringItAtTheTenSecondDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.harness.IgnoreStop(true)
		s := r.sessions(1)["issue-1-development"]

		t0 := time.Now()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if got := time.Since(t0); got != 10*time.Second {
			t.Errorf("Run returned %v after the stop request, want 10s", got)
		}
		if !s.Stopped() {
			t.Error("the session was not stopped")
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{needsAttention}) {
			t.Errorf("issue 1 is in %v, want needs attention", got)
		}
		reports := tr.Reports()
		if len(reports) != 1 || len(reports[0].Failures) != 1 || reports[0].Failures[0].Reason != fake.KilledReason {
			t.Errorf("reports = %+v, want one naming the killed session", reports)
		}
	})
}

// moveCounter is a fake tracker that counts the moves asked for to one state,
// failed ones included.
type moveCounter struct {
	*fake.Tracker

	to crew.State

	mu    sync.Mutex
	moves int
}

func (m *moveCounter) Move(ctx context.Context, id crew.IssueID, from, to crew.State) error {
	if to == m.to {
		m.mu.Lock()
		m.moves++
		m.mu.Unlock()
	}
	return m.Tracker.Move(ctx, id, from, to)
}

func TestStopGivesAFailingVerdictMoveOneFinalTryAndReturns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &moveCounter{Tracker: fake.NewTracker(issue(1, ready)), to: needsAttention}
		r := start(t, config(t, tr, develop))
		r.sessions(1)
		down := errors.New("tracker is down")
		tr.FailMoves("1", down, down, down)

		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		if tr.moves != 2 {
			t.Errorf("tried the needs attention move %d times, want 2: the first and one final try", tr.moves)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{inProgress}) {
			t.Errorf("issue 1 is in %v, want it still in progress", got)
		}
		dropped := slices.ContainsFunc(final.Snapshot.Recent, func(e core.Event) bool {
			d, ok := e.(core.CallDropped)
			return ok && d.Call.Kind == core.CallMove && d.Call.To == needsAttention &&
				d.Result == core.ResultFailed && strings.Contains(d.Reason, "tracker is down")
		})
		if !dropped {
			t.Errorf("last update's recent events = %#v, want the dropped needs attention move", final.Snapshot.Recent)
		}
	})
}
