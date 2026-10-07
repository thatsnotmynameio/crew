package core_test

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

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

// crewRules is crew's own development and fix rules, whose sessions share
// the name lfg, then the draft's implement, whose sessions acceptance and
// development run one after the other.
func crewRules() []crew.Rule {
	return []crew.Rule{
		{
			Name: "development", Labels: crew.Labels{Ready: readyForDev, Running: crewRunning},
			Actions: []crew.Action{sessionAction("lfg", "/lfg {{.Issue.Ref}}")}, Routes: routes(crewReview, crewFailed),
		},
		{
			Name: "fix", Labels: crew.Labels{Ready: readyForFix, Running: crewRunning},
			Actions: []crew.Action{sessionAction("lfg", "/lfg {{.Issue.Ref}} as a bug")},
			Routes:  routes(crewReview, crewFailed),
		},
		draft()[0],
	}
}

// resumeDriver drives a model of crewRules that journals runs, starting
// from past, and can reopen worktrees. Its inputs are seeded after those
// of the drivers that left past, so its runs get ids of their own.
func resumeDriver(t *testing.T, past ...crew.RunEvent) *driver {
	t.Helper()
	return resumeDriverOf(t, crewRules(), past...)
}

// resumeDriverOf is resumeDriver for rules.
func resumeDriverOf(t *testing.T, rules []crew.Rule, past ...crew.RunEvent) *driver {
	t.Helper()
	d := &driver{t: t, m: core.New(rules, 2, core.Journaling(past), core.Reopening()), now: t0}
	for _, e := range past {
		if _, ok := e.(crew.RunTaken); ok {
			d.inputs += 1000
		}
	}
	return d
}

// journaled plays on a driver of rules that journals from past, and
// returns the journal it leaves: past, then every event it recorded.
func journaled(t *testing.T, rules []crew.Rule, past []crew.RunEvent, play func(d *driver)) []crew.RunEvent {
	t.Helper()
	d := resumeDriverOf(t, rules, past...)
	play(d)
	return append(slices.Clone(past), d.recorded...)
}

// failedRun is the journal of a run of development on #9 whose session lfg
// failed for reason, which ended through failed.
func failedRun(t *testing.T, reason string) []crew.RunEvent {
	t.Helper()
	return journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		d.settle(d.ended("9", "lfg", failed(reason)))
		wantHeld(t, d.m)
	})
}

// created is the CreateWorkspace of a new workspace for the last run of
// rule on it.
func (d *driver) created(it crew.Issue, rule crew.RuleName) core.CreateWorkspace {
	return core.CreateWorkspace{Issue: it, Run: d.run(it.ID()), Rule: rule}
}

// takeIssue lists iss, answers its take move and returns the commands that
// follow, the Record commands left out.
func (d *driver) takeIssue(iss crew.Issue) []core.Command {
	d.t.Helper()
	cmds, _ := d.poll(iss)
	cmds, _ = d.send(core.CallResult{ID: moveID(d.t, cmds, iss.ID().Key), Result: core.ResultDone})
	return unrecorded(cmds)
}

// reopen is the ReopenWorkspace of the worktree of the run of rule on key,
// for key's last run.
func (d *driver) reopen(key string, rule crew.RuleName) core.ReopenWorkspace {
	w := space(key, rule)
	return core.ReopenWorkspace{IssueID: issueID(key), Run: d.run(issueID(key)), Workspace: w.Workspace, Branch: w.Branch}
}

// reopened is the WorkspaceReady answering a ReopenWorkspace of the
// worktree of the run of rule on key.
func reopened(key string, rule crew.RuleName) core.WorkspaceReady {
	r := space(key, rule)
	r.Resumed = true
	r.LogFromDir = "../../logs/issue-" + key + "-" + string(rule) + ".log"
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

// unrecorded returns cmds without their Record commands.
func unrecorded(cmds []core.Command) []core.Command {
	return slices.DeleteFunc(slices.Clone(cmds), func(c core.Command) bool {
		_, ok := c.(core.Record)
		return ok
	})
}

// takenOf returns the last take of issue key in d's events.
func takenOf(t *testing.T, d *driver, key string) crew.RunTaken {
	t.Helper()
	for _, e := range slices.Backward(d.events) {
		if taken, ok := e.(crew.RunTaken); ok && taken.IssueID == issueID(key) {
			return taken
		}
	}
	t.Fatalf("no take of #%s", key)
	return crew.RunTaken{}
}

// wantResumeReason fails the test unless the prompt of the session in cmds
// says its last run ended through failed for reason.
func wantResumeReason(t *testing.T, cmds []core.Command, reason string) {
	t.Helper()
	quoted := "That run ended through the route `failed`: " + strconv.Quote(reason) + "."
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, quoted) {
		t.Fatalf("prompt does not say %s:\n%s", quoted, p)
	}
}

