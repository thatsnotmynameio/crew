package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The labels of triage and of promote triage, the rule without actions that
// hands triage's issues on (AE2).
const (
	triageReady      crew.State = "crew:triage:ready"
	triageRunning    crew.State = "crew:triage:running"
	triageDone       crew.State = "crew:triage:done"
	triageFailed     crew.State = "crew:triage:failed"
	triagePromoting  crew.State = "crew:triage:promoting"
	developmentReady crew.State = "crew:development:ready"
)

// promoted is triage, then promote triage, a rule without actions.
func promoted() []crew.Rule {
	return []crew.Rule{
		{
			Name:    "triage",
			Labels:  crew.Labels{Ready: triageReady, Running: triageRunning, Success: triageDone, Failure: triageFailed},
			Actions: []crew.Action{{Name: "triage", Prompt: parsedPrompt("triage", "Triage {{.Issue.Ref}}")}},
		},
		{
			Name:   "promote triage",
			Labels: crew.Labels{Ready: triageDone, Running: triagePromoting, Success: developmentReady},
		},
	}
}

// promoteMove is #1's move to promote triage's success.
func promoteMove() core.Move {
	return core.Move{IssueID: issueID("1"), From: triagePromoting, To: developmentReady}
}

// promoteMoved is the event of #1's move to promote triage's success, in
// d's run of #1, at d.now.
func promoteMoved(d *driver) crew.VerdictMoved {
	return crew.VerdictMoved{EventHead: d.runHead("1"), From: triagePromoting, To: developmentReady}
}

// reportOf is #1's pull request report of its move to state, with no end.
func reportOf(state crew.State) crew.PullRequestReportData {
	return crew.PullRequestReportData{IssueID: issueID("1"), IssueRef: "#1", State: state}
}

// triaged runs #1 through triage with outcome and settles every call.
func triaged(d *driver, outcome crew.Outcome) {
	d.running(issue("1", 1, triageReady))
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "triage", Outcome: outcome})
	d.settle(verdict)
}

// takePromoted polls #1 in triage's success and returns its take move.
func takePromoted(d *driver) []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(issue("1", 1, triageDone))
	wantCommands(d.t, unrecorded(cmds), core.Move{IssueID: issueID("1"), From: triageDone, To: triagePromoting})
	return cmds
}

// Covers AE2.
func TestAE2ARuleWithoutActionsMovesTheLabelWithoutASessionAndKeepsTriagesEntry(t *testing.T) {
	d := newDriver(t, promoted(), 2)
	triaged(d, succeeded)

	take := takePromoted(d)
	verdict, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantCommands(t, verdict, promoteMove())

	_, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	hasEvent(t, events, promoteMoved(d))
	wantHeld(t, d.m)
	if got := onlyEntry(t, d); got.Rule != "triage" || got.To != triageDone || !got.Gone {
		t.Fatalf("entry after promote triage: got %#v, want triage's, gone", got)
	}
}

func TestARuleWithoutActionsAndNoEarlierEntryLeavesItsOwn(t *testing.T) {
	d := newDriver(t, promoted(), 2)
	d.settle(takePromoted(d))

	got := onlyEntry(t, d)
	if got.Rule != "promote triage" || got.To != developmentReady || got.Gone || got.Actions != nil ||
		got.NeedsAttention() {
		t.Fatalf("entry: got %#v, want promote triage's, with no actions", got)
	}
}

func TestARuleWithoutActionsReplacesAnEarlierFailedEntry(t *testing.T) {
	d := newDriver(t, promoted(), 2)
	triaged(d, failed("broke"))
	if got := onlyEntry(t, d); !got.NeedsAttention() {
		t.Fatalf("triage's entry: got %#v, want it to need attention", got)
	}

	d.settle(takePromoted(d))

	if got := onlyEntry(t, d); got.Rule != "promote triage" || got.NeedsAttention() {
		t.Fatalf("entry: got %#v, want promote triage's, ended well", got)
	}
}

