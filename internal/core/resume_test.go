package core_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The states of crew's own rules, which these tests use.
const (
	readyForDev crew.State = "crew:ready for development"
	readyForFix crew.State = "crew:ready for fix"
	crewRunning crew.State = "crew:in progress"
	crewReview  crew.State = "crew:waiting review"
	crewFailed  crew.State = "crew:failed"
)

// crewRules is crew's own development and fix rules, whose actions share
// the name lfg, plus a two-action rule.
func crewRules() []crew.Rule {
	return []crew.Rule{
		{
			Name:    "development",
			Labels:  crew.Labels{Ready: readyForDev, Running: crewRunning, Success: crewReview, Failure: crewFailed},
			Actions: []crew.Action{{Name: "lfg", Prompt: parsedPrompt("lfg", "/lfg {{.Issue.Ref}}")}},
		},
		{
			Name: "fix", Labels: crew.Labels{Ready: readyForFix, Running: crewRunning, Success: crewReview, Failure: crewFailed},
			Actions: []crew.Action{{Name: "lfg", Prompt: parsedPrompt("lfg", "/lfg {{.Issue.Ref}} as a bug")}},
		},
		{
			Name:   "implement",
			Labels: crew.Labels{Ready: ready, Running: inProgress, Success: readyToReview, Failure: needsAttention},
			Actions: []crew.Action{
				{Name: "acceptance", Prompt: parsedPrompt("acceptance", "acceptance for {{.Issue.Ref}}")},
				{Name: "development", Prompt: parsedPrompt("development", "development for {{.Issue.Ref}}")},
			},
		},
	}
}

// resumeDriver drives a model that journals runs, starting from past, and
// can reopen workspaces.
func resumeDriver(t *testing.T, past ...crew.RunEvent) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(crewRules(), 2, core.Journaling(past), core.Reopening()), now: t0}
}

// pastRun is the id of the past run of rule on issue key.
func pastRun(key string, rule crew.RuleName) crew.RuleRunID {
	return crew.RuleRunID("past-" + string(rule) + "-" + key)
}

// startedRun is the start of an action run of action in rule on issue key,
// in the past run of the rule on the issue, in the workspace the engine
// would name issue-<key>-<workspace>.
func startedRun(key string, rule crew.RuleName, action crew.ActionName, workspace string) crew.ActionOpened {
	name := "issue-" + key + "-" + workspace
	return crew.ActionOpened{
		Run: pastRun(key, rule), At: t0, IssueID: issueID(key), IssueRef: "#" + key, Rule: rule,
		Action: action, Workspace: crew.Workspace{Name: crew.WorkspaceName(name), Branch: "crew/" + name},
		Log: ".crew/logs/" + name + ".log",
	}
}

// endedRun is the end of the action run r started, after a session, with
// outcome.
func endedRun(r crew.ActionOpened, outcome crew.Outcome) crew.ActionEnded {
	var end crew.ActionEnd = crew.EndFailed{Reason: outcome.Reason}
	if outcome.Succeeded {
		end = crew.EndSucceeded{Reason: outcome.Reason}
	}
	return crew.ActionEnded{
		EventHead: r.EventHead, Action: r.Action, End: end, SessionStarted: crew.Some(r.At),
		Workspace: crew.Some(crew.OpenedWorkspace{Workspace: r.Workspace, Log: r.Log, Opened: r.At}),
	}
}

// take lists iss, answers its take move and returns the commands that
// start its actions.
func (d *driver) takeIssue(iss crew.Issue) []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(iss)
	cmds, _ = d.send(core.CallResult{ID: moveID(d.t, cmds, iss.ID().Key), Result: core.ResultDone})
	return cmds
}

// reopened is the WorkspaceReady answering a ReopenWorkspace of the
// workspace issue-<key>-<workspace>.
func reopened(key string, action crew.ActionName, workspace string) core.WorkspaceReady {
	r := space(key, crew.ActionName(workspace))
	r.Action = action
	r.Resumed = true
	r.LogFromDir = "../../logs/issue-" + key + "-" + workspace + ".log"
	return r
}

