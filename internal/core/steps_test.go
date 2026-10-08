package core_test

import (
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// shellRouted is the draft rules with implement's failed route running the
// shell steps notify and cleanup before its move.
func shellRouted() []crew.Rule {
	rules := draft()
	rules[0].Routes[1].Steps = []crew.Step{
		crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}},
		crew.ShellStep{Name: "cleanup", Shell: crew.ShellSpec{Script: "./cleanup"}},
		crew.MoveStep{To: needsAttention},
	}
	return rules
}

// latestSession returns the action of the last session of issue key's last
// run that started, as d's events show it; empty when none did.
func (d *driver) latestSession(key string) crew.ActionName {
	run := d.run(issueID(key))
	for _, e := range slices.Backward(d.events) {
		if s, ok := e.(crew.ActionSessionStarted); ok && s.Run == run {
			return s.Action
		}
	}
	return ""
}

// stepShell is the RunStepShell of the shell step at index step of #1's
// last run, which runs the shell action name's script, in the workspace
// space gives the run, after its latest session.
func (d *driver) stepShell(step int, name crew.ActionName, script string) core.RunStepShell {
	d.t.Helper()
	const key = "1"
	rule := d.rule(key)
	w := space(key, rule)
	return core.RunStepShell{IssueID: issueID(key), Run: d.run(issueID(key)), Step: step, Script: core.Script{
		Dir: w.Dir, Name: name, Command: script, Log: w.Log, Rule: rule, IssueRef: "#" + key,
		IssueURL: "https://example.com/issues/" + key, Branch: w.Branch, Session: d.latestSession(key),
	}}
}

// commentID returns the ID of the comment in cmds.
func commentID(t *testing.T, cmds []core.Command) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if comment, ok := c.(core.Comment); ok {
			return comment.ID
		}
	}
	t.Fatalf("no comment in %#v", cmds)
	return 0
}

// closeID returns the ID of the close in cmds.
func closeID(t *testing.T, cmds []core.Command) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if closed, ok := c.(core.Close); ok {
			return closed.ID
		}
	}
	t.Fatalf("no close in %#v", cmds)
	return 0
}

// commented is the draft rules with implement's failed route commenting,
// reporting, then moving.
func commented(t *testing.T) []crew.Rule {
	t.Helper()
	tmpl, err := crew.ParseCommentTemplate(crew.FailedRoute, "{{.Action}} stopped as {{.Verdict}}. Its log is {{.Log}}.")
	if err != nil {
		t.Fatal(err)
	}
	rules := draft()
	rules[0].Routes[1].Steps = []crew.Step{
		crew.CommentStep{Template: tmpl}, crew.ReportStep{}, crew.MoveStep{To: needsAttention},
	}
	return rules
}

// Covers KTD9: a route's tracker steps go one at a time through the
// outbox, so the run's lane never holds two of them.
func TestARoutesStepsRunOneAtATime(t *testing.T) {
	d := newDriver(t, commented(t), 2)
	d.running(issue("1", 1, ready))

	comment := d.ended("1", "acceptance", failed("tests fail"))
	wantCommands(t, comment, core.Comment{
		IssueID: issueID("1"), Body: "acceptance stopped as failed. Its log is .crew/logs/issue-1-implement.log.",
	})
	report, events := d.send(core.CallResult{ID: commentID(t, comment), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))
	wantCommands(t, report, failureOf("1", "implement", "acceptance"))
	moved, _ := d.send(core.CallResult{ID: reportID(t, report, "1"), Result: core.ResultDone})
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
	d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	wantHeld(t, d.m)
}

func TestAnOwedCommentIsRetriedAtTheNextTickAndTheStepsAfterItWait(t *testing.T) {
	d := newDriver(t, commented(t), 2)
	d.running(issue("1", 1, ready))
	comment := d.ended("1", "acceptance", failed("tests fail"))

	cmds, events := d.send(core.CallResult{ID: commentID(t, comment), Result: core.ResultFailed, Reason: "timeout"})
	wantCommands(t, cmds)
	owed := core.Call{Kind: core.CallComment, IssueID: issueID("1"), IssueRef: "#1"}
	wantEvents(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})
	wantOwed(t, d.m, owed)
	wantClaim(t, d.m, "1", core.ClaimOwed)

	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry, core.ListIssues{States: draftListing}, noIDs(comment)[0])
	report, _ := d.send(core.CallResult{ID: commentID(t, retry), Result: core.ResultDone})
	wantCommands(t, report, failureOf("1", "implement", "acceptance"))
	wantOwed(t, d.m)
}

