package core_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// crewNeedsPerson is the label development's needs person route moves to.
const crewNeedsPerson crew.State = "crew:needs person"

// stackingRules is crewRules with stacking on in development, whose
// session lfg ends through needs person, which moves the issue to
// crew:needs person, when it reports needs-person; then question, a rule
// without actions that moves an issue from crew:question on, as crew's
// question rule does, and crew's answered rule.
func stackingRules() []crew.Rule {
	rules := crewRules()
	rules[0].Stacking = true
	rules[0].Actions[0].On = crew.On{"needs-person": crew.ToRoute{Route: "needs person"}}
	rules[0].Routes = append(rules[0].Routes,
		crew.Route{Name: "needs person", Steps: []crew.Step{crew.MoveStep{To: crewNeedsPerson}}})
	return append(rules, crew.Rule{
		Name: "question", Labels: crew.Labels{Ready: questionLabel, Running: questionRunning},
		Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{crew.MoveStep{To: questionWaiting}}}},
	}, answering())
}

// developmentRun is the journal of a run of development on it, on rules,
// whose session lfg ends with outcome and report, after past.
func developmentRun(
	t *testing.T, rules []crew.Rule, past []crew.RunEvent, it crew.Issue, outcome crew.Outcome, report crew.VerdictReport,
) []crew.RunEvent {
	t.Helper()
	return journaled(t, rules, past, func(d *driver) {
		d.running(it)
		cmds, _ := d.send(core.SessionEnded{IssueID: it.ID(), Action: "lfg", Outcome: outcome, Report: report})
		d.settle(cmds)
		wantHeld(t, d.m)
	})
}

// passedOne is the journal of #1's development run, which ended through
// passed.
func passedOne(t *testing.T) []crew.RunEvent {
	t.Helper()
	return developmentRun(t, stackingRules(), nil, issue("1", 1, readyForDev), succeeded, nil)
}

// failedOne is the journal of #1's development run, which ended through
// failed.
func failedOne(t *testing.T) []crew.RunEvent {
	t.Helper()
	return developmentRun(t, stackingRules(), nil, issue("1", 1, readyForDev), failed("boom"), nil)
}

// needsPersonOne is the journal of #1's development run, which ended
// through needs person.
func needsPersonOne(t *testing.T) []crew.RunEvent {
	t.Helper()
	return developmentRun(t, stackingRules(), nil, issue("1", 1, readyForDev), succeeded,
		crew.VerdictReported{Verdict: "needs-person"})
}

// questionAfterFailed is failedOne, then an hour later a run of question
// on #1 that ended through passed.
func questionAfterFailed(t *testing.T) []crew.RunEvent {
	t.Helper()
	return journaled(t, stackingRules(), failedOne(t), func(d *driver) {
		d.now = d.now.Add(time.Hour)
		d.settle(d.takeIssue(issue("1", 1, questionLabel)))
		wantHeld(t, d.m)
	})
}

// answeredAfterFailed is failedOne, then an hour later a run of answered on
// #1 whose check found alice's answer to deps's question, which ended
// through passed.
func answeredAfterFailed(t *testing.T) []crew.RunEvent {
	t.Helper()
	past := failedOne(t)
	rules := append(stackingRules(), depsAsking(t)...)
	m := core.New(rules, 2, core.Journaling(past), core.WithBots(developerBots()), core.WithAnswerers(crewAnswerers()))
	d := &driver{t: t, m: m, now: t0.Add(time.Hour), inputs: 1000}
	d.takeIssue(issue("1", 1, answeredLabel))
	moved, _ := d.send(returnRead(unsureQuestion("crew-clerk[bot]"), crew.Comment{Author: "alice", Body: "yes"}))
	d.settle(moved)
	wantHeld(t, d.m)
	return append(past, d.recorded...)
}

// nothing is an empty journal.
func nothing(*testing.T) []crew.RunEvent { return nil }

