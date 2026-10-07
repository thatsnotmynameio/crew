package core_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// prCheck is the check of the development action in checked.
const prCheck = `gh pr list --head "$CREW_BRANCH" --state open`

// checked is the draft rules with a check on development only.
func checked() []crew.Rule {
	w := draft()
	w[0].Actions[1].Checks = []crew.Check{{Name: "pr-closes-issue", Script: prCheck}}
	return w
}

// runCheck is the RunCheck the development action of issue 74 asks for.
func runCheck() core.RunCheck {
	const key = "74"
	ws := space(key, "development")
	return core.RunCheck{
		IssueID: issueID(key), Action: "development", Dir: ws.Dir, Name: "pr-closes-issue", Command: prCheck, Log: ws.Log,
		IssueRef: "#" + key, IssueURL: "https://example.com/issues/" + key, Branch: ws.Branch,
		Prompt: "Implement development for issue #" + key,
	}
}

// checking runs issue 74 in checked until development's session succeeded
// and its check started, with acceptance ended by acceptance.
func checking(d *driver, acceptance crew.Outcome) {
	d.t.Helper()
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: acceptance})
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	wantCommands(d.t, cmds, runCheck())
}

// failures returns the failure report in cmds.
func failures(t *testing.T, cmds []core.Command) []crew.ActionFailure {
	t.Helper()
	for _, c := range cmds {
		if r, ok := c.(core.ReportFailure); ok {
			return r.Report.Failures
		}
	}
	t.Fatalf("no failure report in %#v", cmds)
	return nil
}

func TestAE3SessionThatFailsRunsNoCheck(t *testing.T) {
	d := newDriver(t, checked(), 2)
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})

	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("tests fail")})
	for _, c := range cmds {
		if _, ok := c.(core.RunCheck); ok {
			t.Fatalf("a failed session ran its check: %#v", cmds)
		}
	}
	ws := space("74", "development")
	want := []crew.ActionFailure{{Action: "development", Workspace: ws.Workspace, Log: ws.Log}}
	if got := failures(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
	d.wantReason("74", "development", "tests fail")
}

func TestAE2SuccessfulSessionIsJudgedOnlyOnceItsCheckPassed(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)
	if got := claimOf(t, d.m, "74"); got != core.ClaimRunning {
		t.Fatalf("claim while the check runs = %v, want running", got)
	}

	cmds, events := d.send(core.CheckEnded{
		IssueID: issueID("74"), Action: "development",
		Passed: true, Reason: checkPassed,
	})
	wantCommands(t, cmds, core.Move{IssueID: issueID("74"), From: inProgress, To: readyToReview})
	// KTD7: the last check's passing reason is the action's.
	want := crew.Outcome{Succeeded: true, Reason: crew.NewSessionText(checkPassed.String())}
	if got := endOf(t, events, "development"); got != want {
		t.Errorf("development ended with %#v, want %#v", got, want)
	}
}

// endOf returns the outcome action ended with in events.
func endOf(t *testing.T, events []core.Event, action crew.ActionName) crew.Outcome {
	t.Helper()
	for _, e := range events {
		if ended, ok := e.(core.ActionEnded); ok && ended.Action == action {
			return ended.Outcome
		}
	}
	t.Fatalf("no end of %s in %#v", action, events)
	return crew.Outcome{}
}

// Each action's session and check act as the action's own bot (KTD9).
func TestSessionAndCheckCarryTheActionsBot(t *testing.T) {
	w := checked()
	w[0].Actions[0].Bot = crew.Bot{Name: "ops"}
	w[0].Actions[1].Bot = crew.Bot{Name: "developer"}
	d := newDriver(t, w, 2)
	cmds, _ := d.poll(issue("74", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})

	acceptance := session("74", "acceptance", "Implement test acceptance for issue #74")
	acceptance.Bot = "ops"
	cmds, _ = d.send(space("74", "acceptance"))
	wantCommands(t, cmds, acceptance)
	development := session("74", "development", "Implement development for issue #74")
	development.Bot = "developer"
	cmds, _ = d.send(space("74", "development"))
	wantCommands(t, cmds, development)

	d.send(core.SessionStarted{IssueID: issueID("74"), Action: "development"})
	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	check := runCheck()
	check.Bot = "developer"
	wantCommands(t, cmds, check)
}

func TestAE1CheckThatFailsFailsItsActionWithTheChecksReason(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)

	reason := "the check failed: no open pull request from crew/issue-74-development"
	cmds, events := d.send(core.CheckEnded{
		IssueID: issueID("74"), Action: "development", Reason: crew.NewCheckReason(reason),
	})
	if got, want := endOf(t, events, "development"), (crew.Outcome{Reason: crew.NewSessionText(reason)}); got != want {
		t.Errorf("development ended with %#v, want %#v", got, want)
	}
	moveID(t, cmds, "74")
	if got := noIDs(cmds)[0]; !reflect.DeepEqual(got, core.Move{IssueID: issueID("74"), From: inProgress,
		To: needsAttention}) {
		t.Fatalf("verdict = %#v, want the move to needs attention", got)
	}
	// AE5: only the action whose check failed is reported.
	ws := space("74", "development")
	want := []crew.ActionFailure{{Action: "development", Workspace: ws.Workspace, Log: ws.Log}}
	if got := failures(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
}

func TestAE9StopWhileCheckingStopsTheCheckAndFailsTheAction(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopCheck{IssueID: issueID("74"), Action: "development"})

	// Even a check that passed just as it was stopped counts as stopped.
	cmds, _ = d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Passed: true, Reason: checkPassed})
	ws := space("74", "development")
	want := []crew.ActionFailure{{Action: "development", Workspace: ws.Workspace, Log: ws.Log}}
	if got := failures(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
	d.wantReason("74", "development", "crew stopped")
}

