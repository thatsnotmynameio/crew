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
		if s.IssueID().Key == key {
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
		if s.IssueID().Key == key {
			t.Fatalf("unexpected status of %s: %#v", key, s)
		}
	}
}

// wrote answers the status write of key as done.
func (d *driver) wrote(key string) ([]core.Command, []core.Published) {
	return d.send(core.StatusResult{IssueID: issueID(key), Result: core.ResultDone, Reason: core.ResultDone.String()})
}

// started returns when the named action of issue 74, the issue these tests
// run, started, from the view.
func started(t *testing.T, m *core.Model, action crew.ActionName) time.Time {
	t.Helper()
	const key = "74"
	for _, iv := range m.View().Issues {
		for _, a := range iv.Actions {
			if iv.Issue.ID().Key == key && a.Name == action {
				return a.Started
			}
		}
	}
	t.Fatalf("no action %s of %s", action, key)
	return time.Time{}
}

// wantStatus compares got with want. A want without a Run takes got's, which
// must not be empty: the run tests below compare runs.
func wantStatus(t *testing.T, got crew.Status, want crew.StatusData) {
	t.Helper()
	if got.Run() == "" {
		t.Fatalf("status without a run: %#v", got)
	}
	if want.Run == "" {
		want.Run = got.Run()
	}
	if !reflect.DeepEqual(got.Data(), want) {
		t.Fatalf("status:\n got %#v\nwant %#v", got.Data(), want)
	}
}

// take polls issue alone, lands its take and answers the first status write.
// It returns the commands the landed take issued.
func (d *driver) take(it crew.Issue) []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(it)
	landed, _ := d.send(core.CallResult{ID: moveID(d.t, cmds, it.ID().Key), Result: core.ResultDone})
	d.wrote(it.ID().Key)
	return landed
}

// runAll creates the workspace landed asked for and starts the session that
// follows.
func (d *driver) runAll(landed []core.Command) {
	d.t.Helper()
	for _, c := range landed {
		if w, ok := c.(core.CreateWorkspace); ok {
			cmds, _ := d.send(space(w.Issue.ID().Key, w.Rule))
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
	blocked := blockedIssue(issue("5", 5, ready))

	cmds, events := d.poll(i2, i3, i4, blocked)
	wantCommands(t, nonStatus(cmds), core.Move{IssueID: issueID("4"), From: readyToReview, To: inReview})
	for _, key := range []string{"2", "3", "5"} {
		noStatusOf(t, cmds, key)
	}
	wantEvents(t, events,
		d.taken(1, i4, "review", readyToReview, inReview, "custom_review"),
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
	urgent, review := prioritized(issue("1", 3, ready), 1), issue("2", 1, readyToReview)
	high := prioritized(issue("3", 2, ready), 2)

	cmds, _ := d.poll(review, high, urgent)
	wantHeld(t, d.m, "1", "3")
	noStatusOf(t, cmds, "2")
}

func TestTakenIssueGetsItsFirstStatusOnceItsTakeLands(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)

	cmds, _ := d.poll(issue("1", 1, ready))
	noStatusOf(t, cmds, "1")

	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "1"), crew.StatusData{
		IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Progress: crew.StatusRunning{}, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionPending{}},
			{Name: "development", State: crew.ActionAwaitingTurn{}},
		},
	})
}

func TestAE2RunningStatusShowsTheSessionsStartAndLastWords(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))

	cmds, _ := d.send(core.Tick{Said: []core.Said{
		{IssueID: issueID("74"), Action: "acceptance", Text: crew.NewSaid("U1 committed: 168 tests pass. Starting U2.")},
		{IssueID: issueID("99"), Action: "acceptance", Text: crew.NewSaid("not held")},
		{IssueID: issueID("74"), Action: "unknown", Text: crew.NewSaid("no such action")},
	}})
	wantStatus(t, statusOf(t, cmds, "74"), crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Progress: crew.StatusRunning{}, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionRunning{Started: started(t, d.m, "acceptance"),
				Said: crew.NewSaid("U1 committed: 168 tests pass. Starting U2.")}},
			{Name: "development", State: crew.ActionAwaitingTurn{}},
		},
	})
	d.wrote("74")

	// A running issue is reported at every tick, changed or not (R6).
	cmds, _ = d.send(core.Tick{})
	got := statusOf(t, cmds, "74")
	if a, _ := got.Actions()[0].State.(crew.ActionRunning); got.Updated() != d.now ||
		a.Said.String() != "U1 committed: 168 tests pass. Starting U2." {
		t.Fatalf("second tick's status: %#v", got)
	}
}

