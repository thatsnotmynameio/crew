package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// TestAE5AFailedReviewIsTheSameFailureInTheStatusTheHandledEntryAndTheEvents
// runs #7 through a rule whose implement succeeds and whose review fails:
// the ended status, the failure report, the handled entry and the
// ActionEnded event all say the run failed and name review, with the run's
// one workspace and log, and the status carries the run's id.
func TestAE5AFailedReviewIsTheSameFailureInTheStatusTheHandledEntryAndTheEvents(t *testing.T) {
	d, run := reviewing(t)
	cmds, events := d.send(core.SessionEnded{IssueID: issueID("7"), Action: "review", Outcome: failed("found a bug")})

	review := crew.ActionFailure{
		Action: "review", Verdict: crew.Failed, Workspace: "issue-7-implement", Log: ".crew/logs/issue-7-implement.log",
	}
	hasEnd(t, events, end{head: d.runHead("7"), action: "review", outcome: failed("found a bug")})

	status := statusOf(t, cmds, "7")
	if status.Run() != run || status.Rule() != "implement" {
		t.Fatalf("status of run %q of %q, want run %q of implement", status.Run(), status.Rule(), run)
	}
	want := crew.StatusEnded{Route: crew.FailedRoute, To: needsAttention, Move: crew.MovePending}
	if status.Progress() != want {
		t.Fatalf("status progress = %#v, want %#v", status.Progress(), want)
	}
	actions := status.Actions()
	wantStates := []crew.ActionState{
		crew.ActionSucceeded{Verdict: crew.Passed}, crew.ActionFailed{Cause: crew.CauseSession, Log: review.Log},
	}
	if len(actions) != 2 || !reflect.DeepEqual([]crew.ActionState{actions[0].State, actions[1].State}, wantStates) {
		t.Fatalf("status actions = %#v, want implement succeeded and review failed by its session", actions)
	}

	report := failureReport(t, cmds, "7")
	if !reflect.DeepEqual(report.Report.Failures, []crew.ActionFailure{review}) {
		t.Fatalf("failure report = %#v, want review alone", report.Report.Failures)
	}
	moved, _ := d.send(core.CallResult{ID: report.ID, Result: core.ResultDone})
	d.send(core.CallResult{ID: moveID(t, moved, "7"), Result: core.ResultDone})

	handled := d.m.View().Handled
	if len(handled) != 1 {
		t.Fatalf("handled = %#v, want #7's entry", handled)
	}
	h := handled[0]
	if h.Issue.ID() != issueID("7") || h.Rule != "implement" || h.To != needsAttention || !h.NeedsAttention() ||
		!reflect.DeepEqual(h.Failures, []crew.ActionFailure{review}) {
		t.Fatalf("handled entry = %#v, want #7's implement run failed by review", h)
	}
}

// failureReport returns the failure report of issue key in cmds.
func failureReport(t *testing.T, cmds []core.Command, key string) core.ReportFailure {
	t.Helper()
	for _, c := range cmds {
		if r, ok := c.(core.ReportFailure); ok && r.Report.IssueID.Key == key {
			return r
		}
	}
	t.Fatalf("no failure report of %s in %#v", key, cmds)
	return core.ReportFailure{}
}

// reviewing runs #7 through a rule whose implement succeeded and whose
// review runs after it, with status reporting on and every status written so
// far, and returns the driver and the id of the run.
func reviewing(t *testing.T) (*driver, crew.RuleRunID) {
	t.Helper()
	rules := []crew.Rule{{
		Name:   "implement",
		Labels: crew.Labels{Ready: ready, Running: inProgress},
		Actions: []crew.Action{
			sessionAction("implement", "Implement {{.Issue.Ref}}"), sessionAction("review", "Review {{.Issue.Ref}}"),
		},
		Routes: routes(readyToReview, needsAttention),
	}}
	d := &driver{t: t, m: core.New(rules, 1, core.ReportingStatus()), now: t0}
	cmds, _ := d.poll(issue("7", 1, ready))
	run := crew.NewRuleRunID(d.listed, 1)
	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "7"), Result: core.ResultDone})
	// The running status lands, so the ended one is sent at once.
	statusOf(t, cmds, "7")
	d.wrote("7")
	d.ready("7")
	d.send(core.SessionStarted{IssueID: issueID("7"), Action: "implement"})
	d.send(core.SessionEnded{IssueID: issueID("7"), Action: "implement", Outcome: succeeded})
	d.send(core.SessionStarted{IssueID: issueID("7"), Action: "review"})
	return d, run
}
