package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The states of crew's own workflow, which these tests use.
const (
	readyForDev crew.State = "crew:ready for development"
	readyForFix crew.State = "crew:ready for fix"
	crewRunning crew.State = "crew:in progress"
	crewReview  crew.State = "crew:waiting review"
	crewFailed  crew.State = "crew:failed"
)

// crewWorkflow is crew's own development and fix stages, whose actions share
// the name lfg, plus a two-action stage.
func crewWorkflow() []crew.Stage {
	return []crew.Stage{
		{
			Name: "development", Label: readyForDev, MovesTo: crewRunning, OnSuccess: crewReview, OnFailure: crewFailed,
			Actions: []crew.Action{{Name: "lfg", Prompt: "/lfg {{.Issue.Ref}}"}},
		},
		{
			Name: "fix", Label: readyForFix, MovesTo: crewRunning, OnSuccess: crewReview, OnFailure: crewFailed,
			Actions: []crew.Action{{Name: "lfg", Prompt: "/lfg {{.Issue.Ref}} as a bug"}},
		},
		{
			Name: "implement", Label: ready, MovesTo: inProgress, OnSuccess: readyToReview, OnFailure: needsAttention,
			Actions: []crew.Action{
				{Name: "acceptance", Prompt: "acceptance for {{.Issue.Ref}}"},
				{Name: "development", Prompt: "development for {{.Issue.Ref}}"},
			},
		},
	}
}

// resumeDriver drives a model that records runs, starting from past, and
// can reopen workspaces.
func resumeDriver(t *testing.T, past ...core.RunRecord) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(crewWorkflow(), 2, core.RecordingRuns(past), core.Reopening()), now: t0}
}

// run is a record of a run of action in stage on issue key, in the
// workspace the engine would name issue-<key>-<workspace>.
func run(event core.RunEvent, key, stage, action, workspace string) core.RunRecord {
	name := "issue-" + key + "-" + workspace
	return core.RunRecord{
		Event: event, At: t0, IssueKey: key, IssueRef: "#" + key, Stage: stage, Action: action,
		Workspace: name, Branch: "crew/" + name, Log: ".crew/logs/" + name + ".log",
	}
}

func endedRun(r core.RunRecord, outcome crew.Outcome) core.RunRecord {
	r.Event = core.RunEnded
	r.Succeeded, r.Reason = outcome.Succeeded, outcome.Reason
	return r
}

// take lists iss, answers its take move and returns the commands that
// start its actions.
func (d *driver) takeIssue(iss crew.Issue) []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(iss)
	cmds, _ = d.send(core.CallResult{ID: moveID(d.t, cmds, iss.Key), Result: core.ResultDone})
	return cmds
}

// reopened is the WorkspaceReady answering a ReopenWorkspace of the
// workspace issue-<key>-<workspace>.
func reopened(key, action, workspace string) core.WorkspaceReady {
	r := space(key, workspace)
	r.Action = action
	r.Resumed = true
	r.LogFromDir = "../../logs/issue-" + key + "-" + workspace + ".log"
	return r
}

// created is the WorkspaceReady answering a CreateWorkspace of action with
// the workspace issue-<key>-<workspace>.
func created(key, action, workspace string) core.WorkspaceReady {
	r := space(key, workspace)
	r.Action = action
	return r
}

// startOf returns the StartSession in cmds, failing when there is none.
func startOf(t *testing.T, cmds []core.Command) core.StartSession {
	t.Helper()
	for _, c := range cmds {
		if s, ok := c.(core.StartSession); ok {
			return s
		}
	}
	t.Fatalf("no StartSession in %#v", cmds)
	return core.StartSession{}
}

// records returns the records the RecordRun commands in cmds carry.
func records(cmds []core.Command) []core.RunRecord {
	var out []core.RunRecord
	for _, c := range cmds {
		if r, ok := c.(core.RecordRun); ok {
			out = append(out, r.Record)
		}
	}
	return out
}

