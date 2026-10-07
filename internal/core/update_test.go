package core_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestAE1TakesUpToMaxParallelIssuesAndStartsTheirFirstAction(t *testing.T) {
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
		d.taken(1, i1, "implement", ready, inProgress, "acceptance", "development"),
		d.taken(2, i2, "implement", ready, inProgress, "acceptance", "development"),
		core.PollDone{At: at, Listed: 3, Taken: 2},
	)

	var all []core.Command
	for _, it := range []crew.Issue{i1, i2} {
		created, events := d.send(core.CallResult{ID: moveID(t, cmds, it.ID().Key), Result: core.ResultDone})
		wantCommands(t, created, core.CreateWorkspace{Issue: it, Run: d.run(it.ID()), Rule: "implement"})
		hasEvent(t, events, crew.TakeMoved{EventHead: d.runHead(it.ID().Key), From: ready, To: inProgress})
		all = append(all, created...)
	}

	sessions := d.workspacesReady("1", "2")
	wantCommands(t, sessions,
		d.session("1", "acceptance", "Implement test acceptance for issue #1"),
		d.session("2", "acceptance", "Implement test acceptance for issue #2"),
	)

	// #3 waits: no command concerns it and the core does not hold it.
	for _, c := range append(all, sessions...) {
		if issueKey(c) == "3" {
			t.Fatalf("#3 was touched by %#v", c)
		}
	}
	wantHeld(t, d.m, "1", "2")
}

// workspacesReady answers the workspace of the last run of each of keys as
// ready, and returns the commands that start their first actions.
func (d *driver) workspacesReady(keys ...string) []core.Command {
	d.t.Helper()
	sessions := make([]core.Command, 0, len(keys))
	for _, key := range keys {
		sessions = append(sessions, d.ready(key)...)
	}
	return sessions
}

// Each action's session starts on its agent's harness (R13).
func TestEverySessionStartsOnItsActionsAgent(t *testing.T) {
	rules := withSpec(draft(), 0, "acceptance", func(s *crew.SessionSpec) { s.Agent = crew.Agent{Name: "tester"} })
	rules = withSpec(rules, 0, "development", func(s *crew.SessionSpec) { s.Agent = crew.Agent{Name: "developer"} })
	d := newDriver(t, rules, 2)
	d.send(core.Tick{})
	cmds, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, ready)}})
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

	acceptance, development := d.session("1", "acceptance", "Implement test acceptance for issue #1"),
		d.session("1", "development", "Implement development for issue #1")
	acceptance.Agent, development.Agent = "tester", "developer"
	wantCommands(t, d.workspacesReady("1"), acceptance)
	wantCommands(t, d.ended("1", "acceptance", succeeded), development)
}

func TestAE2IssueMovesThroughPassedOnlyOnceItsLastActionPassed(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))

	cmds, events := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	wantCommands(t, cmds, d.session("1", "development", "Implement development for issue #1"))
	hasEnd(t, events, end{head: d.runHead("1"), action: "acceptance", outcome: succeeded})

	// A poll meanwhile leaves #1 in progress: only the listing is issued.
	cmds, _ = d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: draftListing})
	if c := claimOf(t, d.m, "1"); c != core.ClaimRunning {
		t.Fatalf("claim of #1: got %v, want running", c)
	}

	cmds, events = d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	wantCommands(t, cmds, core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview})
	hasEvent(t, events, crew.RouteChosen{
		EventHead: d.runHead("1"), Route: crew.PassedRoute, Action: "development",
		Steps: []crew.StepPlan{{Kind: crew.StepMove, To: readyToReview}},
	})

	_, events = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))
	wantHeld(t, d.m)
}

func TestAE3AE5AFailedActionEndsTheSequenceThroughFailed(t *testing.T) {
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

			// The report goes first, then the move: development never starts.
			cmds, events := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: tt.outcome})
			wantCommands(t, cmds, failureOf("1", "implement", "acceptance"))
			hasEvent(t, events, crew.RouteChosen{
				EventHead: d.runHead("1"), Route: crew.FailedRoute, Action: "acceptance",
				Steps: []crew.StepPlan{{Kind: crew.StepReport}, {Kind: crew.StepMove, To: needsAttention}},
			})
			d.wantReason("1", "acceptance", tt.outcome.Reason.String())

			moved, events := d.send(core.CallResult{ID: reportID(t, cmds, "1"), Result: core.ResultDone})
			hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))
			wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
			wantHeld(t, d.m, "1") // its move is still in flight

			_, events = d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
			hasEvent(t, events, d.stepEnded("1", 1, crew.StepLanded{}))
			wantHeld(t, d.m)
		})
	}
}

