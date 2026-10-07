package core_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestAE1TakesUpToMaxParallelIssuesAndStartsEveryAction(t *testing.T) {
	d := newDriver(t, draft(), 2)
	i1, i2, i3 := issue("1", 1, ready), issue("2", 2, ready), issue("3", 3, ready)

	cmds, _ := d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: draftListing})

	cmds, events := d.send(core.IssuesListed{Issues: []crew.Issue{i1, i2, i3}})
	wantCommands(t, cmds,
		core.Move{IssueID: issueID("1"), From: ready, To: inProgress},
		core.Move{IssueID: issueID("2"), From: ready, To: inProgress},
	)
	at := d.now
	wantEvents(t, events,
		core.IssueTaken{At: at, Issue: i1, Rule: "implement", From: ready, To: inProgress},
		core.IssueTaken{At: at, Issue: i2, Rule: "implement", From: ready, To: inProgress},
		core.PollDone{At: at, Listed: 3, Taken: 2},
	)

	var all []core.Command
	for _, it := range []crew.Issue{i1, i2} {
		created, events := d.send(core.CallResult{ID: moveID(t, cmds, it.ID.Key), Result: core.ResultDone})
		wantCommands(t, created,
			core.CreateWorkspace{Issue: it, Action: "acceptance"},
			core.CreateWorkspace{Issue: it, Action: "development"},
		)
		hasEvent(t, events, core.IssueMoved{At: d.now, IssueID: it.ID, IssueRef: it.Ref, From: ready, To: inProgress})
		all = append(all, created...)
	}

	sessions := d.workspacesReady("1", "2")
	wantCommands(t, sessions,
		session("1", "acceptance", "Implement test acceptance for issue #1"),
		session("1", "development", "Implement development for issue #1"),
		session("2", "acceptance", "Implement test acceptance for issue #2"),
		session("2", "development", "Implement development for issue #2"),
	)

	// #3 waits: no command concerns it and the core does not hold it.
	for _, c := range append(all, sessions...) {
		if issueKey(c) == "3" {
			t.Fatalf("#3 was touched by %#v", c)
		}
	}
	wantHeld(t, d.m, "1", "2")
}

// workspacesReady answers the workspace of each draft action of keys as
// ready, and returns the commands that start their sessions.
func (d *driver) workspacesReady(keys ...string) []core.Command {
	d.t.Helper()
	var sessions []core.Command
	for _, key := range keys {
		for _, action := range []crew.ActionName{"acceptance", "development"} {
			started, _ := d.send(space(key, action))
			sessions = append(sessions, started...)
		}
	}
	return sessions
}

// session is the StartSession for prompt in the workspace space gives key
// and action.
func session(key string, action crew.ActionName, prompt string) core.StartSession {
	return core.StartSession{
		IssueID: issueID(key), Action: action, Dir: "/repo/.crew/worktrees/issue-" + key + "-" + string(action),
		Prompt: prompt, Log: ".crew/logs/issue-" + key + "-" + string(action) + ".log",
	}
}

// Each action's session starts on its agent's harness (R13).
func TestEverySessionStartsOnItsActionsAgent(t *testing.T) {
	rules := draft()
	rules[0].Actions[0].Agent, rules[0].Actions[1].Agent = "tester", "developer"
	d := newDriver(t, rules, 2)
	d.send(core.Tick{})
	cmds, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, ready)}})
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

	acceptance, development := session("1", "acceptance", "Implement test acceptance for issue #1"),
		session("1", "development", "Implement development for issue #1")
	acceptance.Agent, development.Agent = "tester", "developer"
	wantCommands(t, d.workspacesReady("1"), acceptance, development)
}

func TestAE2IssueMovesOnSuccessOnlyOnceEveryActionEndedCleanly(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))

	cmds, events := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	wantCommands(t, cmds)
	hasEvent(t, events, core.ActionEnded{
		At: d.now, IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Action: "acceptance", Outcome: succeeded,
		Workspace: "issue-1-acceptance", Log: ".crew/logs/issue-1-acceptance.log",
	})

	// A poll meanwhile leaves #1 in progress: only the listing is issued.
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: draftListing})
	if c := claimOf(t, d.m, "1"); c != core.ClaimRunning {
		t.Fatalf("claim of #1: got %v, want running", c)
	}

	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	wantCommands(t, cmds, core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview})

	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	hasEvent(t, events, core.IssueMoved{At: d.now, IssueID: issueID("1"), IssueRef: "#1", From: inProgress,
		To: readyToReview})
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

			cmds, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: tt.outcome})
			wantCommands(t, cmds)

			cmds, _ = d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
			wantCommands(t, cmds,
				core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention},
				core.ReportFailure{Report: crew.FailureReport{IssueID: issueID("1"), IssueRef: "#1",
					Failures: []crew.ActionFailure{{
						Action: "development", Reason: tt.outcome.Reason,
						Workspace: "issue-1-development", Log: ".crew/logs/issue-1-development.log",
					}}}},
			)

			_, events := d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
			hasEvent(t, events, core.IssueMoved{At: d.now, IssueID: issueID("1"), IssueRef: "#1", From: inProgress,
				To: needsAttention})
			wantHeld(t, d.m, "1") // its report is still in flight
			_, events = d.send(core.CallResult{ID: reportID(t, cmds, "1"), Result: core.ResultDone})
			hasEvent(t, events, core.FailureReported{At: d.now, IssueID: issueID("1"), IssueRef: "#1"})
			wantHeld(t, d.m)
		})
	}
}