func TestAE1AFailedRunResumesInItsWorkspaceWithTheParagraph(t *testing.T) {
	failedRun := endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("no pull request was found"))
	d := resumeDriver(t, run(core.RunStarted, "9", "development", "lfg", "lfg"), failedRun)

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, cmds, core.ReopenWorkspace{
		IssueKey: "9", Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
	if got := claimOf(t, d.m, "9"); got != core.ClaimRunning {
		t.Fatalf("claim = %v, want running", got)
	}
	if got := d.m.View().Issues[0].Actions[0].Phase; got != core.PhaseReopening {
		t.Fatalf("phase = %v, want %v", got, core.PhaseReopening)
	}

	cmds, _ = d.send(reopened("9", "lfg", "lfg"))
	want := "/lfg #9\n\ncrew: this session continues the work of an earlier session on this action, in this worktree, " +
		"on branch `crew/issue-9-lfg`. That run failed: \"no pull request was found\". " +
		"Its output is in the log `.crew/logs/issue-9-lfg.log` of the repository's main checkout " +
		"(`../../logs/issue-9-lfg.log` from this worktree), above the line crew wrote there when this session started. " +
		"Check the worktree's state with `git status` and `git log` before you go on, " +
		"and continue from where it stopped instead of starting over."
	started := run(core.RunStarted, "9", "development", "lfg", "lfg")
	started.At = d.now
	wantCommands(t, cmds,
		core.RecordRun{Record: started},
		core.StartSession{
			IssueKey: "9", Action: "lfg", Dir: "/repo/.crew/worktrees/issue-9-lfg", Prompt: want,
			Log: ".crew/logs/issue-9-lfg.log", Resumed: true,
		})

	_, events := d.send(core.SessionStarted{IssueKey: "9", Action: "lfg"})
	hasEvent(t, events, core.ActionStarted{
		At: d.now, IssueKey: "9", IssueRef: "#9", Stage: "development", Action: "lfg",
		Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg", Log: ".crew/logs/issue-9-lfg.log", Resumed: true,
	})
	if !d.m.View().Issues[0].Actions[0].Resumed {
		t.Fatalf("the view does not show the action resumed")
	}
}

func TestAE2ASucceededRunStartsFresh(t *testing.T) {
	d := resumeDriver(t, endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), succeeded))

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("9", 1, readyForDev), Action: "lfg"})
	cmds, _ = d.send(created("9", "lfg", "lfg-2"))
	if s := startOf(t, cmds); s.Prompt != "/lfg #9" || s.Resumed {
		t.Fatalf("start = %#v, want the bare prompt, not resumed", s)
	}
}

func TestAE3AGoneWorkspaceGetsAFreshOneWithoutTheParagraph(t *testing.T) {
	d := resumeDriver(t, endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("broke")))
	d.takeIssue(issue("9", 1, readyForDev))

	cmds, events := d.send(core.WorkspaceGone{IssueKey: "9", Action: "lfg"})
	hasEvent(t, events, core.WorkspaceMissing{
		At: d.now, IssueKey: "9", IssueRef: "#9", Stage: "development", Action: "lfg", Workspace: "issue-9-lfg",
	})
	wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("9", 1, readyForDev), Action: "lfg"})

	cmds, _ = d.send(created("9", "lfg", "lfg-2"))
	if s := startOf(t, cmds); s.Prompt != "/lfg #9" || s.Resumed {
		t.Fatalf("start = %#v, want the bare prompt, not resumed", s)
	}
}

func TestAE4AnotherStagesActionOfTheSameNameStartsFresh(t *testing.T) {
	d := resumeDriver(t, endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("broke")))

	cmds := d.takeIssue(issue("9", 1, readyForFix))
	wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("9", 1, readyForFix), Action: "lfg"})
}

func TestAE5ARunThatNeverRecordedItsEndResumesAsCrashed(t *testing.T) {
	d := resumeDriver(t, run(core.RunStarted, "9", "development", "lfg", "lfg"))

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, cmds, core.ReopenWorkspace{
		IssueKey: "9", Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
	cmds, _ = d.send(reopened("9", "lfg", "lfg"))
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, `That run failed: "crew stopped before the run ended: it crashed or was killed".`) {
		t.Fatalf("prompt does not give the crash reason:\n%s", p)
	}
}

func TestAE6EachActionOfAStageIsDecidedOnItsOwn(t *testing.T) {
	d := resumeDriver(t,
		endedRun(run(core.RunStarted, "5", "implement", "acceptance", "acceptance"), failed("tests fail")),
		endedRun(run(core.RunStarted, "5", "implement", "development", "development"), succeeded),
	)

	cmds := d.takeIssue(issue("5", 1, ready))
	wantCommands(t, cmds,
		core.ReopenWorkspace{IssueKey: "5", Action: "acceptance", Workspace: "issue-5-acceptance", Branch: "crew/issue-5-acceptance"},
		core.CreateWorkspace{Issue: issue("5", 1, ready), Action: "development"},
	)
}