func TestARuleWithoutActionsTakenWhileCrewStopsMovesToSuccessAndStops(t *testing.T) {
	d := newDriver(t, promoted(), 2)
	take := takePromoted(d)
	if cmds, _ := d.send(core.StopRequested{}); len(cmds) != 0 {
		t.Fatalf("stop issued %#v while the take is in flight", cmds)
	}

	verdict, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantCommands(t, verdict, promoteMove())
	if d.m.Stopped() {
		t.Fatal("stopped while the verdict move is in flight")
	}

	_, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
	wantHeld(t, d.m)
}

func TestTheRunTimeLimitWithOnlyARuleWithoutActionsHeldStopsAfterItsMove(t *testing.T) {
	d := newDriver(t, promoted(), 2)
	take := takePromoted(d)
	d.send(core.TimeUp{Limit: limit})

	verdict, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantCommands(t, verdict, promoteMove())
	if d.m.Stopped() {
		t.Fatal("stopped while the verdict move is in flight")
	}

	_, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	wantEvents(t, events, promoteMoved(d), core.Stopped{At: d.now})
}

func TestARuleWithoutActionsHoldsASlotOfItsQueue(t *testing.T) {
	rules := promoted()
	rules[1].Queue = crew.Queue{Name: "clerk", Slots: 1}
	d := newDriver(t, rules[1:], 2)

	take, _ := d.poll(issue("1", 1, triageDone), issue("2", 2, triageDone))
	wantCommands(t, take, core.Move{IssueID: issueID("1"), From: triageDone, To: triagePromoting})
	verdict, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	if _, events := d.send(core.Tick{}); len(events) != 1 {
		t.Fatalf("tick while #1 holds the clerk slot: %#v, want the listing skipped", events)
	} else if _, ok := events[0].(core.PollSkipped); !ok {
		t.Fatalf("tick while #1 holds the clerk slot: %#v, want the listing skipped", events)
	}

	listing, _ := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	wantCommands(t, listing, core.ListIssues{States: []crew.State{triageDone, triagePromoting}})
	cmds, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("2", 2, triageDone)}})
	wantCommands(t, cmds, core.Move{IssueID: issueID("2"), From: triageDone, To: triagePromoting})
}

func TestARuleWithoutActionsMirrorsItsLabelsOnPullRequestsWithoutAStopComment(t *testing.T) {
	d := newPullRequestDriver(t, promoted())
	landed, _ := d.send(core.CallResult{ID: moveID(t, takePromoted(d), "1"), Result: core.ResultDone})
	wantReport(t, pullRequestReportOf(t, landed), reportOf(triagePromoting))
	d.answerPullRequests("1", core.ResultDone)

	cmds, _ := d.send(core.CallResult{ID: moveID(t, landed, "1"), Result: core.ResultDone})

	wantReport(t, pullRequestReportOf(t, cmds), reportOf(developmentReady))
}

func TestARuleWithoutActionsWritesItsStatusWithNoActionLines(t *testing.T) {
	d := newStatusDriver(t, promoted(), 2)

	landed, _ := d.send(core.CallResult{ID: moveID(t, takePromoted(d), "1"), Result: core.ResultDone})

	got := statusOf(t, landed, "1")
	if got.Progress() != (crew.StatusEnded{To: developmentReady, Move: crew.MovePending}) ||
		got.Rule() != "promote triage" || len(got.Actions()) != 0 {
		t.Fatalf("status: got %#v, want promote triage's ended status, moving to development, with no actions", got)
	}
}

// Its run events are journaled, but none is an action's start or end, so
// none whose append fails says so.
func TestARuleWithoutActionsReportsNoRecordNotWritten(t *testing.T) {
	d := &driver{t: t, m: core.New(promoted(), 2, core.Journaling(nil)), now: t0}
	take := takePromoted(d)
	verdict, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	wantHeld(t, d.m)

	if len(d.recorded) == 0 {
		t.Fatal("no run event journaled")
	}
	for _, e := range d.recorded {
		if _, events := d.send(core.RecordFailed{Event: e, Reason: "disk full"}); len(events) != 0 {
			t.Errorf("%T not written: events %#v, want none", e, events)
		}
	}
}