// blockerOne is #1 as the only open blocker of an issue, in states, with
// its open pull requests prs.
func blockerOne(states []crew.State, prs ...crew.StackPullRequest) crew.StackBlocker {
	return crew.StackBlocker{
		ID: issueID("1"), Ref: "#1", URL: "https://example.com/issues/1", States: states, PullRequests: prs,
	}
}

// reviewedOne is blockerOne in crew:waiting review, with prs.
func reviewedOne(prs ...crew.StackPullRequest) crew.StackBlocker {
	return blockerOne([]crew.State{crewReview}, prs...)
}

// ownPullRequest is #45, #1's pull request from the worktree branch of its
// development run, with the head branches of the pull requests based on
// it.
func ownPullRequest(stacked ...string) crew.StackPullRequest {
	return crew.StackPullRequest{
		Ref: "#45", URL: "https://example.com/pull/45", Head: space("1", "development").Branch, Base: "main",
		Stacked: stacked,
	}
}

// connectedPullRequest is #46, from fix-1, which a person connected to #1.
func connectedPullRequest() crew.StackPullRequest {
	return crew.StackPullRequest{
		Ref: "#46", URL: "https://example.com/pull/46", Head: "fix-1", Base: "main", Connected: true,
	}
}

// blockedOn returns issue key, in crew:ready for development, opened
// minute minutes after t0, blocked only by blocker, with the sub-issues of
// its parent siblings, by key.
func blockedOn(key string, minute int, blocker crew.StackBlocker, siblings ...string) crew.Issue {
	d := issue(key, minute, readyForDev).Data()
	d.Blocked = true
	d.Stacking = crew.StackReading{Blocker: crew.Some(blocker)}
	for _, s := range siblings {
		d.Stacking.Siblings = append(d.Stacking.Siblings, issueID(s))
	}
	return crew.NewIssue(d)
}

// onOne is the stack of a run on pr, a pull request of #1.
func onOne(pr crew.StackPullRequest) crew.Optional[crew.Stack] {
	return crew.Some(crew.Stack{
		Blocker: issueID("1"), BlockerRef: "#1", BlockerURL: "https://example.com/issues/1",
		PullRequestRef: pr.Ref, PullRequestURL: pr.URL, Branch: pr.Head,
	})
}

// unstacked is the stack of a run taken from the default branch.
var unstacked = crew.Optional[crew.Stack]{}

// takeMoves returns the keys of the issues whose take cmds move, in order.
func takeMoves(cmds []core.Command) []string {
	var out []string
	for _, c := range cmds {
		if m, ok := c.(core.Move); ok && m.From == readyForDev {
			out = append(out, m.IssueID.Key)
		}
	}
	return out
}

// wantTaken fails unless cmds take exactly keys, in order.
func wantTaken(t *testing.T, cmds []core.Command, keys ...string) {
	t.Helper()
	if got := takeMoves(cmds); !slices.Equal(got, keys) {
		t.Fatalf("took %v, want %v", got, keys)
	}
}

// wantStack fails unless the last take of key was stacked as want says.
func wantStack(t *testing.T, d *driver, key string, want crew.Optional[crew.Stack]) {
	t.Helper()
	if got := takenOf(t, d, key).Stack; !reflect.DeepEqual(got, want) {
		t.Errorf("take of #%s stacked on %+v, want %+v", key, got, want)
	}
}

// landTake answers the take move of key in cmds, and returns the commands
// that follow, the Record commands left out.
func (d *driver) landTake(cmds []core.Command, key string) []core.Command {
	d.t.Helper()
	out, _ := d.send(core.CallResult{ID: moveID(d.t, cmds, key), Result: core.ResultDone})
	return unrecorded(out)
}