// created is the WorkspaceReady answering a CreateWorkspace of action with
// the workspace issue-<key>-<workspace>.
func created(key string, action crew.ActionName, workspace string) core.WorkspaceReady {
	r := space(key, crew.ActionName(workspace))
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

// records returns the events the Record commands in cmds carry.
func records(cmds []core.Command) []crew.RunEvent {
	var out []crew.RunEvent
	for _, c := range cmds {
		if r, ok := c.(core.Record); ok {
			out = append(out, r.Event)
		}
	}
	return out
}

// ends returns the action runs' ends the Record commands in cmds carry.
func ends(cmds []core.Command) []crew.ActionEnded {
	var out []crew.ActionEnded
	for _, e := range records(cmds) {
		if ended, ok := e.(crew.ActionEnded); ok {
			out = append(out, ended)
		}
	}
	return out
}

// unrecorded returns cmds without their Record commands.
func unrecorded(cmds []core.Command) []core.Command {
	return slices.DeleteFunc(slices.Clone(cmds), func(c core.Command) bool {
		_, ok := c.(core.Record)
		return ok
	})
}

// reasonOf returns the reason of the action run's end e.
func reasonOf(e crew.ActionEnded) string { return e.End.Outcome().Reason.String() }

func TestAE1AFailedRunResumesInItsWorkspaceWithTheParagraph(t *testing.T) {
	failedRun := endedRun(startedRun("9", "development", "lfg", "lfg"), failed("no pull request was found"))
	d := resumeDriver(t, startedRun("9", "development", "lfg", "lfg"), failedRun)

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, unrecorded(cmds), core.ReopenWorkspace{
		IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
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
	opened := crew.ActionOpened{
		EventHead: d.runHead("9"), Action: "lfg", Workspace: crew.Workspace{Name: "issue-9-lfg", Branch: "crew/issue-9-lfg"},
		Log: ".crew/logs/issue-9-lfg.log", Resumed: true,
	}
	// The action run's start is recorded before its session starts.
	wantCommands(t, cmds,
		core.Record{Event: opened},
		core.Record{Event: crew.ActionSessionAsked{EventHead: d.runHead("9"), Action: "lfg"}},
		core.StartSession{
			IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg", Dir: "/repo/.crew/worktrees/issue-9-lfg",
			Prompt: want, Log: ".crew/logs/issue-9-lfg.log", Resumed: true,
		})

	_, events := d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
	hasEvent(t, events, crew.ActionSessionStarted{
		EventHead: d.runHead("9"), Action: "lfg", Workspace: crew.Workspace{Name: "issue-9-lfg", Branch: "crew/issue-9-lfg"},
		Log: ".crew/logs/issue-9-lfg.log", Resumed: true,
	})
	if !d.m.View().Issues[0].Actions[0].Resumed {
		t.Fatalf("the view does not show the action resumed")
	}
}

func TestAE2ASucceededRunStartsFresh(t *testing.T) {
	d := resumeDriver(t, endedRun(startedRun("9", "development", "lfg", "lfg"), succeeded))

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, unrecorded(cmds),
		core.CreateWorkspace{Issue: issue("9", 1, readyForDev), Run: d.run(issueID("9")), Action: "lfg"})
	cmds, _ = d.send(created("9", "lfg", "lfg-2"))
	if s := startOf(t, cmds); s.Prompt != "/lfg #9" || s.Resumed {
		t.Fatalf("start = %#v, want the bare prompt, not resumed", s)
	}
}

func TestAE3AGoneWorkspaceGetsAFreshOneWithoutTheParagraph(t *testing.T) {
	d := resumeDriver(t, endedRun(startedRun("9", "development", "lfg", "lfg"), failed("broke")))
	d.takeIssue(issue("9", 1, readyForDev))

	cmds, events := d.send(core.WorkspaceGone{IssueID: issueID("9"), Action: "lfg"})
	hasEvent(t, events, crew.WorkspaceMissing{
		EventHead: d.runHead("9"), Action: "lfg", Workspace: crew.Workspace{Name: "issue-9-lfg", Branch: "crew/issue-9-lfg"},
	})
	wantCommands(t, unrecorded(cmds),
		core.CreateWorkspace{Issue: issue("9", 1, readyForDev), Run: d.run(issueID("9")), Action: "lfg"})

	cmds, _ = d.send(created("9", "lfg", "lfg-2"))
	if s := startOf(t, cmds); s.Prompt != "/lfg #9" || s.Resumed {
		t.Fatalf("start = %#v, want the bare prompt, not resumed", s)
	}
}

func TestAE4AnotherRulesActionOfTheSameNameStartsFresh(t *testing.T) {
	d := resumeDriver(t, endedRun(startedRun("9", "development", "lfg", "lfg"), failed("broke")))

	cmds := d.takeIssue(issue("9", 1, readyForFix))
	wantCommands(t, unrecorded(cmds),
		core.CreateWorkspace{Issue: issue("9", 1, readyForFix), Run: d.run(issueID("9")), Action: "lfg"})
}

func TestAE5ARunThatNeverRecordedItsEndResumesAsCrashed(t *testing.T) {
	d := resumeDriver(t, startedRun("9", "development", "lfg", "lfg"))

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, unrecorded(cmds), core.ReopenWorkspace{
		IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
	cmds, _ = d.send(reopened("9", "lfg", "lfg"))
	crashed := `That run failed: "crew stopped before the run ended: it crashed or was killed".`
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, crashed) {
		t.Fatalf("prompt does not give the crash reason:\n%s", p)
	}
}

func TestAE6EachActionOfARuleIsDecidedOnItsOwn(t *testing.T) {
	d := resumeDriver(t,
		endedRun(startedRun("5", "implement", "acceptance", "acceptance"), failed("tests fail")),
		endedRun(startedRun("5", "implement", "development", "development"), succeeded),
	)

	cmds := d.takeIssue(issue("5", 1, ready))
	wantCommands(t, unrecorded(cmds),
		core.ReopenWorkspace{
			IssueID: issueID("5"), Run: d.run(issueID("5")), Action: "acceptance",
			Workspace: "issue-5-acceptance", Branch: "crew/issue-5-acceptance",
		},
		core.CreateWorkspace{Issue: issue("5", 1, ready), Run: d.run(issueID("5")), Action: "development"},
	)
}

func TestAE7AResumedRunThatFailsAgainResumesOnceMoreWithItsReason(t *testing.T) {
	d := resumeDriver(t, endedRun(startedRun("9", "development", "lfg", "lfg"), failed("first reason")))
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(reopened("9", "lfg", "lfg"))
	d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})

	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "lfg", Outcome: failed("second reason")})
	got := ends(cmds)
	if len(got) != 1 || got[0].End.Outcome().Succeeded || reasonOf(got[0]) != "second reason" ||
		workspaceOf(got[0]) != "issue-9-lfg" {
		t.Fatalf("ends = %#v, want one failed end in issue-9-lfg with the second reason", got)
	}
	d.settle(cmds)

	cmds = d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, unrecorded(cmds), core.ReopenWorkspace{
		IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
	cmds, _ = d.send(reopened("9", "lfg", "lfg"))
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, `That run failed: "second reason".`) {
		t.Fatalf("prompt does not give the latest reason:\n%s", p)
	}
}

