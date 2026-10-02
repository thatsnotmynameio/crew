package core_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// newStatusDriver is newDriver with status reporting on.
func newStatusDriver(t *testing.T, workflow []crew.Stage, maxParallel int) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(workflow, maxParallel, core.ReportingStatus()), now: t0}
}

// statuses returns the statuses cmds ask to report, in order.
func statuses(cmds []core.Command) []crew.Status {
	var out []crew.Status
	for _, c := range cmds {
		if r, ok := c.(core.ReportStatus); ok {
			out = append(out, r.Status)
		}
	}
	return out
}

// statusOf returns the one status cmds ask to report for key.
func statusOf(t *testing.T, cmds []core.Command, key string) crew.Status {
	t.Helper()
	var found []crew.Status
	for _, s := range statuses(cmds) {
		if s.IssueKey == key {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one status of %s in %#v, got %d", key, cmds, len(found))
	}
	return found[0]
}

// noStatusOf fails when cmds ask to report a status for key.
func noStatusOf(t *testing.T, cmds []core.Command, key string) {
	t.Helper()
	for _, s := range statuses(cmds) {
		if s.IssueKey == key {
			t.Fatalf("unexpected status of %s: %#v", key, s)
		}
	}
}

// wrote answers the status write of key with result.
func (d *driver) wrote(key string, result core.Result) ([]core.Command, []core.Event) {
	return d.send(core.StatusResult{IssueKey: key, Result: result, Reason: result.String()})
}

// started returns when the named action of key started, from the view.
func started(t *testing.T, m *core.Model, key, action string) time.Time {
	t.Helper()
	for _, iv := range m.View().Issues {
		for _, a := range iv.Actions {
			if iv.Issue.Key == key && a.Name == action {
				return a.Started
			}
		}
	}
	t.Fatalf("no action %s of %s", action, key)
	return time.Time{}
}

func wantStatus(t *testing.T, got, want crew.Status) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status:\n got %#v\nwant %#v", got, want)
	}
}

// take polls issue alone, lands its take and answers the first status write.
// It returns the commands the landed take issued.
func (d *driver) take(it crew.Issue) []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(it)
	landed, _ := d.send(core.CallResult{ID: moveID(d.t, cmds, it.Key), Result: core.ResultDone})
	d.wrote(it.Key, core.ResultDone)
	return landed
}

// runAll creates every workspace and starts every session landed asked for.
func (d *driver) runAll(landed []core.Command) {
	d.t.Helper()
	for _, c := range landed {
		if w, ok := c.(core.CreateWorkspace); ok {
			cmds, _ := d.send(space(w.Issue.Key, w.Action))
			for _, s := range cmds {
				if s, ok := s.(core.StartSession); ok {
					d.send(core.SessionStarted{IssueKey: s.IssueKey, Action: s.Action})
				}
			}
		}
	}
}

func TestAE1QueuedIssueGetsOneStatusWhileItWaitsForAFreeSlot(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	i1, i2, i74 := issue("1", 1, ready), issue("2", 2, ready), issue("74", 3, ready)

	cmds, _ := d.poll(i1, i2, i74)
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusQueued, Slots: 2, Updated: d.now,
	})
	noStatusOf(t, cmds, "1")
	noStatusOf(t, cmds, "2")
	d.wrote("74", core.ResultDone)

	cmds, _ = d.poll(i1, i2, i74)
	noStatusOf(t, cmds, "74")
}

func TestQueuedIssueUnderAnotherStageGetsANewStatus(t *testing.T) {
	d := newStatusDriver(t, draft(), 1)
	d.running(issue("1", 1, ready))

	cmds, _ := d.poll(issue("74", 2, ready))
	if got := statusOf(t, cmds, "74"); got.Stage != "implement" {
		t.Fatalf("queued for %q, want implement", got.Stage)
	}
	d.wrote("74", core.ResultDone)

	cmds, _ = d.poll(issue("74", 2, readyToReview))
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueKey: "74", IssueRef: "#74", Stage: "review", Kind: crew.StatusQueued, Slots: 1, Updated: d.now,
	})
}

func TestTakenIssueGetsItsFirstStatusOnceItsTakeLands(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)

	cmds, _ := d.poll(issue("1", 1, ready))
	noStatusOf(t, cmds, "1")

	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "1"), crew.Status{
		IssueKey: "1", IssueRef: "#1", Stage: "implement", Kind: crew.StatusRunning, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionRunning},
			{Name: "development", State: crew.ActionRunning},
		},
	})
}

