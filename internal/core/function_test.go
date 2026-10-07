package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// checkUse is the place of the config the function action check is built
// for.
const checkUse crew.FunctionUse = "rules.implement.actions[1]"

// checkSpec is a call of the function pr-open for use, whose text
// parameter title is a template over the issue.
func checkSpec(t *testing.T, use crew.FunctionUse) crew.FunctionSpec {
	t.Helper()
	title, err := crew.ParseParameterTemplate("title", "PR for {{.Issue.Ref}}")
	if err != nil {
		t.Fatal(err)
	}
	return crew.FunctionSpec{
		Function: "pr-open", Use: use, Texts: []crew.TextParameter{{Name: "title", Template: title}},
	}
}

// checking is botRules with implement's session development, acting as
// developer, followed by the function action check.
func checking(t *testing.T) []crew.Rule {
	t.Helper()
	rules := botRules()
	rules[1].Actions = append(rules[1].Actions, crew.Action{Name: "check", Kind: checkSpec(t, checkUse)})
	return rules
}

// checked runs #74 under checking until development passed, and returns
// the commands that follow: check's call.
func checked(t *testing.T) (*driver, []core.Command) {
	t.Helper()
	d := botsDriver(t, checking(t), crewBots(nil))
	d.running(issue("74", 1, ready))
	return d, d.ended("74", "development", succeeded)
}

// functionCall is the call of check on issue key, in the workspace space
// gives its run of rule, acting as bot.
func functionCall(key string, rule crew.RuleName, use crew.FunctionUse, bot crew.BotName) core.FunctionCall {
	w := space(key, rule)
	return core.FunctionCall{
		Use: use, Name: "check", Function: "pr-open", Texts: map[string]string{"title": "PR for #" + key},
		Dir: w.Dir, Log: w.Log, Rule: rule, IssueRef: "#" + key, IssueURL: "https://example.com/issues/" + key,
		Branch: w.Branch, Bot: bot,
	}
}

// returned is how a function that returned v ended.
func returned(v crew.Verdict) crew.FunctionOutcome {
	return crew.FunctionOutcome{Verdict: crew.Some(v), Reason: crew.NewShellReason("returned " + string(v))}
}

// Covers R29, KTD-F12, KTD13: a function action runs with its text
// parameters rendered for the issue, in the run's workspace, acting as
// the latest session's bot, and its verdict leads the run.
func TestAFunctionActionRunsWithItsTextsRenderedAsTheLatestSessionsBot(t *testing.T) {
	d, cmds := checked(t)

	wantCommands(t, cmds, core.RunFunction{
		IssueID: issueID("74"), Run: d.run(issueID("74")), Action: "check",
		Call: functionCall("74", "implement", checkUse, "developer"),
	})
	hasEvent(t, d.events, crew.ActionFunctionAsked{EventHead: d.runHead("74"), Action: "check",
		Bot: crew.Bot{Name: "developer"}})
	check := core.RunningAction{IssueRef: "#74", Rule: "implement", Action: "check"}
	if got := entry(t, d, "developer").Running; !reflect.DeepEqual(got, []core.RunningAction{check}) {
		t.Fatalf("running on developer = %#v, want check", got)
	}

	moved, _ := d.send(core.FunctionEnded{IssueID: issueID("74"), Action: "check", Outcome: returned(crew.Passed)})
	wantCommands(t, moved, core.Move{IssueID: issueID("74"), From: inProgress, To: readyToReview})
	d.wantReason("74", "check", "returned passed")
}

// Covers R49, KTD-F5: a function that returned no verdict fails its
// action with crew's reason, and the run ends through failed.
func TestAFunctionActionThatReturnedNoVerdictFailsTheRun(t *testing.T) {
	d, _ := checked(t)

	reason := "the function action check failed: no pull request"
	cmds, _ := d.send(core.FunctionEnded{IssueID: issueID("74"), Action: "check", Outcome: crew.FunctionOutcome{
		Reason: crew.NewShellReason(reason),
	}})
	wantCommands(t, cmds, failureOf("74", "implement", "check"))
	d.wantReason("74", "check", reason)
}

// Covers KTD-S17: a function action shows as the running action, with
// the time crew asked for its function.
func TestAFunctionActionShowsAsTheRunningAction(t *testing.T) {
	d, _ := checked(t)
	asked := d.now

	iv := heldView(t, d.m, "74")
	want := []core.Phase{core.PhaseEnded, core.PhaseRunning}
	if !reflect.DeepEqual(phases(iv), want) || !iv.Actions[1].Started.Equal(asked) {
		t.Fatalf("view: got %#v, want check running since %v", iv, asked)
	}
}

// Covers R53: a stop asks the running function to stop, and its end,
// even with a verdict, counts as stopped.
func TestAStopWhileAFunctionActionRunsStopsItAndEndsThroughFailed(t *testing.T) {
	d, _ := checked(t)

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopFunction{IssueID: issueID("74"), Run: d.run(issueID("74")), Action: "check"})

	cmds, _ = d.send(core.FunctionEnded{IssueID: issueID("74"), Action: "check", Outcome: returned(crew.Passed)})
	wantCommands(t, cmds, failureOf("74", "implement", "check"))
}

