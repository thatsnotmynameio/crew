package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// startsSession reports whether cmds start the session of action on issue
// key.
func startsSession(cmds []core.Command, key string, action crew.ActionName) bool {
	for _, c := range cmds {
		if s, ok := c.(core.StartSession); ok && s.IssueID == issueID(key) && s.Action == action {
			return true
		}
	}
	return false
}

// wantPaused fails the test unless the view's pause is want.
func wantPaused(t *testing.T, d *driver, want bool) {
	t.Helper()
	if got := d.m.View().Paused; got != want {
		t.Fatalf("view: Paused %v, want %v", got, want)
	}
}

// Covers AE1 (R1, R2, R3): a paused crew lists at every tick, takes
// nothing, and lets the issues it holds run on, a later action included.
func TestAE1APausedCrewTakesNothingAndLetsItsHeldIssuesRunOn(t *testing.T) {
	d := newDriver(t, draft(), 3)
	d.running(issue("1", 1, ready), issue("2", 2, ready))

	cmds, events := d.send(core.PauseToggled{})
	wantCommands(t, cmds)
	wantEvents(t, events, core.Paused{At: d.now})
	wantPaused(t, d, true)

	cmds, events = d.poll(issue("3", 3, ready))
	wantCommands(t, cmds)
	wantEvents(t, events, core.PollDone{At: d.now, Listed: 1, Taken: 0})
	wantHeld(t, d.m, "1", "2")

	next := d.ended("1", "acceptance", succeeded)
	if !startsSession(next, "1", "development") {
		t.Fatalf("paused: #1's next action did not start: %#v", next)
	}
	ending := d.endActions("2")
	wantCommands(t, ending, core.Move{IssueID: issueID("2"), From: inProgress, To: readyToReview})
	d.send(core.CallResult{ID: moveID(t, ending, "2"), Result: core.ResultDone})
	wantHeld(t, d.m, "1")
	wantPaused(t, d, true)
}

// Covers AE2 (R4, R7): resuming lists at once, and that listing takes.
func TestAE2ResumingListsAtOnceAndTakesAgain(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.send(core.PauseToggled{})
	d.poll(issue("3", 3, ready))

	cmds, events := d.send(core.PauseToggled{})
	wantCommands(t, cmds, core.ListIssues{States: draftListing})
	wantEvents(t, events, core.Resumed{At: d.now})
	wantPaused(t, d, false)

	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("3", 3, ready)}})
	wantCommands(t, cmds, core.Move{IssueID: issueID("3"), From: ready, To: inProgress})
	wantHeld(t, d.m, "3")
}

// Covers KTD3: a listing already in flight when crew resumes takes when it
// arrives, and the resume asks for no second one.
func TestAResumeWhileAListingIsOutstandingWaitsForIt(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.send(core.PauseToggled{})
	d.send(core.Tick{})

	cmds, _ := d.send(core.PauseToggled{})
	wantCommands(t, cmds)

	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("3", 3, ready)}})
	wantCommands(t, cmds, core.Move{IssueID: issueID("3"), From: ready, To: inProgress})
}

// Covers KTD3: with every slot busy, a resume asks for no listing.
func TestAResumeWithEverySlotBusyListsNothing(t *testing.T) {
	d := newDriver(t, draft(), 1)
	d.running(issue("1", 1, ready))
	d.send(core.PauseToggled{})

	cmds, events := d.send(core.PauseToggled{})
	wantCommands(t, cmds)
	wantEvents(t, events, core.Resumed{At: d.now})
}

// Covers KTD3: a resume with every slot busy, after paused ticks that
// listed, still lists at once when the next slot frees, as an unpaused
// crew whose tick skipped its listing does.
func TestAfterAResumeWithEverySlotBusyAFreedSlotListsAtOnce(t *testing.T) {
	d := newDriver(t, draft(), 1)
	d.running(issue("1", 1, ready))
	d.send(core.PauseToggled{})
	d.poll(issue("1", 1, inProgress), issue("2", 2, ready))
	d.send(core.PauseToggled{})

	cmds, _ := d.release("1")

	wantCommands(t, cmds, core.ListIssues{States: draftListing})
}

