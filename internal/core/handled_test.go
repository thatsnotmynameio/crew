package core_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// handled returns the model's handled entries.
func handled(d *driver) []core.HandledView { return d.m.View().Handled }

// onlyEntry returns the model's single handled entry.
func onlyEntry(t *testing.T, d *driver) core.HandledView {
	t.Helper()
	got := handled(d)
	if len(got) != 1 {
		t.Fatalf("handled: got %#v, want one entry", got)
	}
	return got[0]
}

// failure is the failure of action on issue key, as its report carries it.
func failure(key string, action crew.ActionName, reason string) crew.ActionFailure {
	name := "issue-" + key + "-" + string(action)
	return crew.ActionFailure{
		Action: action, Reason: reason, Workspace: crew.WorkspaceName(name), Log: ".crew/logs/" + name + ".log",
	}
}

func TestASucceededRuleIsHandledOnceItsVerdictMoveIsDone(t *testing.T) {
	d := newDriver(t, draft(), 2)
	i1 := issue("1", 1, ready)
	take, events := d.poll(i1)
	taken := d.now
	hasEvent(t, events, core.IssueTaken{At: taken, Issue: i1, Rule: "implement", From: ready, To: inProgress})
	d.settle(take)

	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	ended := d.now
	if got := handled(d); got != nil {
		t.Fatalf("handled while the verdict move is in flight: %#v", got)
	}

	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	want := core.HandledView{
		Issue: i1, Rule: "implement", To: readyToReview, Move: crew.MoveDone, Taken: taken, Ended: ended,
		Actions: []core.HandledAction{
			{Name: "acceptance", Spend: crew.Spend{Sessions: 1}},
			{Name: "development", Spend: crew.Spend{Sessions: 1}},
		},
	}
	got := onlyEntry(t, d)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entry:\n got %#v\nwant %#v", got, want)
	}
	if got.NeedsAttention() {
		t.Fatal("a succeeded rule needs attention")
	}
	if got.Duration() != ended.Sub(taken) {
		t.Fatalf("duration: got %v, want %v", got.Duration(), ended.Sub(taken))
	}
}

// Covers AE2.
func TestAFailedRuleIsHandledWithItsFailedActionsOnceItsReportSettles(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: failed("tests fail")})

	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	if got := handled(d); got != nil {
		t.Fatalf("handled while the failure report is in flight: %#v", got)
	}
	d.send(core.CallResult{ID: reportID(t, verdict, "1"), Result: core.ResultDone})

	got := onlyEntry(t, d)
	if got.To != needsAttention || got.Move != crew.MoveDone {
		t.Fatalf("entry: got to %q, move %v; want needs attention, done", got.To, got.Move)
	}
	if want := []crew.ActionFailure{failure("1", "development", "tests fail")}; !reflect.DeepEqual(got.Failures, want) {
		t.Fatalf("failures:\n got %#v\nwant %#v", got.Failures, want)
	}
	if !got.NeedsAttention() {
		t.Fatal("a failed rule does not need attention")
	}
}

// Covers AE3.
func TestASucceededRuleWhoseVerdictMoveIsGivenUpNeedsAttention(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})

	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultMovedMeanwhile, Reason: "issue closed"})

	got := onlyEntry(t, d)
	if got.To != readyToReview || got.Move != crew.MoveDropped || got.DropReason != "issue closed" {
		t.Fatalf("entry: got to %q, move %v, reason %q; want ready to review, dropped, issue closed",
			got.To, got.Move, got.DropReason)
	}
	if got.Failures != nil {
		t.Fatalf("failures of a succeeded rule: %#v", got.Failures)
	}
	if !got.NeedsAttention() {
		t.Fatal("a given-up verdict move does not need attention")
	}
}

func TestAFailedRuleWhoseReportIsRefusedIsHandledWithTheMoveDone(t *testing.T) {
	d := newDriver(t, draft(), 2)
	verdict := judgedNeedingAttention(d)
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	d.send(core.CallResult{ID: reportID(t, verdict, "1"), Result: core.ResultRefused, Reason: "nope"})

	got := onlyEntry(t, d)
	if got.Move != crew.MoveDone || len(got.Failures) != 1 || !got.NeedsAttention() {
		t.Fatalf("entry: got %#v, want the move done and one failure needing attention", got)
	}
}

func TestAFailedRuleWhoseMoveIsRefusedKeepsItsFailuresAndTheGivenUpMove(t *testing.T) {
	d := newDriver(t, draft(), 2)
	verdict := judgedNeedingAttention(d)
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultRefused, Reason: "label missing"})
	d.send(core.CallResult{ID: reportID(t, verdict, "1"), Result: core.ResultDone})

	got := onlyEntry(t, d)
	if got.Move != crew.MoveDropped || got.DropReason != "label missing" {
		t.Fatalf("entry: got move %v, reason %q; want dropped, label missing", got.Move, got.DropReason)
	}
	if want := []crew.ActionFailure{failure("1", "acceptance", "broke")}; !reflect.DeepEqual(got.Failures, want) {
		t.Fatalf("failures:\n got %#v\nwant %#v", got.Failures, want)
	}
}

