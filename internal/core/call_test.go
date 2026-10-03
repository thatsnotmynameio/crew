package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestVerdictMoveThatFailsTransientlyIsOwedAndRetriedAtTheNextTick(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})

	cmds, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantCommands(t, cmds)
	owed := core.Call{Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview}
	hasEvent(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})
	if got := d.m.View().Owed; !reflect.DeepEqual(got, []core.Call{owed}) {
		t.Fatalf("owed: got %#v, want %#v", got, []core.Call{owed})
	}
	if c := claimOf(t, d.m, "1"); c != core.ClaimOwed {
		t.Fatalf("claim of #1: got %v, want owed", c)
	}

	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry,
		core.ListIssues{States: []crew.State{ready, readyToReview}},
		core.Move{IssueKey: "1", From: inProgress, To: readyToReview},
	)

	// The retry is in flight: the next tick does not issue it again.
	d.send(core.IssuesListed{})
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})

	_, events = d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview})
	wantHeld(t, d.m)
	if got := d.m.View().Owed; got != nil {
		t.Fatalf("owed after the retry succeeded: %#v", got)
	}
}

func TestVerdictCallMovedMeanwhileOrRefusedIsDroppedAndReported(t *testing.T) {
	for _, result := range []core.Result{core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newDriver(t, draft(), 2)
			verdict := judgedNeedingAttention(d)

			_, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: result, Reason: "nope"})
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: inProgress, To: needsAttention,
			}})
			_, events = d.send(core.CallResult{ID: reportID(t, verdict, "1"), Result: result, Reason: "nope"})
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallReport, IssueKey: "1", IssueRef: "#1",
			}})
			wantHeld(t, d.m)

			cmds, _ := d.send(core.Tick{})
			wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})
		})
	}
}

func TestTakeMovedMeanwhileOrRefusedReleasesTheIssue(t *testing.T) {
	for _, result := range []core.Result{core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))

			cmds, events := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: result, Reason: "nope"})
			wantCommands(t, cmds)
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress,
			}})
			wantHeld(t, d.m)

			cmds, _ = d.poll(issue("2", 2, ready))
			wantCommands(t, cmds, core.Move{IssueKey: "2", From: ready, To: inProgress})
		})
	}
}

// A take that failed transiently may have landed, so the issue stays held
// and the take is owed: the retry, which the tracker makes idempotent, either
// moves it or finds it already moved, and the stage proceeds (KTD8).
func TestTakeThatFailsTransientlyIsOwedAndRetriedAtTheNextTick(t *testing.T) {
	d := newDriver(t, draft(), 1)
	i1 := issue("1", 1, ready)
	take, _ := d.poll(i1)

	cmds, events := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantCommands(t, cmds)
	owed := core.Call{Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress}
	hasEvent(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})
	want := core.View{
		Issues: []core.IssueView{{
			Issue: i1, Stage: "implement", Claim: core.ClaimOwed,
			Actions: []core.ActionView{
				{Name: "acceptance", Phase: core.PhaseWaiting},
				{Name: "development", Phase: core.PhaseWaiting},
			},
		}},
		Owed: []core.Call{owed},
	}
	if v := d.m.View(); !reflect.DeepEqual(v, want) {
		t.Fatalf("view:\n got %#v\nwant %#v", v, want)
	}

	// The next tick retries the take. #1 holds the only slot, so the tick
	// does not list (R7).
	retry, events := d.send(core.Tick{})
	wantCommands(t, retry, core.Move{IssueKey: "1", From: ready, To: inProgress})
	hasEvent(t, events, core.PollSkipped{At: d.now, Busy: 1, Slots: 1})

	cmds, events = d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.CreateWorkspace{Issue: i1, Action: "acceptance"},
		core.CreateWorkspace{Issue: i1, Action: "development"},
	)
	hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress})
	if c := claimOf(t, d.m, "1"); c != core.ClaimRunning {
		t.Fatalf("claim of #1: got %v, want running", c)
	}
	if got := d.m.View().Owed; got != nil {
		t.Fatalf("owed after the retry succeeded: %#v", got)
	}
}

func TestOwedTakeRetryMovedMeanwhileOrRefusedReleasesTheIssue(t *testing.T) {
	for _, result := range []core.Result{core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))
			d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
			retry, _ := d.send(core.Tick{})

			// The tick skipped its listing, so the freed slot lists at once (R4).
			cmds, events := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: result, Reason: "nope"})
			wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress,
			}})
			wantHeld(t, d.m)
		})
	}
}
