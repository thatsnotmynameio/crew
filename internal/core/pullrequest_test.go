package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// newPullRequestDriver is newDriver with pull request reports on.
func newPullRequestDriver(t *testing.T, rules []crew.Rule) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(rules, 2, core.ReportingPullRequests()), now: t0}
}

// pullRequestReports returns the reports cmds ask to make, in order.
func pullRequestReports(cmds []core.Command) []crew.PullRequestReport {
	var out []crew.PullRequestReport
	for _, c := range cmds {
		if r, ok := c.(core.ReportPullRequests); ok {
			out = append(out, r.Report)
		}
	}
	return out
}

// pullRequestReportOf returns the one report cmds ask to make.
func pullRequestReportOf(t *testing.T, cmds []core.Command) crew.PullRequestReport {
	t.Helper()
	found := pullRequestReports(cmds)
	if len(found) != 1 {
		t.Fatalf("want one pull request report in %#v, got %d", cmds, len(found))
	}
	return found[0]
}

// noPullRequestReport fails when cmds ask to make a report.
func noPullRequestReport(t *testing.T, cmds []core.Command) {
	t.Helper()
	if found := pullRequestReports(cmds); len(found) > 0 {
		t.Fatalf("unexpected pull request reports: %#v", found)
	}
}

// wantReport compares got with want. A want without an ID takes got's, which
// must not be empty.
func wantReport(t *testing.T, got, want crew.PullRequestReport) {
	t.Helper()
	if got.ID == "" {
		t.Fatalf("report without an ID: %#v", got)
	}
	if want.ID == "" {
		want.ID = got.ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("report:\n got %#v\nwant %#v", got, want)
	}
}

// answerPullRequests answers the report in flight for key with result.
func (d *driver) answerPullRequests(key string, result core.Result) ([]core.Command, []core.Event) {
	return d.send(core.PullRequestsResult{IssueID: issueID(key), Result: result, Reason: result.String()})
}

// takeLanded polls #74 alone and lands its take. It returns the commands the
// landed take issued.
func (d *driver) takeLanded() []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(issue("74", 1, ready))
	landed, _ := d.send(core.CallResult{ID: moveID(d.t, cmds, "74"), Result: core.ResultDone})
	return landed
}

// verdictLanded starts #74's actions from landed, ends them with acceptance
// and development and lands the verdict move. It returns the commands the
// landed verdict move issued.
func (d *driver) verdictLanded(landed []core.Command, acceptance, development crew.Outcome) []core.Command {
	d.t.Helper()
	d.runAll(landed)
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: acceptance})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: development})
	noPullRequestReport(d.t, verdict)
	cmds, _ := d.send(core.CallResult{ID: moveID(d.t, verdict, "74"), Result: core.ResultDone})
	return cmds
}

// succeededRule runs #74 through implement with every action succeeding and
// its take report done. It returns the commands the landed verdict move
// issued.
func (d *driver) succeededRule() []core.Command {
	d.t.Helper()
	landed := d.takeLanded()
	d.answerPullRequests("74", core.ResultDone)
	return d.verdictLanded(landed, succeeded, succeeded)
}

// allSucceeded is the end of implement when both its actions succeeded.
var allSucceeded = &crew.RuleEnd{Rule: "implement", Actions: []crew.ActionStatus{
	{Name: "acceptance", State: crew.ActionSucceeded},
	{Name: "development", State: crew.ActionSucceeded},
}}

func TestWithoutPullRequestReportsARuleReportsNone(t *testing.T) {
	d := newDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("74", 1, ready))
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
	noPullRequestReport(t, landed)
	d.runAll(landed)
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: failed("broke")})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	moved, _ := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	noPullRequestReport(t, moved)
	reported, _ := d.send(core.CallResult{ID: reportID(t, verdict, "74"), Result: core.ResultDone})
	noPullRequestReport(t, reported)
	wantHeld(t, d.m)
}