// Covers AE5 (R3): a paused crew with every slot busy still lists, so a
// new ready issue reaches the board, and still takes nothing.
func TestAE5APausedCrewWithEverySlotBusyListsForTheBoard(t *testing.T) {
	d := newListedDriver(t)
	one, two, three := issue("1", 1, ready), issue("2", 2, ready), issue("3", 3, ready)
	d.running(one, two)
	d.send(core.PauseToggled{})

	cmds, events := d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: append(draftListing, fixReviewReady, fixing)})
	for _, e := range events {
		if _, ok := e.(core.PollSkipped); ok {
			t.Fatalf("a paused tick skipped its listing: %#v", events)
		}
	}

	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{
		issue("1", 1, inProgress), issue("2", 2, inProgress), three,
	}})
	wantCommands(t, cmds)
	wantBoard(t, d, on(issue("1", 1, inProgress), inProgress), on(issue("2", 2, inProgress), inProgress),
		on(three, ready))
	wantHeld(t, d.m, "1", "2")
}

// Unpaused, a tick with every slot busy still skips its listing.
func TestUnpausedATickWithEverySlotBusyStillSkipsItsListing(t *testing.T) {
	d := newDriver(t, draft(), 1)
	d.running(issue("1", 1, ready))
	d.send(core.PauseToggled{})
	d.send(core.PauseToggled{})

	cmds, events := d.send(core.Tick{})
	wantCommands(t, cmds)
	hasEvent(t, events, core.PollSkipped{At: d.now, Busy: 1, Slots: 1})
}

// Covers R3: a paused crew retries an owed call at the next tick.
func TestAPausedCrewRetriesAnOwedCall(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
	d.send(core.PauseToggled{})

	cmds, _ := d.send(core.Tick{})
	wantCommands(t, cmds,
		core.ListIssues{States: draftListing}, core.Move{IssueID: issueID("1"), From: ready, To: inProgress})
}

// Covers AE3 (R5, R9): a paused crew with nothing held keeps running until
// it is stopped, then stops at once.
func TestAE3APausedCrewWithNothingHeldRunsUntilStopped(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.send(core.PauseToggled{})
	for range 3 {
		d.poll()
		if d.m.Stopped() {
			t.Fatal("a paused crew with nothing held stopped by itself")
		}
	}

	cmds, events := d.send(core.StopRequested{})
	wantCommands(t, cmds)
	hasEvent(t, events, core.Stopped{At: d.now})
	wantPaused(t, d, false)
}

// Covers AE4 (R9): a stop reaches a paused crew's running session as it
// would an unpaused one's.
func TestAE4AStopWhilePausedStopsTheRunningSession(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.PauseToggled{})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopSession{IssueID: issueID("1"), Run: d.run(issueID("1")), Action: "acceptance"})
	wantPaused(t, d, false)

	report := d.ended("1", "acceptance", failed("stopped"))
	wantCommands(t, report, failureOf("1", "implement", "acceptance"))
}

// Covers AE6 (R10): the run time limit winds a paused crew down as usual,
// and a toggle changes nothing after it.
func TestAE6TimeUpWhilePausedWindsDownAndTheToggleChangesNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.PauseToggled{})

	_, events := d.send(core.TimeUp{Limit: limit})
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit})
	wantPaused(t, d, false)

	cmds, events := d.send(core.PauseToggled{})
	wantCommands(t, cmds)
	wantEvents(t, events)
	wantPaused(t, d, false)

	report := d.ended("1", "acceptance", succeeded)
	wantCommands(t, report, failureOf("1", "implement", "development"))
	moved, _ := d.send(core.CallResult{ID: reportID(t, report, "1"), Result: core.ResultDone})
	_, events = d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

// Covers R10: a toggle during a stop changes nothing.
func TestTheToggleDuringAStopChangesNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.StopRequested{})

	cmds, events := d.send(core.PauseToggled{})
	wantCommands(t, cmds)
	wantEvents(t, events)
	wantPaused(t, d, false)
}
