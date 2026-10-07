package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The labels of these tests that are no rule's, which crew never moves to:
// a parked idea's and a bug's.
const (
	brainstormReady crew.State = "brainstorm ready"
	bug             crew.State = "bug"
)

// boardLabels is the board of these tests: ideas (brainstorm ready), bugs
// (bug), implement (ready, in progress) and review (ready to review).
var boardLabels = []crew.State{brainstormReady, bug, ready, inProgress, readyToReview}

// newBoardDriver returns a driver whose model reads a written board of one
// column of issues per label.
func newBoardDriver(t *testing.T, rules []crew.Rule, maxParallel int, labels ...crew.State) *driver {
	t.Helper()
	columns := make([]crew.BoardColumn, len(labels))
	for i, l := range labels {
		columns[i] = crew.BoardColumn{Name: string(l), Labels: []crew.State{l}}
	}
	return &driver{t: t, m: core.New(rules, maxParallel, core.ListingBoard(columns)), now: t0}
}

// onBoard returns key on the board with labels.
func onBoard(key string, minute int, labels ...crew.State) crew.BoardIssue {
	return crew.BoardIssue{Issue: issue(key, minute), Labels: labels}
}

// listBoard returns the ListBoard commands in cmds.
func listBoard(cmds []core.Command) []core.ListBoard {
	var out []core.ListBoard
	for _, c := range cmds {
		if l, ok := c.(core.ListBoard); ok {
			out = append(out, l)
		}
	}
	return out
}

func wantBoard(t *testing.T, d *driver, want ...crew.BoardIssue) {
	t.Helper()
	if got := d.m.View().Board; !reflect.DeepEqual(got, want) {
		t.Fatalf("board:\n got %#v\nwant %#v", got, want)
	}
}

func TestTheFirstTickListsTheIssuesAndTheBoard(t *testing.T) {
	d := newBoardDriver(t, draft(), 2, boardLabels...)

	cmds, _ := d.send(core.Tick{})

	wantCommands(t, cmds,
		core.ListBoard{Labels: boardLabels},
		core.ListIssues{States: draftListing},
	)
}

func TestWithoutABoardNoTickListsIt(t *testing.T) {
	d := newDriver(t, draft(), 1)
	d.running(issue("1", 1, ready))

	cmds, _ := d.send(core.Tick{})

	if got := listBoard(cmds); got != nil {
		t.Fatalf("tick without a board listed it: %#v", got)
	}
	if got := d.m.View().Board; got != nil {
		t.Fatalf("board without the option: %#v", got)
	}
}

// Covers AE4.
func TestWithEverySlotBusyATickStillReadsTheBoard(t *testing.T) {
	d := newBoardDriver(t, draft(), 1, boardLabels...)
	d.running(issue("1", 1, ready))
	d.send(core.BoardListed{})

	cmds, events := d.send(core.Tick{})
	wantCommands(t, cmds, core.ListBoard{Labels: boardLabels})
	hasEvent(t, events, core.PollSkipped{At: d.now, Busy: 1, Slots: 1})

	d.send(core.BoardListed{Issues: []crew.BoardIssue{onBoard("30", 30, bug)}})
	wantBoard(t, d, onBoard("30", 30, bug))
}

func TestATickWhileABoardReadIsOutstandingDoesNotReadItAgain(t *testing.T) {
	d := newBoardDriver(t, draft(), 2, boardLabels...)
	d.send(core.Tick{})

	cmds, _ := d.send(core.Tick{})

	wantCommands(t, cmds)
}

func TestATickReadsTheBoardAfterTheRunTimeIsUpButNotWhileStopping(t *testing.T) {
	tests := []struct {
		name string
		end  core.Input
		want []core.Command
	}{
		{name: "run time up", end: core.TimeUp{Limit: limit}, want: []core.Command{core.ListBoard{Labels: boardLabels}}},
		{name: "stopping", end: core.StopRequested{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newBoardDriver(t, draft(), 2, boardLabels...)
			d.running(issue("1", 1, ready))
			d.send(core.BoardListed{})
			d.send(tt.end)

			cmds, _ := d.send(core.Tick{})

			wantCommands(t, cmds, tt.want...)
		})
	}
}

