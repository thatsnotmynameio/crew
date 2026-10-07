package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// usageDriver is a driver whose model records runs and looks up pull
// requests, with opts on top.
func usageDriver(t *testing.T, rules []crew.Rule, opts ...core.Option) *driver {
	t.Helper()
	opts = append([]core.Option{core.Journaling(nil), core.FindingPullRequests()}, opts...)
	return &driver{t: t, m: core.New(rules, 2, opts...), now: t0}
}

var (
	spent = crew.Usage{
		Cost: crew.Some(12.40), Tokens: crew.Some(crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}),
		Turns: crew.Some(7), Models: []string{"claude-opus-5-5"},
	}
	pr45   = crew.PullRequestFound{Ref: "#45", URL: "https://example.com/pull/45"}
	noPR   = crew.PullRequestNone{}
	tokens = crew.Usage{Tokens: crew.Some(crew.Tokens{Output: 5})}
	// partialSpend is spent's session summed with one that reported
	// nothing: two sessions, of which only one reported cost and tokens.
	partialSpend = crew.Spend{Sessions: 2, Cost: 12.40, WithCost: 1, Tokens: spent.Spend().Tokens, WithTokens: 1}
)

// lookups returns the FindPullRequest commands in cmds.
func lookups(cmds []core.Command) []core.FindPullRequest {
	var out []core.FindPullRequest
	for _, c := range cmds {
		if f, ok := c.(core.FindPullRequest); ok {
			out = append(out, f)
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

// reasonOf returns the reason of the action run's end e.
func reasonOf(e crew.ActionEnded) string { return e.End.Outcome().Reason.String() }

// Covers KTD-S6: the run looks up its pull requests once, when it chose its
// route, and its first step waits for the answer.
func TestAE1ARunLooksUpItsPullRequestsOnceItChoseItsRoute(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("31", 1, ready))
	made := d.m.View().Issues[0].Actions[0].Started
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("31"), Action: "acceptance", Outcome: succeeded})
	if len(lookups(cmds)) != 0 {
		t.Fatalf("looked up after acceptance, which went on to the next: %#v", cmds)
	}
	d.settle(cmds)
	started := d.m.View().Issues[0].Actions[1].Started

	cmds, _ = d.send(core.SessionEnded{IssueID: issueID("31"), Action: "development", Outcome: succeeded, Usage: spent})
	w := space("31", "implement")
	if got := lookups(cmds); len(got) != 1 || got[0].Branch != w.Branch || got[0].Run != d.run(issueID("31")) ||
		got[0].Since.After(made) {
		t.Fatalf("lookups = %#v, want one from %s", got, w.Branch)
	}
	end := ends(cmds)
	if len(end) != 1 || !end[0].End.Outcome().Succeeded || !reflect.DeepEqual(end[0].Usage, spent) ||
		end[0].SessionStarted != crew.Some(started) {
		t.Fatalf("ends = %#v, want development's end with its usage and its session's start", end)
	}
	if _, moved := unrecorded(cmds)[0].(core.Move); moved || claimOf(t, d.m, "31") != core.ClaimRouting {
		t.Fatalf("the route's move did not wait for the lookup: %#v", cmds)
	}

	ending, _ := d.send(core.PullRequestFound{IssueID: issueID("31"), PullRequest: pr45})
	wantCommands(t, unrecorded(ending), core.Move{IssueID: issueID("31"), From: inProgress, To: readyToReview})
	d.settle(ending)
	entry := onlyEntry(t, d)
	want := []core.HandledAction{
		{Name: "acceptance", Spend: crew.Spend{Sessions: 1}, PullRequest: pr45},
		{Name: "development", Spend: spent.Spend(), PullRequest: pr45},
	}
	if !reflect.DeepEqual(entry.Actions, want) {
		t.Fatalf("handled actions = %#v, want %#v", entry.Actions, want)
	}
	if got := entry.Spend(); got != partialSpend {
		t.Fatalf("handled spend = %#v, want %#v", got, partialSpend)
	}
}

func TestAStopDuringTheLookupKeepsTheSessionsOwnFailure(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("9", 1, ready))
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "acceptance", Outcome: failed("tests fail")})
	if end := ends(cmds); len(end) != 1 || reasonOf(end[0]) != "tests fail" || len(lookups(cmds)) != 1 {
		t.Fatalf("commands = %#v, want acceptance's own failure, then the lookup", cmds)
	}

	cmds, _ = d.send(core.StopRequested{})
	for _, c := range cmds {
		if _, ok := c.(core.StopSession); ok {
			t.Fatalf("stopped a session that already ended: %#v", c)
		}
	}
	cmds, _ = d.send(core.PullRequestFound{IssueID: issueID("9"), PullRequest: noPR})
	wantCommands(t, unrecorded(cmds), failureOf("9", "implement", "acceptance"))
}

