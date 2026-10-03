package core_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The workflow's states in these tests, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	inReview       crew.State = "in review"
	needsAttention crew.State = "needs attention"
	readyToMerge   crew.State = "ready to merge"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// draft is the workflow of the boss's draft config (KTD5).
func draft() []crew.Stage {
	return []crew.Stage{
		{
			Name: "implement", Label: ready, MovesTo: inProgress, OnSuccess: readyToReview,
			OnFailure: needsAttention,
			Actions: []crew.Action{
				{Name: "acceptance", Prompt: "Implement test acceptance for issue {{.Issue.Ref}}"},
				{Name: "development", Prompt: "Implement development for issue {{.Issue.Ref}}"},
			},
		},
		{
			Name: "review", Label: readyToReview, MovesTo: inReview, OnSuccess: readyToMerge,
			OnFailure: needsAttention,
			Actions:   []crew.Action{{Name: "custom_review", Prompt: "Review implementation for issue {{.Issue.Ref}}"}},
		},
	}
}

// issue returns an issue keyed key, opened minute minutes after t0.
func issue(key string, minute int, states ...crew.State) crew.Issue {
	return crew.Issue{
		Key: key, Ref: "#" + key, Title: "Issue " + key, URL: "https://example.com/issues/" + key,
		Created: t0.Add(time.Duration(minute) * time.Minute), States: states,
	}
}

// space is the workspace an engine would create for key and action.
func space(key, action string) core.WorkspaceReady {
	name := "issue-" + key + "-" + action
	return core.WorkspaceReady{
		IssueKey: key, Action: action, Workspace: name, Dir: "/repo/.crew/worktrees/" + name,
		Branch: "crew/" + name, Log: ".crew/logs/" + name + ".log",
	}
}

// driver feeds a model inputs one second apart, as the engine would stamp them.
type driver struct {
	t   *testing.T
	m   *core.Model
	now time.Time
}

func newDriver(t *testing.T, workflow []crew.Stage, maxParallel int) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(workflow, maxParallel), now: t0}
}

func (d *driver) send(in core.Input) ([]core.Command, []core.Event) {
	d.now = d.now.Add(time.Second)
	return d.m.Update(in.Stamped(d.now))
}

// settle answers cmds as a healthy engine would: moves succeed, workspaces
// are created and sessions start. Listings and stops are left unanswered.
func (d *driver) settle(cmds []core.Command) {
	d.t.Helper()
	for len(cmds) > 0 {
		var next []core.Command
		for _, c := range cmds {
			var out []core.Command
			switch c := c.(type) {
			case core.Move:
				out, _ = d.send(core.CallResult{ID: c.ID, Result: core.ResultDone})
			case core.ReportFailure:
				out, _ = d.send(core.CallResult{ID: c.ID, Result: core.ResultDone})
			case core.CreateWorkspace:
				out, _ = d.send(space(c.Issue.Key, c.Action))
			case core.StartSession:
				out, _ = d.send(core.SessionStarted{IssueKey: c.IssueKey, Action: c.Action})
			}
			next = append(next, out...)
		}
		cmds = next
	}
}

// poll ticks and answers the listing with issues.
func (d *driver) poll(issues ...crew.Issue) ([]core.Command, []core.Event) {
	d.t.Helper()
	cmds, _ := d.send(core.Tick{})
	if len(cmds) == 0 {
		d.t.Fatalf("tick issued no listing")
	}
	return d.send(core.IssuesListed{Issues: issues})
}

// running takes issues in ready and starts all their sessions.
func (d *driver) running(issues ...crew.Issue) {
	d.t.Helper()
	cmds, _ := d.poll(issues...)
	d.settle(cmds)
}

// noIDs returns cmds with the call IDs zeroed, so they compare by content.
func noIDs(cmds []core.Command) []core.Command {
	out := make([]core.Command, 0, len(cmds))
	for _, c := range cmds {
		switch c := c.(type) {
		case core.Move:
			c.ID = 0
			out = append(out, c)
		case core.ReportFailure:
			c.ID = 0
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// moveID returns the ID of the move of issue key in cmds.
func moveID(t *testing.T, cmds []core.Command, key string) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if m, ok := c.(core.Move); ok && m.IssueKey == key {
			return m.ID
		}
	}
	t.Fatalf("no move of %s in %#v", key, cmds)
	return 0
}

// reportID returns the ID of the failure report of issue key in cmds.
func reportID(t *testing.T, cmds []core.Command, key string) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if r, ok := c.(core.ReportFailure); ok && r.Report.IssueKey == key {
			return r.ID
		}
	}
	t.Fatalf("no failure report of %s in %#v", key, cmds)
	return 0
}

func wantCommands(t *testing.T, got []core.Command, want ...core.Command) {
	t.Helper()
	if want == nil {
		want = []core.Command{}
	}
	if g := noIDs(got); !reflect.DeepEqual(g, want) {
		t.Fatalf("commands:\n got %#v\nwant %#v", g, want)
	}
}

func hasEvent(t *testing.T, events []core.Event, want core.Event) {
	t.Helper()
	for _, e := range events {
		if reflect.DeepEqual(e, want) {
			return
		}
	}
	t.Fatalf("no event %#v in %#v", want, events)
}

