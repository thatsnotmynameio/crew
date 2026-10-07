package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// draftListing is what a listing of the draft rules asks for: each rule's
// ready and running labels, rule by rule (KTD10).
var draftListing = []crew.State{ready, inProgress, readyToReview, inReview}

// defaultColumns is the default board of withFixReview's rules, as config
// builds it: one column per rule, with its ready and running labels and
// its kind (R22).
func defaultColumns() []crew.BoardColumn {
	return []crew.BoardColumn{
		{Name: "implement", Labels: []crew.State{ready, inProgress}},
		{Name: "review", Labels: []crew.State{readyToReview, inReview}},
		{Name: "fix review", Labels: []crew.State{fixReviewReady, fixing}, Takes: crew.KindPullRequest},
	}
}

// newListedDriver returns a driver whose model fills the default board of
// withFixReview from its own listings, holding at most two issues.
func newListedDriver(t *testing.T) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(withFixReview(), 2, core.BoardFromListings(defaultColumns())), now: t0}
}

// on is item on the board with labels.
func on(item crew.Issue, labels ...crew.State) crew.BoardIssue {
	return crew.NewBoardIssue(item, labels)
}

func TestTheListingAsksForEveryRulesReadyAndRunningLabelsAndNoBoardRead(t *testing.T) {
	d := newListedDriver(t)

	cmds, _ := d.send(core.Tick{})

	wantCommands(t, cmds, core.ListIssues{States: append(draftListing, fixReviewReady, fixing)})
}

// Covers R22: an item in a column's ready label shows there at once, and
// moves to the same column's running label as soon as crew's take lands,
// before the next listing.
func TestADefaultColumnShowsItsReadyItemsAndTheTakeMovesThemAtOnce(t *testing.T) {
	d := newListedDriver(t)
	one := issue("1", 1, ready)

	take, _ := d.poll(one)
	wantBoard(t, d, on(one, ready))

	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantBoard(t, d, on(one, inProgress))
}

// Covers KTD10: an item left in a rule's running label that crew does not
// hold gets a card in that rule's column, and crew never takes it.
func TestAnItemInARunningLabelCrewDoesNotHoldHasACardAndIsNeverTaken(t *testing.T) {
	d := newListedDriver(t)
	five := issue("5", 5, inProgress)

	cmds, events := d.poll(five)

	wantCommands(t, cmds)
	wantEvents(t, events, core.PollDone{At: d.now, Listed: 1, Taken: 0})
	wantBoard(t, d, on(five, inProgress))
	wantHeld(t, d.m)
}

// Covers R23 and R28: an item whose rule ended in a label no column names
// leaves the board, and no card waits for it anywhere.
func TestAnItemMovedToALabelNoColumnNamesLeavesTheBoard(t *testing.T) {
	d := newListedDriver(t)
	d.running(issue("1", 1, readyToReview))
	wantBoard(t, d, on(issue("1", 1, readyToReview), inReview))

	ending, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "custom_review", Outcome: succeeded})
	d.settle(ending)

	wantBoard(t, d)
}

// Covers R22: a pull request shows only in a column of pull requests; one
// carrying an issue rule's label, mirrored from its issue, shows nowhere,
// and an issue in a pull request rule's label shows nowhere either.
func TestAnItemShowsOnlyInTheColumnsOfItsKind(t *testing.T) {
	d := newListedDriver(t)
	mirrored := pullRequest(issue("91", 2, inProgress))

	take, _ := d.poll(pr90(1, fixReviewReady), mirrored, issue("42", 3, fixReviewReady))
	wantBoard(t, d, on(pr90(1, fixReviewReady), fixReviewReady))

	d.settle(take)
	wantBoard(t, d, on(pr90(1, fixReviewReady), fixing))

	ending, _ := d.send(core.SessionEnded{IssueID: issueID("90"), Action: "fix", Outcome: succeeded})
	d.settle(ending)
	wantBoard(t, d)
}

// A listing asked for before a move landed predates it: the move stays on
// the board until a later listing (KTD4).
func TestAListingThatPredatesAMoveKeepsIt(t *testing.T) {
	d := newListedDriver(t)
	twelve := issue("12", 12, ready)
	d.running(twelve)
	d.send(core.SessionEnded{IssueID: issueID("12"), Action: "acceptance", Outcome: succeeded})
	ending, _ := d.send(core.SessionEnded{IssueID: issueID("12"), Action: "development", Outcome: succeeded})
	d.tick()

	d.settle(ending)
	wantBoard(t, d, on(twelve, readyToReview))

	stale := issue("12", 12, inProgress)
	d.send(core.IssuesListed{Issues: []crew.Issue{stale}})
	wantBoard(t, d, on(stale, readyToReview))

	d.tick()
	d.send(core.IssuesListed{})
	wantBoard(t, d)
}

// Covers KTD5 on the default board: a failed listing keeps the board and
// says why, until a listing succeeds.
func TestAFailedListingKeepsTheDefaultBoardAndSaysSo(t *testing.T) {
	d := newListedDriver(t)
	five := issue("5", 5, inProgress)
	d.poll(five)

	d.tick()
	d.send(core.ListFailed{Reason: "gh: timeout"})
	if v := d.m.View(); v.BoardFailure != "gh: timeout" {
		t.Fatalf("board failure: got %q, want %q", v.BoardFailure, "gh: timeout")
	}
	wantBoard(t, d, on(five, inProgress))

	d.poll()
	if v := d.m.View(); v.BoardFailure != "" {
		t.Fatalf("board failure after a listing: %q", v.BoardFailure)
	}
	wantBoard(t, d)
}