func TestAE5AStoppedSessionIsRecordedWithoutUsage(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("9", 1, ready))
	d.send(core.StopRequested{})

	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "acceptance", Outcome: failed("stopped by crew")})
	end := ends(cmds)
	if len(end) != 1 || !reflect.DeepEqual(end[0].Usage, crew.Usage{}) || reasonOf(end[0]) != "stopped by crew" {
		t.Fatalf("ends = %#v, want a stopped end with no usage", end)
	}
}

func TestWithoutAFinderNothingIsLookedUp(t *testing.T) {
	d := &driver{t: t, m: core.New(draft(), 2, core.Journaling(nil)), now: t0}
	d.running(issue("9", 1, ready))

	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "acceptance", Outcome: failed("broke")})
	if len(lookups(cmds)) != 0 {
		t.Fatalf("commands = %#v, want the route's first step at once, not looked up", cmds)
	}
	wantCommands(t, unrecorded(cmds), failureOf("9", "implement", "acceptance"))
}

func TestARunWithoutASessionLooksNothingUp(t *testing.T) {
	rules := draft()
	rules[0].Actions = []crew.Action{shellAction("lint", "make lint")}
	d := usageDriver(t, rules)
	d.running(issue("9", 1, ready))

	cmds, _ := d.send(core.ShellEnded{IssueID: issueID("9"), Action: "lint", Outcome: exited(0)})
	wantCommands(t, unrecorded(cmds), core.Move{IssueID: issueID("9"), From: inProgress, To: readyToReview})
}

func TestAFreshWorkspaceLooksUpFromItsCreationAndAResumedOneFromAnyTime(t *testing.T) {
	t.Run("fresh", func(t *testing.T) {
		d := usageDriver(t, draft())
		cmds, _ := d.poll(issue("9", 1, ready))
		d.send(core.CallResult{ID: moveID(t, cmds, "9"), Result: core.ResultDone})
		d.ready("9")
		made := d.now
		d.send(core.SessionStarted{IssueID: issueID("9"), Action: "acceptance"})
		cmds, _ = d.send(core.SessionEnded{IssueID: issueID("9"), Action: "acceptance", Outcome: failed("broke")})
		if got := lookups(cmds); len(got) != 1 || !got[0].Since.Equal(made) {
			t.Fatalf("lookups = %#v, want since %v", got, made)
		}
	})
	t.Run("resumed", func(t *testing.T) {
		past := failedRun(t, "broke")
		d := &driver{t: t, m: core.New(crewRules(), 2,
			core.Journaling(past), core.Reopening(), core.FindingPullRequests()), now: t0, inputs: 1000}
		d.takeIssue(issue("9", 1, readyForDev))
		d.send(reopened("9", "development"))
		d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
		cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "lfg", Outcome: succeeded})
		if got := lookups(cmds); len(got) != 1 || !got[0].Since.IsZero() || got[0].Branch != "crew/issue-9-development" {
			t.Fatalf("lookups = %#v, want one from crew/issue-9-development with no since", got)
		}
	})
}

// endWith ends the session of action on key with usage, answers the lookup
// it asks, if any, with no pull request, and settles what follows.
func (d *driver) endWith(key string, action crew.ActionName, usage crew.Usage) {
	d.t.Helper()
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID(key), Action: action, Outcome: succeeded, Usage: usage})
	if len(lookups(cmds)) > 0 {
		cmds, _ = d.send(core.PullRequestFound{IssueID: issueID(key), PullRequest: noPR})
	}
	d.settle(cmds)
}

func TestAE4ARuleMissingACostShowsTheKnownCostAsPartial(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("5", 1, ready))
	d.endWith("5", "acceptance", crew.Usage{})
	d.endWith("5", "development", spent)

	if got := onlyEntry(t, d).Spend(); got != partialSpend {
		t.Fatalf("handled spend = %#v, want the known cost of two sessions, %#v", got, partialSpend)
	}
}

func TestAnActionWithoutASessionAddsNothingAndMakesNothingPartial(t *testing.T) {
	rules := draft()
	rules[0].Actions[0] = shellAction("install", "make deps")
	d := usageDriver(t, rules)
	d.running(issue("5", 1, ready))
	d.settle(func() []core.Command {
		cmds, _ := d.send(core.ShellEnded{IssueID: issueID("5"), Action: "install", Outcome: exited(0)})
		return cmds
	}())
	d.endWith("5", "development", spent)

	if got, want := onlyEntry(t, d).Spend(), spent.Spend(); got != want {
		t.Fatalf("handled spend = %#v, want only the session's, %#v", got, want)
	}
	if got := d.m.View().Spent; got != spent.Spend() {
		t.Fatalf("run spend = %#v, want only the session's", got)
	}
}

