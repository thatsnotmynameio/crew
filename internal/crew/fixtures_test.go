package crew

import (
	"slices"
	"testing"
	"time"
)

// The fixtures of the rule run's tests: one issue, #9, taken by the rule
// implement, whose actions run one after another in the run's one
// workspace: the shell action install, the session lfg, which acts as the
// bot crew-developer and may report blocked, and the shell action judge,
// whose exit status 3 is needs_person.

const (
	labelReady   State     = "crew:ready"
	labelRunning State     = "crew:in progress"
	labelDone    State     = "crew:done"
	labelFailed  State     = "crew:needs attention"
	testRun      RuleRunID = "run-2"
	runLog                 = ".crew/logs/issue-9-implement.log"
)

var (
	t0     = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	testID = IssueID{Repository: "R_1", Key: "9"}
	usage  = Usage{Cost: Some(0.5), Turns: Some(3)}
	// unstarted is the end of an action a stop reached before it started.
	unstarted = EndFailed{Reason: NewSessionText("crew stopped"), Cause: CauseStoppedBeforeStart}
	foundPR   = PullRequestFound{Ref: "#45", URL: "https://example.com/pull/45"}
	developer = Bot{Name: "crew-developer"}
	toFailed  = ToRoute{Route: FailedRoute}
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

// sequence returns the test rule: install, lfg, then judge, with a route
// for each verdict they map.
func sequence() RunDefinition {
	return RunDefinition{Rule: Rule{
		Name:   "implement",
		Labels: Labels{Ready: labelReady, Running: labelRunning},
		Actions: []Action{
			{Name: "install", Kind: ShellSpec{Script: "make deps"}},
			{
				Name: "lfg",
				Kind: SessionSpec{
					Agent:  Agent{Name: "claude", Harness: "claude", Bot: developer.Name},
					Prompt: mustPrompt("lfg", "Implement {{.Issue.Ref}}"), Bot: developer,
				},
				On: On{"blocked": ToRoute{Route: "blocked"}},
			},
			{
				Name: "judge", Kind: ShellSpec{Script: "./judge", Verdicts: map[int]Verdict{3: "needs_person"}},
				On: On{"needs_person": ToRoute{Route: "needs-person"}},
			},
		},
		Routes: []Route{
			{Name: PassedRoute, Steps: []Step{MoveStep{To: labelDone}}},
			{Name: FailedRoute, Steps: []Step{ReportStep{}, MoveStep{To: labelFailed}}},
			{Name: "blocked", Steps: []Step{MoveStep{To: "crew:blocked"}}},
			{Name: "needs-person", Steps: []Step{MoveStep{To: "crew:needs person"}}},
		},
	}}
}

// runWS is the run's one workspace.
func runWS() Workspace { return Workspace{Name: "issue-9-implement", Branch: "crew/issue-9-implement"} }

// opened is the run's new workspace, ready at minute 2.
func opened() OpenedWorkspace { return OpenedWorkspace{Workspace: runWS(), Log: runLog, Opened: at(2)} }

// lfgLatest is lfg's session as the latest session of a run.
var lfgLatest = LatestSession{Action: "lfg", Bot: developer}

// startAt is the start of a run that resumes at action the work of a run
// that failed, whose latest session was lfg's.
func startAt(action ActionName) StartAt {
	return StartAt{
		Workspace: runWS(), Log: runLog, Action: action, Route: FailedRoute, Reason: NewSessionText("tests fail"),
		Session: Some(lfgLatest),
	}
}

// taken is the take of the test rule's three actions at minute 0.
func taken() RunEvent {
	return RunTaken{
		EventHead: eh(0), Issue: testIssue(), From: labelReady, To: labelRunning,
		Actions: []ActionName{"install", "lfg", "judge"},
	}
}

// takenWithoutActions is the take of a rule without actions.
func takenWithoutActions() RunEvent {
	return RunTaken{EventHead: eh(0), Issue: testIssue(), From: labelReady, To: labelRunning}
}

// resumedAt is the take of a run that resumes at action.
func resumedAt(action ActionName) RunEvent {
	e, _ := taken().(RunTaken)
	e.Start = startAt(action)
	return e
}

func takeMoved() RunEvent { return TakeMoved{EventHead: eh(1), From: labelReady, To: labelRunning} }

// asking is a run whose take landed at minute 1 and asked for a new
// workspace.
func asking() []RunEvent {
	return []RunEvent{taken(), takeMoved(), WorkspaceAsked{EventHead: eh(1)}}
}

// reopening is a run that resumes at lfg, whose take landed at minute 1
// and asked to reopen the workspace it resumes.
func reopening() []RunEvent {
	return []RunEvent{resumedAt("lfg"), takeMoved(), WorkspaceAsked{EventHead: eh(1), Reopen: Some(runWS())}}
}

// installing is the run's workspace ready at minute 2, and install asked.
func installing() []RunEvent {
	return append(asking(),
		WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog},
		ActionShellAsked{EventHead: eh(2), Action: "install"},
	)
}

