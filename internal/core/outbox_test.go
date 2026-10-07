package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// takeOf is #key's take move in the draft rules.
func takeOf(key string) core.Call {
	return core.Call{Kind: core.CallMove, IssueID: issueID(key), IssueRef: "#" + key, From: ready, To: inProgress}
}

// wantOwed fails unless the view's owed calls are want.
func wantOwed(t *testing.T, m *core.Model, want ...core.Call) {
	t.Helper()
	if got := m.View().Owed; !reflect.DeepEqual(got, want) {
		t.Fatalf("owed: got %#v, want %#v", got, want)
	}
}

// wantClaim fails unless the claim of #key is want.
func wantClaim(t *testing.T, m *core.Model, key string, want core.Claim) {
	t.Helper()
	if got := claimOf(t, m, key); got != want {
		t.Fatalf("claim of #%s: got %v, want %v", key, got, want)
	}
}

// Covers AE2. The run sees nothing of a transient failure: only the
// delivery's CallOwed is emitted, the card shows owed from the first failure
// until the take lands, through the retries in flight, and the landed take
// starts the actions.
func TestAE2ATakeOwedTwiceStartsItsActionsOnceItLands(t *testing.T) {
	d := newDriver(t, draft(), 1)
	i1 := issue("1", 1, ready)
	take, _ := d.poll(i1)

	_, events := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallOwed{At: d.now, Call: takeOf("1"), Reason: "timeout"})
	wantClaim(t, d.m, "1", core.ClaimOwed)

	retry, _ := d.send(core.Tick{})
	wantOwed(t, d.m, takeOf("1"))
	wantClaim(t, d.m, "1", core.ClaimOwed)

	_, events = d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallOwed{At: d.now, Call: takeOf("1"), Reason: "timeout"})
	wantClaim(t, d.m, "1", core.ClaimOwed)

	retry, _ = d.send(core.Tick{})
	cmds, events := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	wantEvents(t, events, core.IssueMoved{At: d.now, IssueID: issueID("1"), IssueRef: "#1", From: ready, To: inProgress})
	wantCommands(t, cmds,
		core.CreateWorkspace{Issue: i1, Action: "acceptance"},
		core.CreateWorkspace{Issue: i1, Action: "development"},
	)
	wantClaim(t, d.m, "1", core.ClaimRunning)
	wantOwed(t, d.m)
}

// A verdict whose move was owed keeps showing owed until its failure report
// lands too, and only then is the issue released.
func TestAnOwedVerdictShowsOwedUntilEveryVerdictCallSettles(t *testing.T) {
	d := newDriver(t, draft(), 2)
	verdict := judgedNeedingAttention(d)
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	retry, _ := d.send(core.Tick{})

	d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	wantHeld(t, d.m, "1")
	wantClaim(t, d.m, "1", core.ClaimOwed)
	wantOwed(t, d.m)

	d.send(core.CallResult{ID: reportID(t, verdict, "1"), Result: core.ResultDone})
	wantHeld(t, d.m)
}

// An owed verdict move holds its queue's slot, while the global limit has
// room, and frees it once its retry lands.
func TestAnOwedVerdictMoveHoldsItsQueuesSlot(t *testing.T) {
	d := newDriver(t, inQueues(draft(), clerk), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})

	retry, _ := d.send(core.Tick{})
	_, events := d.send(core.IssuesListed{Issues: []crew.Issue{issue("2", 2, ready)}})
	if got := takenKeys(events); got != nil {
		t.Fatalf("taken while the clerk's slot is owed: %v", got)
	}

	d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	wantHeld(t, d.m)
	d.send(core.Tick{})
	_, events = d.send(core.IssuesListed{Issues: []crew.Issue{issue("2", 2, ready)}})
	if got := takenKeys(events); !reflect.DeepEqual(got, []string{"2"}) {
		t.Fatalf("taken once the slot freed: got %v, want [2]", got)
	}
}

