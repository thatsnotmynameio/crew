package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestAE9StopEndsTheRunsOfEndedIssuesAndStopsRunningOnes(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	ending, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	wantCommands(t, ending, core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds,
		core.StopSession{IssueID: issueID("2"), Run: d.run(issueID("2")), Action: "acceptance"},
		core.StopSession{IssueID: issueID("2"), Run: d.run(issueID("2")), Action: "development"},
	)
	if d.m.Stopped() {
		t.Fatal("stopped while issues are held")
	}

	_, events := d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone})
	hasEvent(t, events, crew.EndingMoved{EventHead: d.runHead("1"), From: inProgress, To: readyToReview})

	d.send(core.SessionEnded{IssueID: issueID("2"), Action: "acceptance", Outcome: failed("stopped")})
	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("2"), Action: "development", Outcome: failed("stopped")})
	wantCommands(t, cmds,
		core.Move{IssueID: issueID("2"), From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueID: issueID("2"), IssueRef: "#2", Failures: []crew.ActionFailure{
			failure("2", "acceptance"),
			failure("2", "development"),
		}}},
	)
	d.wantReason("2", "acceptance", "stopped")
	d.wantReason("2", "development", "stopped")

	d.send(core.CallResult{ID: moveID(t, cmds, "2"), Result: core.ResultDone})
	if d.m.Stopped() {
		t.Fatal("stopped while #2's report is in flight")
	}
	_, events = d.send(core.CallResult{ID: reportID(t, cmds, "2"), Result: core.ResultDone})
	if !d.m.Stopped() {
		t.Fatal("not stopped once every ending call settled")
	}
	hasEvent(t, events, core.Stopped{At: d.now})
}

// owedTakeFinalTries are the ways a take reaches its final try at stop.
var owedTakeFinalTries = []struct {
	name string
	// final returns the final try's command, from a take that is owed or
	// in flight at stop.
	final func(d *driver, take []core.Command) []core.Command
}{
	{
		name: "owed at stop",
		final: func(d *driver, take []core.Command) []core.Command {
			d.send(core.CallResult{ID: moveID(d.t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
			cmds, _ := d.send(core.StopRequested{})
			return cmds
		},
	},
	{
		name: "in flight at stop",
		final: func(d *driver, take []core.Command) []core.Command {
			if cmds, _ := d.send(core.StopRequested{}); len(cmds) != 0 {
				d.t.Fatalf("stop issued %#v while the take is in flight", cmds)
			}
			cmds, _ := d.send(core.CallResult{ID: moveID(d.t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
			return cmds
		},
	},
}

// At stop, an owed take gets its one final try. If it lands, the issue is
// in moves_to with nothing started, so it needs attention like an issue
// whose take landed after the stop; if it fails, the core gives it up.
func TestStopGivesAnOwedTakeOneFinalTry(t *testing.T) {
	stoppedReport := core.ReportFailure{Report: crew.FailureReport{
		IssueID: issueID("1"), IssueRef: "#1", Failures: []crew.ActionFailure{
			{Action: "acceptance"},
			{Action: "development"},
		},
	}}
	for _, tt := range owedTakeFinalTries {
		t.Run(tt.name+", final try done", func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))
			final := tt.final(d, take)
			wantCommands(t, final, core.Move{IssueID: issueID("1"), From: ready, To: inProgress})

			cmds, _ := d.send(core.CallResult{ID: moveID(t, final, "1"), Result: core.ResultDone})
			wantCommands(t, cmds, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention}, stoppedReport)
			d.wantReason("1", "acceptance", "crew stopped")
			d.wantReason("1", "development", "crew stopped")
			d.settle(cmds)
			if !d.m.Stopped() {
				t.Fatal("not stopped once the ending calls settled")
			}
		})
		t.Run(tt.name+", final try failed", func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))
			final := tt.final(d, take)

			cmds, events := d.send(core.CallResult{ID: moveID(t, final, "1"), Result: core.ResultFailed, Reason: "still down"})
			wantCommands(t, cmds)
			hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
				Kind: core.CallMove, IssueID: issueID("1"), IssueRef: "#1", From: ready, To: inProgress,
			}})
			if !d.m.Stopped() {
				t.Fatal("not stopped once the owed take had its final try")
			}
			hasEvent(t, events, core.Stopped{At: d.now})
		})
	}
}

func TestStopDuringTakeStartsNothingAndNeedsAttention(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds)
	if c := claimOf(t, d.m, "1"); c != core.ClaimStopping {
		t.Fatalf("claim of #1: got %v, want stopping", c)
	}

	cmds, _ = d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueID: issueID("1"), IssueRef: "#1", Failures: []crew.ActionFailure{
			{Action: "acceptance"},
			{Action: "development"},
		}}},
	)
	d.wantReason("1", "acceptance", "crew stopped")
	d.wantReason("1", "development", "crew stopped")
}

func TestStopDuringSetupStartsNothingMoreAndStopsWhatStarted(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	d.send(space("1", "development")) // its StartSession is in flight

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds)

	cmds, _ = d.send(space("1", "acceptance"))
	wantCommands(t, cmds)
	cmds, _ = d.send(core.SessionStarted{IssueID: issueID("1"), Action: "development"})
	wantCommands(t, cmds, core.StopSession{IssueID: issueID("1"), Run: d.run(issueID("1")), Action: "development"})

	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: failed("stopped")})
	wantCommands(t, cmds,
		core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueID: issueID("1"), IssueRef: "#1", Failures: []crew.ActionFailure{
			{Action: "acceptance", Workspace: "issue-1-acceptance"},
			failure("1", "development"),
		}}},
	)
	d.wantReason("1", "acceptance", "crew stopped")
	d.wantReason("1", "development", "stopped")
}

func TestStopGivesEachOwedCallOneFinalTry(t *testing.T) {
	d := newDriver(t, draft(), 2)
	ending := endedNeedingAttention(d)
	d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})

	// The report, in flight at stop, fails transiently: it gets its final try.
	retry, _ := d.send(core.CallResult{ID: reportID(t, ending, "1"), Result: core.ResultFailed, Reason: "timeout"})
	if len(retry) != 1 {
		t.Fatalf("report retry: got %#v, want one ReportFailure", retry)
	}
	reportRetry := reportID(t, retry, "1")

	_, events := d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultFailed, Reason: "still down"})
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallMove, IssueID: issueID("1"), IssueRef: "#1", From: inProgress, To: needsAttention,
	}})
	cmds, events = d.send(core.CallResult{ID: reportRetry, Result: core.ResultFailed, Reason: "still down"})
	wantCommands(t, cmds)
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallReport, IssueID: issueID("1"), IssueRef: "#1",
	}})
	if !d.m.Stopped() {
		t.Fatal("not stopped once every owed call had its final try")
	}
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestStopWithNothingHeldStopsAtOnceAndPollsNoMore(t *testing.T) {
	d := newDriver(t, draft(), 2)
	cmds, _ := d.send(core.Tick{}) // a listing is outstanding at stop

	_, events := d.send(core.StopRequested{})
	if !d.m.Stopped() {
		t.Fatal("not stopped with nothing held")
	}
	hasEvent(t, events, core.Stopped{At: d.now})
	wantCommands(t, cmds, core.ListIssues{States: draftListing})

	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, ready)}})
	wantCommands(t, cmds)
	cmds, events = d.send(core.Tick{})
	wantCommands(t, cmds)
	_, events2 := d.send(core.StopRequested{})
	if len(events)+len(events2) != 0 {
		t.Fatalf("events after stop: %#v %#v", events, events2)
	}
}