func TestAModelThatCannotReopenCreatesForAFailedRun(t *testing.T) {
	past := endedRun(startedRun("9", "development", "lfg", "lfg"), failed("broke"))
	d := &driver{t: t, m: core.New(crewRules(), 2, core.Journaling([]crew.RunEvent{past})), now: t0}

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, unrecorded(cmds),
		core.CreateWorkspace{Issue: issue("9", 1, readyForDev), Run: d.run(issueID("9")), Action: "lfg"})
}

func TestANewerStartInAWorkspaceRetiresAnotherKeysRecordOfIt(t *testing.T) {
	devFailed := endedRun(startedRun("9", "development", "lfg", "lfg"), failed("broke"))
	fixStarted := startedRun("9", "fix", "lfg", "lfg")
	fixStarted.At = t0.Add(time.Hour)

	t.Run("in the journal", func(t *testing.T) {
		d := resumeDriver(t, devFailed, fixStarted, endedRun(fixStarted, succeeded))
		cmds := d.takeIssue(issue("9", 1, readyForDev))
		wantCommands(t, unrecorded(cmds), core.CreateWorkspace{
			Issue: issue("9", 1, readyForDev), Run: d.run(issueID("9")), Action: "lfg",
		})
	})

	t.Run("in memory", func(t *testing.T) {
		d := resumeDriver(t, devFailed)
		d.takeIssue(issue("9", 1, readyForFix))
		// You removed development's worktree and its branch, so fix
		// gets its name.
		d.send(space("9", "lfg"))
		d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
		cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "lfg", Outcome: succeeded})
		d.settle(cmds)

		cmds = d.takeIssue(issue("9", 2, readyForDev))
		wantCommands(t, unrecorded(cmds), core.CreateWorkspace{
			Issue: issue("9", 2, readyForDev), Run: d.run(issueID("9")), Action: "lfg",
		})
	})

	// Development's failed run no longer resumes, so its fresh workspace,
	// which gets the old name again, carries no reason from it.
	t.Run("a fresh start in the name again", func(t *testing.T) {
		d := resumeDriver(t, devFailed, fixStarted, endedRun(fixStarted, succeeded))
		d.takeIssue(issue("9", 1, readyForDev))
		d.send(created("9", "lfg", "lfg"))
		cmds, _ := d.send(core.SessionFailedToStart{
			IssueID: issueID("9"), Action: "lfg", Reason: crew.NewSessionText("start claude: not found"),
		})
		d.settle(cmds)
		wantResumeReason(t, d, issue("9", 2, readyForDev), "start claude: not found")
	})
}