// Covers AE1, R22, R23: the take starts where the last run's failed route
// says, in its reopened worktree, and the session gets the resume
// paragraph naming that route.
func TestAE1AFailedRunResumesInItsWorktreeWithTheParagraph(t *testing.T) {
	d := resumeDriver(t, failedRun(t, "no pull request was found")...)

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, cmds, d.reopen("9", "development"))
	w := space("9", "development")
	ws := crew.Workspace{Name: w.Workspace, Branch: w.Branch}
	wantStart := crew.StartAt{
		Workspace: ws, Log: w.Log, Action: "lfg", Route: crew.FailedRoute,
		Reason: crew.NewSessionText("no pull request was found"), Session: crew.Some(crew.LatestSession{Action: "lfg"}),
	}
	if got := takenOf(t, d, "9").Start; !reflect.DeepEqual(got, crew.Start(wantStart)) {
		t.Fatalf("start:\n got %#v\nwant %#v", got, wantStart)
	}
	if got := claimOf(t, d.m, "9"); got != core.ClaimRunning {
		t.Fatalf("claim = %v, want running", got)
	}
	if got := d.m.View().Issues[0].Actions[0].Phase; got != core.PhaseReopening {
		t.Fatalf("phase = %v, want %v", got, core.PhaseReopening)
	}

	cmds, _ = d.send(reopened("9", "development"))
	want := "/lfg #9\n\ncrew: this session continues the work of an earlier run of this rule, in this worktree, " +
		"on branch `crew/issue-9-development`. That run ended through the route `failed`: " +
		"\"no pull request was found\". Its output is in the log `.crew/logs/issue-9-development.log` of the " +
		"repository's main checkout (`../../logs/issue-9-development.log` from this worktree), above the line crew " +
		"wrote there when this session started. Check the worktree's state with `git status` and `git log` before " +
		"you go on, and continue from where it stopped instead of starting over."
	// The run's worktree and the action's start are recorded before its
	// session starts.
	wantCommands(t, cmds,
		core.Record{Event: crew.WorkspaceOpened{EventHead: d.runHead("9"), Workspace: ws, Log: w.Log, Resumed: true}},
		core.Record{Event: crew.ActionSessionAsked{EventHead: d.runHead("9"), Action: "lfg"}},
		core.StartSession{
			IssueID: issueID("9"), Run: d.run(issueID("9")), Action: "lfg", Dir: w.Dir, Prompt: want, Log: w.Log,
			Resumed: true,
		})

	_, events := d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
	hasEvent(t, events, crew.ActionSessionStarted{EventHead: d.runHead("9"), Action: "lfg"})
	if !d.m.View().Issues[0].Actions[0].Resumed {
		t.Fatalf("the view does not show the action resumed")
	}
}

func TestAE2ARunThatEndedThroughPassedStartsFresh(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		d.settle(d.ended("9", "lfg", succeeded))
	})
	d := resumeDriver(t, past...)

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, cmds, d.created(issue("9", 1, readyForDev), "development"))
	if s := startOf(t, d.ready("9")); s.Prompt != "/lfg #9" || s.Resumed {
		t.Fatalf("start = %#v, want the bare prompt, not resumed", s)
	}
}

func TestAE3AGoneWorktreeStartsFreshWithoutTheParagraph(t *testing.T) {
	d := resumeDriver(t, failedRun(t, "broke")...)
	d.takeIssue(issue("9", 1, readyForDev))

	cmds, events := d.send(core.WorkspaceGone{IssueID: issueID("9")})
	w := space("9", "development")
	hasEvent(t, events, crew.WorkspaceMissing{
		EventHead: d.runHead("9"), Workspace: crew.Workspace{Name: w.Workspace, Branch: w.Branch},
	})
	wantCommands(t, unrecorded(cmds),
		d.created(issue("9", 1, readyForDev), "development"))

	created := space("9", "development-2")
	if s := startOf(t, func() []core.Command { cmds, _ := d.send(created); return cmds }()); s.Prompt != "/lfg #9" ||
		s.Resumed {
		t.Fatalf("start = %#v, want the bare prompt, not resumed", s)
	}
}

