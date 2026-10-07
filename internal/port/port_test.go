package port_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestPrepareRunsPreparersAndSkipsTheOthers(t *testing.T) {
	tracker := fake.NewPreparingTracker()
	harness := fake.NewHarness() // not a Preparer
	states := []crew.State{"ready", "in progress", "ready to review"}

	if err := port.Prepare(context.Background(), states, tracker, harness, fake.NewWorkspace(t.TempDir())); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if want := [][]crew.State{states}; !reflect.DeepEqual(tracker.Calls(), want) {
		t.Errorf("tracker prepared with %v, want %v", tracker.Calls(), want)
	}
}

func TestPrepareReturnsEveryFailure(t *testing.T) {
	tracker, harness := fake.NewPreparingTracker(), fake.NewPreparingHarness()
	noGh, noClaude := errors.New("gh is not installed"), errors.New("claude is not installed")
	tracker.Fail(noGh)
	harness.Fail(noClaude)

	err := port.Prepare(context.Background(), []crew.State{"ready"}, tracker, harness)
	if !errors.Is(err, noGh) || !errors.Is(err, noClaude) {
		t.Errorf("Prepare = %v, want both failures", err)
	}
	if len(harness.Calls()) != 1 {
		t.Errorf("harness prepared %d times, want once despite the tracker's failure", len(harness.Calls()))
	}
}

func TestRefusedParameterNamesTheParameterAndTheReason(t *testing.T) {
	err := error(port.RefusedParameterError{Parameter: "count", Reason: "must be at least 1"})

	if got, want := err.Error(), "count: must be at least 1"; got != want {
		t.Errorf("Error = %q, want %q", got, want)
	}
}