// exited returns how a script that exited with status ended, saying reason.
func exited(status int, reason string) ShellOutcome {
	return ShellOutcome{Status: Some(status), Reason: NewShellReason(reason)}
}

// passedEnd is the end of an action that passed, saying reason.
func passedEnd(n int, action ActionName, reason string) ActionEnded {
	return ActionEnded{
		EventHead: eh(n), Action: action, End: EndSucceeded{Reason: NewSessionText(reason)},
		Verdict: Passed, Target: Next{},
	}
}

// starting is install passed at minute 3, and lfg asked.
func starting() []RunEvent {
	return append(installing(),
		ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
		passedEnd(3, "install", "install passed"),
		ActionSessionAsked{EventHead: eh(3), Action: "lfg"},
	)
}

// inSession is lfg's session started at minute 4, as crew-developer.
func inSession() []RunEvent {
	return append(starting(), ActionSessionStarted{EventHead: eh(4), Action: "lfg", Bot: developer})
}

// lfgPassed is lfg's end at minute 5, after its session succeeded.
func lfgPassed() ActionEnded {
	e := passedEnd(5, "lfg", "done")
	e.SessionStarted, e.Usage = Some(at(4)), usage
	return e
}

// judging is lfg passed at minute 5, and judge asked as crew-developer.
func judging() []RunEvent {
	return append(inSession(),
		ActionSessionEnded{EventHead: eh(5), Action: "lfg", Outcome: succeeded("done"), Usage: usage},
		lfgPassed(),
		ActionShellAsked{EventHead: eh(5), Action: "judge", Bot: developer},
	)
}

// judgePassed is judge passed at minute 6, the run's last action.
func judgePassed() []RunEvent {
	return append(judging(),
		ActionShellEnded{EventHead: eh(6), Action: "judge", Outcome: exited(0, "judge passed")},
		passedEnd(6, "judge", "judge passed"),
	)
}

// passedAll is the run through passed at minute 6, whose move to done is
// asked.
func passedAll() []RunEvent {
	return append(judgePassed(), chose(6, PassedRoute, "judge"), asked(6, 0))
}

