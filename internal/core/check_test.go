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

// checked is the draft workflow with a check on development only.
func checked() []crew.Stage {
	w := draft()
	w[0].Actions[1].Check = prCheck
	return w
}

// runCheck is the RunCheck the development action of key asks for.
func runCheck(key string) core.RunCheck {
	ws := space(key, "development")
	return core.RunCheck{
		IssueKey: key, Action: "development", Dir: ws.Dir, Command: prCheck, Log: ws.Log,
		IssueRef: "#" + key, IssueURL: "https://example.com/issues/" + key, Branch: ws.Branch,
	}
}

// checking runs issue 74 in checked until development's session succeeded
// and its check started, with acceptance ended by acceptance.
func checking(d *driver, acceptance crew.Outcome) {
	d.t.Helper()
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: acceptance})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
	wantCommands(d.t, cmds, runCheck("74"))
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
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: succeeded})

	cmds, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: failed("tests fail")})
	for _, c := range cmds {
		if _, ok := c.(core.RunCheck); ok {
			t.Fatalf("a failed session ran its check: %#v", cmds)
		}
	}
	ws := space("74", "development")
	want := []crew.ActionFailure{{Action: "development", Reason: "tests fail", Workspace: ws.Workspace, Log: ws.Log}}
	if got := failures(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
}

func TestAE2SuccessfulSessionIsJudgedOnlyOnceItsCheckPassed(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)
	if got := claimOf(t, d.m, "74"); got != core.ClaimRunning {
		t.Fatalf("claim while the check runs = %v, want running", got)
	}

	cmds, _ := d.send(core.CheckEnded{IssueKey: "74", Action: "development", Outcome: crew.Outcome{Succeeded: true, Reason: "the check passed"}})
	wantCommands(t, cmds, core.Move{IssueKey: "74", From: inProgress, To: readyToReview})
}

func TestAE1CheckThatFailsFailsItsActionWithTheChecksReason(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)

	reason := "the check failed: no open pull request from crew/issue-74-development"
	cmds, _ := d.send(core.CheckEnded{IssueKey: "74", Action: "development", Outcome: failed(reason)})
	moveID(t, cmds, "74")
	if got := noIDs(cmds)[0]; !reflect.DeepEqual(got, core.Move{IssueKey: "74", From: inProgress, To: needsAttention}) {
		t.Fatalf("verdict = %#v, want the move to needs attention", got)
	}
	// AE5: only the action whose check failed is reported.
	ws := space("74", "development")
	want := []crew.ActionFailure{{Action: "development", Reason: reason, Workspace: ws.Workspace, Log: ws.Log}}
	if got := failures(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
}

func TestAE9StopWhileCheckingStopsTheCheckAndFailsTheAction(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.StopCheck{IssueKey: "74", Action: "development"})

	// Even a check that passed just as it was stopped counts as stopped.
	cmds, _ = d.send(core.CheckEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
	ws := space("74", "development")
	want := []crew.ActionFailure{{Action: "development", Reason: "crew stopped", Workspace: ws.Workspace, Log: ws.Log}}
	if got := failures(t, cmds); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
}

func TestSessionThatSucceedsAfterAStopStartsNoCheck(t *testing.T) {
	d := newDriver(t, checked(), 2)
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: succeeded})
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
	for _, c := range cmds {
		if _, ok := c.(core.RunCheck); ok {
			t.Fatalf("a check started after a stop: %#v", cmds)
		}
	}
	if got := failures(t, cmds); len(got) != 1 || got[0].Reason != "crew stopped" {
		t.Fatalf("failures = %#v, want development failed as stopped", got)
	}
}

func TestTimeUpLetsARunningCheckFinishBeforeStopping(t *testing.T) {
	d := newDriver(t, checked(), 2)
	checking(d, succeeded)

	cmds, _ := d.send(core.TimeUp{Limit: time.Hour})
	wantCommands(t, cmds)
	if d.m.Stopped() {
		t.Fatal("stopped while a check runs")
	}

	verdict, _ := d.send(core.CheckEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
	_, events := d.send(core.CallResult{ID: moveID(t, verdict, "74"), Result: core.ResultDone})
	if !d.m.Stopped() || !containsStopped(events) {
		t.Fatal("not stopped once the checked issue was judged")
	}
}

func TestCheckingActionIsRunningInItsStatus(t *testing.T) {
	d := newStatusDriver(t, checked(), 2)
	d.runAll(d.take(issue("74", 1, ready)))
	devStarted := started(t, d.m, "74", "development")
	d.send(core.SessionEnded{IssueKey: "74", Action: "acceptance", Outcome: succeeded})
	d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})

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