// An action whose last run moved to another workspace keeps its resume
// point when another rule's action later starts in the workspace it left:
// only the actions whose last run is in that workspace are retired.
func TestAStartRetiresOnlyTheActionsWhoseLastRunIsInItsWorkspace(t *testing.T) {
	fixFirst := startedRun("9", "fix", "lfg", "lfg")
	fixAgain := startedRun("9", "fix", "lfg", "lfg-2")
	devLater := startedRun("9", "development", "lfg", "lfg")
	d := resumeDriver(t,
		fixFirst, endedRun(fixFirst, succeeded),
		fixAgain, endedRun(fixAgain, failed("broke")),
		devLater, endedRun(devLater, succeeded),
	)
	cmds := d.takeIssue(issue("9", 1, readyForFix))
	wantCommands(t, unrecorded(cmds), core.ReopenWorkspace{
		IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg",
		Workspace: "issue-9-lfg-2", Branch: "crew/issue-9-lfg-2",
	})
}

func TestARunWhoseSessionNeverStartedKeepsTheLastSessionsReason(t *testing.T) {
	past := endedRun(startedRun("9", "development", "lfg", "lfg"), failed("the session's reason"))

	t.Run("failed to start", func(t *testing.T) {
		d := resumeDriver(t, past)
		d.takeIssue(issue("9", 1, readyForDev))
		d.send(reopened("9", "lfg", "lfg"))
		cmds, _ := d.send(core.SessionFailedToStart{
			IssueID: issueID("9"), Action: "lfg", Reason: crew.NewSessionText("start claude: not found"),
		})
		// The end shows and records how it failed; the resume quotes more.
		d.wantReason("9", "lfg", "start claude: not found")
		d.settle(cmds)
		journal := append([]crew.RunEvent{past}, d.recorded...)
		wantResumeReason(t, d, issue("9", 2, readyForDev), "the session's reason")
		// So does a crew that starts from the journal.
		wantResumeReason(t, resumeDriver(t, journal...), issue("9", 2, readyForDev), "the session's reason")
	})

	t.Run("stopped while reopening", func(t *testing.T) {
		d := resumeDriver(t, past)
		d.takeIssue(issue("9", 1, readyForDev))
		d.send(core.StopRequested{})
		cmds, _ := d.send(reopened("9", "lfg", "lfg"))
		if got := ends(cmds); len(got) != 1 || reasonOf(got[0]) != "crew stopped" || workspaceOf(got[0]) != "issue-9-lfg" {
			t.Fatalf("ends = %#v, want a stopped end in issue-9-lfg", got)
		}
		wantResumeReason(t, resumeDriver(t, append([]crew.RunEvent{past}, d.recorded...)...),
			issue("9", 2, readyForDev), "the session's reason")
	})
}

