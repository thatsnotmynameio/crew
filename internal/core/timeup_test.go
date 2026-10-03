package core_test

import (
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// limit is the run time limit of the wind-down tests.
const limit = time.Hour

func TestAE2TimeUpWithNothingHeldWindsDownAndStopsAtOnce(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.send(core.Tick{}) // a listing is outstanding when time is up

	cmds, events := d.send(core.TimeUp{Limit: limit})
	wantCommands(t, cmds)
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit}, core.Stopped{At: d.now})
	if !d.m.Stopped() {
		t.Fatal("not stopped with nothing held")
	}

	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, ready)}})
	wantCommands(t, cmds)
}

func TestAE3TimeUpLetsARunningIssueFinishAndTakesNothingNew(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))

	cmds, events := d.send(core.TimeUp{Limit: limit})
	wantCommands(t, cmds)
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit})
	if v := d.m.View(); !v.TimeUp || v.Stopping {
		t.Fatalf("view: TimeUp %v, Stopping %v; want true, false", v.TimeUp, v.Stopping)
	}

	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds)
	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("43", 2, ready)}})
	wantCommands(t, cmds)

	d.send(core.SessionEnded{IssueKey: "42", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "42", Action: "development", Outcome: succeeded})
	wantCommands(t, verdict, core.Move{IssueKey: "42", From: inProgress, To: readyToReview})
	if d.m.Stopped() {
		t.Fatal("stopped while #42's verdict move is in flight")
	}

	_, events = d.send(core.CallResult{ID: moveID(t, verdict, "42"), Result: core.ResultDone})
	wantEvents(t, events,
		core.IssueMoved{At: d.now, IssueKey: "42", IssueRef: "#42", From: inProgress, To: readyToReview},
		core.Stopped{At: d.now},
	)
	if !d.m.Stopped() {
		t.Fatal("not stopped once #42 was judged")
	}
	if d.m.View().Stopping {
		t.Fatal("the view says a stop was requested; none was")
	}
}

func TestAE3AnIssueThatFailsWhileWindingDownNeedsAttentionAsUsual(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	d.send(core.SessionEnded{IssueKey: "42", Action: "acceptance", Outcome: failed("broke")})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "42", Action: "development", Outcome: succeeded})
	wantCommands(t, cmds,
		core.Move{IssueKey: "42", From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "42", IssueRef: "#42", Failures: []crew.ActionFailure{
			failure("42", "acceptance", "broke"),
		}}},
	)

	d.send(core.CallResult{ID: moveID(t, cmds, "42"), Result: core.ResultDone})
	_, events := d.send(core.CallResult{ID: reportID(t, cmds, "42"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestATakeInFlightWhenTimeIsUpStartsItsActions(t *testing.T) {
	d := newDriver(t, draft(), 2)
	i42 := issue("42", 1, ready)
	take, _ := d.poll(i42)
	d.send(core.TimeUp{Limit: limit})

	cmds, _ := d.send(core.CallResult{ID: moveID(t, take, "42"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.CreateWorkspace{Issue: i42, Action: "acceptance"},
		core.CreateWorkspace{Issue: i42, Action: "development"},
	)
}

func TestAnOwedTakeWhenTimeIsUpIsRetriedAtTicksAndThenRuns(t *testing.T) {
	d := newDriver(t, draft(), 2)
	i42 := issue("42", 1, ready)
	take, _ := d.poll(i42)
	d.send(core.CallResult{ID: moveID(t, take, "42"), Result: core.ResultFailed, Reason: "timeout"})

	_, events := d.send(core.TimeUp{Limit: limit})
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit})

	retry, events := d.send(core.Tick{})
	wantCommands(t, retry, core.Move{IssueKey: "42", From: ready, To: inProgress})
	if len(events) != 0 || d.m.Stopped() {
		t.Fatalf("a tick with an owed take: events %#v, stopped %v", events, d.m.Stopped())
	}

	cmds, _ := d.send(core.CallResult{ID: moveID(t, retry, "42"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.CreateWorkspace{Issue: i42, Action: "acceptance"},
		core.CreateWorkspace{Issue: i42, Action: "development"},
	)
}

func TestWhileWindingDownOwedCallsAreRetriedAtTicksThenGetAFinalTry(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	d.send(core.TimeUp{Limit: limit})

	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry, core.Move{IssueKey: "1", From: inProgress, To: readyToReview})
	owed := core.Call{Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview}
	_, events := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})

	// #2's last action ends: nothing is left to end, so the owed move gets
	// its final try.
	d.send(core.SessionEnded{IssueKey: "2", Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "2", Action: "development", Outcome: succeeded})
	wantCommands(t, cmds,
		core.Move{IssueKey: "2", From: inProgress, To: readyToReview},
		core.Move{IssueKey: "1", From: inProgress, To: readyToReview},
	)

	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallDropped{At: d.now, Call: owed, Result: core.ResultFailed, Reason: "timeout"})
	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "2"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestAE4AStopWhileWindingDownStopsRunningSessionsAsUsual(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds,
		core.StopSession{IssueKey: "42", Action: "acceptance"},
		core.StopSession{IssueKey: "42", Action: "development"},
	)
	if !d.m.View().Stopping {
		t.Fatal("the view does not say a stop was requested")
	}

	d.send(core.SessionEnded{IssueKey: "42", Action: "acceptance", Outcome: failed("stopped")})
	cmds, _ = d.send(core.SessionEnded{IssueKey: "42", Action: "development", Outcome: failed("stopped")})
	wantCommands(t, cmds,
		core.Move{IssueKey: "42", From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "42", IssueRef: "#42", Failures: []crew.ActionFailure{
			failure("42", "acceptance", "stopped"),
			failure("42", "development", "stopped"),
		}}},
	)
}

func TestTimeUpAfterAStopOrASecondTimeChangesNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.StopRequested{})
	cmds, events := d.send(core.TimeUp{Limit: limit})
	if len(cmds)+len(events) != 0 || d.m.View().TimeUp {
		t.Fatalf("time up after a stop: commands %#v, events %#v, TimeUp %v", cmds, events, d.m.View().TimeUp)
	}

	d = newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})
	cmds, events = d.send(core.TimeUp{Limit: limit})
	if len(cmds)+len(events) != 0 {
		t.Fatalf("a second time up: commands %#v, events %#v", cmds, events)
	}
}
