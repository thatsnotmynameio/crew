package engine_test

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// namedBoardTracker is a board tracker that names its repository and
// records the issue of each move it is asked for.
type namedBoardTracker struct {
	fake.BoardTracker

	repository crew.Repository
	mu         sync.Mutex
	moved      []crew.IssueID
}

func (n *namedBoardTracker) Repository() crew.Repository { return n.repository }

func (n *namedBoardTracker) Move(ctx context.Context, id crew.IssueID, from, to crew.State) error {
	n.mu.Lock()
	n.moved = append(n.moved, id)
	n.mu.Unlock()
	return n.BoardTracker.Move(ctx, id, from, to)
}

// The tracker sets only each issue's key; the engine puts the repository
// the tracker names on every issue a listing or a board read returns,
// before the core sees it (KTD6).
func TestTheEngineQualifiesListedAndBoardIssuesWithTheTrackersRepository(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		widgets := crew.Repository{ID: "R_kgDOWidgets", Name: "acme/widgets"}
		tr := &namedBoardTracker{BoardTracker: fake.NewBoardTracker(issue(1, ready), issue(2)), repository: widgets}
		tr.SetLabels("2", "bug")
		cfg := config(t, tr, develop)
		cfg.Board, cfg.BoardWritten = bugs, true
		r := start(t, cfg)

		r.sessions(1)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		one, two := crew.IssueID{Repository: widgets.ID, Key: "1"}, crew.IssueID{Repository: widgets.ID, Key: "2"}
		tr.mu.Lock()
		moved := tr.moved
		tr.mu.Unlock()
		if len(moved) == 0 || slices.ContainsFunc(moved, func(id crew.IssueID) bool { return id != one }) {
			t.Errorf("moves asked for %#v, want only %#v", moved, one)
		}
		board := make([]crew.IssueID, 0, len(final.Snapshot.Board))
		for _, b := range final.Snapshot.Board {
			board = append(board, b.Issue().ID())
		}
		if want := []crew.IssueID{two}; !reflect.DeepEqual(board, want) {
			t.Errorf("board issues = %#v, want %#v", board, want)
		}
	})
}
