package core_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// newStatusDriver is newDriver with status reporting on.
func newStatusDriver(t *testing.T, rules []crew.Rule, maxParallel int) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(rules, maxParallel, core.ReportingStatus()), now: t0}
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
		if s.IssueID.Key == key {
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
		if s.IssueID.Key == key {
			t.Fatalf("unexpected status of %s: %#v", key, s)
		}
	}
}

// wrote answers the status write of key as done.
func (d *driver) wrote(key string) ([]core.Command, []core.Event) {
	return d.send(core.StatusResult{IssueID: issueID(key), Result: core.ResultDone, Reason: core.ResultDone.String()})
}

// started returns when the named action of issue 74, the issue these tests
// run, started, from the view.
func started(t *testing.T, m *core.Model, action crew.ActionName) time.Time {
	t.Helper()
	const key = "74"
	for _, iv := range m.View().Issues {
		for _, a := range iv.Actions {
			if iv.Issue.ID.Key == key && a.Name == action {
				return a.Started
			}
		}
	}
	t.Fatalf("no action %s of %s", action, key)
	return time.Time{}
}

// wantStatus compares got with want. A want without a Run takes got's, which
// must not be empty: the run tests below compare runs.
func wantStatus(t *testing.T, got, want crew.Status) {
	t.Helper()
	if got.Run == "" {
		t.Fatalf("status without a run: %#v", got)
	}
	if want.Run == "" {
		want.Run = got.Run
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status:\n got %#v\nwant %#v", got, want)
	}
}

// take polls issue alone, lands its take and answers the first status write.
// It returns the commands the landed take issued.
func (d *driver) take(it crew.Issue) []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(it)
	landed, _ := d.send(core.CallResult{ID: moveID(d.t, cmds, it.ID.Key), Result: core.ResultDone})
	d.wrote(it.ID.Key)
	return landed
}

// runAll creates every workspace and starts every session landed asked for.
func (d *driver) runAll(landed []core.Command) {
	d.t.Helper()
	for _, c := range landed {
		if w, ok := c.(core.CreateWorkspace); ok {
			cmds, _ := d.send(space(w.Issue.ID.Key, w.Action))
			for _, s := range cmds {
				if s, ok := s.(core.StartSession); ok {
					d.send(core.SessionStarted{IssueID: s.IssueID, Action: s.Action})
				}
			}
		}
	}
}

// Covers AE5.
func TestAE5AListingReportsNothingForTheIssuesItLeaves(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("1", 1, ready)))
	i2, i3, i4 := issue("2", 2, ready), issue("3", 3, ready), issue("4", 4, readyToReview)
	blocked := issue("5", 5, ready)
	blocked.Blocked = true

	cmds, events := d.poll(i2, i3, i4, blocked)
	wantCommands(t, nonStatus(cmds), core.Move{IssueID: issueID("4"), From: readyToReview, To: inReview})
	for _, key := range []string{"2", "3", "5"} {
		noStatusOf(t, cmds, key)
	}
	wantEvents(t, events,
		core.IssueTaken{At: d.now, Issue: i4, Rule: "review", From: readyToReview, To: inReview},
		core.PollDone{At: d.now, Listed: 4, Taken: 1},
	)
}

// nonStatus returns cmds without their status reports.
func nonStatus(cmds []core.Command) []core.Command {
	var out []core.Command
	for _, c := range cmds {
		if _, ok := c.(core.ReportStatus); !ok {
			out = append(out, c)
		}
	}
	return out
}

func TestPrioritizedIssuesTakeTheSlotsAndALaterRuleIssueLeftGetsNoStatus(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	urgent, review, high := issue("1", 3, ready), issue("2", 1, readyToReview), issue("3", 2, ready)
	urgent.Priority, high.Priority = 1, 2

	cmds, _ := d.poll(review, high, urgent)
	wantHeld(t, d.m, "1", "3")
	noStatusOf(t, cmds, "2")
}

func TestTakenIssueGetsItsFirstStatusOnceItsTakeLands(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)

	cmds, _ := d.poll(issue("1", 1, ready))
	noStatusOf(t, cmds, "1")

	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "1"), crew.Status{
		IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Kind: crew.StatusRunning, Updated: d.now,
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
		{IssueID: issueID("74"), Action: "development", Text: "U1 committed: 168 tests pass. Starting U2."},
		{IssueID: issueID("99"), Action: "development", Text: "not held"},
	}})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Kind: crew.StatusRunning, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionRunning, Started: started(t, d.m, "acceptance")},
			{Name: "development", State: crew.ActionRunning, Started: started(t, d.m, "development"),
				Said: "U1 committed: 168 tests pass. Starting U2."},
		},
	})
	d.wrote("74")

	// A running issue is reported at every tick, changed or not (R6).
	cmds, _ = d.send(core.Tick{})
	got := statusOf(t, cmds, "74")
	if got.Updated != d.now || got.Actions[1].Said != "U1 committed: 168 tests pass. Starting U2." {
		t.Fatalf("second tick's status: %#v", got)
	}
}