func claimOf(t *testing.T, m *core.Model, key string) core.Claim {
	t.Helper()
	for _, iv := range m.View().Issues {
		if iv.Issue.Key == key {
			return iv.Claim
		}
	}
	t.Fatalf("issue %s is not held", key)
	return 0
}

func wantHeld(t *testing.T, m *core.Model, keys ...string) {
	t.Helper()
	var got []string
	for _, iv := range m.View().Issues {
		got = append(got, iv.Issue.Key)
	}
	if !reflect.DeepEqual(got, keys) {
		t.Fatalf("held issues: got %v, want %v", got, keys)
	}
}

func failed(reason string) crew.Outcome { return crew.Outcome{Succeeded: false, Reason: reason} }

var succeeded = crew.Outcome{Succeeded: true, Reason: "done"}

func TestAE1TakesUpToMaxParallelIssuesAndStartsEveryAction(t *testing.T) {
	d := newDriver(t, draft(), 2)
	i1, i2, i3 := issue("1", 1, ready), issue("2", 2, ready), issue("3", 3, ready)

	cmds, _ := d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})

	cmds, events := d.send(core.IssuesListed{Issues: []crew.Issue{i1, i2, i3}})
	wantCommands(t, cmds,
		core.Move{IssueKey: "1", From: ready, To: inProgress},
		core.Move{IssueKey: "2", From: ready, To: inProgress},
	)
	at := d.now
	wantEvents := []core.Event{
		core.IssueTaken{At: at, Issue: i1, Stage: "implement", From: ready, To: inProgress},
		core.IssueTaken{At: at, Issue: i2, Stage: "implement", From: ready, To: inProgress},
		core.PollDone{At: at, Listed: 3, Taken: 2},
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events:\n got %#v\nwant %#v", events, wantEvents)
	}

	var all []core.Command
	for _, it := range []crew.Issue{i1, i2} {
		created, events := d.send(core.CallResult{ID: moveID(t, cmds, it.Key), Result: core.ResultDone})
		wantCommands(t, created,
			core.CreateWorkspace{Issue: it, Action: "acceptance"},
			core.CreateWorkspace{Issue: it, Action: "development"},
		)
		hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: it.Key, IssueRef: it.Ref, From: ready, To: inProgress})
		all = append(all, created...)
	}

	var sessions []core.Command
	for _, key := range []string{"1", "2"} {
		for _, action := range []string{"acceptance", "development"} {
			started, _ := d.send(space(key, action))
			sessions = append(sessions, started...)
		}
	}
	wantCommands(t, sessions,
		core.StartSession{IssueKey: "1", Action: "acceptance", Dir: "/repo/.crew/worktrees/issue-1-acceptance", Prompt: "Implement test acceptance for issue #1", Log: ".crew/logs/issue-1-acceptance.log"},
		core.StartSession{IssueKey: "1", Action: "development", Dir: "/repo/.crew/worktrees/issue-1-development", Prompt: "Implement development for issue #1", Log: ".crew/logs/issue-1-development.log"},
		core.StartSession{IssueKey: "2", Action: "acceptance", Dir: "/repo/.crew/worktrees/issue-2-acceptance", Prompt: "Implement test acceptance for issue #2", Log: ".crew/logs/issue-2-acceptance.log"},
		core.StartSession{IssueKey: "2", Action: "development", Dir: "/repo/.crew/worktrees/issue-2-development", Prompt: "Implement development for issue #2", Log: ".crew/logs/issue-2-development.log"},
	)

	// #3 waits: no command concerns it and the core does not hold it.
	for _, c := range append(all, sessions...) {
		if issueKey(c) == "3" {
			t.Fatalf("#3 was touched by %#v", c)
		}
	}
	wantHeld(t, d.m, "1", "2")
}

// issueKey returns the key of the issue c concerns.
func issueKey(c core.Command) string {
	switch c := c.(type) {
	case core.Move:
		return c.IssueKey
	case core.ReportFailure:
		return c.Report.IssueKey
	case core.CreateWorkspace:
		return c.Issue.Key
	case core.StartSession:
		return c.IssueKey
	case core.StopSession:
		return c.IssueKey
	}
	return ""
}

func TestAE2IssueMovesOnSuccessOnlyOnceEveryActionEndedCleanly(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))

	cmds, events := d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	wantCommands(t, cmds)
	hasEvent(t, events, core.ActionEnded{
		At: d.now, IssueKey: "1", IssueRef: "#1", Stage: "implement", Action: "acceptance", Outcome: succeeded,
		Workspace: "issue-1-acceptance", Log: ".crew/logs/issue-1-acceptance.log",
	})

	// A poll meanwhile leaves #1 in progress: only the listing is issued.
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})
	if c := claimOf(t, d.m, "1"); c != core.ClaimRunning {
		t.Fatalf("claim of #1: got %v, want running", c)
	}

	cmds, _ = d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
	wantCommands(t, cmds, core.Move{IssueKey: "1", From: inProgress, To: readyToReview})

	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview})
	wantHeld(t, d.m)
}