// wantStartsOn fails unless start's base is base and its stack is stack.
func wantStartsOn(t *testing.T, base string, stack crew.Optional[crew.Stack], gotBase string,
	gotStack crew.Optional[crew.Stack],
) {
	t.Helper()
	if gotBase != base || !reflect.DeepEqual(gotStack, stack) {
		t.Errorf("starts on base %q stacked on %+v, want %q on %+v", gotBase, gotStack, base, stack)
	}
}

// Covers AE1, R1: a rule without stacking leaves an issue blocked only by
// #1, though #1's run passed and its pull request is open.
func TestARuleWithoutStackingTakesNoBlockedIssue(t *testing.T) {
	d := resumeDriverOf(t, crewRules(), passedOne(t)...)

	cmds, _ := d.poll(blockedOn("2", 2, reviewedOne(ownPullRequest())))

	wantTaken(t, cmds)
	wantHeld(t, d.m)
}

// Covers AE2, R2, R5, R7, R8, KTD6: with stacking, an issue blocked only by
// #1, whose run passed and whose pull request is open with nothing on it,
// is taken stacked on that pull request. The take journals its stack, its
// workspace is made from the pull request's branch, and its session gets
// the branch as its base.
func TestARuleWithStackingTakesTheIssueOnItsBlockersPullRequest(t *testing.T) {
	pr := ownPullRequest()
	d := resumeDriverOf(t, stackingRules(), passedOne(t)...)
	it := blockedOn("2", 2, reviewedOne(pr))

	cmds, _ := d.poll(it)

	wantTaken(t, cmds, "2")
	wantStack(t, d, "2", onOne(pr))
	if taken, ok := records(cmds)[0].(crew.RunTaken); !ok || !reflect.DeepEqual(taken.Stack, onOne(pr)) {
		t.Errorf("journaled %#v, want the take stacked on #1's pull request", records(cmds)[0])
	}
	cmds = d.landTake(cmds, "2")
	wantCommands(t, cmds, core.CreateWorkspace{Issue: it, Run: d.run(it.ID()), Rule: "development", Base: pr.Head})

	ready := space("2", "development")
	ready.DefaultBranch = "main"
	cmds, _ = d.send(ready)
	start := startOf(t, cmds)
	wantStartsOn(t, pr.Head, onOne(pr), start.Base, start.Stack)
}

// Covers R8: a stacked run's scripts get its base and stack, as its
// sessions do.
func TestAStackedRunsScriptsGetItsBaseAndStack(t *testing.T) {
	rules := stackingRules()
	rules[0].Actions = append(rules[0].Actions, shellAction("open", "gh pr create"))
	pr := ownPullRequest()
	d := resumeDriverOf(t, rules, passedOne(t)...)
	d.running(blockedOn("2", 2, reviewedOne(pr)))

	script := runShellOf(t, d.ended("2", "lfg", succeeded)).Script

	wantStartsOn(t, pr.Head, onOne(pr), script.Base, script.Stack)
}

// Covers AE3, R2: while crew holds #1's run, an issue it blocks waits;
// once the run ends through passed, the issue stacks on #1's pull request.
func TestAnIssueWaitsWhileCrewHoldsItsBlockersRun(t *testing.T) {
	pr := ownPullRequest()
	d := resumeDriverOf(t, stackingRules())
	d.running(issue("1", 1, readyForDev))

	cmds, _ := d.poll(blockedOn("2", 2, blockerOne(nil, pr)))
	wantTaken(t, cmds)
	wantHeld(t, d.m, "1")

	d.settle(d.ended("1", "lfg", succeeded))
	wantHeld(t, d.m)
	cmds, _ = d.poll(blockedOn("2", 2, reviewedOne(pr)))

	wantTaken(t, cmds, "2")
	wantStack(t, d, "2", onOne(pr))
}