// Covers AE3.
func TestAVerdictMovePutsTheIssueOnTheBoardAtOnceAndAStaleReadKeepsIt(t *testing.T) {
	d := newBoardDriver(t, draft(), 2, bug, readyToReview)
	held := issue("12", 12, ready)
	d.running(held)
	d.send(core.BoardListed{})
	wantBoard(t, d)
	d.send(core.SessionEnded{IssueKey: "12", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "12", Action: "development", Outcome: succeeded})
	if got := listBoard(d.tick()); got == nil {
		t.Fatal("tick did not read the board")
	}

	d.settle(verdict)
	moved := crew.BoardIssue{Issue: held, Labels: []crew.State{readyToReview}}
	wantBoard(t, d, moved)

	d.send(core.BoardListed{Issues: []crew.BoardIssue{onBoard("12", 12, inProgress)}})
	wantBoard(t, d, crew.BoardIssue{Issue: issue("12", 12), Labels: []crew.State{readyToReview}})

	d.tick()
	d.send(core.BoardListed{})
	wantBoard(t, d)
}

// tick ticks and returns its commands.
func (d *driver) tick() []core.Command {
	cmds, _ := d.send(core.Tick{})
	return cmds
}

func TestATakeMoveChangesTheIssuesLabelsOnTheBoard(t *testing.T) {
	tests := []struct {
		name   string
		state  crew.State // the issue's rule label, which crew takes it from
		before []crew.State
		want   []crew.BoardIssue
	}{
		{
			name: "a board label replaced by the next", state: ready, before: []crew.State{ready},
			want: []crew.BoardIssue{onBoard("1", 1, inProgress)},
		},
		{
			name: "a label that is not crew's kept", state: readyToReview, before: []crew.State{bug, readyToReview},
			want: []crew.BoardIssue{onBoard("1", 1, bug)},
		},
		{
			// Covers AE6: only the rules' labels are crew's.
			name: "a label no rule names kept", state: ready, before: []crew.State{brainstormReady, ready},
			want: []crew.BoardIssue{onBoard("1", 1, brainstormReady, inProgress)},
		},
		{
			name: "an issue left with no board label off the board", state: readyToReview,
			before: []crew.State{readyToReview},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newBoardDriver(t, draft(), 2, boardLabels...)
			d.send(core.Tick{})
			d.send(core.BoardListed{Issues: []crew.BoardIssue{onBoard("1", 1, tt.before...)}})

			take, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, tt.state)}})
			wantBoard(t, d, onBoard("1", 1, tt.before...))
			d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})

			wantBoard(t, d, tt.want...)
		})
	}
}

func TestAPullRequestsMoveNeverPutsItOnTheBoard(t *testing.T) {
	d := newBoardDriver(t, withFixReview(), 2, fixing, readyToReview)
	d.send(core.Tick{})
	d.send(core.BoardListed{})

	take, _ := d.send(core.IssuesListed{Issues: []crew.Issue{pr90(1, fixReviewReady)}})
	d.settle(take)
	wantBoard(t, d)

	verdict, _ := d.send(core.SessionEnded{IssueKey: "90", Action: "fix", Outcome: succeeded})
	d.settle(verdict)
	wantBoard(t, d)
}

func TestAFailedBoardReadKeepsTheBoardUntilTheNextRead(t *testing.T) {
	d := newBoardDriver(t, draft(), 2, boardLabels...)
	d.send(core.Tick{})
	d.send(core.BoardListed{Issues: []crew.BoardIssue{onBoard("30", 30, bug)}})
	d.tick()

	d.send(core.BoardListFailed{Reason: "gh: timeout"})
	if v := d.m.View(); v.BoardFailure != "gh: timeout" {
		t.Fatalf("board failure: got %q, want %q", v.BoardFailure, "gh: timeout")
	}
	wantBoard(t, d, onBoard("30", 30, bug))

	if got := listBoard(d.tick()); got == nil {
		t.Fatal("tick after a failed read did not read the board")
	}
	d.send(core.BoardListed{Issues: []crew.BoardIssue{onBoard("31", 31, bug)}})
	if v := d.m.View(); v.BoardFailure != "" {
		t.Fatalf("board failure after a read: %q", v.BoardFailure)
	}
	wantBoard(t, d, onBoard("31", 31, bug))
}

func TestTheBoardIsOldestFirstAndSharesNoMemory(t *testing.T) {
	d := newBoardDriver(t, draft(), 2, boardLabels...)
	d.send(core.Tick{})
	d.send(core.BoardListed{Issues: []crew.BoardIssue{
		onBoard("9", 9, bug), onBoard("8", 1, bug), onBoard("7", 1, ready),
	}})

	v := d.m.View()
	v.Board[0].Labels[0] = "changed"

	wantBoard(t, d, onBoard("7", 1, ready), onBoard("8", 1, bug), onBoard("9", 9, bug))
}