// lookingUp is the run through passed at minute 6, whose pull requests are
// looked up before its first step.
func lookingUp() []RunEvent {
	return append(judgePassed(), chose(6, PassedRoute, "judge"), RunLookupAsked{EventHead: eh(6)})
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

// failure returns the failure of action, in the run's workspace and log.
func failure(action ActionName) ActionFailure {
	return ActionFailure{Action: action, Workspace: runWS().Name, Log: runLog}
}

// The function fixtures: the test rule with check, a call of the function
// check-pr whose text parameter title renders the issue's ref and which
// declares blocked, in place of judge, sending blocked to the route
// blocked.

// mustParameter returns the text parameter name, parsed from text, which
// must parse.
func mustParameter(name, text string) TextParameter {
	tmpl, err := ParseParameterTemplate(name, text)
	if err != nil {
		panic(err)
	}
	return TextParameter{Name: name, Template: tmpl}
}

// checkSpec is check's call.
func checkSpec() FunctionSpec {
	return FunctionSpec{
		Function: "check-pr", Use: "rules.implement.actions[2]",
		Texts: []TextParameter{mustParameter("title", "Fixes {{.Issue.Ref}}")}, Verdicts: []Verdict{"blocked"},
	}
}

// badCheckSpec is check's call with a title that renders for the sample
// issue's title, and fails on the shorter "Issue 9".
func badCheckSpec() FunctionSpec {
	s := checkSpec()
	s.Texts = []TextParameter{mustParameter("title", "{{slice .Issue.Title 0 10}}")}
	return s
}

// checkError returns the reason a text that does not render gives.
func checkError() string {
	_, err := badCheckSpec().RenderTexts(NewIssue(testIssue()))
	if err == nil {
		panic("the bad text rendered")
	}
	return err.Error()
}

// checkAction is the action check, calling spec.
func checkAction(spec FunctionSpec) Action {
	return Action{Name: "check", Kind: spec, On: On{"blocked": ToRoute{Route: "blocked"}}}
}

// withCheck replaces judge with check.
func withCheck(d RunDefinition) RunDefinition {
	d.Rule.Actions = slices.Clone(d.Rule.Actions)
	d.Rule.Actions[2] = checkAction(checkSpec())
	return d
}

// withBadCheck replaces judge with check, whose text does not render for
// the test issue.
func withBadCheck(d RunDefinition) RunDefinition {
	d = withCheck(d)
	d.Rule.Actions[2] = checkAction(badCheckSpec())
	return d
}

// checkResumesSelf makes check resume at itself.
func checkResumesSelf(d RunDefinition) RunDefinition {
	d = withCheck(d)
	spec := checkSpec()
	spec.ResumeSelf = true
	d.Rule.Actions[2] = checkAction(spec)
	return d
}

// onlyCheck makes check the test rule's only action.
func onlyCheck(d RunDefinition) RunDefinition {
	d.Rule.Actions = []Action{checkAction(checkSpec())}
	return d
}

// checkTake is the take of install, lfg and check at minute 0.
func checkTake() RunEvent {
	e, _ := taken().(RunTaken)
	e.Actions = []ActionName{"install", "lfg", "check"}
	return e
}

// onlyCheckTake is the take of check alone at minute 0.
func onlyCheckTake() RunEvent {
	e, _ := taken().(RunTaken)
	e.Actions = []ActionName{"check"}
	return e
}

// checked returns events, a run of the test rule, with its take naming
// check in place of judge.
func checked(events []RunEvent) []RunEvent {
	events = slices.Clone(events)
	events[0] = checkTake()
	return events
}

// checking is lfg passed at minute 5, and check asked as crew-developer.
func checking() []RunEvent {
	return append(checked(inSession()),
		ActionSessionEnded{EventHead: eh(5), Action: "lfg", Outcome: succeeded("done"), Usage: usage},
		lfgPassed(),
		ActionFunctionAsked{EventHead: eh(5), Action: "check", Bot: developer},
	)
}

// functionEnded is the fact of action's function that ended as outcome
// says at minute n.
func functionEnded(n int, action ActionName, outcome FunctionOutcome) Fact {
	return FunctionEnded{FactHead: fh(n), Action: action, Outcome: outcome}
}

// checkStep is check's call as a route's step.
var checkStep = FunctionStep{Name: "check", Function: checkSpec()}

// functionStepEnded is the fact of the route's function step at index
// step that ended as o says at minute n.
func functionStepEnded(n, step int, o FunctionOutcome) Fact {
	return StepFunctionEnded{FactHead: fh(n), Step: step, Outcome: o}
}