func TestAE3AE5FailedActionWaitsForSiblingsThenNeedsAttention(t *testing.T) {
	tests := []struct {
		name    string
		outcome crew.Outcome
	}{
		{name: "AE3 session failed", outcome: failed("tests do not pass")},
		{name: "AE5 usage limit is an ordinary failure", outcome: failed("usage limit")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, draft(), 2)
			d.running(issue("1", 1, ready))

			cmds, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: tt.outcome})
			wantCommands(t, cmds)

			cmds, _ = d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
			wantCommands(t, cmds,
				core.Move{IssueKey: "1", From: inProgress, To: needsAttention},
				core.ReportFailure{Report: crew.FailureReport{IssueKey: "1", IssueRef: "#1", Failures: []crew.ActionFailure{{
					Action: "development", Reason: tt.outcome.Reason,
					Workspace: "issue-1-development", Log: ".crew/logs/issue-1-development.log",
				}}}},
			)

			_, events := d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
			hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: "1", IssueRef: "#1", From: inProgress, To: needsAttention})
			wantHeld(t, d.m, "1") // its report is still in flight
			_, events = d.send(core.CallResult{ID: reportID(t, cmds, "1"), Result: core.ResultDone})
			hasEvent(t, events, core.FailureReported{At: d.now, IssueKey: "1", IssueRef: "#1"})
			wantHeld(t, d.m)
		})
	}
}

func TestAE1AE5FailedStageMovesToItsOwnOnFailure(t *testing.T) {
	workflow := draft()
	workflow[1].OnFailure = workflow[0].Label // a failed review goes back to implement
	d := newDriver(t, workflow, 2)

	// AE1: implement fails, so #1 moves to implement's on_failure.
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: failed("tests do not pass")})
	if got := noIDs(cmds)[0]; got != (core.Move{IssueKey: "1", From: inProgress, To: needsAttention}) {
		t.Fatalf("failed implement: got %#v, want the move to needs attention", got)
	}
	d.settle(cmds)

	// AE5: review fails, so #2 moves to review's on_failure, implement's label.
	d.running(issue("2", 2, readyToReview))
	cmds, _ = d.send(core.SessionEnded{IssueKey: "2", Action: "custom_review", Outcome: failed("changes requested")})
	wantCommands(t, cmds,
		core.Move{IssueKey: "2", From: inReview, To: ready},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "2", IssueRef: "#2", Failures: []crew.ActionFailure{{
			Action: "custom_review", Reason: "changes requested",
			Workspace: "issue-2-custom_review", Log: ".crew/logs/issue-2-custom_review.log",
		}}}},
	)
	d.settle(cmds)
	wantHeld(t, d.m)

	// On the next listing implement takes #2 again.
	cmds, _ = d.poll(issue("2", 2, ready))
	wantCommands(t, cmds, core.Move{IssueKey: "2", From: ready, To: inProgress})
}

func TestAE8IssueInTwoStatesIsSkippedUntilItIsInOne(t *testing.T) {
	d := newDriver(t, draft(), 2)

	cmds, events := d.poll(issue("4", 1, ready, needsAttention))
	wantCommands(t, cmds)
	hasEvent(t, events, core.IssueSkipped{At: d.now, IssueKey: "4", IssueRef: "#4", States: []crew.State{ready, needsAttention}})
	wantHeld(t, d.m)

	cmds, _ = d.poll(issue("4", 1, ready))
	wantCommands(t, cmds, core.Move{IssueKey: "4", From: ready, To: inProgress})
}

func TestBlockedIssueIsNotTakenUntilNothingBlocksIt(t *testing.T) {
	d := newDriver(t, draft(), 1)
	blocked := issue("4", 1, ready)
	blocked.Blocked = true

	cmds, _ := d.poll(blocked, issue("5", 2, ready))
	wantCommands(t, cmds, core.Move{IssueKey: "5", From: ready, To: inProgress})
	wantHeld(t, d.m, "5")

	d = newDriver(t, draft(), 1)
	cmds, _ = d.poll(blocked)
	wantCommands(t, cmds)
	wantHeld(t, d.m)

	cmds, _ = d.poll(issue("4", 1, ready))
	wantCommands(t, cmds, core.Move{IssueKey: "4", From: ready, To: inProgress})

	d = newDriver(t, draft(), 1)
	cmds, _ = d.poll(prioritized(blocked, 1), issue("5", 2, ready))
	wantCommands(t, cmds, core.Move{IssueKey: "5", From: ready, To: inProgress})
}

