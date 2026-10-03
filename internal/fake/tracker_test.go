package fake_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestTrackerListsOpenIssuesInAnyGivenStateWithAllTheirStates(t *testing.T) {
	tr := fake.NewTracker(
		issue("1", ready),
		issue("2", readyToReview),
		issue("3", "done"),
		issue("4", ready, needsAttention),
		issue("5", ready),
	)
	tr.Close("5")

	got, err := tr.List(context.Background(), []crew.State{ready, readyToReview})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"1", "2", "4"}; !reflect.DeepEqual(keys(got), want) {
		t.Fatalf("List keys = %v, want %v", keys(got), want)
	}
	if want := []crew.State{ready, needsAttention}; !reflect.DeepEqual(got[2].States, want) {
		t.Errorf("issue 4 states = %v, want %v", got[2].States, want)
	}
	if got[0].Ref != "#1" || got[0].Title != "Issue 1" {
		t.Errorf("issue 1 = %+v, want its ref and title", got[0])
	}
}

func TestTrackerMoveLeavesTheIssueInExactlyTheNewState(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	ctx := context.Background()

	if err := tr.Move(ctx, "1", ready, inProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	got, ok := tr.Issue("1")
	if !ok {
		t.Fatal("issue 1 is gone")
	}
	if want := []crew.State{inProgress}; !reflect.DeepEqual(got.States, want) {
		t.Errorf("states = %v, want %v", got.States, want)
	}
	if want := []fake.Move{{Key: "1", From: ready, To: inProgress}}; !reflect.DeepEqual(tr.Moves(), want) {
		t.Errorf("Moves = %v, want %v", tr.Moves(), want)
	}
	listed, err := tr.List(ctx, []crew.State{ready})
	if err != nil || len(listed) != 0 {
		t.Errorf("List(ready) = %v, %v; want no issues", keys(listed), err)
	}
}

// An extra label is not a state: List does not report it, an issue whose
// only crew label is an extra is not listed, and a move clears the extras.
func TestTrackerExtrasAreNotListedAndAMoveClearsThem(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready), issue("2"))
	tr.SetExtras("1", waitingBrainstorm)
	tr.SetExtras("2", waitingBrainstorm)
	ctx := context.Background()

	listed, err := tr.List(ctx, []crew.State{ready, waitingBrainstorm})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"1"}; !reflect.DeepEqual(keys(listed), want) {
		t.Fatalf("List keys = %v, want %v", keys(listed), want)
	}
	if want := []crew.State{ready}; !reflect.DeepEqual(listed[0].States, want) {
		t.Errorf("issue 1 states = %v, want %v", listed[0].States, want)
	}

	if err := tr.Move(ctx, "1", ready, inProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := tr.Extras("1"); len(got) != 0 {
		t.Errorf("issue 1 extras after the move = %v, want none", got)
	}
	if want := []crew.State{waitingBrainstorm}; !reflect.DeepEqual(tr.Extras("2"), want) {
		t.Errorf("issue 2 extras = %v, want %v", tr.Extras("2"), want)
	}
}

func TestTrackerMoveFromAStateTheIssueLeftIsMovedMeanwhile(t *testing.T) {
	tr := fake.NewTracker(issue("1", needsAttention))

	err := tr.Move(context.Background(), "1", ready, inProgress)
	if !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Fatalf("Move = %v, want ErrMovedMeanwhile", err)
	}
	got, _ := tr.Issue("1")
	if want := []crew.State{needsAttention}; !reflect.DeepEqual(got.States, want) {
		t.Errorf("states = %v, want them unchanged at %v", got.States, want)
	}
	if len(tr.Moves()) != 0 {
		t.Errorf("Moves = %v, want none", tr.Moves())
	}
}

// A retry of a move that already landed finds the issue exactly in to: it is
// done, and records no second move, as the github adapter sends no edit.
func TestTrackerMoveOfAnIssueAlreadyInToIsDone(t *testing.T) {
	tr := fake.NewTracker(issue("1", inProgress))

	if err := tr.Move(context.Background(), "1", ready, inProgress); err != nil {
		t.Fatalf("Move = %v, want nil", err)
	}
	got, _ := tr.Issue("1")
	if want := []crew.State{inProgress}; !reflect.DeepEqual(got.States, want) {
		t.Errorf("states = %v, want %v", got.States, want)
	}
	if len(tr.Moves()) != 0 {
		t.Errorf("Moves = %v, want none", tr.Moves())
	}
}

func TestTrackerMoveOfAnIssueInToAndAnotherStateIsMovedMeanwhile(t *testing.T) {
	tr := fake.NewTracker(issue("1", inProgress, "paused"))

	err := tr.Move(context.Background(), "1", ready, inProgress)
	if !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Fatalf("Move = %v, want ErrMovedMeanwhile", err)
	}
}

func TestTrackerMoveOfAClosedIssueIsMovedMeanwhile(t *testing.T) {
	tr := fake.NewTracker(issue("1", inProgress))
	tr.Close("1")

	err := tr.Move(context.Background(), "1", inProgress, readyToReview)
	if !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Fatalf("Move = %v, want ErrMovedMeanwhile", err)
	}
}

