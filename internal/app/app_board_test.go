package app_test

import (
	"context"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// withBoard is oneAction with a board of one column.
const withBoard = oneAction + `board:
  bugs: bug
`

// boardReads is a board tracker that counts its board reads.
type boardReads struct {
	fake.BoardTracker

	reads atomic.Int32
}

func (b *boardReads) ListBoard(ctx context.Context, labels []string) ([]crew.BoardIssue, error) {
	b.reads.Add(1)
	return b.BoardTracker.ListBoard(ctx, labels)
}

// A board needs a tracker that lists issues by any label: crew refuses to
// start without one, before any listing, and names the tracker (KTD3).
func TestABoardWithATrackerThatCannotListItExitsTwoNamingTheTracker(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	r := options(t, withBoard, tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	want := `board: tracker "fake" cannot list issues by any label`
	if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
}

// With a board and a tracker that lists it, crew starts and reads the board.
func TestABoardWithATrackerThatListsItStartsAndReadsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &boardReads{BoardTracker: fake.NewBoardTracker(issue("1", ready))}
		h := fake.NewHarness()
		r := options(t, withBoard, tr, h)
		r.start()

		next(t, h).End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if n := tr.reads.Load(); n == 0 {
			t.Error("the board was never read")
		}
	})
}