// Covers R16: a shell step that fails is recorded and the route goes on.
func TestAShellStepThatFailsIsRecordedAndTheRouteGoesOn(t *testing.T) {
	d := newDriver(t, shellRouted(), 2)
	d.running(issue("1", 1, ready))
	notify := d.ended("1", "acceptance", failed("tests fail"))
	wantCommands(t, notify, d.stepShell(0, "notify", "./notify"))

	cleanup, events := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: exited(2)})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepFailed{Reason: exited(2).Reason}))
	wantCommands(t, cleanup, d.stepShell(1, "cleanup", "./cleanup"))

	moved, events := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 1, Outcome: exited(0)})
	hasEvent(t, events, d.stepEnded("1", 1, crew.StepRan{Reason: exited(0).Reason}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
}

// Covers KTD9: a route's shell step of a run without a worktree names no
// directory, log or branch; the engine runs it in a temporary directory.
func TestARouteShellStepOfARunWithoutAWorktreeNamesNone(t *testing.T) {
	rules := promoted()[1:]
	rules[0].Routes[0].Steps = []crew.Step{
		crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}},
		crew.MoveStep{To: developmentReady},
	}
	d := newDriver(t, rules, 2)
	cmds, _ := d.poll(issue("1", 1, triageDone))

	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantCommands(t, cmds, core.RunStepShell{IssueID: issueID("1"), Run: d.run(issueID("1")), Step: 0, Script: core.Script{
		Name: "notify", Command: "./notify", Rule: "promote triage", IssueRef: "#1",
		IssueURL: "https://example.com/issues/1",
	}})
}

// closing is promote triage, the rule without actions, closing the issue
// through its passed route.
func closing() []crew.Rule {
	rules := promoted()[1:]
	rules[0].Routes[0].Steps = []crew.Step{crew.CloseStep{}}
	return rules
}

// Covers R51, KTD-S14: a close waits for the pull request report in flight,
// so that report cannot put a crew label back on a pull request the close
// took them off.
func TestARuleWithoutActionsClosesOnlyOnceTheTakesPullRequestReportSettled(t *testing.T) {
	d := newPullRequestDriver(t, closing())
	take, _ := d.poll(issue("1", 1, triageDone))

	landed, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantReport(t, pullRequestReportOf(t, landed), reportOf(triagePromoting))
	for _, c := range landed {
		if _, ok := c.(core.Close); ok {
			t.Fatalf("closed with the take's report in flight: %#v", landed)
		}
	}

	closed, _ := d.answerPullRequests("1", core.ResultDone)
	wantCommands(t, closed, core.Close{IssueID: issueID("1"), From: triagePromoting})
	_, events := d.send(core.CallResult{ID: closeID(t, closed), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))
	wantHeld(t, d.m)
}

// Covers KTD-S14: a close drops its issue's owed pull request report, which
// would otherwise put the take's label back on its pull requests.
func TestACloseDropsAnOwedPullRequestReportOfTheTake(t *testing.T) {
	d := newPullRequestDriver(t, closing())
	take, _ := d.poll(issue("1", 1, triageDone))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})

	closed, _ := d.answerPullRequests("1", core.ResultFailed)
	wantCommands(t, closed, core.Close{IssueID: issueID("1"), From: triagePromoting})
	wantOwed(t, d.m)

	d.send(core.CallResult{ID: closeID(t, closed), Result: core.ResultDone})
	cmds, _ := d.send(core.Tick{})
	noPullRequestReport(t, cmds)
	_, events := d.send(core.StopRequested{})
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("a stop waits for a pull request report the close dropped")
	}
}

// A report step of a run without actions names no action that ended it.
func TestAReportStepOfARunWithoutActionsNamesNoAction(t *testing.T) {
	rules := promoted()[1:]
	rules[0].Routes[0].Steps = []crew.Step{crew.ReportStep{}, crew.MoveStep{To: developmentReady}}
	d := newDriver(t, rules, 2)
	cmds, _ := d.poll(issue("1", 1, triageDone))

	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantCommands(t, cmds, core.ReportFailure{Report: crew.FailureReport{
		IssueID: issueID("1"), IssueRef: "#1", Rule: "promote triage", Route: crew.PassedRoute,
	}})
}

// Covers KTD4: the answered rule's return step moves #1 from the rule's
// running label to the label its check found, and its end names both.
func TestAReturnStepMovesTheItemFromTheRunningLabelToTheCheckedLabel(t *testing.T) {
	d := answeredDriver(t)
	d.checking()
	moved, _ := d.send(returnRead(unsureQuestion("boss"), crew.Comment{Author: "bob", Body: "yes"}))
	wantCommands(t, unrecorded(moved), core.Move{IssueID: issueID("1"), From: answeredRunning, To: depsReady})

	_, events := d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})

	h := d.runHead("1")
	hasEvent(t, events, core.RouteStepEnded{
		At: h.At, IssueID: issueID("1"), IssueRef: "#1", Rule: "answered", Route: crew.PassedRoute, Step: 0,
		Plan: crew.StepPlan{Kind: crew.StepMove, To: depsReady}, From: answeredRunning, Outcome: crew.StepLanded{},
	})
}