func TestTrackerSetStatesChangesAnIssueFromOutside(t *testing.T) {
	tr := fake.NewTracker(issue("1", inProgress))
	tr.SetStates("1", "paused")

	err := tr.Move(context.Background(), "1", inProgress, readyToReview)
	if !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Fatalf("Move = %v, want ErrMovedMeanwhile", err)
	}
}

func TestTrackerScriptedMoveFailuresComeInOrderThenMovesSucceed(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	transient := errors.New("network down")
	tr.FailMoves("1", transient, port.ErrRefused)
	ctx := context.Background()

	err := tr.Move(ctx, "1", ready, inProgress)
	if !errors.Is(err, transient) || errors.Is(err, port.ErrRefused) || errors.Is(err, port.ErrMovedMeanwhile) {
		t.Errorf("first Move = %v, want the transient error", err)
	}
	if err := tr.Move(ctx, "1", ready, inProgress); !errors.Is(err, port.ErrRefused) {
		t.Errorf("second Move = %v, want ErrRefused", err)
	}
	if got, _ := tr.Issue("1"); !reflect.DeepEqual(got.States, []crew.State{ready}) {
		t.Errorf("states after failed moves = %v, want [ready]", got.States)
	}
	if err := tr.Move(ctx, "1", ready, inProgress); err != nil {
		t.Errorf("third Move = %v, want success", err)
	}
}

func TestTrackerRecordsFailureReportsAndScriptsTheirFailures(t *testing.T) {
	tr := fake.NewTracker(issue("1", needsAttention))
	report := crew.FailureReport{IssueKey: "1", IssueRef: "#1", Failures: []crew.ActionFailure{
		{
			Action: "development", Reason: "tests fail",
			Workspace: "issue-1-development", Log: ".crew/logs/issue-1-development.log",
		},
	}}
	tr.FailReports("1", port.ErrRefused)
	ctx := context.Background()

	if err := tr.ReportFailure(ctx, report); !errors.Is(err, port.ErrRefused) {
		t.Fatalf("first ReportFailure = %v, want ErrRefused", err)
	}
	if len(tr.Reports()) != 0 {
		t.Fatalf("Reports = %v, want none after a failed report", tr.Reports())
	}
	if err := tr.ReportFailure(ctx, report); err != nil {
		t.Fatalf("second ReportFailure: %v", err)
	}
	if want := []crew.FailureReport{report}; !reflect.DeepEqual(tr.Reports(), want) {
		t.Errorf("Reports = %+v, want %+v", tr.Reports(), want)
	}
}

func TestTrackerIsSafeForConcurrentUse(t *testing.T) {
	tr := fake.NewTracker()
	for i := range rune(20) {
		tr.Add(issue(string('a'+i), ready))
	}
	var wg sync.WaitGroup
	for i := range rune(20) {
		wg.Go(func() {
			key := string('a' + i)
			if err := tr.Move(context.Background(), key, ready, inProgress); err != nil {
				t.Errorf("Move %s: %v", key, err)
			}
			if _, err := tr.List(context.Background(), []crew.State{ready}); err != nil {
				t.Errorf("List: %v", err)
			}
		})
	}
	wg.Wait()
	if got := len(tr.Moves()); got != 20 {
		t.Errorf("Moves = %d, want 20", got)
	}
}

func TestTrackerFactoryValidatesItsSectionAndReturnsTheTracker(t *testing.T) {
	tr := fake.NewTracker()
	factory := fake.TrackerFactory(tr)

	var got any
	built, err := factory(func(target any) error {
		got = target
		return nil
	}, []crew.State{ready}, []crew.State{waitingBrainstorm})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if built != port.Tracker(tr) {
		t.Errorf("factory built %v, want the given tracker", built)
	}
	if _, ok := got.(*fake.TrackerSettings); !ok {
		t.Errorf("factory decoded into %T, want *fake.TrackerSettings", got)
	}

	invalid := errors.New("tracker.lables (line 3): unknown key")
	if _, err := factory(func(any) error { return invalid }, nil, nil); !errors.Is(err, invalid) {
		t.Errorf("factory with an invalid section = %v, want the decode error", err)
	}
}

func TestStatusBoardRecordsStatusesAndScriptsTheirFailures(t *testing.T) {
	tr := fake.NewReportingTracker(issue("74", ready))
	var reporter port.StatusReporter = tr
	queued := crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusQueued, Slots: 2}
	running := crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusRunning,
		Actions: []crew.ActionStatus{{Name: "development", State: crew.ActionRunning}}}
	tr.FailStatuses("74", port.ErrRefused)
	ctx := context.Background()

	if err := reporter.ReportStatus(ctx, queued); !errors.Is(err, port.ErrRefused) {
		t.Fatalf("first ReportStatus = %v, want ErrRefused", err)
	}
	for _, s := range []crew.Status{queued, running} {
		if err := reporter.ReportStatus(ctx, s); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	running.Actions[0].Said = "changed after the write"

	got := tr.Statuses("74")
	if len(got) != 2 || got[0].Kind != crew.StatusQueued || got[1].Actions[0].Said != "" {
		t.Errorf("Statuses = %+v, want the queued then the running status, as written", got)
	}
	if _, ok := any(fake.NewPreparingTracker()).(port.StatusReporter); ok {
		t.Error("a PreparingTracker reports statuses; only a ReportingTracker should")
	}
}