func TestAE9StopJudgesEndedIssuesAndStopsRunningOnes(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
	wantCommands(t, verdict, core.Move{IssueKey: "1", From: inProgress, To: readyToReview})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds,
		core.StopSession{IssueKey: "2", Action: "acceptance"},
		core.StopSession{IssueKey: "2", Action: "development"},
	)
	if d.m.Stopped() {
		t.Fatal("stopped while issues are held")
	}

	_, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview})

	d.send(core.SessionEnded{IssueKey: "2", Action: "acceptance", Outcome: failed("stopped")})
	cmds, _ = d.send(core.SessionEnded{IssueKey: "2", Action: "development", Outcome: failed("stopped")})
	wantCommands(t, cmds,
		core.Move{IssueKey: "2", From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "2", IssueRef: "#2", Failures: []crew.ActionFailure{
			{Action: "acceptance", Reason: "stopped", Workspace: "issue-2-acceptance", Log: ".crew/logs/issue-2-acceptance.log"},
			{Action: "development", Reason: "stopped", Workspace: "issue-2-development", Log: ".crew/logs/issue-2-development.log"},
		}}},
	)

	d.send(core.CallResult{ID: moveID(t, cmds, "2"), Result: core.ResultDone})
	if d.m.Stopped() {
		t.Fatal("stopped while #2's report is in flight")
	}
	_, events = d.send(core.CallResult{ID: reportID(t, cmds, "2"), Result: core.ResultDone})
	if !d.m.Stopped() {
		t.Fatal("not stopped once every verdict call settled")
	}
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestActionThatFailsToStartFailsAloneWhileSiblingsRun(t *testing.T) {
	tests := []struct {
		name      string
		fail      func(d *driver) []core.Command
		workspace string
		log       string
	}{
		{
			name: "workspace failed",
			fail: func(d *driver) []core.Command {
				cmds, _ := d.send(core.WorkspaceFailed{IssueKey: "1", Action: "acceptance", Reason: "fetch failed"})
				return cmds
			},
		},
		{
			name: "session failed to start",
			fail: func(d *driver) []core.Command {
				d.send(space("1", "acceptance"))
				cmds, _ := d.send(core.SessionFailedToStart{IssueKey: "1", Action: "acceptance", Reason: "fetch failed"})
				return cmds
			},
			workspace: "issue-1-acceptance",
			log:       ".crew/logs/issue-1-acceptance.log",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, draft(), 2)
			cmds, _ := d.poll(issue("1", 1, ready))
			d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

			wantCommands(t, tt.fail(d))
			cmds, _ = d.send(space("1", "development"))
			wantCommands(t, cmds, core.StartSession{
				IssueKey: "1", Action: "development", Dir: "/repo/.crew/worktrees/issue-1-development",
				Prompt: "Implement development for issue #1", Log: ".crew/logs/issue-1-development.log",
			})
			d.send(core.SessionStarted{IssueKey: "1", Action: "development"})

			cmds, _ = d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
			wantCommands(t, cmds,
				core.Move{IssueKey: "1", From: inProgress, To: needsAttention},
				core.ReportFailure{Report: crew.FailureReport{IssueKey: "1", IssueRef: "#1", Failures: []crew.ActionFailure{
					{Action: "acceptance", Reason: "fetch failed", Workspace: tt.workspace, Log: tt.log},
				}}},
			)
		})
	}
}

func TestPromptThatFailsToRenderFailsItsAction(t *testing.T) {
	workflow := draft()
	workflow[0].Actions[0].Prompt = "Fix {{.Issue.Number}}"
	d := newDriver(t, workflow, 2)
	cmds, _ := d.poll(issue("1", 1, ready))

	cmds, events := d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("1", 1, ready), Action: "development"})
	for _, e := range events {
		if ended, ok := e.(core.ActionEnded); ok && ended.Action == "acceptance" {
			if ended.Outcome.Succeeded || !strings.Contains(ended.Outcome.Reason, "Number") {
				t.Fatalf("acceptance ended with %#v, want a failure naming the render error", ended.Outcome)
			}
			return
		}
	}
	t.Fatalf("no ActionEnded for acceptance in %#v", events)
}

// judgedNeedingAttention runs #1 to a failed verdict and returns the verdict
// commands, both in flight.
func judgedNeedingAttention(d *driver) []core.Command {
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: failed("broke")})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
	return cmds
}

func TestVerdictMoveThatFailsTransientlyIsOwedAndRetriedAtTheNextTick(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})

	cmds, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantCommands(t, cmds)
	owed := core.Call{Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview}
	hasEvent(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})
	if got := d.m.View().Owed; !reflect.DeepEqual(got, []core.Call{owed}) {
		t.Fatalf("owed: got %#v, want %#v", got, []core.Call{owed})
	}
	if c := claimOf(t, d.m, "1"); c != core.ClaimOwed {
		t.Fatalf("claim of #1: got %v, want owed", c)
	}

	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry,
		core.ListIssues{States: []crew.State{ready, readyToReview}},
		core.Move{IssueKey: "1", From: inProgress, To: readyToReview},
	)

	// The retry is in flight: the next tick does not issue it again.
	d.send(core.IssuesListed{})
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})

	_, events = d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview})
	wantHeld(t, d.m)
	if got := d.m.View().Owed; got != nil {
		t.Fatalf("owed after the retry succeeded: %#v", got)
	}
}

func TestVerdictCallMovedMeanwhileOrRefusedIsDroppedAndReported(t *testing.T) {
	for _, result := range []core.Result{core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newDriver(t, draft(), 2)
			verdict := judgedNeedingAttention(d)

			_, events := d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: result, Reason: "nope"})
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: inProgress, To: needsAttention,
			}})
			_, events = d.send(core.CallResult{ID: reportID(t, verdict, "1"), Result: result, Reason: "nope"})
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallReport, IssueKey: "1", IssueRef: "#1",
			}})
			wantHeld(t, d.m)

			cmds, _ := d.send(core.Tick{})
			wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})
		})
	}
}