func TestAE4AnotherRulesRunOnTheIssueStartsFresh(t *testing.T) {
	d := resumeDriver(t, failedRun(t, "broke")...)

	cmds := d.takeIssue(issue("9", 1, readyForFix))
	wantCommands(t, cmds, d.created(issue("9", 1, readyForFix), "fix"))
}

func TestAE5ARunThatNeverChoseARouteResumesAsCrashed(t *testing.T) {
	// crew died while lfg's session ran: the journal ends with its start.
	past := journaled(t, crewRules(), nil, func(d *driver) { d.running(issue("9", 1, readyForDev)) })
	d := resumeDriver(t, past...)

	cmds := d.takeIssue(issue("9", 1, readyForDev))
	wantCommands(t, cmds, d.reopen("9", "development"))
	cmds, _ = d.send(reopened("9", "development"))
	crashed := `That run stopped before it chose a route: "crew stopped before the run ended: it crashed or was killed".`
	if p := startOf(t, cmds).Prompt; !strings.Contains(p, crashed) {
		t.Fatalf("prompt does not give the crash reason:\n%s", p)
	}
}

// Covers AE6: the actions before the restart point went on to the next, so
// they do not run again.
func TestAResumeSkipsTheActionsThatWentOnToTheNext(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("5", 1, ready))
		d.settle(d.ended("5", "acceptance", succeeded))
		d.settle(d.ended("5", "development", failed("tests fail")))
	})
	d := resumeDriver(t, past...)

	cmds := d.takeIssue(issue("5", 1, ready))
	wantCommands(t, cmds, d.reopen("5", "implement"))
	cmds, _ = d.send(reopened("5", "implement"))
	if s := startOf(t, cmds); s.Action != "development" || !s.Resumed {
		t.Fatalf("start = %#v, want development resumed", s)
	}
	wantResumeReason(t, cmds, "tests fail")
	if got := d.m.View().Issues[0].Actions[0].Phase; got != core.PhaseWaiting {
		t.Fatalf("acceptance's phase = %v, want it waiting, done in an earlier run", got)
	}
}

func TestAE7AResumedRunThatFailsAgainResumesOnceMoreWithItsReason(t *testing.T) {
	d := resumeDriver(t, failedRun(t, "first reason")...)
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(reopened("9", "development"))
	d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
	d.settle(d.ended("9", "lfg", failed("second reason")))
	wantHeld(t, d.m)

	cmds := d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, cmds, d.reopen("9", "development"))
	cmds, _ = d.send(reopened("9", "development"))
	wantResumeReason(t, cmds, "second reason")
}

// Without a workspace that reopens, a run that would resume starts fresh,
// and the passed route alone runs without its worktree (KTD19).
func TestAModelThatCannotReopenStartsFresh(t *testing.T) {
	t.Run("a failed run", func(t *testing.T) {
		past := failedRun(t, "broke")
		d := &driver{t: t, m: core.New(crewRules(), 2, core.Journaling(past)), now: t0, inputs: 1000}

		cmds := d.takeIssue(issue("9", 1, readyForDev))
		wantCommands(t, cmds, d.created(issue("9", 1, readyForDev), "development"))
		if got := takenOf(t, d, "9").Start; got != crew.Start(crew.StartFresh{}) {
			t.Fatalf("start = %#v, want fresh", got)
		}
	})

	t.Run("the passed route alone", func(t *testing.T) {
		past := journaled(t, crewRules(), nil, func(d *driver) {
			d.running(issue("9", 1, readyForDev))
			moved := d.ended("9", "lfg", succeeded)
			d.send(core.CallResult{ID: moveID(t, moved, "9"), Result: core.ResultRefused, Reason: "nope"})
		})
		d := &driver{t: t, m: core.New(crewRules(), 2, core.Journaling(past)), now: t0, inputs: 1000}

		cmds := d.takeIssue(issue("9", 1, readyForDev))
		wantCommands(t, cmds, core.Move{IssueID: issueID("9"), From: crewRunning, To: crewReview})
		want := crew.StartPassedRoute{Session: crew.Some(crew.LatestSession{Action: "lfg"})}
		if got := takenOf(t, d, "9").Start; !reflect.DeepEqual(got, crew.Start(want)) {
			t.Fatalf("start = %#v, want %#v", got, want)
		}
	})
}

