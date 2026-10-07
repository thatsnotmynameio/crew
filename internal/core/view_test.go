package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// heldView returns the view of held issue key.
func heldView(t *testing.T, m *core.Model, key string) core.IssueView {
	t.Helper()
	for _, iv := range m.View().Issues {
		if iv.Issue.ID().Key == key {
			return iv
		}
	}
	t.Fatalf("issue %s is not held", key)
	return core.IssueView{}
}

// phases returns the phases of iv's actions, in order.
func phases(iv core.IssueView) []core.Phase {
	out := make([]core.Phase, 0, len(iv.Actions))
	for _, a := range iv.Actions {
		out = append(out, a.Phase)
	}
	return out
}

// Covers KTD-S17: a take in flight shows as taking at the action that runs
// first, the others awaiting their turn.
func TestATakeInFlightShowsAsTaking(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.poll(issue("1", 1, ready))

	iv := heldView(t, d.m, "1")
	want := []core.Phase{core.PhaseTaking, core.PhaseAwaitingTurn}
	if iv.Claim != core.ClaimTaking || !reflect.DeepEqual(phases(iv), want) || iv.Route != "" {
		t.Fatalf("view: got claim %v, phases %v, route %q; want taking, %v, no route", iv.Claim, phases(iv), iv.Route, want)
	}
}

// Covers KTD-S17: a shell action shows as the running action, like a
// session, with the time crew asked for its script.
func TestAShellActionShowsAsTheRunningAction(t *testing.T) {
	d := newDriver(t, judging(), 2)
	d.judged()
	asked := d.now

	iv := heldView(t, d.m, "1")
	want := []core.Phase{core.PhaseEnded, core.PhaseRunning}
	if iv.Claim != core.ClaimRunning || !reflect.DeepEqual(phases(iv), want) || !iv.Actions[1].Started.Equal(asked) {
		t.Fatalf("view: got %#v, want judge running since %v", iv, asked)
	}
}

// Covers KTD-S17: a run in its routing phase shows the route it ends
// through, and the actions it never reached as not run.
func TestARoutingRunShowsItsRouteAndTheActionsItDidNotRun(t *testing.T) {
	d := newDriver(t, draft(), 2)
	endedNeedingAttention(d)

	iv := heldView(t, d.m, "1")
	want := []core.Phase{core.PhaseEnded, core.PhaseNotRun}
	if iv.Claim != core.ClaimRouting || iv.Route != crew.FailedRoute || !reflect.DeepEqual(phases(iv), want) {
		t.Fatalf("view: got claim %v, route %q, phases %v; want routing through failed, %v",
			iv.Claim, iv.Route, phases(iv), want)
	}
}

// Covers KTD23: the run's worktree, branch and log reach the views as a
// published event, once its workspace is ready.
func TestTheRunsWorkspaceIsPublishedOnceReady(t *testing.T) {
	d := newDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

	w := space("1", "implement")
	w.Run = d.run(issueID("1"))
	_, events := d.send(w)

	hasEvent(t, events, crew.WorkspaceOpened{
		EventHead: d.runHead("1"), Workspace: crew.Workspace{Name: w.Workspace, Branch: w.Branch}, Log: w.Log,
	})
}

// Covers R16, KTD23: a route's step that settled reaches the views with
// what it does, and a move with the state it takes the issue from.
func TestARouteStepThatSettledIsPublishedWithWhatItDoes(t *testing.T) {
	d := newDriver(t, draft(), 2)
	report := endedNeedingAttention(d)

	moved, events := d.send(core.CallResult{ID: reportID(t, report, "1"), Result: core.ResultDone})
	head := d.runHead("1")
	hasEvent(t, events, core.RouteStepEnded{
		At: d.now, IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Route: crew.FailedRoute, Step: 0,
		Plan: crew.StepPlan{Kind: crew.StepReport}, Outcome: crew.StepLanded{},
	})
	_, events = d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.RouteStepEnded{
		At: d.now, IssueID: head.IssueID, IssueRef: "#1", Rule: "implement", Route: crew.FailedRoute, Step: 1,
		Plan: crew.StepPlan{Kind: crew.StepMove, To: needsAttention}, From: inProgress, Outcome: crew.StepLanded{},
	})
	for _, e := range events {
		if _, raw := e.(crew.StepEnded); raw {
			t.Fatalf("published the run's own StepEnded: %#v", events)
		}
	}
}