func TestALandedTakeReportsItsMovesToWithNoEnd(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	wantReport(t, pullRequestReportOf(t, d.takeLanded()), crew.PullRequestReport{
		IssueID: issueID("74"), IssueRef: "#74", State: inProgress,
	})
}

func TestATakeThatFailsReportsNothing(t *testing.T) {
	for _, result := range []core.Result{core.ResultFailed, core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newPullRequestDriver(t, draft())
			cmds, _ := d.poll(issue("74", 1, ready))
			cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: result})
			noPullRequestReport(t, cmds)
		})
	}
}

func TestAE1ASucceededRuleReportsOnSuccessAndItsEndOnceItsMoveLands(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	wantReport(t, pullRequestReportOf(t, d.succeededRule()), crew.PullRequestReport{
		IssueID: issueID("74"), IssueRef: "#74", State: readyToReview, End: allSucceeded,
	})
}

func TestAFailedRuleReportsOnFailureWithEachFailedActionsCause(t *testing.T) {
	d := newPullRequestDriver(t, checked())
	landed := d.takeLanded()
	d.answerPullRequests("74", core.ResultDone)
	d.runAll(landed)
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: failed("broke")})
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	verdict, _ := d.send(core.CheckEnded{
		IssueID: issueID("74"), Action: "development", Reason: crew.NewCheckReason("no pull request"),
	})
	noPullRequestReport(t, verdict)

	cmds, _ := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	wantReport(t, pullRequestReportOf(t, cmds), crew.PullRequestReport{
		IssueID: issueID("74"), IssueRef: "#74", State: needsAttention,
		End: &crew.RuleEnd{Rule: "implement", Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionFailed, Cause: crew.CauseSession, Log: space("74", "acceptance").Log},
			{Name: "development", State: crew.ActionFailed, Cause: crew.CauseCheck, Log: space("74", "development").Log,
				Checks: []crew.CheckResult{{Name: "pr-closes-issue", Reason: crew.NewCheckReason("no pull request")}}},
		}},
	})
}

func TestAE2AStopWhileTheSessionRunsReportsOnFailureWithTheActionStopped(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	landed := d.takeLanded()
	d.answerPullRequests("74", core.ResultDone)
	d.runAll(landed)
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	d.send(core.StopRequested{})
	verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("killed")})
	d.send(core.CallResult{ID: reportID(t, verdict, "74"), Result: core.ResultDone})

	cmds, _ := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	wantReport(t, pullRequestReportOf(t, cmds), crew.PullRequestReport{
		IssueID: issueID("74"), IssueRef: "#74", State: needsAttention,
		End: &crew.RuleEnd{Rule: "implement", Actions: []crew.ActionStatus{
			{Name: "acceptance", State: crew.ActionSucceeded},
			{Name: "development", State: crew.ActionFailed, Cause: crew.CauseStopped, Log: space("74", "development").Log},
		}},
	})
}

func TestADroppedVerdictMoveReportsNothing(t *testing.T) {
	for _, result := range []core.Result{core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newPullRequestDriver(t, draft())
			landed := d.takeLanded()
			d.answerPullRequests("74", core.ResultDone)
			d.runAll(landed)
			d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
			verdict, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})

			cmds, _ := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: result})
			noPullRequestReport(t, cmds)
			wantHeld(t, d.m)
		})
	}
}

func TestTheVerdictReportWaitsForTheTakeReportInFlight(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	landed := d.takeLanded()
	run := crew.NewRuleRunID(d.listed, 1)
	take := pullRequestReportOf(t, landed)

	noPullRequestReport(t, d.verdictLanded(landed, succeeded, succeeded))

	cmds, _ := d.answerPullRequests("74", core.ResultDone)
	verdict := pullRequestReportOf(t, cmds)
	wantReport(t, verdict, crew.PullRequestReport{
		IssueID: issueID("74"), IssueRef: "#74", State: readyToReview, End: allSucceeded,
	})
	if take.ID != run.TakeReport() || verdict.ID != run.VerdictReport() {
		t.Fatalf("report IDs: take %q and verdict %q, want %q and %q",
			take.ID, verdict.ID, run.TakeReport(), run.VerdictReport())
	}
}

