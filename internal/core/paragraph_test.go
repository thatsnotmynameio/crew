package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// blocking is the draft rules with acceptance ending with blocked or
// needs-info through the route blocked, which moves the issue to blocked.
func blocking() []crew.Rule {
	rules := draft()
	toBlocked := crew.ToRoute{Route: "blocked"}
	rules[0].Actions[0].On = crew.On{"needs-info": toBlocked, "blocked": toBlocked}
	rules[0].Routes = append(rules[0].Routes,
		crew.Route{Name: "blocked", Steps: []crew.Step{crew.MoveStep{To: "blocked"}}})
	return rules
}

// verdicts is the paragraph of a session whose on: names blocked and
// needs-info.
const verdicts = "crew: you may end this session with a verdict, one of `blocked`, `needs-info`, by writing it " +
	"on the first line of the file the environment variable `CREW_VERDICT_FILE` names. crew then sends the " +
	"issue where that verdict leads. Leave the file empty to be judged by how the session ends."

// Covers R9: a session whose on: names verdicts is told it may report one,
// in name order; a session whose on: names none is told nothing.
func TestASessionWhoseOnNamesVerdictsGetsTheVerdictParagraph(t *testing.T) {
	d := newDriver(t, blocking(), 2)
	cmds, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

	acceptance := d.session("1", "acceptance", "Implement test acceptance for issue #1\n\n"+verdicts)
	wantCommands(t, d.ready("1"), acceptance)
	d.send(core.SessionStarted{IssueID: issueID("1"), Action: "acceptance"})
	wantCommands(t, d.ended("1", "acceptance", succeeded),
		d.session("1", "development", "Implement development for issue #1"))
}

// Covers R23: a resumed session is told the route its last run ended
// through, before the verdict paragraph.
func TestAResumedSessionsParagraphNamesTheRouteItsLastRunEndedThrough(t *testing.T) {
	past := journaled(t, blocking(), nil, func(d *driver) {
		d.running(issue("1", 1, ready))
		cmds, _ := d.send(core.SessionEnded{
			IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded,
			Report: crew.VerdictReported{Verdict: "blocked"},
		})
		d.settle(cmds)
		wantHeld(t, d.m)
	})
	d := resumeDriverOf(t, blocking(), past...)

	cmds := d.takeIssue(issue("1", 2, ready))
	wantCommands(t, cmds, d.reopen("1", "implement"))
	cmds, _ = d.send(reopened("1", "implement"))
	want := "Implement test acceptance for issue #1\n\n" +
		"crew: this session continues the work of an earlier run of this rule, in this worktree, " +
		"on branch `crew/issue-1-implement`. That run ended through the route `blocked`: \"done\". " +
		"Its output is in the log `.crew/logs/issue-1-implement.log` of the repository's main checkout " +
		"(`../../logs/issue-1-implement.log` from this worktree), above the line crew wrote there when this " +
		"session started. Check the worktree's state with `git status` and `git log` before you go on, " +
		"and continue from where it stopped instead of starting over.\n\n" + verdicts
	if got := startOf(t, cmds).Prompt; got != want {
		t.Fatalf("prompt:\n got %q\nwant %q", got, want)
	}
}