// wantResumeReason fails the test unless d, taking iss, reopens its lfg
// action's workspace with a prompt that quotes reason.
func wantResumeReason(t *testing.T, d *driver, iss crew.Issue, reason string) {
	t.Helper()
	d.takeIssue(iss)
	cmds, _ := d.send(reopened(iss.ID().Key, "lfg", "lfg"))
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, `That run failed: "`+reason+`".`) {
		t.Fatalf("prompt does not quote %q:\n%s", reason, p)
	}
}

// workspaceOf returns the name of the workspace the action run's end e
// names; empty when it names none.
func workspaceOf(e crew.ActionEnded) crew.WorkspaceName {
	w, _ := e.Workspace.Get()
	return w.Workspace.Name
}

func TestAFailureWithoutAWorkspaceRecordsItsEndAndKeepsTheFailedRun(t *testing.T) {
	past := endedRun(startedRun("9", "development", "lfg", "lfg"), failed("broke"))
	d := resumeDriver(t, past)
	d.takeIssue(issue("9", 1, readyForDev))

	cmds, _ := d.send(core.WorkspaceFailed{
		IssueID: issueID("9"), Action: "lfg", Reason: crew.NewSessionText("git worktree list failed"),
	})
	if got := ends(cmds); len(got) != 1 || workspaceOf(got[0]) != "" || reasonOf(got[0]) != "git worktree list failed" {
		t.Fatalf("ends = %#v, want one end without a workspace", got)
	}
	d.settle(cmds)

	cmds = d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, unrecorded(cmds), core.ReopenWorkspace{
		IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
}

func TestAStopThenAGoneWorkspaceCreatesNothing(t *testing.T) {
	past := endedRun(startedRun("9", "development", "lfg", "lfg"), failed("broke"))
	d := resumeDriver(t, past)
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.WorkspaceGone{IssueID: issueID("9"), Action: "lfg"})
	for _, c := range cmds {
		if _, ok := c.(core.CreateWorkspace); ok {
			t.Fatalf("got %#v after a stop, want no workspace", c)
		}
	}
	if got := ends(cmds); len(got) != 1 || workspaceOf(got[0]) != "" || reasonOf(got[0]) != "crew stopped" {
		t.Fatalf("ends = %#v, want one stopped end without a workspace", got)
	}
	if a := d.m.View().Issues[0].Actions[0]; a.Phase != core.PhaseEnded || a.Outcome.Reason.String() != "crew stopped" {
		t.Fatalf("action = %#v, want ended with crew stopped", a)
	}
}

func TestAFreshRunIsRecordedFromItsWorkspaceToItsEnd(t *testing.T) {
	d := resumeDriver(t)
	d.takeIssue(issue("9", 1, readyForDev))

	cmds, _ := d.send(space("9", "lfg"))
	start := records(cmds)
	opened := d.now
	wantStart := crew.ActionOpened{
		EventHead: d.runHead("9"), Action: "lfg", Workspace: crew.Workspace{Name: "issue-9-lfg", Branch: "crew/issue-9-lfg"},
		Log: ".crew/logs/issue-9-lfg.log",
	}
	d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
	sessionStarted := d.now
	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("9"), Action: "lfg", Outcome: succeeded})
	end := ends(cmds)

	wantEnd := crew.ActionEnded{
		EventHead: d.runHead("9"), Action: "lfg", End: crew.EndSucceeded{Reason: succeeded.Reason},
		Workspace: crew.Some(crew.OpenedWorkspace{
			Workspace: wantStart.Workspace, Log: wantStart.Log, Opened: opened,
		}),
		SessionStarted: crew.Some(sessionStarted),
	}
	if len(start) == 0 || !reflect.DeepEqual(start[0], crew.RunEvent(wantStart)) || len(end) != 1 ||
		!reflect.DeepEqual(end[0], wantEnd) {
		t.Fatalf("records = %#v then %#v, want %#v then %#v", start, end, wantStart, wantEnd)
	}
}