// Covers R2: an issue waits while crew holds a run of its blocker, of any
// rule, even when the labels its reading found show none.
func TestAnIssueWaitsWhileCrewHoldsAnyRunOfItsBlocker(t *testing.T) {
	d := resumeDriverOf(t, stackingRules(), passedOne(t)...)
	d.now = d.now.Add(time.Hour)

	cmds, _ := d.poll(issue("1", 1, questionLabel), blockedOn("2", 2, blockerOne(nil, ownPullRequest())))

	wantHeld(t, d.m, "1")
	wantTaken(t, cmds)
}

// Covers AE4, R2: an issue that more than one issue blocks has no blocker
// in its reading and never stacks, not after one of its blockers closes
// either; once nothing blocks it, it is taken from the default branch.
func TestAnIssueWithTwoBlockersNeverStacks(t *testing.T) {
	d := resumeDriverOf(t, stackingRules(), passedOne(t)...)
	blocked := blockedIssue(issue("3", 3, readyForDev))

	for range 2 {
		cmds, _ := d.poll(blocked)
		wantTaken(t, cmds)
	}

	it := issue("3", 3, readyForDev)
	cmds, _ := d.poll(it)
	wantTaken(t, cmds, "3")
	wantStack(t, d, "3", unstacked)
	cmds = d.landTake(cmds, "3")
	wantCommands(t, cmds, core.CreateWorkspace{Issue: it, Run: d.run(it.ID()), Rule: "development"})
}

// Covers AE9, R2, KTD4: an issue blocked only by #1 stacks once crew's
// latest run of #1 that worked it ended through passed, with #1 in no
// rule's ready or running label.
func TestAnIssueStacksOnlyOnceItsBlockersRunPassed(t *testing.T) {
	pr := ownPullRequest()
	tests := []struct {
		name    string
		past    func(t *testing.T) []crew.RunEvent
		states  []crew.State
		stacked bool
	}{
		{name: "passed", past: passedOne, states: []crew.State{crewReview}, stacked: true},
		{name: "failed (AE9)", past: failedOne, states: []crew.State{crewFailed}},
		{name: "needs a person (AE9)", past: needsPersonOne, states: []crew.State{crewNeedsPerson}},
		{
			name: "a rule without actions passed after its development failed", past: questionAfterFailed,
			states: []crew.State{questionWaiting},
		},
		{
			name: "the answered rule passed after its development failed", past: answeredAfterFailed,
			states: []crew.State{depsReady},
		},
		{name: "back in a rule's ready label after a passed run", past: passedOne, states: []crew.State{readyForDev}},
		{name: "in a rule's running label after a passed run", past: passedOne, states: []crew.State{crewRunning}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := resumeDriverOf(t, stackingRules(), tt.past(t)...)

			cmds, _ := d.poll(blockedOn("2", 2, blockerOne(tt.states, pr)))

			if !tt.stacked {
				wantTaken(t, cmds)
				return
			}
			wantTaken(t, cmds, "2")
			wantStack(t, d, "2", onOne(pr))
		})
	}
}

// Covers AE5, AE6, R2, KTD3: an issue blocked only by #1 stacks only when
// #1 has exactly one open pull request of its own, not a draft: one from
// the branch of crew's run of #1, or one a person connected to #1.
func TestAnIssueStacksOnlyOnItsBlockersOnePullRequest(t *testing.T) {
	own, connected := ownPullRequest(), connectedPullRequest()
	draft := own
	draft.Draft = true
	mentioned := connected
	mentioned.Connected = false
	tests := []struct {
		name    string
		past    func(t *testing.T) []crew.RunEvent
		prs     []crew.StackPullRequest
		stacked bool
	}{
		{name: "one of its own", past: passedOne, prs: []crew.StackPullRequest{own}, stacked: true},
		{name: "two of its own (AE5)", past: passedOne, prs: []crew.StackPullRequest{own, connected}},
		{name: "none: its only one came from a fork (AE6)", past: passedOne},
		{name: "a draft", past: passedOne, prs: []crew.StackPullRequest{draft}},
		{name: "never run, one connected to it", past: nothing, prs: []crew.StackPullRequest{connected}, stacked: true},
		{name: "never run, one that only mentions it", past: nothing, prs: []crew.StackPullRequest{mentioned}},
		{name: "never run, one from a crew branch it has no run of", past: nothing, prs: []crew.StackPullRequest{own}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := resumeDriverOf(t, stackingRules(), tt.past(t)...)

			cmds, _ := d.poll(blockedOn("2", 2, blockerOne(nil, tt.prs...)))

			if !tt.stacked {
				wantTaken(t, cmds)
				return
			}
			wantTaken(t, cmds, "2")
			wantStack(t, d, "2", onOne(tt.prs[0]))
		})
	}
}