func TestAE3EndedStatusSaysWhereTheIssueGoesThenThatItMoved(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	devStarted := started(t, d.m, "development")

	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Kind: crew.StatusRunning, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionSucceeded},
			{Name: "development", State: crew.ActionRunning, Started: devStarted},
		},
	})
	d.wrote("74")

	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("tests fail")})
	final := []crew.ActionStatus{
		{Name: "acceptance", State: crew.ActionSucceeded},
		{Name: "development", State: crew.ActionFailed, Cause: crew.CauseSession, Log: space("74", "development").Log},
	}
	wantStatus(t, statusOf(t, verdict, "74"), crew.Status{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Kind: crew.StatusEnded, Updated: d.now,
		Actions: final, To: needsAttention, Move: crew.MovePending,
	})
	reportID(t, verdict, "74") // the failure report is still its own command (R2)
	d.wrote("74")

	cmds, _ = d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Kind: crew.StatusEnded, Updated: d.now,
		Actions: final, To: needsAttention, Move: crew.MoveDone,
	})
}

func TestEndedStatusSaysWhenTheMoveWasDropped(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	d.wrote("74")

	cmds, _ := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultMovedMeanwhile})
	got := statusOf(t, cmds, "74")
	if got.Kind != crew.StatusEnded || got.To != readyToReview || got.Move != crew.MoveDropped {
		t.Fatalf("status after a dropped move: %#v", got)
	}
}

func TestAE4StopWhileASessionRunsEndsWithTheMoveOnTheComment(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopSession{IssueID: issueID("74"), Action: "development"})
	if cmds, _ := d.send(core.Tick{}); len(cmds) != 0 {
		t.Fatalf("tick after stop issued %#v", cmds)
	}

	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("stopped")})
	if got := statusOf(t, verdict, "74"); got.Move != crew.MovePending || got.To != needsAttention {
		t.Fatalf("status at the verdict: %#v", got)
	}
	reportID(t, verdict, "74")
	d.send(core.CallResult{ID: reportID(t, verdict, "74"), Result: core.ResultDone})
	d.wrote("74")

	cmds, events := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "74"), crew.Status{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Kind: crew.StatusEnded, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionSucceeded},
			{Name: "development", State: crew.ActionFailed, Cause: crew.CauseStopped, Log: space("74", "development").Log},
		},
		To: needsAttention, Move: crew.MoveDone,
	})
	if d.m.Stopped() || containsStopped(events) {
		t.Fatal("stopped with the last status write in flight")
	}

	_, events = d.wrote("74")
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

	cmds, _ = d.wrote("74")
	if got := statusOf(t, cmds, "74"); got.Updated != newest {
		t.Fatalf("sent the status of %v, want the newest, of %v", got.Updated, newest)
	}
	cmds, _ = d.wrote("74")
	noStatusOf(t, cmds, "74")
}

func TestFailedRunningWriteIsWrittenAgainAtTheNextTickAndReportedOnce(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("74", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})

	_, events := d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	hasEvent(t, events, core.StatusFailed{
		At: d.now, IssueID: issueID("74"), IssueRef: "#74", Result: core.ResultFailed, Reason: "timeout",
	})

	cmds, _ = d.send(core.Tick{})
	statusOf(t, cmds, "74")
	_, events = d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	for _, e := range events {
		if _, ok := e.(core.StatusFailed); ok {
			t.Fatalf("second failure in a row reported again: %#v", e)
		}
	}

	cmds, _ = d.send(core.Tick{})
	statusOf(t, cmds, "74")
	d.wrote("74")
	cmds, _ = d.send(core.Tick{})
	statusOf(t, cmds, "74")
	_, events = d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "down again"})
	hasEvent(t, events, core.StatusFailed{
		At: d.now, IssueID: issueID("74"), IssueRef: "#74", Result: core.ResultFailed, Reason: "down again",
	})
}

// ended runs #74 to a verdict whose move lands, and returns the commands of
// the move's result, which carry the ended status.
func ended(d *driver) []core.Command {
	d.t.Helper()
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	d.wrote("74")
	cmds, _ := d.send(core.CallResult{ID: moveID(d.t, verdict, "74"), Result: core.ResultDone})
	return cmds
}

