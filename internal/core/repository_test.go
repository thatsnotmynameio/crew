package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Covers AE4.
func TestAE4IssuesOfTwoRepositoriesWithTheSameKeyAreTwoIssues(t *testing.T) {
	d := newStatusDriver(t, draft(), 2)
	one, two := issue("42", 1, ready), issue("42", 2, ready)
	one.ID.Repository, two.ID.Repository = "R_one", "R_two"

	cmds, _ := d.poll(one, two)
	wantCommands(t, nonStatus(cmds),
		core.Move{IssueID: one.ID, From: ready, To: inProgress},
		core.Move{IssueID: two.ID, From: ready, To: inProgress})
	issues := d.m.View().Issues
	held := make([]crew.IssueID, 0, len(issues))
	for _, iv := range issues {
		held = append(held, iv.Issue.ID)
	}
	if len(held) != 2 || held[0] != one.ID || held[1] != two.ID {
		t.Fatalf("held issues: got %#v, want %#v and %#v", held, one.ID, two.ID)
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
