package engine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// bugs is a board of one column, for the engine's board tests.
var bugs = []crew.BoardColumn{{Name: "bugs", Labels: []crew.State{"bug"}}}

// boardCounter is a board tracker that counts its board reads and fails
// them with err when it is set.
type boardCounter struct {
	fake.BoardTracker

	reads atomic.Int32
	err   error
}

func (b *boardCounter) ListBoard(ctx context.Context, labels []crew.State) ([]crew.BoardIssue, error) {
	b.reads.Add(1)
	if b.err != nil {
		return nil, b.err
	}
	return b.BoardTracker.ListBoard(ctx, labels)
}

// boardKeys returns the keys and labels of board, in order, as "key:label,…".
func boardKeys(board []crew.BoardIssue) []string {
	out := make([]string, 0, len(board))
	for _, b := range board {
		labels := make([]string, len(b.Labels))
		for i, l := range b.Labels {
			labels[i] = string(l)
		}
		out = append(out, b.Issue.Key+":"+strings.Join(labels, ","))
	}
	return out
}

// Covers AE4 through the engine: the first poll reads the board, and each
// poll interval reads it again, so a label set on the tracker meanwhile
// shows after the next poll.
func TestTheEngineReadsTheBoardAtEachPoll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewBoardTracker(issue(30), issue(31))
		tr.SetLabels("30", "bug")
		cfg := config(t, tr, develop)
		cfg.Board, cfg.BoardWritten = bugs, true
		r := start(t, cfg)

		time.Sleep(time.Second) // the poll at 0s
		tr.SetLabels("31", "Bug")
		time.Sleep(poll) // the poll at 300s
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got, want := boardKeys(final.Snapshot.Board), []string{"30:bug", "31:bug"}; !reflect.DeepEqual(got, want) {
			t.Errorf("board = %v, want %v", got, want)
		}
		if f := final.Snapshot.BoardFailure; f != "" {
			t.Errorf("board failure = %q, want none", f)
		}
	})
}

// A board read that fails shows in the snapshot, and crew keeps running.
func TestAFailingBoardReadShowsInTheSnapshotAndCrewKeepsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &boardCounter{BoardTracker: fake.NewBoardTracker(issue(1, ready)), err: errors.New("gh: rate limited")}
		cfg := config(t, tr, develop)
		cfg.Board, cfg.BoardWritten = bugs, true
		r := start(t, cfg)
		r.sessions(1)["issue-1-development"].End(crew.Outcome{Succeeded: true})

		time.Sleep(poll + time.Second) // the polls at 0s and 300s
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if f := final.Snapshot.BoardFailure; f != "gh: rate limited" {
			t.Errorf("board failure = %q, want the read's error", f)
		}
		if n := tr.reads.Load(); n < 2 {
			t.Errorf("board reads = %d, want one per poll", n)
		}
		if got := states(t, tr.Tracker, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#1 states = %v, want it moved on", got)
		}
	})
}

// Without a board, the engine never reads one, even from a tracker that
// can.
func TestWithoutABoardTheEngineNeverReadsOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &boardCounter{BoardTracker: fake.NewBoardTracker()}
		r := start(t, config(t, tr, develop))

		time.Sleep(poll + time.Second)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if n := tr.reads.Load(); n != 0 {
			t.Errorf("board reads = %d, want none", n)
		}
		if b := final.Snapshot.Board; b != nil {
			t.Errorf("board = %v, want none", b)
		}
	})
}

// Covers KTD10 through the engine: the default board needs no board read
// and no port.BoardLister. Its cards come from the listings, an item left
// in the running label crew does not hold included, and crew's moves.
func TestTheDefaultBoardComesFromTheListingsOfATrackerThatCannotListABoard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready), issue(2, inProgress))
		cfg := config(t, tr, develop)
		cfg.Board = []crew.BoardColumn{{Name: "implement", Labels: []crew.State{ready, inProgress}}}
		r := start(t, cfg)

		r.sessions(1)["issue-1-development"].End(crew.Outcome{Succeeded: true})
		time.Sleep(poll + time.Second) // the polls at 0s and 300s
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		taken := []string{"1:" + string(inProgress), "2:" + string(inProgress)}
		showed := false
		for u := range r.queue.Updates() {
			showed = showed || reflect.DeepEqual(boardKeys(u.Snapshot.Board), taken)
		}
		if !showed {
			t.Errorf("no update showed the board %v", taken)
		}
		if got, want := boardKeys(final.Snapshot.Board), []string{"2:" + string(inProgress)}; !reflect.DeepEqual(got, want) {
			t.Errorf("final board = %v, want %v: #1 moved to a label no column names", got, want)
		}
		if got := states(t, tr, "2"); !reflect.DeepEqual(got, []crew.State{inProgress}) {
			t.Errorf("#2 states = %v, want it never taken", got)
		}
	})
}