func TestTakeMovedMeanwhileOrRefusedReleasesTheIssue(t *testing.T) {
	for _, result := range []core.Result{core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))

			cmds, events := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: result, Reason: "nope"})
			wantCommands(t, cmds)
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress,
			}})
			wantHeld(t, d.m)

			cmds, _ = d.poll(issue("2", 2, ready))
			wantCommands(t, cmds, core.Move{IssueKey: "2", From: ready, To: inProgress})
		})
	}
}

// A take that failed transiently may have landed, so the issue stays held
// and the take is owed: the retry, which the tracker makes idempotent, either
// moves it or finds it already moved, and the stage proceeds (KTD8).
func TestTakeThatFailsTransientlyIsOwedAndRetriedAtTheNextTick(t *testing.T) {
	d := newDriver(t, draft(), 1)
	i1 := issue("1", 1, ready)
	take, _ := d.poll(i1)

	cmds, events := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantCommands(t, cmds)
	owed := core.Call{Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress}
	hasEvent(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})
	want := core.View{
		Issues: []core.IssueView{{
			Issue: i1, Stage: "implement", Claim: core.ClaimOwed,
			Actions: []core.ActionView{
				{Name: "acceptance", Phase: core.PhaseWaiting},
				{Name: "development", Phase: core.PhaseWaiting},
			},
		}},
		Owed: []core.Call{owed},
	}
	if v := d.m.View(); !reflect.DeepEqual(v, want) {
		t.Fatalf("view:\n got %#v\nwant %#v", v, want)
	}

	// The next tick retries the take; its listing, still showing #1 in
	// ready, neither takes #1 again nor takes #2 into the slot #1 holds.
	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry,
		core.ListIssues{States: []crew.State{ready, readyToReview}},
		core.Move{IssueKey: "1", From: ready, To: inProgress},
	)
	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{i1, issue("2", 2, ready)}})
	wantCommands(t, cmds)

	cmds, events = d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.CreateWorkspace{Issue: i1, Action: "acceptance"},
		core.CreateWorkspace{Issue: i1, Action: "development"},
	)
	hasEvent(t, events, core.IssueMoved{At: d.now, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress})
	if c := claimOf(t, d.m, "1"); c != core.ClaimRunning {
		t.Fatalf("claim of #1: got %v, want running", c)
	}
	if got := d.m.View().Owed; got != nil {
		t.Fatalf("owed after the retry succeeded: %#v", got)
	}
}

func TestOwedTakeRetryMovedMeanwhileOrRefusedReleasesTheIssue(t *testing.T) {
	for _, result := range []core.Result{core.ResultMovedMeanwhile, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))
			d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
			retry, _ := d.send(core.Tick{})

			cmds, events := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: result, Reason: "nope"})
			wantCommands(t, cmds)
			hasEvent(t, events, core.CallDropped{At: d.now, Result: result, Reason: "nope", Call: core.Call{
				Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress,
			}})
			wantHeld(t, d.m)
		})
	}
}

// At stop, an owed take gets its one final try. If it lands, the issue is
// in moves_to with nothing started, so it needs attention like an issue
// whose take landed after the stop; if it fails, the core gives it up.
func TestStopGivesAnOwedTakeOneFinalTry(t *testing.T) {
	stoppedReport := core.ReportFailure{Report: crew.FailureReport{IssueKey: "1", IssueRef: "#1", Failures: []crew.ActionFailure{
		{Action: "acceptance", Reason: "crew stopped"},
		{Action: "development", Reason: "crew stopped"},
	}}}
	tests := []struct {
		name string
		// final returns the final try's command, from a take that is owed or
		// in flight at stop.
		final func(d *driver, take []core.Command) []core.Command
	}{
		{
			name: "owed at stop",
			final: func(d *driver, take []core.Command) []core.Command {
				d.send(core.CallResult{ID: moveID(d.t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
				cmds, _ := d.send(core.StopRequested{})
				return cmds
			},
		},
		{
			name: "in flight at stop",
			final: func(d *driver, take []core.Command) []core.Command {
				if cmds, _ := d.send(core.StopRequested{}); len(cmds) != 0 {
					d.t.Fatalf("stop issued %#v while the take is in flight", cmds)
				}
				cmds, _ := d.send(core.CallResult{ID: moveID(d.t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})
				return cmds
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name+", final try done", func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))
			final := tt.final(d, take)
			wantCommands(t, final, core.Move{IssueKey: "1", From: ready, To: inProgress})

			cmds, _ := d.send(core.CallResult{ID: moveID(t, final, "1"), Result: core.ResultDone})
			wantCommands(t, cmds, core.Move{IssueKey: "1", From: inProgress, To: needsAttention}, stoppedReport)
			d.settle(cmds)
			if !d.m.Stopped() {
				t.Fatal("not stopped once the verdict calls settled")
			}
		})
		t.Run(tt.name+", final try failed", func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			take, _ := d.poll(issue("1", 1, ready))
			final := tt.final(d, take)

			cmds, events := d.send(core.CallResult{ID: moveID(t, final, "1"), Result: core.ResultFailed, Reason: "still down"})
			wantCommands(t, cmds)
			hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
				Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: ready, To: inProgress,
			}})
			if !d.m.Stopped() {
				t.Fatal("not stopped once the owed take had its final try")
			}
			hasEvent(t, events, core.Stopped{At: d.now})
		})
	}
}

