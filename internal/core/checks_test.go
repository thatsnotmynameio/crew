package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// checkPassed is the reason of a check that passed.
var checkPassed = crew.NewCheckReason("the check passed")

// judgeCheck is the check that runs before prCheck on development in
// twoChecks.
const judgeCheck = `./judge "$CREW_LAST_MESSAGE_FILE"`

// twoChecks is the draft rules with two checks on development: judge, then
// pr-closes-issue.
func twoChecks() []crew.Rule {
	w := draft()
	w[0].Actions[1].Checks = []crew.Check{{Name: "judge", Script: judgeCheck}, {Name: "pr-closes-issue", Script: prCheck}}
	return w
}

// judging runs issue 74 in twoChecks until development's session succeeded
// with lastMessage and its first check, judge, started.
func judging(d *driver, lastMessage string) {
	d.t.Helper()
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{
		IssueID: issueID("74"), Action: "development", Outcome: succeeded, LastMessage: lastMessage,
	})
	wantCommands(d.t, cmds, judgeRun(d, lastMessage))
}

// judgeRun is the RunCheck of development's judge in d's last run of issue
// 74, after a session whose last message was lastMessage.
func judgeRun(d *driver, lastMessage string) core.RunCheck {
	c := d.runCheck()
	c.Name, c.Command, c.LastMessage = "judge", judgeCheck, lastMessage
	return c
}

// Covers AE3: a check that passes starts the next with the same prompt and
// last message; the next one's failure fails the action.
func TestAPassingCheckStartsTheNextWhichDecidesTheAction(t *testing.T) {
	d := newDriver(t, twoChecks(), 2)
	judging(d, "PR #20 is open.\nMerging is yours.")

	cmds, _ := d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development",
		Passed: true, Reason: crew.NewCheckReason("the check judge passed: done (0.97)")})
	next := d.runCheck()
	next.LastMessage = "PR #20 is open.\nMerging is yours."
	wantCommands(t, cmds, next)

	reason := "the check pr-closes-issue failed: no open pull request from crew/issue-74-development"
	cmds, _ = d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Reason: crew.NewCheckReason(reason)})
	if got := noIDs(cmds)[0]; !reflect.DeepEqual(got, core.Move{IssueID: issueID("74"), From: inProgress,
		To: needsAttention}) {
		t.Fatalf("ending = %#v, want the move to needs attention", got)
	}
	ws := space("74", "development")
	want := []crew.ActionFailure{{Action: "development", Workspace: ws.Workspace, Log: ws.Log}}
	if got := failures(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
	d.wantReason("74", "development", reason)
}

// Covers AE1: the first check that fails ends the action; the checks after
// it never run.
func TestAFailingCheckEndsTheActionBeforeTheNext(t *testing.T) {
	d := newDriver(t, twoChecks(), 2)
	judging(d, "The suite is still running in the background.")

	reason := "the check judge failed: unfinished (1.00)"
	cmds, _ := d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Reason: crew.NewCheckReason(reason)})
	for _, c := range cmds {
		if _, ok := c.(core.RunCheck); ok {
			t.Fatalf("a check ran after one failed: %#v", cmds)
		}
	}
	if got := failures(t, cmds); len(got) != 1 || got[0].Action != "development" {
		t.Fatalf("failures = %#v, want development failed", got)
	}
	d.wantReason("74", "development", reason)
}

// Covers AE2: the action succeeds once its last check passed.
func TestAnActionSucceedsOnceEveryCheckPassed(t *testing.T) {
	d := newDriver(t, twoChecks(), 2)
	judging(d, "PR #20 is open.")

	d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Passed: true, Reason: checkPassed})
	cmds, _ := d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Passed: true, Reason: checkPassed})
	wantCommands(t, cmds, core.Move{IssueID: issueID("74"), From: inProgress, To: readyToReview})
}

// Covers AE9: a session that fails runs none of its checks.
func TestAFailedSessionRunsNoneOfItsChecks(t *testing.T) {
	d := newDriver(t, twoChecks(), 2)
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("tests fail")})
	for _, c := range cmds {
		if _, ok := c.(core.RunCheck); ok {
			t.Fatalf("a failed session ran a check: %#v", cmds)
		}
	}
}

// A stop while the first of two checks runs stops it, and the action ends
// stopped without the second, even when the first passed as it stopped.
func TestAStopWhileTheFirstCheckRunsEndsTheActionWithoutTheSecond(t *testing.T) {
	d := newDriver(t, twoChecks(), 2)
	judging(d, "")

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopCheck{IssueID: issueID("74"), Run: d.run(issueID("74")), Action: "development"})
	cmds, _ = d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Passed: true, Reason: checkPassed})
	for _, c := range cmds {
		if _, ok := c.(core.RunCheck); ok {
			t.Fatalf("a check started after a stop: %#v", cmds)
		}
	}
	if got := failures(t, cmds); len(got) != 1 || got[0].Action != "development" {
		t.Fatalf("failures = %#v, want development failed", got)
	}
	d.wantReason("74", "development", "crew stopped")
}

// R6: while its second check runs, an action's status shows how its first
// ended; once it ended, both, in order.
func TestAnActionsStatusShowsEveryCheckThatRan(t *testing.T) {
	d := newStatusDriver(t, twoChecks(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	devStarted := started(t, d.m, "development")
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	judged := crew.CheckResult{
		Name: "judge", Passed: true, Reason: crew.NewCheckReason("the check judge passed: done (0.97)"),
	}
	d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Passed: true, Reason: judged.Reason})

	cmds, _ := d.send(core.Tick{})
	got := statusOf(t, cmds, "74")
	want := []crew.ActionStatus{
		{Name: "acceptance", State: crew.ActionSucceeded{}},
		{Name: "development", State: crew.ActionRunning{Started: devStarted}, Checks: []crew.CheckResult{judged}},
	}
	if got.Progress() != (crew.StatusRunning{}) || !reflect.DeepEqual(got.Actions(), want) {
		t.Fatalf("status while the second check runs: %#v\nwant actions %#v", got, want)
	}

	d.wrote("74")
	closes := crew.CheckResult{
		Name: "pr-closes-issue", Passed: true, Reason: crew.NewCheckReason("the check pr-closes-issue passed"),
	}
	cmds, _ = d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Passed: true, Reason: closes.Reason})
	ended := statusOf(t, cmds, "74")
	dev := ended.Actions()[1]
	if dev.State != (crew.ActionSucceeded{}) || !reflect.DeepEqual(dev.Checks, []crew.CheckResult{judged, closes}) {
		t.Fatalf("development once ended: %#v", dev)
	}
}