// developing runs #74 through acceptance until development's session
// started, the status writes answered.
func (d *driver) developing() {
	d.t.Helper()
	d.runAll(d.take(issue("74", 1, ready)))
	d.settle(d.ended("74", "acceptance", succeeded))
}

func TestAE3EndedStatusSaysWhereTheIssueGoesThenThatItMoved(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.developing()
	devStarted := started(t, d.m, "development")

	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Progress: crew.StatusRunning{}, Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionSucceeded{Verdict: crew.Passed}},
			{Name: "development", State: crew.ActionRunning{Started: devStarted}},
		},
	})
	d.wrote("74")

	report, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("tests fail")})
	final := []crew.ActionStatus{
		{Name: "acceptance", State: crew.ActionSucceeded{Verdict: crew.Passed}},
		{Name: "development", State: crew.ActionFailed{Cause: crew.CauseSession, Log: space("74", "implement").Log}},
	}
	reportStep := crew.StepPlan{Kind: crew.StepReport}
	moveStep := crew.StepPlan{Kind: crew.StepMove, To: needsAttention}
	ended := crew.StatusEnded{Route: crew.FailedRoute, To: needsAttention, Move: crew.MovePending}
	wantStatus(t, statusOf(t, report, "74"), crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Updated: d.now,
		Actions: final, Progress: ended, Steps: []crew.StepStatus{{Step: reportStep}, {Step: moveStep}},
	})
	d.wrote("74")

	// The failure report is the route's first step; the move waits for it,
	// and the status says the report landed (KTD-S17).
	moved, _ := d.send(core.CallResult{ID: reportID(t, report, "74"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, moved, "74"), crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Updated: d.now, Actions: final, Progress: ended,
		Steps: []crew.StepStatus{{Step: reportStep, Outcome: crew.StepLanded{}}, {Step: moveStep}},
	})
	d.wrote("74")
	cmds, _ = d.send(core.CallResult{ID: moveID(t, moved, "74"), Result: core.ResultDone})
	ended.Move = crew.MoveDone
	wantStatus(t, statusOf(t, cmds, "74"), crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Updated: d.now, Actions: final, Progress: ended,
		Steps: []crew.StepStatus{
			{Step: reportStep, Outcome: crew.StepLanded{}}, {Step: moveStep, Outcome: crew.StepLanded{}},
		},
	})
}

func TestEndedStatusSaysWhenTheMoveWasDropped(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	ending := d.endActions("74")
	d.wrote("74")

	cmds, _ := d.send(core.CallResult{ID: moveID(t, ending, "74"), Result: core.ResultMovedMeanwhile})
	got := statusOf(t, cmds, "74")
	if got.Progress() != (crew.StatusEnded{Route: crew.PassedRoute, To: readyToReview, Move: crew.MoveDropped}) {
		t.Fatalf("status after a dropped move: %#v", got)
	}
}

func TestAE4StopWhileASessionRunsEndsWithTheMoveOnTheComment(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	d.developing()

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopSession{IssueID: issueID("74"), Run: d.run(issueID("74")), Action: "development"})
	if cmds, _ := d.send(core.Tick{}); len(cmds) != 0 {
		t.Fatalf("tick after stop issued %#v", cmds)
	}

	report, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("stopped")})
	got := statusOf(t, report, "74")
	if got.Progress() != (crew.StatusEnded{Route: crew.FailedRoute, To: needsAttention, Move: crew.MovePending}) {
		t.Fatalf("status at the ending: %#v", got)
	}
	moved, _ := d.send(core.CallResult{ID: reportID(t, report, "74"), Result: core.ResultDone})
	d.wrote("74")
	d.wrote("74")

	cmds, events := d.send(core.CallResult{ID: moveID(t, moved, "74"), Result: core.ResultDone})
	wantStatus(t, statusOf(t, cmds, "74"), crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Updated: d.now,
		Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionSucceeded{Verdict: crew.Passed}},
			{Name: "development", State: crew.ActionFailed{Cause: crew.CauseStopped, Log: space("74", "implement").Log}},
		},
		Progress: crew.StatusEnded{Route: crew.FailedRoute, To: needsAttention, Move: crew.MoveDone},
		Steps: []crew.StepStatus{
			{Step: crew.StepPlan{Kind: crew.StepReport}, Outcome: crew.StepLanded{}},
			{Step: crew.StepPlan{Kind: crew.StepMove, To: needsAttention}, Outcome: crew.StepLanded{}},
		},
	})
	if d.m.Stopped() || containsStopped(events) {
		t.Fatal("stopped with the last status write in flight")
	}

	_, events = d.wrote("74")
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the last status write returned")
	}
}