func TestAE5AReportThatFailsTransientlyIsOwedAndResentAtTheNextTick(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	want := pullRequestReportOf(t, d.succeededRule())

	cmds, events := d.send(core.PullRequestsResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "timeout"})
	noPullRequestReport(t, cmds)
	hasEvent(t, events, core.CallOwed{At: d.now, Reason: "timeout", Call: core.Call{
		Kind: core.CallPullRequests, IssueID: issueID("74"), IssueRef: "#74", To: readyToReview,
	}})
	if got := onlyEntry(t, d); got.Move != crew.MoveDone {
		t.Fatalf("handled move: got %v, want done", got.Move)
	}

	cmds, _ = d.send(core.Tick{})
	if got := pullRequestReportOf(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("resent report:\n got %#v\nwant %#v", got, want)
	}
}

func TestAnOwedReportIsInTheViewAndHoldsBackTheIssuesLaterReports(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	landed := d.takeLanded()
	d.answerPullRequests("74", core.ResultFailed)
	owed := core.Call{Kind: core.CallPullRequests, IssueID: issueID("74"), IssueRef: "#74", To: inProgress}
	if got := d.m.View().Owed; !reflect.DeepEqual(got, []core.Call{owed}) {
		t.Fatalf("owed: got %#v, want %#v", got, []core.Call{owed})
	}

	noPullRequestReport(t, d.verdictLanded(landed, succeeded, succeeded))

	cmds, _ := d.send(core.Tick{})
	if got := pullRequestReportOf(t, cmds); got.State != inProgress {
		t.Fatalf("tick sent the report to %q, want the owed one to %q", got.State, inProgress)
	}
	d.send(core.IssuesListed{})

	cmds, _ = d.answerPullRequests("74", core.ResultDone)
	if got := pullRequestReportOf(t, cmds); got.State != readyToReview {
		t.Fatalf("sent the report to %q once the owed one landed, want %q", got.State, readyToReview)
	}
	if got := d.m.View().Owed; got != nil {
		t.Fatalf("owed after the report landed: %#v", got)
	}
}

func TestARefusedOrMovedMeanwhileReportIsDroppedAndTheNextOneSent(t *testing.T) {
	for _, result := range []core.Result{core.ResultRefused, core.ResultMovedMeanwhile} {
		t.Run(result.String(), func(t *testing.T) {
			d := newPullRequestDriver(t, draft())
			landed := d.takeLanded()
			d.verdictLanded(landed, succeeded, succeeded)

			cmds, events := d.answerPullRequests("74", result)
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: result.String(), Call: core.Call{
				Kind: core.CallPullRequests, IssueID: issueID("74"), IssueRef: "#74", To: inProgress,
			}})
			if got := pullRequestReportOf(t, cmds); got.State != readyToReview {
				t.Fatalf("sent the report to %q after the drop, want %q", got.State, readyToReview)
			}
		})
	}
}

func TestStopGivesAnOwedReportOneFinalTryThenDropsIt(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	want := pullRequestReportOf(t, d.succeededRule())
	d.answerPullRequests("74", core.ResultFailed)

	cmds, _ := d.send(core.StopRequested{})
	if got := pullRequestReportOf(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("final try:\n got %#v\nwant %#v", got, want)
	}
	if d.m.Stopped() {
		t.Fatal("stopped with the final try in flight")
	}

	cmds, events := d.send(core.PullRequestsResult{IssueID: issueID("74"), Result: core.ResultFailed,
		Reason: "still down"})
	noPullRequestReport(t, cmds)
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallPullRequests, IssueID: issueID("74"), IssueRef: "#74", To: readyToReview,
	}})
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the final try failed")
	}
}