func TestAE1AE5FailedRuleMovesToItsOwnOnFailure(t *testing.T) {
	rules := draft()
	rules[1].Labels.Failure = rules[0].Labels.Ready // a failed review goes back to implement
	d := newDriver(t, rules, 2)

	// AE1: implement fails, so #1 moves to implement's on_failure.
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development",
		Outcome: failed("tests do not pass")})
	if got := noIDs(cmds)[0]; got != (core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention}) {
		t.Fatalf("failed implement: got %#v, want the move to needs attention", got)
	}
	d.settle(cmds)

	// AE5: review fails, so #2 moves to review's on_failure, implement's label.
	d.running(issue("2", 2, readyToReview))
	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("2"), Action: "custom_review",
		Outcome: failed("changes requested")})
	wantCommands(t, cmds,
		core.Move{IssueID: issueID("2"), From: inReview, To: ready},
		core.ReportFailure{Report: crew.FailureReport{IssueID: issueID("2"), IssueRef: "#2", Failures: []crew.ActionFailure{{
			Action: "custom_review", Reason: "changes requested",
			Workspace: "issue-2-custom_review", Log: ".crew/logs/issue-2-custom_review.log",
		}}}},
	)
	d.settle(cmds)
	wantHeld(t, d.m)

	// On the next listing implement takes #2 again.
	cmds, _ = d.poll(issue("2", 2, ready))
	wantCommands(t, cmds, core.Move{IssueID: issueID("2"), From: ready, To: inProgress})
}

func TestAE8IssueInTwoStatesIsSkippedUntilItIsInOne(t *testing.T) {
	d := newDriver(t, draft(), 2)

	cmds, events := d.poll(issue("4", 1, ready, needsAttention))
	wantCommands(t, cmds)
	hasEvent(t, events, core.IssueSkipped{
		At: d.now, IssueID: issueID("4"), IssueRef: "#4", States: []crew.State{ready, needsAttention},
	})
	wantHeld(t, d.m)

	cmds, _ = d.poll(issue("4", 1, ready))
	wantCommands(t, cmds, core.Move{IssueID: issueID("4"), From: ready, To: inProgress})
}

func TestBlockedIssueIsNotTakenUntilNothingBlocksIt(t *testing.T) {
	d := newDriver(t, draft(), 1)
	blocked := issue("4", 1, ready)
	blocked.Blocked = true

	cmds, _ := d.poll(blocked, issue("5", 2, ready))
	wantCommands(t, cmds, core.Move{IssueID: issueID("5"), From: ready, To: inProgress})
	wantHeld(t, d.m, "5")

	d = newDriver(t, draft(), 1)
	cmds, _ = d.poll(blocked)
	wantCommands(t, cmds)
	wantHeld(t, d.m)

	cmds, _ = d.poll(issue("4", 1, ready))
	wantCommands(t, cmds, core.Move{IssueID: issueID("4"), From: ready, To: inProgress})

	d = newDriver(t, draft(), 1)
	cmds, _ = d.poll(prioritized(blocked, 1), issue("5", 2, ready))
	wantCommands(t, cmds, core.Move{IssueID: issueID("5"), From: ready, To: inProgress})
}

func TestActionThatFailsToStartFailsAloneWhileSiblingsRun(t *testing.T) {
	tests := []struct {
		name      string
		fail      func(d *driver) []core.Command
		workspace crew.WorkspaceName
		log       string
	}{
		{
			name: "workspace failed",
			fail: func(d *driver) []core.Command {
				cmds, _ := d.send(core.WorkspaceFailed{IssueID: issueID("1"), Action: "acceptance", Reason: "fetch failed"})
				return cmds
			},
		},
		{
			name: "session failed to start",
			fail: func(d *driver) []core.Command {
				d.send(space("1", "acceptance"))
				cmds, _ := d.send(core.SessionFailedToStart{IssueID: issueID("1"), Action: "acceptance", Reason: "fetch failed"})
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
				IssueID: issueID("1"), Action: "development", Dir: "/repo/.crew/worktrees/issue-1-development",
				Prompt: "Implement development for issue #1", Log: ".crew/logs/issue-1-development.log",
			})
			d.send(core.SessionStarted{IssueID: issueID("1"), Action: "development"})

			cmds, _ = d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
			wantCommands(t, cmds,
				core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention},
				core.ReportFailure{Report: crew.FailureReport{IssueID: issueID("1"), IssueRef: "#1", Failures: []crew.ActionFailure{
					{Action: "acceptance", Reason: "fetch failed", Workspace: tt.workspace, Log: tt.log},
				}}},
			)
		})
	}
}