// Covers AE7, F2, R6: sub-issues of one parent, each blocked only by #1,
// all stack on #1's pull request, in the same listing, while crew holds
// the first stacked there, or once the first opened its pull request.
func TestSiblingsStackOnTheSameBlocker(t *testing.T) {
	pr := ownPullRequest()
	b, c := blockedOn("2", 2, reviewedOne(pr), "2", "3"), blockedOn("3", 3, reviewedOne(pr), "2", "3")

	t.Run("same listing", func(t *testing.T) {
		d := resumeDriverOf(t, stackingRules(), passedOne(t)...)
		cmds, _ := d.poll(b, c)
		wantTaken(t, cmds, "2", "3")
		wantStack(t, d, "3", onOne(pr))
	})
	t.Run("while the first runs", func(t *testing.T) {
		d := resumeDriverOf(t, stackingRules(), passedOne(t)...)
		d.running(b)
		cmds, _ := d.poll(c)
		wantTaken(t, cmds, "3")
		wantStack(t, d, "3", onOne(pr))
	})
	t.Run("once the first opened its pull request", func(t *testing.T) {
		past := developmentRun(t, stackingRules(), passedOne(t), b, succeeded, nil)
		d := resumeDriverOf(t, stackingRules(), past...)
		cmds, _ := d.poll(blockedOn("3", 3, reviewedOne(ownPullRequest(space("2", "development").Branch)), "2", "3"))
		wantTaken(t, cmds, "3")
	})
}

// Covers AE8, R4, R6: an issue that is not a sub-issue of the parent of
// the one stacked on #1's pull request waits, in the same listing or a
// later one, and is taken from the default branch once #1 closes.
func TestAnIssueThatIsNoSiblingOfTheOneStackedWaits(t *testing.T) {
	blocker := reviewedOne(ownPullRequest())
	x, y := blockedOn("4", 4, blocker, "4", "6"), blockedOn("5", 5, blocker)

	d := resumeDriverOf(t, stackingRules(), passedOne(t)...)
	cmds, _ := d.poll(y, x)
	wantTaken(t, cmds, "4")

	d = resumeDriverOf(t, stackingRules(), passedOne(t)...)
	d.running(x)
	cmds, _ = d.poll(y)
	wantTaken(t, cmds)

	cmds, _ = d.poll(issue("5", 5, readyForDev))
	wantTaken(t, cmds, "5")
	wantStack(t, d, "5", unstacked)
}

// Covers KTD5: a pull request based on #1's branch counts as the issue's
// own when it comes from the issue's earlier run, and as another issue's
// when no crew run made its branch.
func TestThePullRequestsStackedOnTheBlockerAreMappedThroughCrewsBranches(t *testing.T) {
	past := passedOne(t)
	ownEarlier := developmentRun(t, crewRules(), past, issue("2", 2, readyForDev), failed("boom"), nil)
	tests := []struct {
		name    string
		past    []crew.RunEvent
		stacked string
		taken   []string
	}{
		{
			name: "the issue's own earlier one", past: ownEarlier, stacked: space("2", "development").Branch,
			taken: []string{"2"},
		},
		{name: "one from a branch crew did not make", past: past, stacked: "someone-else"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := resumeDriverOf(t, stackingRules(), tt.past...)
			cmds, _ := d.poll(blockedOn("2", 2, reviewedOne(ownPullRequest(tt.stacked))))
			wantTaken(t, cmds, tt.taken...)
		})
	}
}