// Covers KTD18: once another run opens a worktree of its name, a past run's
// worktree is gone, whether crew saw it open or reads it from the journal.
func TestAWorktreeOpenedUnderTheNameOfAPastRunsRetiresIt(t *testing.T) {
	devFailed := failedRun(t, "broke")
	// You removed development's worktree and its branch, so fix got its
	// name.
	fixed := func(d *driver) {
		cmds, _ := d.poll(issue("9", 1, readyForFix))
		d.send(core.CallResult{ID: moveID(t, cmds, "9"), Result: core.ResultDone})
		d.send(space("9", "development"))
		d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
		d.settle(d.ended("9", "lfg", succeeded))
	}

	t.Run("in memory", func(t *testing.T) {
		d := resumeDriver(t, devFailed...)
		fixed(d)
		cmds := d.takeIssue(issue("9", 2, readyForDev))
		wantCommands(t, cmds, d.created(issue("9", 2, readyForDev), "development"))
	})

	t.Run("in the journal", func(t *testing.T) {
		d := resumeDriver(t, journaled(t, crewRules(), devFailed, fixed)...)
		cmds := d.takeIssue(issue("9", 2, readyForDev))
		wantCommands(t, cmds, d.created(issue("9", 2, readyForDev), "development"))
	})
}

// Covers KTD-S9: a run that started no action and opened no worktree of its
// own passes its start on, and so do the failures around a reopen.
func TestARunThatStartedNothingPassesItsStartOn(t *testing.T) {
	tests := []struct {
		name string
		cut  func(d *driver)
	}{
		{"stopped while reopening", func(d *driver) {
			d.send(core.StopRequested{})
			d.settle(func() []core.Command { cmds, _ := d.send(reopened("9", "development")); return cmds }())
		}},
		{"its worktree failed to reopen", func(d *driver) {
			cmds, _ := d.send(core.WorkspaceFailed{IssueID: issueID("9"), Reason: crew.NewSessionText("git failed")})
			d.settle(cmds)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			past := journaled(t, crewRules(), failedRun(t, "the session's reason"),
				func(d *driver) {
					d.takeIssue(issue("9", 1, readyForDev))
					tt.cut(d)
					wantHeld(t, d.m)
				})
			d := resumeDriver(t, past...)

			cmds := d.takeIssue(issue("9", 2, readyForDev))
			wantCommands(t, cmds, d.reopen("9", "development"))
			cmds, _ = d.send(reopened("9", "development"))
			wantResumeReason(t, cmds, "the session's reason")
		})
	}
}

func TestAStopThenAGoneWorktreeCreatesNothing(t *testing.T) {
	d := resumeDriver(t, failedRun(t, "broke")...)
	d.takeIssue(issue("9", 1, readyForDev))
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.WorkspaceGone{IssueID: issueID("9")})
	for _, c := range cmds {
		if _, ok := c.(core.CreateWorkspace); ok {
			t.Fatalf("got %#v after a stop, want no workspace", c)
		}
	}
	d.wantReason("9", "lfg", "crew stopped")
	if a := d.m.View().Issues[0].Actions[0]; a.Phase != core.PhaseEnded || a.Outcome.Reason.String() != "crew stopped" {
		t.Fatalf("action = %#v, want ended with crew stopped", a)
	}
}

func TestAFreshRunIsRecordedFromItsTakeToItsRelease(t *testing.T) {
	d := resumeDriver(t)
	d.running(issue("9", 1, readyForDev))
	started := d.events[len(d.events)-1].Time()
	d.settle(d.ended("9", "lfg", succeeded))

	w := space("9", "development")
	ws := crew.Workspace{Name: w.Workspace, Branch: w.Branch}
	got := make([]string, 0, len(d.recorded))
	for _, e := range d.recorded {
		got = append(got, strings.TrimPrefix(reflect.TypeOf(e).String(), "crew."))
		if opened, ok := e.(crew.WorkspaceOpened); ok && (opened.Workspace != ws || opened.Log != w.Log) {
			t.Errorf("opened %#v, want %v with its log", opened, ws)
		}
		if ended, ok := e.(crew.ActionEnded); ok {
			if s, _ := ended.SessionStarted.Get(); ended.Verdict != crew.Passed || ended.Target != (crew.Next{}) ||
				!s.Equal(started) {
				t.Errorf("end %#v, want passed, to the next, with its session's start", ended)
			}
		}
	}
	want := []string{
		"RunTaken", "TakeMoved", "WorkspaceAsked", "WorkspaceOpened", "ActionSessionAsked", "ActionSessionStarted",
		"ActionSessionEnded", "ActionEnded", "RouteChosen", "StepAsked", "StepEnded", "RunReleased",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("recorded:\n got %v\nwant %v", got, want)
	}
}