func TestAE2RunningStatusShowsEachSessionsStartAndLastWords(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))

	cmds, _ := d.send(core.Tick{Said: []core.Said{
		{IssueKey: "74", Action: "development", Text: "U1 committed: 168 tests pass. Starting U2."},
		{IssueKey: "99", Action: "development", Text: "not held"},
	}})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusRunning, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionRunning, Started: started(t, d.m, "74", "acceptance")},
			{Name: "development", State: crew.ActionRunning, Started: started(t, d.m, "74", "development"),
				Said: "U1 committed: 168 tests pass. Starting U2."},
		},
	})
	d.wrote("74", core.ResultDone)

	// A running issue is reported at every tick, changed or not (R6).
	cmds, _ = d.send(core.Tick{})
	if got := statusOf(t, cmds, "74"); got.Updated != d.now || got.Actions[1].Said != "U1 committed: 168 tests pass. Starting U2." {
		t.Fatalf("second tick's status: %#v", got)
	}
}

func TestAE3EndedStatusSaysWhereTheIssueGoesThenThatItMoved(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	devStarted := started(t, d.m, "74", "development")

	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusRunning, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionSucceeded},
			{Name: "development", State: crew.ActionRunning, Started: devStarted},
		},
	})
	d.wrote("74", core.ResultDone)

	verdict, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: failed("tests fail")})
	final := []crew.ActionStatus{
		{Name: "acceptance", State: crew.ActionSucceeded},
		{Name: "development", State: crew.ActionFailed},
	}
	wantStatus(t, statusOf(t, verdict, "74"), crew.Status{
		IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded, Updated: d.now,
		Actions: final, To: needsAttention, Move: crew.MovePending,
	})
	reportID(t, verdict, "74") // the failure report is still its own command (R2)
	d.wrote("74", core.ResultDone)

	cmds, _ = d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded, Updated: d.now,
		Actions: final, To: needsAttention, Move: crew.MoveDone,
	})
}

func TestEndedStatusSaysWhenTheMoveWasDropped(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
	d.wrote("74", core.ResultDone)

	cmds, _ := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultMovedMeanwhile})
	got := statusOf(t, cmds, "74")
	if got.Kind != crew.StatusEnded || got.To != readyToReview || got.Move != crew.MoveDropped {
		t.Fatalf("status after a dropped move: %#v", got)
	}
}

func TestAE4StopWhileASessionRunsEndsWithTheMoveOnTheComment(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: succeeded})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopSession{IssueKey: "74", Action: "development"})
	if cmds, _ := d.send(core.Tick{}); len(cmds) != 0 {
		t.Fatalf("tick after stop issued %#v", cmds)
	}

	verdict, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: failed("stopped")})
	if got := statusOf(t, verdict, "74"); got.Move != crew.MovePending || got.To != needsAttention {
		t.Fatalf("status at the verdict: %#v", got)
	}
	reportID(t, verdict, "74")
	d.send(core.CallResult{ID: reportID(t, verdict, "74"), Result: core.ResultDone})
	d.wrote("74", core.ResultDone)

	cmds, events := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionSucceeded},
			{Name: "development", State: crew.ActionFailed},
		},
		To: needsAttention, Move: crew.MoveDone,
	})
	if d.m.Stopped() || containsStopped(events) {
		t.Fatal("stopped with the last status write in flight")
	}

	_, events = d.wrote("74", core.ResultDone)
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the last status write returned")
	}
}

func containsStopped(events []core.Event) bool {
	for _, e := range events {
		if _, ok := e.(core.Stopped); ok {
			return true
		}
	}
	return false
}

func TestOneStatusWriteInFlightPerIssueAndTheNewestWaits(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("74", 1, ready))
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
	statusOf(t, landed, "74") // in flight, unanswered

	cmds, _ = d.send(core.Tick{})
	noStatusOf(t, cmds, "74")
	d.send(core.IssuesListed{})
	d.send(core.Tick{})
	d.send(core.IssuesListed{})
	newest := d.now.Add(time.Second)
	cmds, _ = d.send(core.Tick{})
	noStatusOf(t, cmds, "74")
	d.send(core.IssuesListed{})

	cmds, _ = d.wrote("74", core.ResultDone)
	if got := statusOf(t, cmds, "74"); got.Updated != newest {
		t.Fatalf("sent the status of %v, want the newest, of %v", got.Updated, newest)
	}
	cmds, _ = d.wrote("74", core.ResultDone)
	noStatusOf(t, cmds, "74")
}

func TestFailedRunningWriteIsWrittenAgainAtTheNextTickAndReportedOnce(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("74", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})

	_, events := d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "timeout"})
	hasEvent(t, events, core.StatusFailed{At: d.now, IssueKey: "74", IssueRef: "#74", Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ = d.send(core.Tick{})
	statusOf(t, cmds, "74")
	_, events = d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "timeout"})
	for _, e := range events {
		if _, ok := e.(core.StatusFailed); ok {
			t.Fatalf("second failure in a row reported again: %#v", e)
		}
	}

	cmds, _ = d.send(core.Tick{})
	statusOf(t, cmds, "74")
	d.wrote("74", core.ResultDone)
	cmds, _ = d.send(core.Tick{})
	statusOf(t, cmds, "74")
	_, events = d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "down again"})
	hasEvent(t, events, core.StatusFailed{At: d.now, IssueKey: "74", IssueRef: "#74", Result: core.ResultFailed, Reason: "down again"})
}