func TestSessionThatSucceedsAfterAStopStartsNoCheck(t *testing.T) {
	d := newDriver(t, checked(), 2)
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
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

func TestTimeUpLetsARunningCheckFinishBeforeStopping(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)

	cmds, _ := d.send(core.TimeUp{Limit: time.Hour})
	wantCommands(t, cmds)
	if d.m.Stopped() {
		t.Fatal("stopped while a check runs")
	}

	verdict, _ := d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Passed: true, Reason: checkPassed})
	_, events := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the checked issue was judged")
	}
}

func TestCheckingActionIsRunningInItsStatus(t *testing.T) {
	d := newStatusDriver(t, checked(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	devStarted := started(t, d.m, "development")
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "acceptance", Outcome: succeeded})
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})

	cmds, _ := d.send(core.Tick{})
	got := statusOf(t, cmds, "74")
	want := []crew.ActionStatus{
		{Name: "acceptance", State: crew.ActionSucceeded},
		{Name: "development", State: crew.ActionRunning, Started: devStarted},
	}
	if got.Kind != crew.StatusRunning || !reflect.DeepEqual(got.Actions, want) {
		t.Fatalf("status while checking: %#v", got)
	}
}

// devSpace is the workspace of issue 74's development.
var devSpace = space("74", "development")

// draftWith is the draft rules with actions as its first rule's.
func draftWith(actions ...crew.Action) []crew.Rule {
	w := draft()
	w[0].Actions = actions
	return w
}

// failedCauseCases are the ways development fails, each with the status it
// ends with.
var failedCauseCases = []struct {
	name   string
	action crew.Action
	// end ends development once its issue was taken.
	end  func(d *driver, landed []core.Command) []core.Command
	want crew.ActionStatus
}{
	{
		name:   "session",
		action: crew.Action{Name: "development", Prompt: parsedPrompt("development", "Do {{.Issue.Ref}}")},
		end: func(d *driver, landed []core.Command) []core.Command {
			d.runAll(landed)
			cmds, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("token=secret")})
			return cmds
		},
		want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseSession, Log: devSpace.Log},
	},
	{
		name: "check",
		action: crew.Action{
			Name: "development", Prompt: parsedPrompt("development", "Do {{.Issue.Ref}}"),
			Checks: []crew.Check{{Name: "never", Script: "false"}},
		},
		end: func(d *driver, landed []core.Command) []core.Command {
			d.runAll(landed)
			d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
			cmds, _ := d.send(core.CheckEnded{
				IssueID: issueID("74"), Action: "development", Reason: crew.NewCheckReason("the check failed: no pull request"),
			})
			return cmds
		},
		want: crew.ActionStatus{
			Name: "development", State: crew.ActionFailed, Cause: crew.CauseCheck, Log: devSpace.Log,
			Checks: []crew.CheckResult{{Name: "never", Reason: crew.NewCheckReason("the check failed: no pull request")}},
		},
	},
	{
		name:   "stopped",
		action: crew.Action{Name: "development", Prompt: parsedPrompt("development", "Do {{.Issue.Ref}}")},
		end: func(d *driver, landed []core.Command) []core.Command {
			d.runAll(landed)
			d.send(core.StopRequested{})
			cmds, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development",
				Outcome: failed("stopped by crew")})
			return cmds
		},
		want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseStopped, Log: devSpace.Log},
	},
	{
		name:   "workspace",
		action: crew.Action{Name: "development", Prompt: parsedPrompt("development", "Do {{.Issue.Ref}}")},
		end: func(d *driver, _ []core.Command) []core.Command {
			cmds, _ := d.send(core.WorkspaceFailed{
				IssueID: issueID("74"), Action: "development", Reason: crew.NewSessionText("git: no origin"),
			})
			return cmds
		},
		want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseWorkspace},
	},
	{
		name:   "start",
		action: crew.Action{Name: "development", Prompt: parsedPrompt("development", "Do {{.Issue.Ref}}")},
		end: func(d *driver, _ []core.Command) []core.Command {
			d.send(devSpace)
			cmds, _ := d.send(core.SessionFailedToStart{
				IssueID: issueID("74"), Action: "development", Reason: crew.NewSessionText("claude: not found"),
			})
			return cmds
		},
		want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseStart, Log: devSpace.Log},
	},
}

func TestEndedStatusGivesEachFailedActionsCauseNotItsWords(t *testing.T) {
	for _, tt := range failedCauseCases {
		t.Run(tt.name, func(t *testing.T) {
			d := newStatusDriver(t, draftWith(tt.action), 2)
			got := statusOf(t, tt.end(d, d.take(issue("74", 1, ready))), "74")
			if got.Kind != crew.StatusEnded || !reflect.DeepEqual(got.Actions, []crew.ActionStatus{tt.want}) {
				t.Fatalf("ended status: %#v\nwant actions %#v", got, []crew.ActionStatus{tt.want})
			}
		})
	}
}

func TestPromptThatFailsToRenderGivesItsCause(t *testing.T) {
	w := draft()
	// Renders for the sample issue's title, and fails on the shorter "Issue 74".
	w[0].Actions = []crew.Action{
		{Name: "development", Prompt: parsedPrompt("development", "Do {{index .Issue.Title 11}}")},
	}
	d := newStatusDriver(t, w, 2)
	cmds, _ := d.poll(issue("74", 1, ready))
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
	got := statusOf(t, landed, "74")
	want := []crew.ActionStatus{{Name: "development", State: crew.ActionFailed, Cause: crew.CausePrompt}}
	if got.Kind != crew.StatusEnded || !reflect.DeepEqual(got.Actions, want) {
		t.Fatalf("ended status: %#v", got)
	}
}