// prioritized returns i with priority p, 1 the highest.
func prioritized(i crew.Issue, p int) crew.Issue {
	i.Priority = p
	return i
}

func TestPicksTheHighestPriorityThenLaterStagesThenTheOldestIssue(t *testing.T) {
	tests := []struct {
		name   string
		issues []crew.Issue
		want   core.Move
	}{
		{
			name:   "review before implement",
			issues: []crew.Issue{issue("5", 1, ready), issue("6", 2, readyToReview)},
			want:   core.Move{IssueKey: "6", From: readyToReview, To: inReview},
		},
		{
			name:   "oldest first within a stage",
			issues: []crew.Issue{issue("8", 9, ready), issue("7", 3, ready)},
			want:   core.Move{IssueKey: "7", From: ready, To: inProgress},
		},
		{
			// AE1: an Urgent issue passes an unprioritized one of a later stage.
			name:   "priority before a later stage",
			issues: []crew.Issue{issue("6", 1, readyToReview), prioritized(issue("5", 2, ready), 1)},
			want:   core.Move{IssueKey: "5", From: ready, To: inProgress},
		},
		{
			// AE2: same priority and stage, the older issue first.
			name:   "oldest first at the same priority and stage",
			issues: []crew.Issue{prioritized(issue("8", 9, ready), 2), prioritized(issue("7", 3, ready), 2)},
			want:   core.Move{IssueKey: "7", From: ready, To: inProgress},
		},
		{
			// AE3: same priority, the later stage first.
			name:   "later stage first at the same priority",
			issues: []crew.Issue{prioritized(issue("5", 1, ready), 3), prioritized(issue("6", 2, readyToReview), 3)},
			want:   core.Move{IssueKey: "6", From: readyToReview, To: inReview},
		},
		{
			// AE4: the lowest priority still passes an older issue with none.
			name:   "any priority before none",
			issues: []crew.Issue{issue("5", 1, ready), prioritized(issue("6", 9, ready), 4)},
			want:   core.Move{IssueKey: "6", From: ready, To: inProgress},
		},
		{
			name:   "higher priority before an older lower one",
			issues: []crew.Issue{prioritized(issue("5", 1, ready), 2), prioritized(issue("6", 9, ready), 1)},
			want:   core.Move{IssueKey: "6", From: ready, To: inProgress},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, draft(), 1)
			cmds, _ := d.poll(tt.issues...)
			wantCommands(t, cmds, tt.want)
		})
	}
}

func TestHeldIssueIsNotTakenAgain(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.poll(issue("1", 1, ready)) // its take move stays in flight

	cmds, _ := d.poll(issue("1", 1, ready))
	wantCommands(t, cmds)
	wantHeld(t, d.m, "1")
}

func TestAtMostOneListingIsOutstanding(t *testing.T) {
	d := newDriver(t, draft(), 2)
	list := core.ListIssues{States: []crew.State{ready, readyToReview}}

	cmds, _ := d.send(core.Tick{})
	wantCommands(t, cmds, list)
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds)

	_, events := d.send(core.ListFailed{Reason: "gh: network down"})
	hasEvent(t, events, core.ListingFailed{At: d.now, Reason: "gh: network down"})
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds, list)
}

func TestStopDuringTakeStartsNothingAndNeedsAttention(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds)
	if c := claimOf(t, d.m, "1"); c != core.ClaimStopping {
		t.Fatalf("claim of #1: got %v, want stopping", c)
	}

	cmds, _ = d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.Move{IssueKey: "1", From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "1", IssueRef: "#1", Failures: []crew.ActionFailure{
			{Action: "acceptance", Reason: "crew stopped"},
			{Action: "development", Reason: "crew stopped"},
		}}},
	)
}

func TestStopDuringSetupStartsNothingMoreAndStopsWhatStarted(t *testing.T) {
	d := newDriver(t, draft(), 2)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	d.send(space("1", "development")) // its StartSession is in flight

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds)

	cmds, _ = d.send(space("1", "acceptance"))
	wantCommands(t, cmds)
	cmds, _ = d.send(core.SessionStarted{IssueKey: "1", Action: "development"})
	wantCommands(t, cmds, core.StopSession{IssueKey: "1", Action: "development"})

	cmds, _ = d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: failed("stopped")})
	wantCommands(t, cmds,
		core.Move{IssueKey: "1", From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "1", IssueRef: "#1", Failures: []crew.ActionFailure{
			{Action: "acceptance", Reason: "crew stopped", Workspace: "issue-1-acceptance"},
			{Action: "development", Reason: "stopped", Workspace: "issue-1-development", Log: ".crew/logs/issue-1-development.log"},
		}}},
	)
}

