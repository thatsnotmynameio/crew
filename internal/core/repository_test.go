package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Covers AE4.
func TestAE4IssuesOfTwoRepositoriesWithTheSameKeyAreTwoIssues(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	one, two := issue("42", 1, ready).WithRepository("R_one"), issue("42", 2, ready).WithRepository("R_two")

	cmds, _ := d.poll(one, two)
	wantCommands(t, nonStatus(cmds),
		core.Move{IssueID: one.ID(), From: ready, To: inProgress},
		core.Move{IssueID: two.ID(), From: ready, To: inProgress})
	issues := d.m.View().Issues
	held := make([]crew.IssueID, 0, len(issues))
	for _, iv := range issues {
		held = append(held, iv.Issue.ID())
	}
	if len(held) != 2 || held[0] != one.ID() || held[1] != two.ID() {
		t.Fatalf("held issues: got %#v, want %#v and %#v", held, one.ID(), two.ID())
	}
	// Each issue's status slot is its own: the second take's status is
	// sent while the first's is still in flight.
	for _, c := range nonStatus(cmds) {
		mv, ok := c.(core.Move)
		if !ok {
			t.Fatalf("not a move: %#v", c)
		}
		landed, _ := d.send(core.CallResult{ID: mv.ID, Result: core.ResultDone})
		if got := statuses(landed); len(got) != 1 || got[0].IssueID != mv.IssueID {
			t.Fatalf("statuses once the take of %#v landed: got %#v, want one of it", mv.IssueID, got)
		}
	}
}

// runOf lists it alone, lands its take and returns the run of its running
// status, then the id the listing's seed gives the run it took first.
func runOf(t *testing.T, d *driver, it crew.Issue) (crew.RuleRunID, crew.RuleRunID) {
	t.Helper()
	cmds, _ := d.poll(it)
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, it.ID().Key), Result: core.ResultDone})
	return statusOf(t, landed, it.ID().Key).Run, crew.NewRuleRunID(d.listed, 1)
}

// Covers AE4.
func TestAE4RuleRunsOfTwoRepositoriesIssuesKeyed42HaveTheirOwnIDs(t *testing.T) {
	one, two := issue("42", 1, ready).WithRepository("R_one"), issue("42", 1, ready).WithRepository("R_two")
	first, other := newStatusDriver(t, draft(), 2), newStatusDriver(t, draft(), 2)
	other.inputs = 1000 // another process: other seeds

	run1, want1 := runOf(t, first, one)
	run2, want2 := runOf(t, other, two)
	if run1 != want1 || run2 != want2 {
		t.Fatalf("runs: got %q and %q, want %q and %q", run1, run2, want1, want2)
	}
	if run1 == run2 {
		t.Fatalf("both repositories' runs are %q", run1)
	}

	// The same inputs with the same seeds give the same ids.
	if again, _ := runOf(t, newStatusDriver(t, draft(), 2), one); again != run1 {
		t.Fatalf("run with the same seed: got %q, want %q", again, run1)
	}
}
