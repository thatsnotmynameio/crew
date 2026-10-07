package tui

import (
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// saying is runningSnapshot with said as what its sessions last said.
func saying(said ...core.Said) engine.Update {
	u := runningSnapshot()
	u.Snapshot.Said = said
	return u
}

// wantCode fails t unless h's memory holds message and branch for
// issue's code action.
func wantCode(t *testing.T, h *harness, issue, message, branch string) {
	t.Helper()
	gotMessage, gotBranch := h.current().messages.last(issueID(issue), "code")
	if gotMessage != message || gotBranch != branch {
		t.Errorf("#%s code remembers %q on %q, want %q on %q", issue, gotMessage, gotBranch, message, branch)
	}
}

// Covers AE5 (memory side), R18 and KTD9 of #151: an action's last
// message and branch outlive its session.
func TestAnEndedActionKeepsItsLastMessage(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: "running the tests now"})))

	ended := runningSnapshot()
	code := &ended.Snapshot.Issues[0].Actions[0]
	code.Phase, code.Outcome = core.PhaseEnded, crew.Outcome{Succeeded: true}
	h.send(updateMsg(ended))

	wantCode(t, h, "1", "running the tests now", "crew/1-code")
}

// Covers R18: a later message replaces the earlier one, and an empty one
// leaves it.
func TestALaterMessageReplacesTheEarlierOneAndAnEmptyOneLeavesIt(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: "reading the issue"})))
	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: "writing the parser"})))
	wantCode(t, h, "1", "writing the parser", "crew/1-code")

	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: " \n "})))
	wantCode(t, h, "1", "writing the parser", "crew/1-code")
}

// gone is runningSnapshot with #1 no longer held nor on the board, and
// handled as the issues handled this run.
func gone(handled ...core.HandledView) engine.Update {
	u := runningSnapshot()
	u.Snapshot.Issues = u.Snapshot.Issues[1:]
	u.Snapshot.Board = u.Snapshot.Board[1:]
	u.Snapshot.Handled = handled
	return u
}

// Covers R18 and KTD9 of #151: the memory forgets an issue that has no
// card left, and keeps one whose rule ended while it still has a card.
func TestTheMemoryForgetsAnIssueWithNoCardLeft(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: "running the tests now"})))

	u := gone(entry("1", "Add login form", "implement", "ready to review", 7, 0))
	u.Snapshot.Board = append(u.Snapshot.Board,
		crew.BoardIssue{Issue: u.Snapshot.Handled[0].Issue, Labels: []crew.State{"ready to review"}})
	h.send(updateMsg(u))
	wantCode(t, h, "1", "running the tests now", "crew/1-code")

	h.send(updateMsg(gone()))
	wantCode(t, h, "1", "", "")
}

// Covers KTD8 of #151: a message is remembered clean of escape codes and
// control characters.
func TestAMessageIsRememberedClean(t *testing.T) {
	h := newHarness(t, 120)

	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: "\x1b[31mtests\x1b[0m\tfail\n"})))

	wantCode(t, h, "1", "tests fail", "crew/1-code")
}

// Covers KTD9 of #151: two issues with an action of the same name keep
// their messages apart.
func TestTwoIssuesKeepTheirMessagesApart(t *testing.T) {
	h := newHarness(t, 120)
	u := saying(
		core.Said{IssueID: issueID("1"), Action: "code", Text: "adding the form"},
		core.Said{IssueID: issueID("3"), Action: "code", Text: "dropping the flag"},
	)
	three := crew.Issue{ID: issueID("3"), Ref: "#3", Title: "Drop the old flag"}
	u.Snapshot.Issues = append(u.Snapshot.Issues, core.IssueView{
		Issue: three, Rule: "implement", Queue: crew.DefaultQueue, Claim: core.ClaimRunning,
		Actions: []core.ActionView{
			{Name: "code", Phase: core.PhaseRunning, Branch: "crew/3-code", Started: start.Add(-time.Minute)},
		},
	})
	u.Snapshot.Board = append(u.Snapshot.Board, crew.BoardIssue{Issue: three, Labels: []crew.State{"in progress"}})

	h.send(updateMsg(u))

	wantCode(t, h, "1", "adding the form", "crew/1-code")
	wantCode(t, h, "3", "dropping the flag", "crew/3-code")
}

// Covers KTD9 of #151: when the rule takes the issue again, the action's
// new run drops the last run's message and branch.
func TestANewRunDropsTheLastRunsMessage(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(saying(core.Said{IssueID: issueID("1"), Action: "code", Text: "running the tests now"})))

	again := runningSnapshot()
	code := &again.Snapshot.Issues[0].Actions[0]
	code.Branch, code.Started = "crew/1-code-again", start.Add(-time.Minute)
	h.send(updateMsg(again))

	wantCode(t, h, "1", "", "crew/1-code-again")
}