func TestAE7AResumedRunThatFailsAgainResumesOnceMoreWithItsReason(t *testing.T) {
	d := resumeDriver(t, endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("first reason")))
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(reopened("9", "lfg", "lfg"))
	d.send(core.SessionStarted{IssueKey: "9", Action: "lfg"})

	cmds, _ := d.send(core.SessionEnded{IssueKey: "9", Action: "lfg", Outcome: failed("second reason")})
	got := records(cmds)
	if len(got) != 1 || got[0].Event != core.RunEnded || got[0].Succeeded || got[0].Reason != "second reason" ||
		got[0].Workspace != "issue-9-lfg" {
		t.Fatalf("records = %#v, want one failed end in issue-9-lfg with the second reason", got)
	}
	d.settle(cmds)

	cmds = d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, cmds, core.ReopenWorkspace{
		IssueKey: "9", Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
	cmds, _ = d.send(reopened("9", "lfg", "lfg"))
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, `That run failed: "second reason".`) {
		t.Fatalf("prompt does not give the latest reason:\n%s", p)
	}
}

func TestAModelThatCannotReopenCreatesForAFailedRun(t *testing.T) {
	past := endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("broke"))
	d := &driver{t: t, m: core.New(crewWorkflow(), 2, core.RecordingRuns([]core.RunRecord{past})), now: t0}

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("9", 1, readyForDev), Action: "lfg"})
}

func TestANewerStartInAWorkspaceRetiresAnotherKeysRecordOfIt(t *testing.T) {
	devFailed := endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("broke"))
	fixStarted := run(core.RunStarted, "9", "fix", "lfg", "lfg")
	fixStarted.At = t0.Add(time.Hour)

	t.Run("in the journal", func(t *testing.T) {
		d := resumeDriver(t, devFailed, fixStarted, endedRun(fixStarted, succeeded))
		cmds := d.takeIssue(issue("9", 1, readyForDev))
		wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("9", 1, readyForDev), Action: "lfg"})
	})

	t.Run("in memory", func(t *testing.T) {
		d := resumeDriver(t, devFailed)
		d.takeIssue(issue("9", 1, readyForFix))
		// The boss removed development's worktree and its branch, so fix
		// gets its name.
		d.send(space("9", "lfg"))
		d.send(core.SessionStarted{IssueKey: "9", Action: "lfg"})
		cmds, _ := d.send(core.SessionEnded{IssueKey: "9", Action: "lfg", Outcome: succeeded})
		d.settle(cmds)

		cmds = d.takeIssue(issue("9", 2, readyForDev))
		wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("9", 2, readyForDev), Action: "lfg"})
	})
}

func TestARunWhoseSessionNeverStartedKeepsTheLastSessionsReason(t *testing.T) {
	past := endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("the session's reason"))

	t.Run("failed to start", func(t *testing.T) {
		d := resumeDriver(t, past)
		d.takeIssue(issue("9", 1, readyForDev))
		d.send(reopened("9", "lfg", "lfg"))
		cmds, _ := d.send(core.SessionFailedToStart{IssueKey: "9", Action: "lfg", Reason: "start claude: not found"})
		got := records(cmds)
		if len(got) != 1 || got[0].Reason != "the session's reason" || got[0].Succeeded {
			t.Fatalf("records = %#v, want a failed end keeping the session's reason", got)
		}
		d.settle(cmds)

		d.takeIssue(issue("9", 2, readyForDev))
		cmds, _ = d.send(reopened("9", "lfg", "lfg"))
		if p := startOf(t, cmds).Prompt; !strings.Contains(p, `"the session's reason"`) {
			t.Fatalf("prompt does not quote the session's reason:\n%s", p)
		}
	})

	t.Run("stopped while reopening", func(t *testing.T) {
		d := resumeDriver(t, past)
		d.takeIssue(issue("9", 1, readyForDev))
		d.send(core.StopRequested{})
		cmds, _ := d.send(reopened("9", "lfg", "lfg"))
		got := records(cmds)
		if len(got) != 2 || got[1].Event != core.RunEnded || got[1].Reason != "the session's reason" {
			t.Fatalf("records = %#v, want a start then a failed end keeping the session's reason", got)
		}
	})
}

func TestAFailureWithoutAWorkspaceRecordsNothingAndKeepsTheFailedRun(t *testing.T) {
	past := endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("broke"))
	d := resumeDriver(t, past)
	d.takeIssue(issue("9", 1, readyForDev))

	cmds, _ := d.send(core.WorkspaceFailed{IssueKey: "9", Action: "lfg", Reason: "git worktree list failed"})
	if got := records(cmds); len(got) != 0 {
		t.Fatalf("records = %#v, want none", got)
	}
	d.settle(cmds)

	cmds = d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, cmds, core.ReopenWorkspace{
		IssueKey: "9", Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
}

func TestAStopThenAGoneWorkspaceCreatesNothing(t *testing.T) {
	past := endedRun(run(core.RunStarted, "9", "development", "lfg", "lfg"), failed("broke"))
	d := resumeDriver(t, past)
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.WorkspaceGone{IssueKey: "9", Action: "lfg"})
	for _, c := range cmds {
		switch c.(type) {
		case core.CreateWorkspace, core.RecordRun:
			t.Fatalf("got %#v after a stop, want no workspace and no record", c)
		}
	}
	if a := d.m.View().Issues[0].Actions[0]; a.Phase != core.PhaseEnded || a.Outcome.Reason != "crew stopped" {
		t.Fatalf("action = %#v, want ended with crew stopped", a)
	}
}