func containsStopped(events []core.Published) bool {
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
	if got := statusOf(t, cmds, "74"); got.Updated() != newest {
		t.Fatalf("sent the status of %v, want the newest, of %v", got.Updated(), newest)
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

// ended runs #74 to an ending whose move lands, and returns the commands of
// the move's result, which carry the ended status.
func ended(d *driver) []core.Command {
	d.t.Helper()
	d.runAll(d.take(issue("74", 1, ready)))
	ending := d.endActions("74")
	d.wrote("74")
	cmds, _ := d.send(core.CallResult{ID: moveID(d.t, ending, "74"), Result: core.ResultDone})
	return cmds
}

func TestFailedEndedStatusIsRetriedAtEachTickUntilItLands(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	want := statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), want.Data())
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
	wantStatus(t, statusOf(t, cmds, "74"), want.Data())
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
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: failed("stopped")})

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
	tick, _ := d.send(core.Tick{Said: []core.Said{
		{IssueID: issueID("1"), Action: "development", Text: crew.NewSaid("hi")},
	}})
	all = append(all, tick...)
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	ending, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	all = append(all, ending...)
	if got := statuses(all); len(got) != 0 {
		t.Fatalf("statuses reported without status reporting: %#v", got)
	}
}

func TestStatusesOfOneRuleRunShareItsRun(t *testing.T) {
	d := newStatusDriver(t, draft(), 1)
	cmds, _ := d.poll(issue("74", 2, ready))
	run := crew.NewRuleRunID(d.listed, 1)
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
	running := statusOf(t, landed, "74")
	d.wrote("74")
	d.runAll(landed)
	ending := d.endActions("74")
	pending := statusOf(t, ending, "74")
	d.wrote("74")
	cmds, _ = d.send(core.CallResult{ID: moveID(t, ending, "74"), Result: core.ResultDone})
	done := statusOf(t, cmds, "74")

	if running.Run() != run || pending.Run() != run || done.Run() != run {
		t.Fatalf("runs of one rule run: running %q, ended %q and %q, want %q", running.Run(), pending.Run(), done.Run(), run)
	}
}

func TestIssuesTakenByOneListingGetRunsOfTheirOwn(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("1", 1, ready), issue("2", 2, ready))
	for i, key := range []string{"1", "2"} {
		landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, key), Result: core.ResultDone})
		if got, want := statusOf(t, landed, key).Run(), crew.NewRuleRunID(d.listed, i+1); got != want {
			t.Fatalf("run of #%s: got %q, want %q", key, got, want)
		}
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
			run := crew.NewRuleRunID(d.listed, 1)
			landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
			if got := statusOf(t, landed, "74"); got.Run() != run || got.Run() == done.Run() {
				t.Fatalf("new rule run's run = %q, want %q; the ended one's = %q", got.Run(), run, done.Run())
			}
		})
	}
}

func TestEndedStatusOfAnEarlierRunIsWrittenBeforeTheNextRuns(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	want := statusOf(t, ended(d), "74")
	d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ := d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), want.Data())
	listed, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("74", 1, readyToReview)}})
	landed, _ := d.send(core.CallResult{ID: moveID(t, listed, "74"), Result: core.ResultDone})
	noStatusOf(t, landed, "74")

	// The ended write fails again: the next run's status still waits.
	cmds, _ = d.send(core.StatusResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	noStatusOf(t, cmds, "74")
	cmds, _ = d.send(core.Tick{})
	wantStatus(t, statusOf(t, cmds, "74"), want.Data())
	d.send(core.IssuesListed{})

	cmds, _ = d.wrote("74")
	got := statusOf(t, cmds, "74")
	if got.Rule() != "review" || got.Progress() != (crew.StatusRunning{}) || got.Run() == want.Run() {
		t.Fatalf("status after the earlier run's landed: %#v", got)
	}
}