func TestPromptThatFailsToRenderFailsItsAction(t *testing.T) {
	rules := draft()
	rules[0].Actions[0].Prompt = "Fix {{.Issue.Number}}"
	d := newDriver(t, rules, 2)
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

// prioritized returns i with priority p, 1 the highest.
func prioritized(i crew.Issue, p int) crew.Issue {
	i.Priority = p
	return i
}

func TestPicksTheHighestPriorityThenLaterRulesThenTheOldestIssue(t *testing.T) {
	tests := []struct {
		name   string
		issues []crew.Issue
		want   core.Move
	}{
		{
			name:   "review before implement",
			issues: []crew.Issue{issue("5", 1, ready), issue("6", 2, readyToReview)},
			want:   core.Move{IssueID: issueID("6"), From: readyToReview, To: inReview},
		},
		{
			name:   "oldest first within a rule",
			issues: []crew.Issue{issue("8", 9, ready), issue("7", 3, ready)},
			want:   core.Move{IssueID: issueID("7"), From: ready, To: inProgress},
		},
		{
			// AE1: an Urgent issue passes an unprioritized one of a later rule.
			name:   "priority before a later rule",
			issues: []crew.Issue{issue("6", 1, readyToReview), prioritized(issue("5", 2, ready), 1)},
			want:   core.Move{IssueID: issueID("5"), From: ready, To: inProgress},
		},
		{
			// AE2: same priority and rule, the older issue first.
			name:   "oldest first at the same priority and rule",
			issues: []crew.Issue{prioritized(issue("8", 9, ready), 2), prioritized(issue("7", 3, ready), 2)},
			want:   core.Move{IssueID: issueID("7"), From: ready, To: inProgress},
		},
		{
			// AE3: same priority, the later rule first.
			name:   "later rule first at the same priority",
			issues: []crew.Issue{prioritized(issue("5", 1, ready), 3), prioritized(issue("6", 2, readyToReview), 3)},
			want:   core.Move{IssueID: issueID("6"), From: readyToReview, To: inReview},
		},
		{
			// AE4: the lowest priority still passes an older issue with none.
			name:   "any priority before none",
			issues: []crew.Issue{issue("5", 1, ready), prioritized(issue("6", 9, ready), 4)},
			want:   core.Move{IssueID: issueID("6"), From: ready, To: inProgress},
		},
		{
			name:   "higher priority before an older lower one",
			issues: []crew.Issue{prioritized(issue("5", 1, ready), 2), prioritized(issue("6", 9, ready), 1)},
			want:   core.Move{IssueID: issueID("6"), From: ready, To: inProgress},
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
	list := core.ListIssues{States: draftListing}

	cmds, _ := d.send(core.Tick{})
	wantCommands(t, cmds, list)
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds)

	_, events := d.send(core.ListFailed{Reason: "gh: network down"})
	hasEvent(t, events, core.ListingFailed{At: d.now, Reason: "gh: network down"})
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds, list)
}

func TestViewShowsRunningActionsAndSharesNoMemory(t *testing.T) {
	d := newDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	d.send(space("1", "acceptance"))
	d.send(core.SessionStarted{IssueID: issueID("1"), Action: "acceptance"})
	started := d.now

	want := core.View{Issues: []core.IssueView{{
		Issue: issue("1", 1, ready), Rule: "implement", Claim: core.ClaimRunning,
		Actions: []core.ActionView{
			{
				Name: "acceptance", Phase: core.PhaseRunning, Workspace: "issue-1-acceptance",
				Branch: "crew/issue-1-acceptance", Log: ".crew/logs/issue-1-acceptance.log", Started: started,
			},
			{Name: "development", Phase: core.PhaseCreating},
		},
	}}, Queues: []core.QueueView{{Slots: 2, Busy: 1}}, Bots: []core.BotView{{
		Name: "you", You: true, Writes: true, Pairs: draftPairs,
		Running: []core.RunningAction{{IssueRef: "#1", Rule: "implement", Action: "acceptance"}},
	}}}
	v := d.m.View()
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("view:\n got %#v\nwant %#v", v, want)
	}

	v.Issues[0].Issue.States[0] = "done"
	v.Issues[0].Actions[0].Name = "changed"
	v.Queues[0].Busy = 9
	v.Bots[0].Pairs[0] = "changed"
	if again := d.m.View(); !reflect.DeepEqual(again, want) {
		t.Fatalf("changing a view changed the model:\n got %#v\nwant %#v", again, want)
	}
}
