package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// listings counts the listings cmds ask for.
func listings(cmds []core.Command) int {
	n := 0
	for _, c := range cmds {
		if _, ok := c.(core.ListIssues); ok {
			n++
		}
	}
	return n
}

// skips returns the skipped polls in events.
func skips(events []core.Published) []core.Published {
	var out []core.Published
	for _, e := range events {
		if _, ok := e.(core.PollSkipped); ok {
			out = append(out, e)
		}
	}
	return out
}

// wantListings fails unless cmds ask for n listings.
func wantListings(t *testing.T, cmds []core.Command, n int) {
	t.Helper()
	if got := listings(cmds); got != n {
		t.Fatalf("listings: got %d, want %d in %#v", got, n, cmds)
	}
}

// endActions ends both actions of the running issue key successfully and
// returns the commands of its verdict.
func (d *driver) endActions(key string) []core.Command {
	d.t.Helper()
	d.send(core.SessionEnded{IssueID: issueID(key), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID(key), Action: "development", Outcome: succeeded})
	return verdict
}

// release ends both actions of the running issue key successfully and lands
// its verdict move, which releases it. It returns what the landing produced.
func (d *driver) release(key string) ([]core.Command, []core.Published) {
	d.t.Helper()
	verdict := d.endActions(key)
	return d.send(core.CallResult{ID: moveID(d.t, verdict, key), Result: core.ResultDone})
}

// busy runs issues 1 and 2 on a crew of two slots.
func busy(t *testing.T) *driver {
	t.Helper()
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	return d
}

// Covers AE1.
func TestAE1ABusyTickDoesNotListAndSaysSo(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("1", 1, ready)))
	d.runAll(d.take(issue("2", 2, ready)))
	d.wrote("1")
	d.wrote("2")

	cmds, events := d.send(core.Tick{})
	wantListings(t, cmds, 0)
	wantEvents(t, skips(events), core.PollSkipped{At: d.now, Busy: 2, Slots: 2})
	statusOf(t, cmds, "1")
	statusOf(t, cmds, "2")
}

func TestATickWithAFreeSlotListsAndSaysNothingMore(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))

	cmds, events := d.send(core.Tick{})
	wantListings(t, cmds, 1)
	if s := skips(events); s != nil {
		t.Fatalf("skipped polls: %#v", s)
	}
}

func TestEverySkippedTickSaysSo(t *testing.T) {
	d := busy(t)
	for range 3 {
		cmds, events := d.send(core.Tick{})
		wantListings(t, cmds, 0)
		wantEvents(t, skips(events), core.PollSkipped{At: d.now, Busy: 2, Slots: 2})
	}
}

// Covers AE2.
func TestAE2AFreedSlotAfterASkippedTickListsAtOnce(t *testing.T) {
	d := busy(t)
	d.send(core.Tick{})

	cmds, _ := d.release("1")
	wantCommands(t, cmds, core.ListIssues{States: draftListing})

	// The listing started the count again: another release waits for a tick.
	d.send(core.IssuesListed{})
	cmds, _ = d.release("2")
	wantListings(t, cmds, 0)
}

// Covers AE3.
func TestAE3AFreedSlotWithNoSkippedTickWaitsForTheNextTick(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.running(issue("2", 2, ready))

	cmds, _ := d.release("1")
	wantListings(t, cmds, 0)
}

// Covers AE4.
func TestAE4ASecondReleaseAfterAnImmediateListingWaitsForTheNextTick(t *testing.T) {
	d := busy(t)
	d.send(core.Tick{})

	cmds, _ := d.release("1")
	wantListings(t, cmds, 1)
	took, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("3", 3, ready)}})
	d.settle(took)
	wantHeld(t, d.m, "2", "3")

	cmds, _ = d.release("2")
	wantListings(t, cmds, 0)
}

// Covers AE6.
func TestAE6AnIssueWithAnOwedMoveKeepsItsSlot(t *testing.T) {
	d := busy(t)
	verdict := d.endActions("1")
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})

	cmds, events := d.send(core.Tick{})
	wantCommands(t, cmds, core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview})
	wantEvents(t, skips(events), core.PollSkipped{At: d.now, Busy: 2, Slots: 2})
}

// Covers AE7.
func TestAE7NoSkipLineOnceTheRunTimeIsUp(t *testing.T) {
	d := busy(t)
	d.send(core.TimeUp{Limit: limit})

	cmds, events := d.send(core.Tick{})
	wantListings(t, cmds, 0)
	if s := skips(events); s != nil {
		t.Fatalf("skipped polls once the run time is up: %#v", s)
	}
}

func TestAReleaseAfterASkippedTickListsNothingOnceTheRunTimeIsUp(t *testing.T) {
	d := newDriver(t, draft(), 3)
	d.running(issue("1", 1, ready), issue("2", 2, ready), issue("3", 3, ready))
	d.send(core.Tick{})
	d.send(core.TimeUp{Limit: limit})

	cmds, _ := d.release("1")
	wantListings(t, cmds, 0)
}

func TestAReleaseAfterASkippedTickListsNothingWhileStopping(t *testing.T) {
	d := busy(t)
	verdict := d.endActions("1")
	d.send(core.Tick{})
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	wantListings(t, cmds, 0)
}