func TestFailedEndedStatusIsRetriedAtEachTickUntilItLands(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	want := statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), want)
	d.send(core.IssuesListed{})
	d.wrote("74")

	cmds, _ = d.send(core.Tick{})
	noStatusOf(t, cmds, "74")
}

func TestStopGivesAnOwedEndedStatusOneFinalTry(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	want := statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	if d.m.Stopped() {
		t.Fatal("stopped before a stop")
	}

	cmds, _ := d.send(core.StopRequested{})
	wantStatus(t, statusOf(t, cmds, "74"), want)
	if d.m.Stopped() {
		t.Fatal("stopped with the final try in flight")
	}

	cmds, events := d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "still down"})
	noStatusOf(t, cmds, "74")
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the final try failed")
	}
}

func TestEndedStatusFailingAfterStopGetsOneMoreTry(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	d.send(core.StopRequested{})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("stopped")})
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: failed("stopped")})
	_ = verdict

	cmds, _ := d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	statusOf(t, cmds, "74")
	cmds, _ = d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	noStatusOf(t, cmds, "74")
}

func TestRefusedEndedStatusIsDroppedAndNotWaitedFor(t *testing.T) {
	for _, result := range []core.Result{core.ResultRefused, core.ResultMovedMeanwhile} {
		t.Run(result.String(), func(t *testing.T) {
			d := newStatusDriver(t, draft(), 2)
			statusOf(t, ended(d), "74")
			_, events := d.send(core.StatusResult{IssueID: issueID("74"), Result: result, Reason: "no"})
			hasEvent(t, events, core.StatusFailed{At: d.now, IssueID: issueID("74"), IssueRef: "#74", Result: result,
				Reason: "no"})

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

func TestAE7IssueNoRuleTakesOrInTwoStatesGetsNoStatus(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	for range 2 {
		cmds, _ := d.poll(issue("60", 1, inReview), issue("61", 2, ready, readyToReview))
		if got := statuses(cmds); len(got) != 0 {
			t.Fatalf("statuses for issues no rule takes: %#v", got)
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
	tick, _ := d.send(core.Tick{Said: []core.Said{{IssueID: issueID("1"), Action: "development", Text: "hi"}}})
	all = append(all, tick...)
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	all = append(all, verdict...)
	if got := statuses(all); len(got) != 0 {
		t.Fatalf("statuses reported without status reporting: %#v", got)
	}
}

func TestStatusesOfOneRuleRunShareItsRun(t *testing.T) {
	d := newStatusDriver(t, draft(), 1)
	cmds, _ := d.poll(issue("74", 2, ready))
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
	running := statusOf(t, landed, "74")
	d.wrote("74")
	d.runAll(landed)
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	pending := statusOf(t, verdict, "74")
	d.wrote("74")
	cmds, _ = d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	done := statusOf(t, cmds, "74")

	if running.Run == "" || pending.Run != running.Run || done.Run != running.Run {
		t.Fatalf("runs of one rule run: running %q, ended %q and %q", running.Run, pending.Run, done.Run)
	}
}

func TestEachRuleRunAfterAnEndedOneGetsANewRun(t *testing.T) {
	tests := []struct {
		name string
		next crew.State
	}{
		{name: "the next rule", next: readyToReview},
		{name: "the same rule again", next: ready},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStatusDriver(t, draft(), 2)
			done := statusOf(t, ended(d), "74")
			d.wrote("74")

			cmds, _ := d.poll(issue("74", 1, tt.next))
			landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
			if got := statusOf(t, landed, "74"); got.Run == "" || got.Run == done.Run {
				t.Fatalf("new rule run's run = %q, the ended one's = %q", got.Run, done.Run)
			}
		})
	}
}

func TestEndedStatusOfAnEarlierRunIsWrittenBeforeTheNextRuns(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	want := statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), want)
	listed, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("74", 1, readyToReview)}})
	landed, _ := d.send(core.CallResult{ID: moveID(t, listed, "74"), Result: core.ResultDone})
	noStatusOf(t, landed, "74")

	// The ended write fails again: the next run's status still waits.
	cmds, _ = d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	noStatusOf(t, cmds, "74")
	cmds, _ = d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), want)
	d.send(core.IssuesListed{})

	cmds, _ = d.wrote("74")
	if got := statusOf(t, cmds, "74"); got.Rule != "review" || got.Kind != crew.StatusRunning || got.Run == want.Run {
		t.Fatalf("status after the earlier run's landed: %#v", got)
	}
}