// A RecordFailed reaches no run, so it is reported even after its run was
// released, as the past run these events belong to was.
func TestAnActionsStartOrEndThatFailsToWriteIsReported(t *testing.T) {
	d := resumeDriver(t)
	started := startedRun("9", "development", "lfg", "lfg")

	for _, e := range []crew.RunEvent{started, endedRun(started, failed("broke"))} {
		_, events := d.send(core.RecordFailed{Event: e, Reason: "disk full"})
		wantEvents(t, events, core.RunNotRecorded{
			At: d.now, IssueID: issueID("9"), IssueRef: "#9", Rule: "development", Action: "lfg", Reason: "disk full",
		})
	}
	_, events := d.send(core.RecordFailed{
		Event: crew.TakeMoved{EventHead: started.EventHead, From: readyForDev, To: crewRunning}, Reason: "disk full",
	})
	wantEvents(t, events)
}

func TestTheStatusOfAResumedActionNamesItsWorkspace(t *testing.T) {
	past := endedRun(startedRun("5", "implement", "acceptance", "acceptance"), failed("tests fail"))
	m := core.New(crewRules(), 2, core.Journaling([]crew.RunEvent{past}), core.Reopening(), core.ReportingStatus())
	d := &driver{t: t, m: m, now: t0}
	// last answers every status write and keeps the newest status.
	var last crew.Status
	answer := func(cmds []core.Command) {
		for _, c := range cmds {
			if r, ok := c.(core.ReportStatus); ok {
				last = r.Status
				d.send(core.StatusResult{IssueID: r.Status.IssueID(), Result: core.ResultDone})
			}
		}
	}
	cmds, _ := d.poll(issue("5", 1, ready))
	answer(cmds)
	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "5"), Result: core.ResultDone})
	answer(cmds)
	for _, in := range []core.Input{
		reopened("5", "acceptance", "acceptance"), created("5", "development", "development"),
		core.SessionStarted{IssueID: issueID("5"), Action: "acceptance"}, core.SessionStarted{IssueID: issueID("5"),
			Action: "development"},
		core.Tick{},
	} {
		cmds, _ = d.send(in)
		answer(cmds)
	}
	if a := last.Actions(); len(a) != 2 || a[0].Workspace != "issue-5-acceptance" || a[1].Workspace != "" {
		t.Fatalf("actions = %#v, want the resumed acceptance naming its workspace and development none", a)
	}
}

func TestAE1AFailedCheckIsTheReasonTheResumedSessionIsGiven(t *testing.T) {
	wf := crewRules()
	wf[0].Actions[0].Checks = []crew.Check{{Name: "pr-open", Script: "gh pr view --json url"}}
	d := &driver{t: t, m: core.New(wf, 2, core.Journaling(nil), core.Reopening()), now: t0}
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(created("9", "lfg", "lfg"))
	d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
	d.send(core.SessionEnded{IssueID: issueID("9"), Action: "lfg", Outcome: succeeded})

	reason := "the check failed: no pull requests found for branch \"crew/issue-9-lfg\""
	cmds, _ := d.send(core.CheckEnded{IssueID: issueID("9"), Action: "lfg", Reason: crew.NewCheckReason(reason)})
	got := ends(cmds)
	if len(got) != 1 || got[0].End.Outcome().Succeeded || reasonOf(got[0]) != reason {
		t.Fatalf("ends = %#v, want one failed end with the check's reason", got)
	}
	d.settle(cmds)

	cmds = d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, unrecorded(cmds), core.ReopenWorkspace{
		IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg", Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
	})
	cmds, _ = d.send(reopened("9", "lfg", "lfg"))
	quoted := `That run failed: "the check failed: no pull requests found for branch \"crew/issue-9-lfg\"".`
	start := startOf(t, cmds)
	if !strings.Contains(start.Prompt, quoted) {
		t.Fatalf("prompt does not quote the check's reason:\n%s", start.Prompt)
	}

	// R1: the resumed run's check reads the prompt with its resume note.
	d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("9"), Action: "lfg", Outcome: succeeded,
		LastMessage: "PR #12 is open."})
	for _, c := range cmds {
		if run, ok := c.(core.RunCheck); ok {
			if run.Prompt != start.Prompt || run.LastMessage != "PR #12 is open." {
				t.Fatalf("check's prompt %q and last message %q, want the resumed session's %q and its last message",
					run.Prompt, run.LastMessage, start.Prompt)
			}
			return
		}
	}
	t.Fatalf("the resumed run ran no check: %#v", cmds)
}