func TestStopGivesEachOwedCallOneFinalTry(t *testing.T) {
	d := newDriver(t, draft(), 2)
	verdict := judgedNeedingAttention(d)
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds, core.Move{IssueKey: "1", From: inProgress, To: needsAttention})

	// The report, in flight at stop, fails transiently: it gets its final try.
	retry, _ := d.send(core.CallResult{ID: reportID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	if len(retry) != 1 {
		t.Fatalf("report retry: got %#v, want one ReportFailure", retry)
	}
	reportRetry := reportID(t, retry, "1")

	_, events := d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultFailed, Reason: "still down"})
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: inProgress, To: needsAttention,
	}})
	cmds, events = d.send(core.CallResult{ID: reportRetry, Result: core.ResultFailed, Reason: "still down"})
	wantCommands(t, cmds)
	hasEvent(t, events, core.CallDropped{At: d.now, Result: core.ResultFailed, Reason: "still down", Call: core.Call{
		Kind: core.CallReport, IssueKey: "1", IssueRef: "#1",
	}})
	if !d.m.Stopped() {
		t.Fatal("not stopped once every owed call had its final try")
	}
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestStopWithNothingHeldStopsAtOnceAndPollsNoMore(t *testing.T) {
	d := newDriver(t, draft(), 2)
	cmds, _ := d.send(core.Tick{}) // a listing is outstanding at stop

	_, events := d.send(core.StopRequested{})
	if !d.m.Stopped() {
		t.Fatal("not stopped with nothing held")
	}
	hasEvent(t, events, core.Stopped{At: d.now})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{ready, readyToReview}})

	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, ready)}})
	wantCommands(t, cmds)
	cmds, events = d.send(core.Tick{})
	wantCommands(t, cmds)
	_, events2 := d.send(core.StopRequested{})
	if len(events)+len(events2) != 0 {
		t.Fatalf("events after stop: %#v %#v", events, events2)
	}
}

func TestViewShowsRunningActionsAndSharesNoMemory(t *testing.T) {
	d := newDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	d.send(space("1", "acceptance"))
	d.send(core.SessionStarted{IssueKey: "1", Action: "acceptance"})
	started := d.now

	want := core.View{Issues: []core.IssueView{{
		Issue: issue("1", 1, ready), Stage: "implement", Claim: core.ClaimRunning,
		Actions: []core.ActionView{
			{
				Name: "acceptance", Phase: core.PhaseRunning, Workspace: "issue-1-acceptance",
				Branch: "crew/issue-1-acceptance", Log: ".crew/logs/issue-1-acceptance.log", Started: started,
			},
			{Name: "development", Phase: core.PhaseCreating},
		},
	}}}
	v := d.m.View()
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("view:\n got %#v\nwant %#v", v, want)
	}

	v.Issues[0].Issue.States[0] = "done"
	v.Issues[0].Actions[0].Name = "changed"
	if again := d.m.View(); !reflect.DeepEqual(again, want) {
		t.Fatalf("changing a view changed the model:\n got %#v\nwant %#v", again, want)
	}
}

// limit is the run time limit of the wind-down tests.
const limit = time.Hour

func wantEvents(t *testing.T, got []core.Event, want ...core.Event) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got %#v\nwant %#v", got, want)
	}
}

func TestAE2TimeUpWithNothingHeldWindsDownAndStopsAtOnce(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.send(core.Tick{}) // a listing is outstanding when time is up

	cmds, events := d.send(core.TimeUp{Limit: limit})
	wantCommands(t, cmds)
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit}, core.Stopped{At: d.now})
	if !d.m.Stopped() {
		t.Fatal("not stopped with nothing held")
	}

	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, ready)}})
	wantCommands(t, cmds)
}

func TestAE3TimeUpLetsARunningIssueFinishAndTakesNothingNew(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))

	cmds, events := d.send(core.TimeUp{Limit: limit})
	wantCommands(t, cmds)
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit})
	if v := d.m.View(); !v.TimeUp || v.Stopping {
		t.Fatalf("view: TimeUp %v, Stopping %v; want true, false", v.TimeUp, v.Stopping)
	}

	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds)
	cmds, _ = d.send(core.IssuesListed{Issues: []crew.Issue{issue("43", 2, ready)}})
	wantCommands(t, cmds)

	d.send(core.SessionEnded{IssueKey: "42", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "42", Action: "development", Outcome: succeeded})
	wantCommands(t, verdict, core.Move{IssueKey: "42", From: inProgress, To: readyToReview})
	if d.m.Stopped() {
		t.Fatal("stopped while #42's verdict move is in flight")
	}

	_, events = d.send(core.CallResult{ID: moveID(t, verdict, "42"), Result: core.ResultDone})
	wantEvents(t, events,
		core.IssueMoved{At: d.now, IssueKey: "42", IssueRef: "#42", From: inProgress, To: readyToReview},
		core.Stopped{At: d.now},
	)
	if !d.m.Stopped() {
		t.Fatal("not stopped once #42 was judged")
	}
	if d.m.View().Stopping {
		t.Fatal("the view says a stop was requested; none was")
	}
}