// Covers KTD10: without a run journal, a blocked issue in a stacking rule
// waits.
func TestWithoutAJournalNoBlockedIssueStacks(t *testing.T) {
	d := newDriver(t, stackingRules(), 2)

	cmds, _ := d.poll(blockedOn("2", 2, blockerOne(nil, connectedPullRequest())))

	wantTaken(t, cmds)
}

// Covers KTD6: a take of an issue whose last run was stacked decides its
// stack anew and reopens the run's worktree: stacked again while #1 still
// blocks it, unstacked on the default branch once #1 closed; a worktree
// gone is made again from this take's stack.
func TestAContinuingTakeDecidesItsStackAnew(t *testing.T) {
	pr := ownPullRequest()
	stackedRun := blockedOn("2", 2, reviewedOne(pr))
	past := developmentRun(t, stackingRules(), passedOne(t), stackedRun, failed("boom"), nil)
	if taken, ok := past[len(passedOne(t))].(crew.RunTaken); !ok || !reflect.DeepEqual(taken.Stack, onOne(pr)) {
		t.Fatalf("the failed run of #2 was not stacked: %#v", past)
	}

	t.Run("still blocked", func(t *testing.T) {
		d := resumeDriverOf(t, stackingRules(), past...)
		cmds, _ := d.poll(stackedRun)
		wantStack(t, d, "2", onOne(pr))
		wantCommands(t, d.landTake(cmds, "2"), d.reopen("2", "development"))

		cmds, _ = d.send(core.WorkspaceGone{IssueID: stackedRun.ID()})
		wantCommands(t, unrecorded(cmds),
			core.CreateWorkspace{Issue: stackedRun, Run: d.run(stackedRun.ID()), Rule: "development", Base: pr.Head})
	})
	t.Run("blocker closed", func(t *testing.T) {
		d := resumeDriverOf(t, stackingRules(), past...)
		cmds, _ := d.poll(issue("2", 2, readyForDev))
		wantStack(t, d, "2", unstacked)
		wantCommands(t, d.landTake(cmds, "2"), d.reopen("2", "development"))

		ready := reopened("2", "development")
		ready.DefaultBranch = "main"
		cmds, _ = d.send(ready)
		start := startOf(t, cmds)
		wantStartsOn(t, "main", unstacked, start.Base, start.Stack)
	})
}

// Covers KTD1: a listing whose stack read failed publishes StacksNotFound
// and takes the issues it can; its blocked issues, unread, wait.
func TestAFailedStackReadIsAWarning(t *testing.T) {
	d := resumeDriverOf(t, stackingRules(), passedOne(t)...)
	d.send(core.Tick{})

	cmds, events := d.send(core.IssuesListed{
		Issues:       []crew.Issue{blockedIssue(issue("2", 2, readyForDev)), issue("3", 3, readyForDev)},
		StacksFailed: "rate limited",
	})

	wantTaken(t, cmds, "3")
	hasEvent(t, events, core.StacksNotFound{At: d.now, Reason: "rate limited"})
}

// Covers KTD1: a listing asks for the stacks of the issues in the ready
// labels of the rules with stacking on.
func TestAListingNamesTheStackingRulesReadyLabels(t *testing.T) {
	d := newDriver(t, stackingRules(), 2)

	cmds, _ := d.send(core.Tick{})

	for _, c := range cmds {
		if l, ok := c.(core.ListIssues); ok {
			if !slices.Equal(l.Stacking, []crew.State{readyForDev}) {
				t.Errorf("listing reads stacks in %v, want [%s]", l.Stacking, readyForDev)
			}
			return
		}
	}
	t.Fatalf("no listing in %#v", cmds)
}
