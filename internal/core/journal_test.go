package core_test

import (
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestANewRunContinuesTheLastRunOfItsIssueAndRule(t *testing.T) {
	d := resumeDriver(t, endedRun(startedRun("9", "development", "lfg", "lfg"), succeeded))

	d.poll(issue("9", 1, readyForDev), issue("10", 1, readyForDev))

	for _, e := range d.events {
		taken, ok := e.(crew.RunTaken)
		if !ok {
			continue
		}
		want := crew.Optional[crew.RuleRunID]{}
		if taken.IssueID == issueID("9") {
			want = crew.Some(pastRun("9", "development"))
		}
		if taken.Continues != want {
			t.Errorf("#%s's run continues %v, want %v", taken.IssueID.Key, taken.Continues, want)
		}
	}
}

func TestReplayingThePastTakesNoSlotAndPublishesNothing(t *testing.T) {
	started := startedRun("9", "development", "lfg", "lfg")
	h := started.EventHead
	ended := endedRun(started, failed("broke"))
	ended.Usage = crew.Usage{Cost: crew.Some(3.0)}
	d := resumeDriver(t,
		crew.RunTaken{
			EventHead: h, Issue: issue("9", 1, readyForDev).Data(), From: readyForDev, To: crewRunning,
			Actions: []crew.ActionTaken{{Name: "lfg"}},
		},
		crew.TakeMoved{EventHead: h, From: readyForDev, To: crewRunning},
		started, ended,
		crew.RunJudged{EventHead: h, Verdict: crew.Verdict{To: crewFailed, Failures: []crew.ActionFailure{{Action: "lfg"}}}},
		crew.VerdictMoved{EventHead: h, From: crewRunning, To: crewFailed},
		crew.RunReleased{EventHead: h},
	)

	if v := d.m.View(); len(v.Issues) != 0 || len(v.Handled) != 0 || v.Spent != (crew.Spend{}) || v.Queues[0].Busy != 0 {
		t.Fatalf("view = %#v, want nothing held, handled or spent", v)
	}
	cmds, events := d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{readyForDev, crewRunning, readyForFix, ready, inProgress}})
	wantEvents(t, events)
}

// v1Start and v1End are the start and end of an action run of lfg, in
// issue-<key>-lfg, as the run journal loads them from version 1 lines
// written by the crew process: in a rule run named after the process, the
// issue and the rule.
func v1Start(process, key string, rule crew.RuleName) crew.ActionOpened {
	e := startedRun(key, rule, "lfg", "lfg")
	e.Run = crew.RuleRunID("v1/" + process + "/" + key + "/" + string(rule))
	return e
}

func v1End(process, key string, rule crew.RuleName, outcome crew.Outcome) crew.ActionEnded {
	return endedRun(v1Start(process, key, rule), outcome)
}

func TestAVersion1JournalGivesTheDecisionsItsLinesGaveBefore(t *testing.T) {
	past := []crew.RunEvent{
		// #9: an older line, without its crew process, of a failed run.
		v1Start("", "9", "development"), v1End("", "9", "development", failed("old reason")),
		// #10: two runs of development from one crew process.
		v1Start("p", "10", "development"), v1End("p", "10", "development", failed("first")),
		v1Start("p", "10", "development"), v1End("p", "10", "development", failed("second")),
		// #11: fix started in development's failed workspace after it.
		v1Start("p", "11", "development"), v1End("p", "11", "development", failed("broke")),
		v1Start("p", "11", "fix"), v1End("p", "11", "fix", succeeded),
	}
	d := &driver{t: t, m: core.New(crewRules(), 3, core.Journaling(past), core.Reopening()), now: t0}
	issues := []crew.Issue{issue("9", 1, readyForDev), issue("10", 2, readyForDev), issue("11", 3, readyForDev)}

	cmds, _ := d.poll(issues...)
	opened := make([]core.Command, 0, len(issues))
	for _, iss := range issues {
		landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, iss.ID().Key), Result: core.ResultDone})
		opened = append(opened, unrecorded(landed)...)
	}
	reopen := func(key string) core.ReopenWorkspace {
		return core.ReopenWorkspace{
			IssueID: issueID(key), Run: d.run(issueID(key)), Action: "lfg",
			Workspace: crew.WorkspaceName("issue-" + key + "-lfg"), Branch: "crew/issue-" + key + "-lfg",
		}
	}
	wantCommands(t, opened, reopen("9"), reopen("10"),
		core.CreateWorkspace{Issue: issues[2], Run: d.run(issueID("11")), Action: "lfg"})
	for key, reason := range map[string]string{"9": "old reason", "10": "second"} {
		cmds, _ := d.send(reopened(key, "lfg", "lfg"))
		if p := startOf(t, cmds).Prompt; !strings.Contains(p, `That run failed: "`+reason+`".`) {
			t.Errorf("#%s's prompt does not quote %q:\n%s", key, reason, p)
		}
	}
}