func TestAFreshRunIsRecordedFromItsWorkspaceToItsEnd(t *testing.T) {
	d := resumeDriver(t)
	d.takeIssue(issue("9", 1, readyForDev))

	cmds, _ := d.send(space("9", "lfg"))
	start := records(cmds)
	d.send(core.SessionStarted{IssueKey: "9", Action: "lfg"})
	cmds, _ = d.send(core.SessionEnded{IssueKey: "9", Action: "lfg", Outcome: succeeded})
	end := records(cmds)

	wantStart := run(core.RunStarted, "9", "development", "lfg", "lfg")
	wantStart.At = t0.Add(4 * time.Second)
	wantEnd := endedRun(wantStart, succeeded)
	wantEnd.At = t0.Add(6 * time.Second)
	if len(start) != 1 || start[0] != wantStart || len(end) != 1 || end[0] != wantEnd {
		t.Fatalf("records = %#v then %#v, want %#v then %#v", start, end, wantStart, wantEnd)
	}
}

func TestARecordThatFailsToWriteIsReported(t *testing.T) {
	d := resumeDriver(t)
	r := run(core.RunStarted, "9", "development", "lfg", "lfg")

	_, events := d.send(core.RecordFailed{Record: r, Reason: "disk full"})
	wantEvents(t, events, core.RunNotRecorded{
		At: d.now, IssueKey: "9", IssueRef: "#9", Stage: "development", Action: "lfg", Reason: "disk full",
	})
}

func TestTheStatusOfAResumedActionNamesItsWorkspace(t *testing.T) {
	past := endedRun(run(core.RunStarted, "5", "implement", "acceptance", "acceptance"), failed("tests fail"))
	d := &driver{t: t, m: core.New(crewWorkflow(), 2, core.RecordingRuns([]core.RunRecord{past}), core.Reopening(), core.ReportingStatus()), now: t0}
	// last answers every status write and keeps the newest status.
	var last crew.Status
	answer := func(cmds []core.Command) {
		for _, c := range cmds {
			if r, ok := c.(core.ReportStatus); ok {
				last = r.Status
				d.send(core.StatusResult{IssueKey: r.Status.IssueKey, Result: core.ResultDone})
			}
		}
	}
	cmds, _ := d.poll(issue("5", 1, ready))
	answer(cmds)
	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "5"), Result: core.ResultDone})
	answer(cmds)
	for _, in := range []core.Input{
		reopened("5", "acceptance", "acceptance"), created("5", "development", "development"),
		core.SessionStarted{IssueKey: "5", Action: "acceptance"}, core.SessionStarted{IssueKey: "5", Action: "development"},
		core.Tick{},
	} {
		cmds, _ = d.send(in)
		answer(cmds)
	}
	if len(last.Actions) != 2 || last.Actions[0].Workspace != "issue-5-acceptance" || last.Actions[1].Workspace != "" {
		t.Fatalf("actions = %#v, want the resumed acceptance naming its workspace and development none", last.Actions)
	}
}

func TestAE1AFailedCheckIsTheReasonTheResumedSessionIsGiven(t *testing.T) {
	wf := crewWorkflow()
	wf[0].Actions[0].Check = "gh pr view --json url"
	d := &driver{t: t, m: core.New(wf, 2, core.RecordingRuns(nil), core.Reopening()), now: t0}
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(created("9", "lfg", "lfg"))
	d.send(core.SessionStarted{IssueKey: "9", Action: "lfg"})
	d.send(core.SessionEnded{IssueKey: "9", Action: "lfg", Outcome: succeeded})

	reason := "the check failed: no pull requests found for branch \"crew/issue-9-lfg\""
	cmds, _ := d.send(core.CheckEnded{IssueKey: "9", Action: "lfg", Outcome: failed(reason)})
	if got := records(cmds); len(got) != 1 || got[0].Event != core.RunEnded || got[0].Succeeded || got[0].Reason != reason {
		t.Fatalf("records = %#v, want one failed end with the check's reason", got)
	}
	d.settle(cmds)

	cmds = d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, cmds, core.ReopenWorkspace{
		IssueKey: "9", Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
	cmds, _ = d.send(reopened("9", "lfg", "lfg"))
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, `That run failed: "the check failed: no pull requests found for branch \"crew/issue-9-lfg\"".`) {
		t.Fatalf("prompt does not quote the check's reason:\n%s", p)
	}
}