func TestEndedStatusGivesEachFailedActionsCauseNotItsWords(t *testing.T) {
	stage := func(actions ...crew.Action) []crew.Stage {
		w := draft()
		w[0].Actions = actions
		return w
	}
	ws := space("74", "development")
	tests := []struct {
		name   string
		action crew.Action
		// end ends development once its issue was taken.
		end  func(d *driver, landed []core.Command) []core.Command
		want crew.ActionStatus
	}{
		{
			name:   "session",
			action: crew.Action{Name: "development", Prompt: "Do {{.Issue.Ref}}"},
			end: func(d *driver, landed []core.Command) []core.Command {
				d.runAll(landed)
				cmds, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: failed("token=secret")})
				return cmds
			},
			want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseSession, Log: ws.Log},
		},
		{
			name:   "check",
			action: crew.Action{Name: "development", Prompt: "Do {{.Issue.Ref}}", Check: "false"},
			end: func(d *driver, landed []core.Command) []core.Command {
				d.runAll(landed)
				d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
				cmds, _ := d.send(core.CheckEnded{IssueKey: "74", Action: "development", Outcome: failed("the check failed: no pull request")})
				return cmds
			},
			want: crew.ActionStatus{
				Name: "development", State: crew.ActionFailed, Cause: crew.CauseCheck,
				Reason: "the check failed: no pull request", Log: ws.Log,
			},
		},
		{
			name:   "stopped",
			action: crew.Action{Name: "development", Prompt: "Do {{.Issue.Ref}}"},
			end: func(d *driver, landed []core.Command) []core.Command {
				d.runAll(landed)
				d.send(core.StopRequested{})
				cmds, _ := d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: failed("stopped by crew")})
				return cmds
			},
			want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseStopped, Log: ws.Log},
		},
		{
			name:   "workspace",
			action: crew.Action{Name: "development", Prompt: "Do {{.Issue.Ref}}"},
			end: func(d *driver, _ []core.Command) []core.Command {
				cmds, _ := d.send(core.WorkspaceFailed{IssueKey: "74", Action: "development", Reason: "git: no origin"})
				return cmds
			},
			want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseWorkspace},
		},
		{
			name:   "start",
			action: crew.Action{Name: "development", Prompt: "Do {{.Issue.Ref}}"},
			end: func(d *driver, _ []core.Command) []core.Command {
				d.send(ws)
				cmds, _ := d.send(core.SessionFailedToStart{IssueKey: "74", Action: "development", Reason: "claude: not found"})
				return cmds
			},
			want: crew.ActionStatus{Name: "development", State: crew.ActionFailed, Cause: crew.CauseStart, Log: ws.Log},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStatusDriver(t, stage(tt.action), 2)
			got := statusOf(t, tt.end(d, d.take(issue("74", 1, ready))), "74")
			if got.Kind != crew.StatusEnded || !reflect.DeepEqual(got.Actions, []crew.ActionStatus{tt.want}) {
				t.Fatalf("ended status: %#v\nwant actions %#v", got, []crew.ActionStatus{tt.want})
			}
		})
	}
}

func TestPromptThatFailsToRenderGivesItsCause(t *testing.T) {
	w := draft()
	w[0].Actions = []crew.Action{{Name: "development", Prompt: "Do {{.Issue.Numbr}}"}}
	d := newStatusDriver(t, w, 2)
	cmds, _ := d.poll(issue("74", 1, ready))
	landed, _ := d.send(core.CallResult{ID: moveID(t, cmds, "74"), Result: core.ResultDone})
	got := statusOf(t, landed, "74")
	want := []crew.ActionStatus{{Name: "development", State: crew.ActionFailed, Cause: crew.CausePrompt}}
	if got.Kind != crew.StatusEnded || !reflect.DeepEqual(got.Actions, want) {
		t.Fatalf("ended status: %#v", got)
	}
}