func TestAE1AE5AFailedRuleMovesWhereItsFailedRouteSays(t *testing.T) {
	rules := draft()
	rules[1].Routes = routes(readyToMerge, ready) // a failed review goes back to implement
	d := newDriver(t, rules, 2)

	// AE1: implement fails, so #1 is reported, then moves to needs attention.
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development",
		Outcome: failed("tests do not pass")})
	wantCommands(t, cmds, failureOf("1", "implement", "development"))
	moved, _ := d.send(core.CallResult{ID: reportID(t, cmds, "1"), Result: core.ResultDone})
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: inProgress, To: needsAttention})
	d.settle(moved)

	// AE5: review fails, so #2 moves through its failed route to implement's
	// label.
	d.running(issue("2", 2, readyToReview))
	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("2"), Action: "custom_review",
		Outcome: failed("changes requested")})
	wantCommands(t, cmds, failureOf("2", "review", "custom_review"))
	d.wantReason("2", "custom_review", "changes requested")
	moved, _ = d.send(core.CallResult{ID: reportID(t, cmds, "2"), Result: core.ResultDone})
	wantCommands(t, moved, core.Move{IssueID: issueID("2"), From: inReview, To: ready})
	d.settle(moved)
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
	blocked := blockedIssue(issue("4", 1, ready))

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

func TestAnActionThatFailsToStartEndsTheSequenceThroughFailed(t *testing.T) {
	fetchFailed := crew.NewSessionText("fetch failed")
	tests := []struct {
		name string
		fail func(d *driver) []core.Command
		want core.ReportFailure
	}{
		{
			name: "workspace failed",
			fail: func(d *driver) []core.Command {
				cmds, _ := d.send(core.WorkspaceFailed{IssueID: issueID("1"), Reason: fetchFailed})
				return cmds
			},
			want: core.ReportFailure{Report: crew.FailureReport{
				IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Route: crew.FailedRoute,
				Failures: []crew.ActionFailure{{Action: "acceptance", Verdict: crew.Failed}},
			}},
		},
		{
			name: "session failed to start",
			fail: func(d *driver) []core.Command {
				d.ready("1")
				cmds, _ := d.send(core.SessionFailedToStart{IssueID: issueID("1"), Action: "acceptance", Reason: fetchFailed})
				return cmds
			},
			want: failureOf("1", "implement", "acceptance"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, draft(), 2)
			cmds, _ := d.poll(issue("1", 1, ready))
			d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

			wantCommands(t, tt.fail(d), tt.want)
			d.wantReason("1", "acceptance", "fetch failed")
			cmds, _ = d.send(core.SessionStarted{IssueID: issueID("1"), Action: "development"})
			wantCommands(t, cmds)
		})
	}
}

func TestPromptThatFailsToRenderFailsItsAction(t *testing.T) {
	// Renders for the sample issue's title, and fails on the shorter "Issue 1".
	rules := withSpec(draft(), 0, "acceptance", func(s *crew.SessionSpec) {
		s.Prompt = parsedPrompt("acceptance", "Fix {{index .Issue.Title 11}}")
	})
	d := newDriver(t, rules, 2)
	cmds, _ := d.poll(issue("1", 1, ready))

	cmds, _ = d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})
	wantCommands(t, cmds, core.CreateWorkspace{Issue: issue("1", 1, ready), Run: d.run(issueID("1")), Rule: "implement"})
	cmds, events := d.send(space("1", "implement"))
	wantCommands(t, cmds, failureOf("1", "implement", "acceptance"))
	for _, e := range events {
		if ended, ok := e.(crew.ActionEnded); ok && ended.Action == "acceptance" {
			outcome := ended.End.Outcome()
			if outcome.Succeeded || !strings.Contains(outcome.Reason.String(), `render prompt of action "acceptance"`) {
				t.Fatalf("acceptance ended with %#v, want a failure naming the render error", outcome)
			}
			return
		}
	}
	t.Fatalf("no ActionEnded for acceptance in %#v", events)
}

// prioritized returns i with priority p, 1 the highest.
func prioritized(i crew.Issue, p int) crew.Issue {
	d := i.Data()
	d.Priority = p
	return crew.NewIssue(d)
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
	d.ready("1")
	d.send(core.SessionStarted{IssueID: issueID("1"), Action: "acceptance"})
	started := d.now

	w := space("1", "implement")
	want := core.View{Issues: []core.IssueView{{
		Issue: issue("1", 1, ready), Rule: "implement", Claim: core.ClaimRunning,
		Actions: []core.ActionView{
			{
				Name: "acceptance", Phase: core.PhaseRunning, Workspace: w.Workspace, Branch: w.Branch, Log: w.Log,
				Started: started,
			},
			{Name: "development", Phase: core.PhaseAwaitingTurn, Workspace: w.Workspace, Branch: w.Branch, Log: w.Log},
		},
	}}, Queues: []core.QueueView{{Slots: 2, Busy: 1}}, Bots: []core.BotView{{
		Name: "you", You: true, Writes: true, Pairs: draftPairs,
		Running: []core.RunningAction{{IssueRef: "#1", Rule: "implement", Action: "acceptance"}},
	}}}
	v := d.m.View()
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("view:\n got %#v\nwant %#v", v, want)
	}

	v.Issues[0].Issue.States()[0] = "done"
	v.Issues[0].Actions[0].Name = "changed"
	v.Queues[0].Busy = 9
	v.Bots[0].Pairs[0] = "changed"
	if again := d.m.View(); !reflect.DeepEqual(again, want) {
		t.Fatalf("changing a view changed the model:\n got %#v\nwant %#v", again, want)
	}
}