func TestAE3AnIssueThatFailsWhileWindingDownNeedsAttentionAsUsual(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	d.send(core.SessionEnded{IssueKey: "42", Action: "acceptance", Outcome: failed("broke")})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "42", Action: "development", Outcome: succeeded})
	wantCommands(t, cmds,
		core.Move{IssueKey: "42", From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "42", IssueRef: "#42", Failures: []crew.ActionFailure{
			{Action: "acceptance", Reason: "broke", Workspace: "issue-42-acceptance", Log: ".crew/logs/issue-42-acceptance.log"},
		}}},
	)

	d.send(core.CallResult{ID: moveID(t, cmds, "42"), Result: core.ResultDone})
	_, events := d.send(core.CallResult{ID: reportID(t, cmds, "42"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestATakeInFlightWhenTimeIsUpStartsItsActions(t *testing.T) {
	d := newDriver(t, draft(), 2)
	i42 := issue("42", 1, ready)
	take, _ := d.poll(i42)
	d.send(core.TimeUp{Limit: limit})

	cmds, _ := d.send(core.CallResult{ID: moveID(t, take, "42"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.CreateWorkspace{Issue: i42, Action: "acceptance"},
		core.CreateWorkspace{Issue: i42, Action: "development"},
	)
}

func TestAnOwedTakeWhenTimeIsUpIsRetriedAtTicksAndThenRuns(t *testing.T) {
	d := newDriver(t, draft(), 2)
	i42 := issue("42", 1, ready)
	take, _ := d.poll(i42)
	d.send(core.CallResult{ID: moveID(t, take, "42"), Result: core.ResultFailed, Reason: "timeout"})

	_, events := d.send(core.TimeUp{Limit: limit})
	wantEvents(t, events, core.WindingDown{At: d.now, Limit: limit})

	retry, events := d.send(core.Tick{})
	wantCommands(t, retry, core.Move{IssueKey: "42", From: ready, To: inProgress})
	if len(events) != 0 || d.m.Stopped() {
		t.Fatalf("a tick with an owed take: events %#v, stopped %v", events, d.m.Stopped())
	}

	cmds, _ := d.send(core.CallResult{ID: moveID(t, retry, "42"), Result: core.ResultDone})
	wantCommands(t, cmds,
		core.CreateWorkspace{Issue: i42, Action: "acceptance"},
		core.CreateWorkspace{Issue: i42, Action: "development"},
	)
}

func TestWhileWindingDownOwedCallsAreRetriedAtTicksThenGetAFinalTry(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready), issue("2", 2, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	d.send(core.TimeUp{Limit: limit})

	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry, core.Move{IssueKey: "1", From: inProgress, To: readyToReview})
	owed := core.Call{Kind: core.CallMove, IssueKey: "1", IssueRef: "#1", From: inProgress, To: readyToReview}
	_, events := d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallOwed{At: d.now, Call: owed, Reason: "timeout"})

	// #2's last action ends: nothing is left to end, so the owed move gets
	// its final try.
	d.send(core.SessionEnded{IssueKey: "2", Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "2", Action: "development", Outcome: succeeded})
	wantCommands(t, cmds,
		core.Move{IssueKey: "2", From: inProgress, To: readyToReview},
		core.Move{IssueKey: "1", From: inProgress, To: readyToReview},
	)

	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultFailed, Reason: "timeout"})
	wantEvents(t, events, core.CallDropped{At: d.now, Call: owed, Result: core.ResultFailed, Reason: "timeout"})
	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "2"), Result: core.ResultDone})
	hasEvent(t, events, core.Stopped{At: d.now})
}

func TestAE4AStopWhileWindingDownStopsRunningSessionsAsUsual(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	cmds, _ := d.send(core.StopRequested{})
	wantCommands(t, cmds,
		core.StopSession{IssueKey: "42", Action: "acceptance"},
		core.StopSession{IssueKey: "42", Action: "development"},
	)
	if !d.m.View().Stopping {
		t.Fatal("the view does not say a stop was requested")
	}

	d.send(core.SessionEnded{IssueKey: "42", Action: "acceptance", Outcome: failed("stopped")})
	cmds, _ = d.send(core.SessionEnded{IssueKey: "42", Action: "development", Outcome: failed("stopped")})
	wantCommands(t, cmds,
		core.Move{IssueKey: "42", From: inProgress, To: needsAttention},
		core.ReportFailure{Report: crew.FailureReport{IssueKey: "42", IssueRef: "#42", Failures: []crew.ActionFailure{
			{Action: "acceptance", Reason: "stopped", Workspace: "issue-42-acceptance", Log: ".crew/logs/issue-42-acceptance.log"},
			{Action: "development", Reason: "stopped", Workspace: "issue-42-development", Log: ".crew/logs/issue-42-development.log"},
		}}},
	)
}

func TestTimeUpAfterAStopOrASecondTimeChangesNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.StopRequested{})
	cmds, events := d.send(core.TimeUp{Limit: limit})
	if len(cmds)+len(events) != 0 || d.m.View().TimeUp {
		t.Fatalf("time up after a stop: commands %#v, events %#v, TimeUp %v", cmds, events, d.m.View().TimeUp)
	}

	d = newDriver(t, draft(), 2)
	d.running(issue("42", 1, ready))
	d.send(core.TimeUp{Limit: limit})
	cmds, events = d.send(core.TimeUp{Limit: limit})
	if len(cmds)+len(events) != 0 {
		t.Fatalf("a second time up: commands %#v, events %#v", cmds, events)
	}
}