// Covers R25, KTD-F6: a rule whose actions are all functions asks for no
// worktree: its function runs with no directory, log or branch.
func TestARuleOfFunctionsRunsWithoutAWorktree(t *testing.T) {
	rules := promoted()[1:]
	const use crew.FunctionUse = "rules.promote triage.actions[0]"
	rules[0].Actions = []crew.Action{{Name: "check", Kind: checkSpec(t, use)}}
	d := newDriver(t, rules, 2)
	cmds, _ := d.poll(issue("1", 1, triageDone))

	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantCommands(t, cmds, core.RunFunction{IssueID: issueID("1"), Run: d.run(issueID("1")), Action: "check",
		Call: core.FunctionCall{
			Use: use, Name: "check", Function: "pr-open", Texts: map[string]string{"title": "PR for #1"},
			Rule: "promote triage", IssueRef: "#1", IssueURL: "https://example.com/issues/1",
		}})
	cmds, _ = d.send(core.FunctionEnded{IssueID: issueID("1"), Action: "check", Outcome: returned(crew.Passed)})
	wantCommands(t, cmds, promoteMove())
}

// Covers KTD18: a function action's start is one of the events a resume
// depends on.
func TestAFunctionActionsStartSaysSoWhenItFailsToWrite(t *testing.T) {
	d := &driver{t: t, m: core.New(checking(t), 2, core.Journaling(nil)), now: t0}
	d.running(issue("74", 1, ready))
	d.ended("74", "development", succeeded)
	var asked crew.ActionFunctionAsked
	for _, e := range d.recorded {
		if a, ok := e.(crew.ActionFunctionAsked); ok {
			asked = a
		}
	}

	_, events := d.send(core.RecordFailed{Event: asked, Reason: "disk full"})
	wantEvents(t, events, core.RunNotRecorded{
		At: d.now, IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Action: "check",
		What: "the start of check", Reason: "disk full",
	})
}

// checkRouteUse is the place of the config the function step check is
// built for.
const checkRouteUse crew.FunctionUse = "rules.implement.routes.failed[0]"

// functionRouted is the draft rules with implement's failed route calling
// the function step check before its move.
func functionRouted(t *testing.T) []crew.Rule {
	t.Helper()
	rules := draft()
	rules[0].Routes[1].Steps = []crew.Step{
		crew.FunctionStep{Name: "check", Function: checkSpec(t, checkRouteUse)},
		crew.MoveStep{To: needsAttention},
	}
	return rules
}

// stepChecked runs #1 under functionRouted until its failed route calls
// check, which it returns.
func stepChecked(t *testing.T) (*driver, []core.Command) {
	t.Helper()
	d := newDriver(t, functionRouted(t), 2)
	d.running(issue("1", 1, ready))
	return d, d.ended("1", "acceptance", failed("tests fail"))
}

// stepCall is the RunStepFunction of check, the first step of #1's route.
func (d *driver) stepCall() core.RunStepFunction {
	return core.RunStepFunction{
		IssueID: issueID("1"), Run: d.run(issueID("1")), Step: 0, Call: functionCall("1", "implement", checkRouteUse, ""),
	}
}

// Covers R14, R16: a route's function step that returns anything but
// passed is a failed step, and the route goes on.
func TestARouteFunctionStepThatReturnsBlockedFailsAndTheRouteGoesOn(t *testing.T) {
	d, cmds := stepChecked(t)
	wantCommands(t, cmds, d.stepCall())

	moved, events := d.send(core.StepFunctionEnded{IssueID: issueID("1"), Step: 0, Outcome: returned("blocked")})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepFailed{Reason: returned("blocked").Reason}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
}

// Covers R53: a stop stops the route's function step that runs, records
// it stopped, and lets the route's final move land.
func TestAStopWhileARouteFunctionStepRunsStopsIt(t *testing.T) {
	d, _ := stepChecked(t)

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopStepFunction{IssueID: issueID("1"), Run: d.run(issueID("1")), Step: 0})

	stopped := crew.FunctionOutcome{Reason: crew.NewShellReason("the route's function step check was stopped")}
	moved, events := d.send(core.StepFunctionEnded{IssueID: issueID("1"), Step: 0, Outcome: stopped})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepStopped{Reason: stopped.Reason}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
}

// Covers R52, KTD12: time-up never cuts a route's function step short;
// crew stops once it ended and the route's move landed.
func TestTimeUpWaitsForARoutesFunctionStepBeforeItStops(t *testing.T) {
	d, _ := stepChecked(t)

	cmds, _ := d.send(core.TimeUp{Limit: limit})
	wantCommands(t, cmds)
	if d.m.Stopped() {
		t.Fatal("stopped while a function step runs")
	}

	moved, _ := d.send(core.StepFunctionEnded{IssueID: issueID("1"), Step: 0, Outcome: returned(crew.Passed)})
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
	_, events := d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}