func TestStopWaitsForAReportInFlightWithNoIssueHeld(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	pullRequestReportOf(t, d.succeededRule())
	wantHeld(t, d.m)

	_, events := d.send(core.StopRequested{})
	if d.m.Stopped() || containsStopped(events) {
		t.Fatal("stopped with a report in flight")
	}

	_, events = d.answerPullRequests("74", core.ResultDone)
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the report landed")
	}
}

func TestAReportThatFailsInFlightAfterAStopGetsOneFinalTry(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	want := pullRequestReportOf(t, d.succeededRule())
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.PullRequestsResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "down"})
	if got := pullRequestReportOf(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("final try:\n got %#v\nwant %#v", got, want)
	}
	if d.m.Stopped() {
		t.Fatal("stopped with the final try in flight")
	}

	cmds, events := d.send(core.PullRequestsResult{IssueID: issueID("74"), Result: core.ResultFailed,
		Reason: "still down"})
	noPullRequestReport(t, cmds)
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallPullRequests, IssueID: issueID("74"), IssueRef: "#74", To: readyToReview,
	}})
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the final try failed")
	}
}

func TestAReportQueuedBehindOneThatFailsAfterAStopIsStillSent(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	landed := d.takeLanded()
	take := pullRequestReportOf(t, landed)
	noPullRequestReport(t, d.verdictLanded(landed, succeeded, succeeded))
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.PullRequestsResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "down"})
	if got := pullRequestReportOf(t, cmds); got.ID != take.ID {
		t.Fatalf("final try sent report %q, want the take report %q", got.ID, take.ID)
	}

	cmds, _ = d.send(core.PullRequestsResult{IssueID: issueID("74"), Result: core.ResultFailed, Reason: "still down"})
	verdict := pullRequestReportOf(t, cmds)
	wantReport(t, verdict, crew.PullRequestReport{
		IssueID: issueID("74"), IssueRef: "#74", State: readyToReview, End: allSucceeded,
	})
	if d.m.Stopped() {
		t.Fatal("stopped with the verdict report in flight")
	}

	_, events := d.answerPullRequests("74", core.ResultDone)
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the verdict report landed")
	}
}

func TestAnOwedTakeReportLeavesTheIssueRunning(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	landed := d.takeLanded()
	var created []crew.ActionName
	for _, c := range landed {
		if w, ok := c.(core.CreateWorkspace); ok {
			created = append(created, w.Action)
		}
	}
	if want := []crew.ActionName{"acceptance", "development"}; !reflect.DeepEqual(created, want) {
		t.Fatalf("the landed take created workspaces for %v, want %v", created, want)
	}

	d.answerPullRequests("74", core.ResultFailed)
	if c := claimOf(t, d.m, "74"); c != core.ClaimRunning {
		t.Fatalf("claim of #74: got %v, want running", c)
	}
	d.runAll(landed)
	for _, a := range d.m.View().Issues[0].Actions {
		if a.Phase != core.PhaseRunning {
			t.Fatalf("action %s is %v, want running", a.Name, a.Phase)
		}
	}
}

func TestEachReportHasItsOwnIDAndARetryKeepsIt(t *testing.T) {
	d := newPullRequestDriver(t, draft())
	cmds, _ := d.poll(issue("1", 1, ready), issue("2", 2, ready))
	landed1, _ := d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	landed2, _ := d.send(core.CallResult{ID: moveID(t, cmds, "2"), Result: core.ResultDone})
	first, second := pullRequestReportOf(t, landed1), pullRequestReportOf(t, landed2)
	want1, want2 := crew.NewRuleRunID(d.listed, 1).TakeReport(), crew.NewRuleRunID(d.listed, 2).TakeReport()
	if first.ID != want1 || second.ID != want2 {
		t.Fatalf("report IDs: got %q and %q, want %q and %q", first.ID, second.ID, want1, want2)
	}

	d.answerPullRequests("1", core.ResultFailed)
	retry, _ := d.send(core.Tick{})
	if got := pullRequestReportOf(t, retry); got.ID != first.ID {
		t.Fatalf("retry's ID: got %q, want %q", got.ID, first.ID)
	}
}
