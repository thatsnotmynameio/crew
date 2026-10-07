package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// unstartedReport is the failure report of #1 whose first action,
// acceptance, a stop kept from starting before its run had a workspace.
var unstartedReport = core.ReportFailure{Report: crew.FailureReport{
	IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Route: crew.FailedRoute,
	Failures: []crew.ActionFailure{{Action: "acceptance", Verdict: crew.Failed}},
}}

func TestAE9StopLetsRoutingRunsEndAndStopsRunningOnes(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	ending := d.endActions("1")
	wantCommands(t, ending, core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopSession{IssueID: issueID("2"), Run: d.run(issueID("2")), Action: "acceptance"})
	if d.m.Stopped() {
		t.Fatal("stopped while issues are held")
	}

	_, events := d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))

	report := d.ended("2", "acceptance", failed("stopped"))
	wantCommands(t, report, failureOf("2", "implement", "acceptance"))
	d.wantReason("2", "acceptance", "stopped")

	moved, _ := d.send(core.CallResult{ID: reportID(t, report, "2"), Result: core.ResultDone})
	wantCommands(t, moved, core.Move{IssueID: issueID("2"), From: inProgress, To: needsAttention})
	if d.m.Stopped() {
		t.Fatal("stopped while #2's move is in flight")
	}
	_, events = d.send(core.CallResult{ID: moveID(t, moved, "2"), Result: core.ResultDone})
	if !d.m.Stopped() {
		t.Fatal("not stopped once every route ended")
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
// in the running label with nothing started, so its first action ends
// unstarted and the run ends through failed; if it fails, the core gives
// it up.
func TestStopGivesAnOwedTakeOneFinalTry(t *testing.T) {
	for _, tt := range owedTakeFinalTries {
		t.Run(tt.name+", final try done", func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))
			final := tt.final(d, take)
			wantCommands(t, final, core.Move{IssueID: issueID("1"), From: ready, To: inProgress})

			cmds, _ := d.send(core.CallResult{ID: moveID(t, final, "1"), Result: core.ResultDone})
			wantCommands(t, cmds, unstartedReport)
			d.wantReason("1", "acceptance", "crew stopped")
			d.settle(cmds)
			if !d.m.Stopped() {
				t.Fatal("not stopped once the route's steps settled")
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

func TestStopDuringTakeStartsNothingAndEndsThroughFailed(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds)
	if c := claimOf(t, d.m, "1"); c != core.ClaimStopping {
		t.Fatalf("claim of #1: got %v, want stopping", c)
	}

	cmds, _ = d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantCommands(t, cmds, unstartedReport)
	d.wantReason("1", "acceptance", "crew stopped")
}

func TestStopWhileTheWorkspaceIsMadeStartsNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds)

	// The workspace names no log: no action will write it.
	cmds = d.ready("1")
	wantCommands(t, cmds, core.ReportFailure{Report: crew.FailureReport{
		IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Route: crew.FailedRoute,
		Failures: []crew.ActionFailure{{Action: "acceptance", Verdict: crew.Failed, Workspace: "issue-1-implement"}},
	}})
	d.wantReason("1", "acceptance", "crew stopped")
}

func TestStopWhileASessionStartsStopsItOnceItStarted(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	d.ready("1") // its StartSession is in flight

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds)
	cmds, _ = d.send(core.SessionStarted{IssueID: issueID("1"), Action: "acceptance"})
	wantCommands(t, cmds, core.StopSession{IssueID: issueID("1"), Run: d.run(issueID("1")), Action: "acceptance"})

	wantCommands(t, d.ended("1", "acceptance", failed("stopped")), failureOf("1", "implement", "acceptance"))
	d.wantReason("1", "acceptance", "stopped")
}

func TestStopGivesEachOwedCallOneFinalTry(t *testing.T) {
	d := newDriver(t, draft(), 2)
	report := endedNeedingAttention(d)
	d.send(core.CallResult{ID: reportID(t, report, "1"), Result: core.ResultFailed, Reason: "timeout"})

	final, _ := d.send(core.StopRequested{})
	wantCommands(t, final, failureOf("1", "implement", "acceptance"))

	// The report's final try fails: it is given up, and the route goes on
	// with its move, whose first try after the stop fails too, then its
	// final one.
	moved, events := d.send(core.CallResult{ID: reportID(t, final, "1"), Result: core.ResultFailed, Reason: "still down"})
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallReport, IssueID: issueID("1"), IssueRef: "#1",
	}})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepGivenUp{Reason: "still down"}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})

	retry, _ := d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultFailed, Reason: "still down"})
	wantCommands(t, retry, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
	if d.m.Stopped() {
		t.Fatal("stopped before the move's final try")
	}
	cmds, events := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultFailed, Reason: "still down"})
	wantCommands(t, cmds)
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallMove, IssueID: issueID("1"), IssueRef: "#1", From: inProgress, To: needsAttention,
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

// Covers R53, KTD-S14: a stop stops the route's shell step that runs,
// records it stopped, skips the shell steps after it, and lets the route's
// final move land.
func TestAStopWhileARouteShellStepRunsStopsItAndSkipsTheShellStepsAfterIt(t *testing.T) {
	rules := shellRouted()
	d := newDriver(t, rules, 2)
	d.running(issue("1", 1, ready))
	notify := d.ended("1", "acceptance", failed("tests fail"))
	wantCommands(t, notify, d.stepShell(0, "notify", "./notify"))

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopStepShell{IssueID: issueID("1"), Run: d.run(issueID("1")), Step: 0})

	stopped := crew.ShellOutcome{Reason: crew.NewShellReason("crew stopped notify")}
	moved, events := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: stopped})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepStopped{Reason: stopped.Reason}))
	hasEvent(t, events, d.stepEnded("1", 1, crew.StepSkipped{}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})

	_, events = d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 2, crew.StepLanded{}))
	if !d.m.Stopped() {
		t.Fatal("not stopped once the route's final move landed")
	}
}

func TestAStopWhileAShellActionRunsStopsItAndEndsThroughFailed(t *testing.T) {
	rules := draft()
	rules[0].Actions = append(rules[0].Actions[:1], shellAction("judge", "./judge"))
	d := newDriver(t, rules, 2)
	d.running(issue("1", 1, ready))
	runShellOf(t, d.ended("1", "acceptance", succeeded))

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopShell{IssueID: issueID("1"), Run: d.run(issueID("1")), Action: "judge"})

	// Even a script that exited 0 as it was stopped counts as stopped.
	cmds, _ = d.send(core.ShellEnded{IssueID: issueID("1"), Action: "judge", Outcome: exited(0)})
	wantCommands(t, cmds, failureOf("1", "implement", "judge"))
	d.wantReason("1", "judge", "exited 0")
}