func TestAE7TheRunSpendCountsEveryRuleRunOfThisRun(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("7", 1, ready))
	d.endWith("7", "acceptance", spent)
	d.endWith("7", "development", spent)

	d.running(issue("7", 1, readyToReview))
	d.endWith("7", "custom_review", tokens)

	if got := onlyEntry(t, d).Rule; got != "review" {
		t.Fatalf("handled shows %q, want only the review rule", got)
	}
	want := crew.Spend{Sessions: 3, Cost: 24.80, WithCost: 2, Tokens: crew.Tokens{
		Input: 20, Output: 45, CacheRead: 600, CacheWrite: 80,
	}, WithTokens: 3}
	if got := d.m.View().Spent; got != want {
		t.Fatalf("run spend = %#v, want all three sessions, %#v", got, want)
	}
}

// TestAHandledEntryCarriesTheSpendOfTheRulesThatEndedOnItBefore checks
// KTD14: a rule that replaces an issue's Handled entry carries the old
// entry's spend, and its own earlier spend, in Earlier.
func TestAHandledEntryCarriesTheSpendOfTheRulesThatEndedOnItBefore(t *testing.T) {
	rules := append(draft(), crew.Rule{
		Name: "merge", Labels: crew.Labels{Ready: readyToMerge, Running: "merging"},
		Actions: []crew.Action{sessionAction("merge_it", "Merge issue {{.Issue.Ref}}")},
		Routes:  routes("merged", needsAttention),
	})
	d := usageDriver(t, rules)
	cost := func(dollars float64, output int64) crew.Usage {
		return crew.Usage{Cost: crew.Some(dollars), Tokens: crew.Some(crew.Tokens{Output: output})}
	}

	d.running(issue("8", 1, ready))
	d.endWith("8", "acceptance", cost(1, 10))
	d.endWith("8", "development", cost(2, 20))
	if got := onlyEntry(t, d); got.Rule != "implement" || got.Earlier != (crew.Spend{}) {
		t.Fatalf("entry after implement: rule %q, earlier %#v; want implement with no earlier spend", got.Rule, got.Earlier)
	}

	d.running(issue("8", 1, readyToReview))
	d.endWith("8", "custom_review", cost(4, 40))
	implement := crew.Spend{Sessions: 2, Cost: 3, WithCost: 2, Tokens: crew.Tokens{Output: 30}, WithTokens: 2}
	if got := onlyEntry(t, d); got.Rule != "review" || got.Earlier != implement {
		t.Fatalf("entry after review: rule %q, earlier %#v; want review with implement's spend %#v",
			got.Rule, got.Earlier, implement)
	}

	d.running(issue("8", 1, readyToMerge))
	d.endWith("8", "merge_it", cost(8, 80))
	both := crew.Spend{Sessions: 3, Cost: 7, WithCost: 3, Tokens: crew.Tokens{Output: 70}, WithTokens: 3}
	if got := onlyEntry(t, d); got.Rule != "merge" || got.Earlier != both {
		t.Fatalf("entry after merge: rule %q, earlier %#v; want merge with implement's and review's spend %#v",
			got.Rule, got.Earlier, both)
	}
}

func TestStatusShowsAnEndedActionsSpendOnlyWhenSetTo(t *testing.T) {
	run := func(opts ...core.Option) crew.Status {
		t.Helper()
		opts = append([]core.Option{core.ReportingStatus()}, opts...)
		d := usageDriver(t, draft(), opts...)
		d.runAll(d.take(issue("1", 1, ready)))
		d.endWith("1", "acceptance", spent)
		cmds, _ := d.send(core.Tick{})
		return statusOf(t, cmds, "1")
	}

	off := run()
	if got := off.Actions()[0].State; got != (crew.ActionSucceeded{Verdict: crew.Passed}) {
		t.Fatalf("status without the setting holds %#v", got)
	}
	on := run(core.ReportingUsage())
	shown := crew.Some(crew.ShownUsage{Spend: spent.Spend()})
	if got := on.Actions()[0].State; got != (crew.ActionSucceeded{Verdict: crew.Passed, Usage: shown}) {
		t.Fatalf("ended action's status = %#v, want its spend, with no pull request looked up yet", got)
	}
	if got, running := on.Actions()[1].State.(crew.ActionRunning); !running {
		t.Fatalf("running action's status = %#v, want no spend", got)
	}
}

func TestARunningActionShowsNoSpend(t *testing.T) {
	d := usageDriver(t, draft(), core.ReportingStatus(), core.ReportingUsage())
	d.runAll(d.take(issue("1", 1, ready)))

	cmds, _ := d.send(core.Tick{})
	st := statusOf(t, cmds, "1")
	if got, running := st.Actions()[0].State.(crew.ActionRunning); !running {
		t.Fatalf("running action's status = %#v, want running with no spend", got)
	}
	if got := d.m.View().Spent; got != (crew.Spend{}) {
		t.Fatalf("run spend = %#v before any action ended", got)
	}
}
