package engine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// listCounter is a preparing fake tracker that counts its listings.
type listCounter struct {
	fake.PreparingTracker

	mu    sync.Mutex
	lists int
}

func (l *listCounter) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	l.mu.Lock()
	l.lists++
	l.mu.Unlock()
	return l.PreparingTracker.List(ctx, states)
}

func TestAFailingPreparerStopsTheEngineBeforeAnyListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker(issue(1, ready))}
		notLoggedIn := errors.New("gh is not logged in")
		tr.Fail(notLoggedIn)
		harness := fake.NewPreparingHarness()
		cfg := config(t, tr, develop)
		cfg.Harness = harness

		err := engine.New(cfg).Run(context.Background())

		if !errors.Is(err, notLoggedIn) {
			t.Fatalf("Run = %v, want the preparer's error", err)
		}
		if !strings.Contains(err.Error(), "tracker") {
			t.Errorf("Run = %q, want it to name the tracker", err)
		}
		if tr.lists != 0 {
			t.Errorf("tracker listed %d times, want none", tr.lists)
		}
		want := [][]crew.State{{ready, inProgress, readyToReview, needsAttention}}
		if got := tr.Calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("tracker prepared for %v, want the workflow's states %v", got, want)
		}
		if got := harness.Calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("harness prepared for %v, want %v", got, want)
		}
	})
}

func TestPrepareGetsOnlyTheStatesTheWorkflowNames(t *testing.T) {
	blocked := develop
	blocked.OnFailure = "blocked"
	tr := fake.NewPreparingTracker()

	if err := engine.New(config(t, tr, blocked)).Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	want := [][]crew.State{{ready, inProgress, readyToReview, "blocked"}}
	if got := tr.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("tracker prepared for %v, want %v and no needs attention", got, want)
	}
}

func TestRunAfterPrepareDoesNotPrepareAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker()}
		e := engine.New(config(t, tr, develop))

		if err := e.Prepare(context.Background()); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if got := len(tr.Calls()); got != 1 {
			t.Fatalf("Prepare ran the tracker's Preparer %d times, want 1", got)
		}
		if tr.lists != 0 {
			t.Fatalf("Prepare listed %d times, want none", tr.lists)
		}

		e.Stop()
		if err := e.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := len(tr.Calls()); got != 1 {
			t.Errorf("the tracker's Preparer ran %d times in all, want once", got)
		}
		if tr.lists != 1 {
			t.Errorf("Run listed %d times, want the first poll's listing", tr.lists)
		}
	})
}