// Covers KTD18: a RecordFailed reaches no run, so it is reported even after
// its run was released, worded for each event a resume depends on.
func TestEveryEventAResumeDependsOnSaysSoWhenItFailsToWrite(t *testing.T) {
	journal := failedRun(t, "broke")
	d := resumeDriver(t)
	want := map[string]core.RunNotRecorded{
		"WorkspaceOpened":      {What: "its worktree issue-9-development"},
		"ActionSessionAsked":   {Action: "lfg", What: "the start of lfg"},
		"ActionSessionStarted": {Action: "lfg", What: "the start of lfg's session"},
		"ActionEnded":          {Action: "lfg", What: "the end of lfg"},
		"RouteChosen":          {Action: "lfg", What: "the route failed it chose"},
		"RunReleased":          {What: "its release"},
	}
	steps := 0
	for _, e := range journal {
		_, events := d.send(core.RecordFailed{Event: e, Reason: "disk full"})
		name := strings.TrimPrefix(reflect.TypeOf(e).String(), "crew.")
		w, says := want[name]
		if step, ok := e.(crew.StepEnded); ok {
			steps++
			w, says = core.RunNotRecorded{What: "the outcome of step " + strconv.Itoa(step.Step+1) + " of its route"}, true
		}
		if !says {
			wantEvents(t, events)
			continue
		}
		w.At, w.IssueID, w.IssueRef, w.Rule, w.Reason = d.now, issueID("9"), "#9", "development", "disk full"
		wantEvents(t, events, w)
	}
	if steps != 2 {
		t.Fatalf("the journal holds %d step outcomes, want the report's and the move's", steps)
	}
}

func TestTheStatusOfAResumedRunNamesItsWorktree(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("5", 1, ready))
		d.settle(d.ended("5", "acceptance", succeeded))
		d.settle(d.ended("5", "development", failed("tests fail")))
	})
	m := core.New(crewRules(), 2, core.Journaling(past), core.Reopening(), core.ReportingStatus())
	d := &driver{t: t, m: m, now: t0, inputs: 1000}
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
		reopened("5", "implement"), core.SessionStarted{IssueID: issueID("5"), Action: "development"}, core.Tick{},
	} {
		cmds, _ = d.send(in)
		answer(cmds)
	}
	if a := last.Actions(); len(a) != 2 || a[0].Workspace != "" || a[1].Workspace != "issue-5-implement" {
		t.Fatalf("actions = %#v, want the resumed development naming its worktree and acceptance none", a)
	}
}

// Covers R22, KTD-S12: a judge after a session that failed restarts the
// run at the session, which is told the judge's reason, and the judge then
// runs again as that session.
func TestAJudgeThatFailedIsTheReasonTheResumedSessionIsGiven(t *testing.T) {
	rules := crewRules()
	rules[0].Actions = append(rules[0].Actions, shellAction("pr-open", "gh pr view --json url"))
	reason := `the script exited 1: no pull requests found for branch "crew/issue-9-development"`
	past := journaled(t, rules, nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		d.ended("9", "lfg", succeeded)
		cmds, _ := d.send(core.ShellEnded{IssueID: issueID("9"), Action: "pr-open", Outcome: crew.ShellOutcome{
			Status: crew.Some(1), Reason: crew.NewCheckReason(reason),
		}})
		d.settle(cmds)
		wantHeld(t, d.m)
	})
	d := resumeDriverOf(t, rules, past...)

	cmds := d.takeIssue(issue("9", 2, readyForDev))
	wantCommands(t, cmds, d.reopen("9", "development"))
	cmds, _ = d.send(reopened("9", "development"))
	if s := startOf(t, cmds); s.Action != "lfg" {
		t.Fatalf("resumed at %q, want lfg", s.Action)
	}
	wantResumeReason(t, cmds, reason)

	d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
	run := runShellOf(t, d.ended("9", "lfg", succeeded))
	if run.Action != "pr-open" || run.Script.Session != "lfg" || run.Script.Dir != space("9", "development").Dir {
		t.Fatalf("judge = %#v, want pr-open after lfg in the reopened worktree", run)
	}
}
