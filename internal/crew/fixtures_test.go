package crew

import (
	"slices"
	"testing"
	"time"
)

// The fixtures of the rule run's tests: one issue, #9, taken by the rule
// implement, whose actions are development, with the checks build and test,
// and review, without checks.

const (
	labelReady   State     = "crew:ready"
	labelRunning State     = "crew:in progress"
	labelDone    State     = "crew:done"
	labelFailed  State     = "crew:needs attention"
	testRun      RuleRunID = "run-2"
)

var (
	t0      = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	testID  = IssueID{Repository: "R_1", Key: "9"}
	usage   = Usage{Cost: Some(0.5), Turns: Some(3)}
	stopEnd = EndFailed{Reason: NewSessionText("crew stopped"), Cause: CauseStopped}
	foundPR = PullRequestFound{Ref: "#45", URL: "https://example.com/pull/45"}
)

// at returns the time n minutes after t0.
func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

// eh returns the head of an event of the test run at minute n.
func eh(n int) EventHead {
	return EventHead{Run: testRun, At: at(n), IssueID: testID, IssueRef: "#9", Rule: "implement"}
}

// fh returns the head of a fact of the test run at minute n.
func fh(n int) FactHead { return FactHead{Run: testRun, At: at(n)} }

func testIssue() IssueData {
	return IssueData{
		ID: testID, Ref: "#9", Title: "Issue 9", URL: "https://example.com/issues/9", States: []State{labelReady},
	}
}

// mustPrompt returns the parsed prompt text of action, which must parse.
func mustPrompt(action ActionName, text string) Prompt {
	p, err := ParsePrompt(action, text)
	if err != nil {
		panic(err)
	}
	return p
}

// badPrompt renders for the sample issue's title, and fails on the shorter
// "Issue 9".
func badPrompt(action ActionName) Prompt {
	return mustPrompt(action, "Fix {{index .Issue.Title 11}}")
}

// renderError returns the reason a prompt that does not render gives.
func renderError(action ActionName) SessionText {
	_, err := badPrompt(action).Render(NewIssue(testIssue()))
	if err == nil {
		panic("the bad prompt rendered")
	}
	return NewSessionText(err.Error())
}

// definition returns the test rule: development with the checks build and
// test, then review without checks.
func definition() RunDefinition {
	return RunDefinition{Rule: Rule{
		Name:   "implement",
		Labels: Labels{Ready: labelReady, Running: labelRunning, Success: labelDone, Failure: labelFailed},
		Actions: []Action{
			{
				Name: "development", Prompt: mustPrompt("development", "Implement {{.Issue.Ref}}"),
				Checks: []Check{{Name: "build", Script: "make"}, {Name: "test", Script: "make test"}},
			},
			{Name: "review", Prompt: mustPrompt("review", "Review {{.Issue.Ref}}")},
		},
	}}
}

func ws(action ActionName) Workspace {
	return Workspace{Name: WorkspaceName("issue-9-" + action), Branch: "crew/issue-9-" + string(action)}
}

func logOf(action ActionName) string { return ".crew/logs/issue-9-" + string(action) + ".log" }

func resumePoint() ResumePoint {
	return ResumePoint{Workspace: ws("development"), Log: logOf("development"), Reason: NewSessionText("tests fail")}
}

// opened returns the new workspace an action opened at minute 3.
func opened(action ActionName) Optional[OpenedWorkspace] {
	return Some(OpenedWorkspace{Workspace: ws(action), Log: logOf(action), Opened: at(3)})
}

func taken(actions ...ActionTaken) RunEvent {
	return RunTaken{EventHead: eh(0), Issue: testIssue(), From: labelReady, To: labelRunning, Actions: actions}
}

func bothActions() []ActionTaken { return []ActionTaken{{Name: "development"}, {Name: "review"}} }

func takeMoved() RunEvent { return TakeMoved{EventHead: eh(1), From: labelReady, To: labelRunning} }

// preparing is a run whose take landed at minute 1 and whose two actions'
// new workspaces were asked for at minute 2.
func preparing() []RunEvent {
	return []RunEvent{
		taken(bothActions()...), takeMoved(),
		ActionWorkspaceAsked{EventHead: eh(2), Action: "development"},
		ActionWorkspaceAsked{EventHead: eh(2), Action: "review"},
	}
}

// reopening is a run whose development resumes in its failed run's
// workspace.
func reopening() []RunEvent {
	return []RunEvent{
		taken(ActionTaken{Name: "development", Resume: Some(resumePoint())}, ActionTaken{Name: "review"}), takeMoved(),
		ActionWorkspaceAsked{EventHead: eh(2), Action: "development", Reopen: Some(ws("development"))},
		ActionWorkspaceAsked{EventHead: eh(2), Action: "review"},
	}
}

// starting is action's workspace ready at minute 3 and its session asked.
func starting(action ActionName) []RunEvent {
	return []RunEvent{
		ActionOpened{EventHead: eh(3), Action: action, Workspace: ws(action), Log: logOf(action)},
		ActionSessionAsked{EventHead: eh(3), Action: action},
	}
}

// inSession is action's session started at minute 4.
func inSession(action ActionName) []RunEvent {
	return append(starting(action), ActionSessionStarted{
		EventHead: eh(4), Action: action, Workspace: ws(action), Log: logOf(action),
	})
}

// inChecks is development's session succeeded at minute 5, and its build
// check running.
func inChecks() []RunEvent {
	return append(inSession("development"),
		ActionSessionEnded{EventHead: eh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
		ActionCheckAsked{EventHead: eh(5), Action: "development", Check: "build"},
	)
}

// reviewEnded is review's session ended with end at minute 6.
func reviewEnded(end ActionEnd) []RunEvent {
	return append(inSession("review"),
		ActionSessionEnded{EventHead: eh(6), Action: "review", Outcome: end.Outcome(), Usage: usage},
		ActionEnded{
			EventHead: eh(6), Action: "review", End: end, Workspace: opened("review"),
			SessionStarted: Some(at(4)), Usage: usage,
		},
	)
}

func succeeded(reason string) Outcome {
	return Outcome{Succeeded: true, Reason: NewSessionText(reason)}
}

func failedOutcome(reason string) Outcome { return Outcome{Reason: NewSessionText(reason)} }

func seq(parts ...[]RunEvent) []RunEvent { return slices.Concat(parts...) }

func stopped(n int) []RunEvent { return []RunEvent{RunStopped{EventHead: eh(n)}} }

// given builds a run from events, failing the test when one is refused.
func given(tb testing.TB, events []RunEvent) RuleRun {
	tb.Helper()
	var run RuleRun
	for _, e := range events {
		var err error
		if run, err = Apply(run, e); err != nil {
			tb.Fatalf("Apply(%#v) = %v", e, err)
		}
	}
	return run
}

// developmentEnded is development's end at minute n, after its session
// started at minute 4.
func developmentEnded(n int, end ActionEnd) ActionEnded {
	return ActionEnded{
		EventHead: eh(n), Action: "development", End: end, Workspace: opened("development"),
		SessionStarted: Some(at(4)), Usage: usage,
	}
}

// failure returns the failure of action, which had its workspace and log.
func failure(action ActionName) ActionFailure {
	return ActionFailure{Action: action, Workspace: ws(action).Name, Log: logOf(action)}
}