// A tick retries each held issue's owed calls and reports its running
// status issue by issue, in the order they were taken.
func TestATickRetriesAndReportsIssueByIssueInTakenOrder(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	takes, _ := d.poll(issue("1", 1, ready), issue("2", 2, ready))
	d.send(core.CallResult{ID: moveID(t, takes, "1"), Result: core.ResultFailed, Reason: "timeout"})
	cmds, _ := d.send(core.CallResult{ID: moveID(t, takes, "2"), Result: core.ResultDone})
	d.settle(cmds)
	for {
		cmds, _ := d.wrote("2")
		if len(statuses(cmds)) == 0 {
			break
		}
	}

	cmds, _ = d.send(core.Tick{})
	if len(cmds) != 2 {
		t.Fatalf("tick: got %#v, want the retry of #1 then the status of #2", cmds)
	}
	wantCommands(t, cmds[:1], core.Move{IssueID: issueID("1"), From: ready, To: inProgress})
	if st := statusOf(t, cmds[1:], "2"); st.Kind != crew.StatusRunning {
		t.Fatalf("status of #2: got %#v, want running", st)
	}
}

// A stop gives an owed take its final try and stops the running sessions,
// issue by issue in the order they were taken; the owed issue still shows
// owed.
func TestAStopTriesOwedCallsAndStopsSessionsInTakenOrder(t *testing.T) {
	d := newDriver(t, draft(), 2)
	takes, _ := d.poll(issue("1", 1, ready), issue("2", 2, ready))
	d.send(core.CallResult{ID: moveID(t, takes, "1"), Result: core.ResultFailed, Reason: "timeout"})
	cmds, _ := d.send(core.CallResult{ID: moveID(t, takes, "2"), Result: core.ResultDone})
	d.settle(cmds)

	cmds, _ = d.send(core.StopRequested{})
	wantCommands(t, cmds,
		core.Move{IssueID: issueID("1"), From: ready, To: inProgress},
		core.StopSession{IssueID: issueID("2"), Action: "acceptance"},
		core.StopSession{IssueID: issueID("2"), Action: "development"},
	)
	wantClaim(t, d.m, "1", core.ClaimOwed)
	wantClaim(t, d.m, "2", core.ClaimStopping)
}

// The view lists the held issues' owed calls in the order the issues were
// taken, then the owed pull request reports.
func TestOwedCallsListRunCallsInTakenOrderThenReports(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	takes, _ := d.poll(issue("1", 1, ready), issue("2", 2, ready))
	d.send(core.CallResult{ID: moveID(t, takes, "2"), Result: core.ResultFailed, Reason: "timeout"})
	d.send(core.CallResult{ID: moveID(t, takes, "1"), Result: core.ResultDone})
	d.send(core.PullRequestsResult{IssueID: issueID("1"), Result: core.ResultFailed, Reason: "timeout"})

	wantOwed(t, d.m,
		takeOf("2"),
		core.Call{Kind: core.CallPullRequests, IssueID: issueID("1"), IssueRef: "#1", To: inProgress},
	)
}

// A rule without actions whose owed take lands is judged at once and shows
// judging while its verdict move is in flight.
func TestARuleWithoutActionsWhoseOwedTakeLandsShowsJudging(t *testing.T) {
	d := newDriver(t, promoted(), 2)
	take := takePromoted(d)
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
	retry, _ := d.send(core.Tick{})

	verdict, _ := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	wantCommands(t, verdict, promoteMove())
	wantClaim(t, d.m, "1", core.ClaimJudging)
	wantOwed(t, d.m)
}

// An owed take whose final try lands after a stop ends its actions
// unstarted and shows judging while its verdict calls are in flight.
func TestAnOwedTakeLandingOnItsFinalTryShowsJudging(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
	final, _ := d.send(core.StopRequested{})

	d.send(core.CallResult{ID: moveID(t, final, "1"), Result: core.ResultDone})
	wantClaim(t, d.m, "1", core.ClaimJudging)
	wantOwed(t, d.m)
}

