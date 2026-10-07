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

// Covers R52: the action that runs finishes, the next does not start, and
// the run ends through failed, naming the action time-up kept from
// starting.
func TestAE3TimeUpLetsTheRunningActionFinishAndStartsNoOther(t *testing.T) {
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

	report := d.ended("42", "acceptance", succeeded)
	wantCommands(t, report, failureOf("42", "implement", "development"))
	d.wantReason("42", "development", "crew's run time was up")
	if d.m.Stopped() {
		t.Fatal("stopped while #42's route runs")
	}

	moved, _ := d.send(core.CallResult{ID: reportID(t, report, "42"), Result: core.ResultDone})
	_, events = d.send(core.CallResult{ID: moveID(t, moved, "42"), Result: core.ResultDone})
	wantEvents(t, events, d.stepEnded("42", 1, crew.StepLanded{}), core.Stopped{At: d.now})
	if d.m.View().Stopping {
		t.Fatal("the view says a stop was requested; none was")
	}
}

func TestAE3TheLastActionThatPassesWhileWindingDownEndsThroughPassed(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.settle(d.ended("42", "acceptance", succeeded))
	d.send(core.TimeUp{Limit: limit})

	ending := d.ended("42", "development", succeeded)
	wantCommands(t, ending, core.Move{IssueID: issueID("42"), From: inProgress, To: readyToReview})
	_, events := d.send(core.CallResult{ID: moveID(t, ending, "42"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestAE3AnIssueThatFailsWhileWindingDownEndsThroughFailedAsUsual(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	report := d.ended("42", "acceptance", failed("broke"))
	wantCommands(t, report, failureOf("42", "implement", "acceptance"))
	d.wantReason("42", "acceptance", "broke")

	moved, _ := d.send(core.CallResult{ID: reportID(t, report, "42"), Result: core.ResultDone})
	_, events := d.send(core.CallResult{ID: moveID(t, moved, "42"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

// timeUpReport is the failure report of #42 whose first action time-up kept
// from starting, before its run had a workspace.
var timeUpReport = core.ReportFailure{Report: crew.FailureReport{
	IssueID: issueID("42"), IssueRef: "#42", Rule: "implement", Route: crew.FailedRoute,
	Failures: []crew.ActionFailure{{Action: "acceptance", Verdict: crew.Failed}},
}}

func TestATakeThatLandsAfterTimeUpStartsNoActionAndEndsThroughFailed(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	cmds, _ := d.send(core.CallResult{ID: moveID(t, take, "42"), Result: core.ResultDone})
	wantCommands(t, cmds, timeUpReport)
	d.wantReason("42", "acceptance", "crew's run time was up")
}

func TestAnOwedTakeWhenTimeIsUpIsRetriedAtTicksAndThenEndsThroughFailed(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("42", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "42"), Result: core.ResultFailed, Reason: "timeout"})

	_, events := d.send(core.TimeUp{Limit: limit})
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit})

	retry, events := d.send(core.Tick{})
	wantCommands(t, retry, core.Move{IssueID: issueID("42"), From: ready, To: inProgress})
	if len(events) != 0 || d.m.Stopped() {
		t.Fatalf("a tick with an owed take: events %#v, stopped %v", events, d.m.Stopped())
	}

	cmds, _ := d.send(core.CallResult{ID: moveID(t, retry, "42"), Result: core.ResultDone})
	wantCommands(t, cmds, timeUpReport)
}

func TestWhileWindingDownOwedCallsAreRetriedAtTicksThenGetAFinalTry(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	ending := d.endActions("1")
	d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultFailed, Reason: "timeout"})
	d.send(core.TimeUp{Limit: limit})

	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry, core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview})
	owed := core.Call{Kind: core.CallMove, IssueID: issueID("1"), IssueRef: "#1", From: inProgress, To: readyToReview}
	_, events := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})

	// #2's running action ends: no run holds crew, so the owed move gets
	// its final try.
	cmds := d.ended("2", "acceptance", succeeded)
	wantCommands(t, cmds, failureOf("2", "implement", "development"),
		core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview})

	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallDropped{At: d.now, Call: owed, Result: core.ResultFailed, Reason: "timeout"},
		d.stepEnded("1", 0, crew.StepGivenUp{Reason: "timeout"}))
	moved, _ := d.send(core.CallResult{ID: reportID(t, cmds, "2"), Result: core.ResultDone})
	_, events = d.send(core.CallResult{ID: moveID(t, moved, "2"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

// Covers R52, KTD12: time-up never cuts a route's shell step short; crew
// stops once no shell step is left.
func TestTimeUpWaitsForARoutesShellStepsBeforeItStops(t *testing.T) {
	d := newDriver(t, shellRouted(), 2)
	d.running(issue("1", 1, ready))
	d.ended("1", "acceptance", failed("tests fail"))

	cmds, _ := d.send(core.TimeUp{Limit: limit})
	wantCommands(t, cmds)
	cmds, _ = d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: exited(0)})
	wantCommands(t, cmds, d.stepShell(1, "cleanup", "./cleanup"))
	if d.m.Stopped() {
		t.Fatal("stopped while a shell step runs")
	}

	moved, events := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 1, Outcome: exited(0)})
	hasEvent(t, events, d.stepEnded("1", 1, crew.StepRan{Reason: exited(0).Reason}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
	_, events = d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

// Covers KTD12: a route whose comment is owed does not hold crew past its
// run time: the stop gives the comment its final try and skips the shell
// step after it, and the final move still lands.
func TestTimeUpDoesNotWaitForARouteWhoseTrackerStepIsOwed(t *testing.T) {
	rules := draft()
	rules[0].Routes[1].Steps = []crew.Step{
		crew.CommentStep{}, crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}},
		crew.MoveStep{To: needsAttention},
	}
	d := newDriver(t, rules, 2)
	d.running(issue("1", 1, ready))
	comment := d.ended("1", "acceptance", failed("tests fail"))
	wantCommands(t, comment, core.Comment{IssueID: issueID("1")})
	d.send(core.TimeUp{Limit: limit})

	final, _ := d.send(core.CallResult{ID: commentID(t, comment), Result: core.ResultFailed, Reason: "timeout"})
	wantCommands(t, final, core.Comment{IssueID: issueID("1")})

	moved, events := d.send(core.CallResult{ID: commentID(t, final), Result: core.ResultFailed, Reason: "still down"})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepGivenUp{Reason: "still down"}))
	hasEvent(t, events, d.stepEnded("1", 1, crew.StepSkipped{}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
	_, events = d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestAE4AStopWhileWindingDownStopsTheRunningSessionAsUsual(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopSession{IssueID: issueID("42"), Run: d.run(issueID("42")), Action: "acceptance"})
	if !d.m.View().Stopping {
		t.Fatal("the view does not say a stop was requested")
	}

	wantCommands(t, d.ended("42", "acceptance", failed("stopped")), failureOf("42", "implement", "acceptance"))
	d.wantReason("42", "acceptance", "stopped")
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