func TestFailedQueuedWriteIsWrittenAgainAtTheNextPoll(t *testing.T) {
	d := newStatusDriver(t, draft(), 1)
	d.running(issue("1", 1, ready))
	cmds, _ := d.poll(issue("74", 2, ready))
	statusOf(t, cmds, "74")
	d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ = d.poll(issue("74", 2, ready))
	statusOf(t, cmds, "74")
}

// ended runs #74 to a verdict whose move lands, and returns the commands of
// the move's result, which carry the ended status.
func ended(d *driver) []core.Command {
	d.t.Helper()
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
	d.wrote("74", core.ResultDone)
	cmds, _ := d.send(core.CallResult{ID: moveID(d.t, verdict, "74"), Result: core.ResultDone})
	return cmds
}

func TestFailedEndedStatusIsRetriedAtEachTickUntilItLands(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	want := statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), want)
	d.send(core.IssuesListed{})
	d.wrote("74", core.ResultDone)

	cmds, _ = d.send(core.Tick{})
	noStatusOf(t, cmds, "74")
}

func TestStopGivesAnOwedEndedStatusOneFinalTry(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	want := statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "timeout"})
	if d.m.Stopped() {
		t.Fatal("stopped before a stop")
	}

	cmds, _ := d.send(core.StopRequested{})
	wantStatus(t, statusOf(t, cmds, "74"), want)
	if d.m.Stopped() {
		t.Fatal("stopped with the final try in flight")
	}

	cmds, events := d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "still down"})
	noStatusOf(t, cmds, "74")
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the final try failed")
	}
}

func TestEndedStatusFailingAfterStopGetsOneMoreTry(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.StopRequested{})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: failed("stopped")})
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: failed("stopped")})
	_ = verdict

	cmds, _ := d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "timeout"})
	statusOf(t, cmds, "74")
	cmds, _ = d.send(core.StatusResult{IssueKey: "74", Result: core.ResultFailed, Reason: "timeout"})
	noStatusOf(t, cmds, "74")
}

func TestRefusedEndedStatusIsDroppedAndNotWaitedFor(t *testing.T) {
	for _, result := range []core.Result{core.ResultRefused, core.ResultMovedMeanwhile} {
		t.Run(result.String(), func(t *testing.T) {
			d := newStatusDriver(t, draft(), 2)
			statusOf(t, ended(d), "74")
			_, events := d.send(core.StatusResult{IssueKey: "74", Result: result, Reason: "no"})
			hasEvent(t, events, core.StatusFailed{At: d.now, IssueKey: "74", IssueRef: "#74", Result: result, Reason: "no"})

			cmds, _ := d.send(core.Tick{})
			noStatusOf(t, cmds, "74")
			d.send(core.IssuesListed{})
			d.send(core.StopRequested{})
			if !d.m.Stopped() {
				t.Fatal("a stop waits for a dropped status")
			}
		})
	}
}

func TestOwedOrDroppedTakeReportsNoStatus(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	take, _ := d.poll(issue("74", 1, ready))
	cmds, _ := d.send(core.CallResult{ID: moveID(t, take, "74"), Result: core.ResultFailed, Reason: "timeout"})
	noStatusOf(t, cmds, "74")

	retry, _ := d.send(core.Tick{})
	noStatusOf(t, retry, "74")
	cmds, _ = d.send(core.CallResult{ID: moveID(t, retry, "74"), Result: core.ResultDone})
	statusOf(t, cmds, "74")

	d2 := newStatusDriver(t, draft(), 2)
	take, _ = d2.poll(issue("75", 1, ready))
	cmds, _ = d2.send(core.CallResult{ID: moveID(t, take, "75"), Result: core.ResultRefused})
	noStatusOf(t, cmds, "75")
}

func TestAE7IssueNoStageTakesOrInTwoStatesGetsNoStatus(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	for range 2 {
		cmds, _ := d.poll(issue("60", 1, inReview), issue("61", 2, ready, readyToReview))
		if got := statuses(cmds); len(got) != 0 {
			t.Fatalf("statuses for issues no stage takes: %#v", got)
		}
	}
}

func TestWithoutStatusReportingNoStatusIsReported(t *testing.T) {
	d := newDriver(t, draft(), 1)
	cmds, _ := d.poll(issue("1", 1, ready), issue("74", 2, ready))
	all := append([]core.Command{}, cmds...)
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	all = append(all, landed...)
	d.runAll(landed)
	tick, _ := d.send(core.Tick{Said: []core.Said{{IssueKey: "1", Action: "development", Text: "hi"}}})
	all = append(all, tick...)
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
	all = append(all, verdict...)
	if got := statuses(all); len(got) != 0 {
		t.Fatalf("statuses reported without status reporting: %#v", got)
	}
}