// A take dropped on its final try gives its run nothing to report: only the
// drop and the stop are emitted, and nothing is handled.
func TestATakeDroppedAfterAStopReportsOnlyTheDrop(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
	final, _ := d.send(core.StopRequested{})

	cmds, events := d.send(core.CallResult{ID: moveID(t, final, "1"), Result: core.ResultFailed, Reason: "still down"})
	wantCommands(t, cmds)
	wantEvents(t, events,
		core.CallDropped{At: d.now, Call: takeOf("1"), Result: core.ResultFailed, Reason: "still down"},
		core.Stopped{At: d.now},
	)
	if got := d.m.View().Handled; got != nil {
		t.Fatalf("handled after a dropped take: %#v", got)
	}
}

// An owed ended status holds no slot: its issue is released and the next
// listing takes another issue while the status waits for its retry.
func TestAnOwedEndedStatusHoldsNoSlot(t *testing.T) {
	d := newStatusDriver(t, draft(), 1)
	statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	wantHeld(t, d.m)

	_, events := d.poll(issue("75", 2, ready))
	if got := takenKeys(events); !reflect.DeepEqual(got, []string{"75"}) {
		t.Fatalf("taken while #74's ended status is owed: got %v, want [75]", got)
	}
}

// Two runs of a rule without actions on one issue edit one status entry: a
// status lane keeps what the comment shows for the whole run.
func TestTwoRunsOfARuleWithoutActionsEditOneStatusEntry(t *testing.T) {
	d := newStatusDriver(t, promoted(), 2)
	runOnce := func() crew.Status {
		t.Helper()
		take := takePromoted(d)
		verdict, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
		st := statusOf(t, verdict, "1")
		d.wrote("1")
		landed, _ := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
		d.settle(landed)
		d.wrote("1")
		return st
	}

	first, second := runOnce(), runOnce()
	if second.Run != first.Run {
		t.Fatalf("second run's entry = %q, want the first's, %q", second.Run, first.Run)
	}
}

// After a stop, an issue's status lane gets one final try in all:
// a later ended status of the issue that fails transiently is given up.
func TestAfterAStopAnIssuesStatusesGetOneFinalTryInAll(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.StopRequested{})
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("stopped")})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: failed("stopped")})

	cmds, _ := d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	if st := statusOf(t, cmds, "74"); st.Move != crew.MovePending {
		t.Fatalf("final try: got %#v, want the ended status with the move pending", st)
	}
	d.wrote("74")

	cmds, _ = d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	if st := statusOf(t, cmds, "74"); st.Move != crew.MoveDone {
		t.Fatalf("after the move landed: got %#v, want the ended status with the move done", st)
	}
	cmds, _ = d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	noStatusOf(t, cmds, "74")

	d.send(core.CallResult{ID: reportID(t, verdict, "74"), Result: core.ResultDone})
	if !d.m.Stopped() {
		t.Fatal("not stopped once the verdict calls settled")
	}
}

// A result that answers no delivery in flight changes nothing: one with an
// id the outbox never issued, and a second one for a delivery already owed.
func TestAResultForNoDeliveryInFlightChangesNothing(t *testing.T) {
	d := newDriver(t, draft(), 1)
	take, _ := d.poll(issue("1", 1, ready))
	failedTake := core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"}
	d.send(failedTake)
	before := d.m.View()

	for _, r := range []core.CallResult{
		{ID: moveID(t, take, "1") + 100, Result: core.ResultDone},
		failedTake,
	} {
		cmds, events := d.send(r)
		wantCommands(t, cmds)
		wantEvents(t, events)
		if after := d.m.View(); !reflect.DeepEqual(after, before) {
			t.Fatalf("view after %#v:\n got %#v\nwant %#v", r, after, before)
		}
	}
}