// Covers AE1.
func TestAnIssueTakenAgainKeepsItsEntryMarkedWithTheRuleHoldingIt(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	d.settle(verdict)
	if got := onlyEntry(t, d); got.Rule != "implement" || got.HeldBy != "" {
		t.Fatalf("first entry: got rule %q, held by %q; want implement, held by none", got.Rule, got.HeldBy)
	}

	take, _ := d.poll(issue("1", 1, readyToReview))
	taken := d.now
	if got := onlyEntry(t, d); got.Rule != "implement" || got.HeldBy != "review" {
		t.Fatalf("entry while #1 is held again: got rule %q, held by %q; want implement, held by review",
			got.Rule, got.HeldBy)
	}
	d.settle(take)
	verdict, _ = d.send(core.SessionEnded{IssueID: issueID("1"), Action: "custom_review",
		Outcome: failed("changes requested")})
	ended := d.now
	d.settle(verdict)

	got := onlyEntry(t, d)
	if got.Rule != "review" || got.To != needsAttention || got.Taken != taken || got.Ended != ended || got.HeldBy != "" {
		t.Fatalf("entry after review: got %#v, want review, needs attention, taken %v, ended %v, held by none",
			got, taken, ended)
	}
	want := []crew.ActionFailure{failure("1", "custom_review", "changes requested")}
	if !reflect.DeepEqual(got.Failures, want) {
		t.Fatalf("failures:\n got %#v\nwant %#v", got.Failures, want)
	}
}

func TestATakeGivenUpOnAHandledIssueKeepsItsEarlierEntryNoLongerHeld(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	d.settle(verdict)
	before := onlyEntry(t, d)

	take, _ := d.poll(issue("1", 1, readyToReview))
	if got := onlyEntry(t, d); got.HeldBy != "review" {
		t.Fatalf("entry while the take is in flight: got held by %q, want review", got.HeldBy)
	}
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultRefused, Reason: "nope"})

	if got := onlyEntry(t, d); !reflect.DeepEqual(got, before) {
		t.Fatalf("entry after the given-up take:\n got %#v\nwant %#v", got, before)
	}
}

func TestATakeGivenUpOnANewIssueIsNotHandled(t *testing.T) {
	d := newDriver(t, draft(), 1)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultMovedMeanwhile, Reason: "closed"})
	if got := handled(d); got != nil {
		t.Fatalf("handled after a given-up take: %#v", got)
	}
}

func TestAnOwedVerdictMoveIsHandledWhenItsRetryLandsWithTheRulesOwnDuration(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	ended := d.now

	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	if got := handled(d); got != nil {
		t.Fatalf("handled while the verdict move is owed: %#v", got)
	}
	d.now = d.now.Add(time.Hour)
	retry, _ := d.send(core.Tick{})
	d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})

	if got := onlyEntry(t, d); got.Ended != ended || got.Move != crew.MoveDone {
		t.Fatalf("entry: got ended %v, move %v; want ended %v, done", got.Ended, got.Move, ended)
	}
}

func TestAnActionEndedByAStopIsHandledAsAFailure(t *testing.T) {
	d := newDriver(t, draft(), 1)
	d.running(issue("1", 1, ready))
	d.send(core.StopRequested{})
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: failed("crew stopped")})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: failed("crew stopped")})
	d.settle(verdict)

	got := onlyEntry(t, d)
	if got.To != needsAttention || len(got.Failures) != 2 || !got.NeedsAttention() {
		t.Fatalf("entry: got %#v, want needs attention with two failures", got)
	}
}

func TestEntriesAreInTheOrderTheirIssuesWereReleased(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	for _, key := range []string{"2", "1"} {
		d.send(core.SessionEnded{IssueID: issueID(key), Action: "acceptance", Outcome: succeeded})
		verdict, _ := d.send(core.SessionEnded{IssueID: issueID(key), Action: "development", Outcome: succeeded})
		d.settle(verdict)
	}
	entries := handled(d)
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Issue.ID.Key)
	}
	if !reflect.DeepEqual(got, []string{"2", "1"}) {
		t.Fatalf("handled order: got %v, want [2 1]", got)
	}
}

// Covers AE6.
func TestANewModelHandledNothing(t *testing.T) {
	if got := core.New(draft(), 2).View().Handled; got != nil {
		t.Fatalf("handled of a new model: %#v", got)
	}
}

func TestAViewsHandledEntriesShareNoMemoryWithTheModel(t *testing.T) {
	d := newDriver(t, draft(), 2)
	verdict := judgedNeedingAttention(d)
	d.settle(verdict)

	first := d.m.View()
	first.Handled[0].Failures[0].Reason = "changed"
	first.Handled[0].Issue.States[0] = "changed"
	if got := onlyEntry(t, d); got.Failures[0].Reason != "broke" || got.Issue.States[0] != ready {
		t.Fatalf("entry after changing a view: %#v", got)
	}
}

// reviewed runs #1 through implement, then through review with outcome, and
// settles every move.
func reviewed(d *driver, outcome crew.Outcome) {
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	d.settle(verdict)
	d.running(issue("1", 1, readyToReview))
	verdict, _ = d.send(core.SessionEnded{IssueID: issueID("1"), Action: "custom_review", Outcome: outcome})
	d.settle(verdict)
}

// A rule with actions replaces the earlier entry even when both ended well:
// only a rule without actions keeps it (KTD6).
func TestARuleWithActionsThatSucceedsReplacesAnEarlierEntryThatEndedWell(t *testing.T) {
	d := newDriver(t, draft(), 2)
	reviewed(d, succeeded)

	if got := onlyEntry(t, d); got.Rule != "review" || got.To != readyToMerge || got.Gone {
		t.Fatalf("entry after review: got %#v, want review's, not gone", got)
	}
}
