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
	opts = append([]core.Option{core.RecordingRuns(nil), core.FindingPullRequests()}, opts...)
	return &driver{t: t, m: core.New(rules, 2, opts...), now: t0}
}

var (
	spent = crew.Usage{
		Cost: 12.40, HasCost: true, Tokens: crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40},
		HasTokens: true, Turns: 7, HasTurns: true, Models: []string{"claude-opus-5-5"},
	}
	pr45   = crew.PullRequest{Lookup: crew.PullRequestFound, Ref: "#45", URL: "https://example.com/pull/45"}
	noPR   = crew.PullRequest{Lookup: crew.PullRequestNone}
	tokens = crew.Usage{Tokens: crew.Tokens{Output: 5}, HasTokens: true}
	// partialSpend is spent's session summed with one that reported
	// nothing: two sessions, of which only one reported cost and tokens.
	partialSpend = crew.Spend{Sessions: 2, Cost: 12.40, WithCost: 1, Tokens: spent.Tokens, WithTokens: 1}
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

// phaseOf returns the phase of the named action of key, from the view.
func phaseOf(t *testing.T, m *core.Model, key string, action crew.ActionName) core.Phase {
	t.Helper()
	for _, iv := range m.View().Issues {
		for _, a := range iv.Actions {
			if iv.Issue.ID.Key == key && a.Name == action {
				return a.Phase
			}
		}
	}
	t.Fatalf("no action %s of %s", action, key)
	return 0
}

func TestAE1AnEndedSessionLooksUpItsPullRequestAndRecordsItWithItsUsage(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("31", 1, ready))
	ready := d.m.View().Issues[0].Actions[1]

	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("31"), Action: "development", Outcome: succeeded, Usage: spent})
	got := lookups(cmds)
	ws := space("31", "development")
	if len(got) != 1 || got[0].Branch != ws.Branch || got[0].IssueID != issueID("31") || got[0].Action != "development" {
		t.Fatalf("lookups = %#v, want one from %s", got, ws.Branch)
	}
	if len(records(cmds)) != 0 || phaseOf(t, d.m, "31", "development") != core.PhaseFinishing {
		t.Fatalf("the action ended before its lookup: %#v", cmds)
	}

	cmds, _ = d.send(core.PullRequestFound{IssueID: issueID("31"), Action: "development", PullRequest: pr45})
	end := records(cmds)
	if len(end) != 1 || end[0].Event != core.RunEnded || !end[0].Succeeded ||
		!reflect.DeepEqual(end[0].Usage, spent) || end[0].PullRequest != pr45 ||
		end[0].SessionStarted != ready.Started {
		t.Fatalf("records = %#v, want the end with the usage, #45 and the session's start", end)
	}

	d.send(core.SessionEnded{IssueID: issueID("31"), Action: "acceptance", Outcome: succeeded})
	verdict, _ := d.send(core.PullRequestFound{IssueID: issueID("31"), Action: "acceptance", PullRequest: noPR})
	d.send(core.CallResult{ID: moveID(t, verdict, "31"), Result: core.ResultDone})
	entry := onlyEntry(t, d)
	want := []core.HandledAction{
		{Name: "acceptance", Spend: crew.Spend{Sessions: 1}, PullRequest: noPR},
		{Name: "development", Spend: spent.Spend(), PullRequest: pr45},
	}
	if !reflect.DeepEqual(entry.Actions, want) {
		t.Fatalf("handled actions = %#v, want %#v", entry.Actions, want)
	}
	if got := entry.Spend(); got != partialSpend {
		t.Fatalf("handled spend = %#v, want %#v", got, partialSpend)
	}
}

func TestALookupThatAnswersFirstWaitsForTheCheck(t *testing.T) {
	d := usageDriver(t, checked())
	d.running(issue("74", 1, ready))
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded, Usage: spent})
	if len(lookups(cmds)) != 1 {
		t.Fatalf("commands = %#v, want a lookup next to the check", cmds)
	}
	wantPhase := func(want core.Phase) {
		t.Helper()
		if got := phaseOf(t, d.m, "74", "development"); got != want {
			t.Fatalf("phase = %v, want %v", got, want)
		}
	}

	d.send(core.PullRequestFound{IssueID: issueID("74"), Action: "development", PullRequest: pr45})
	wantPhase(core.PhaseChecking)
	cmds, _ = d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Outcome: failed("no pull request")})
	wantPhase(core.PhaseEnded)
	if end := records(cmds); len(end) != 1 || end[0].Succeeded || end[0].PullRequest != pr45 || !end[0].Usage.HasCost {
		t.Fatalf("records = %#v, want a failed end keeping the session's usage and #45", end)
	}
}

func TestACheckThatEndsFirstWaitsForTheLookup(t *testing.T) {
	d := usageDriver(t, checked())
	d.running(issue("74", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})

	cmds, _ := d.send(core.CheckEnded{IssueID: issueID("74"), Action: "development", Outcome: succeeded})
	if len(records(cmds)) != 0 || phaseOf(t, d.m, "74", "development") != core.PhaseFinishing {
		t.Fatalf("the action ended before its lookup: %#v", cmds)
	}
	cmds, _ = d.send(core.PullRequestFound{IssueID: issueID("74"), Action: "development", PullRequest: pr45})
	if end := records(cmds); len(end) != 1 || !end[0].Succeeded || end[0].PullRequest != pr45 {
		t.Fatalf("records = %#v, want a succeeded end with #45", end)
	}
}

func TestAStopDuringTheLookupKeepsTheSessionsOwnFailure(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("9", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("9"), Action: "development", Outcome: failed("tests fail")})

	cmds, _ := d.send(core.StopRequested{})
	for _, c := range cmds {
		if s, ok := c.(core.StopSession); ok && s.Action == "development" {
			t.Fatalf("stopped a session that already ended: %#v", c)
		}
	}
	cmds, _ = d.send(core.PullRequestFound{IssueID: issueID("9"), Action: "development", PullRequest: noPR})
	end := records(cmds)
	if len(end) != 1 || end[0].Reason != "tests fail" || end[0].PullRequest != noPR {
		t.Fatalf("records = %#v, want the session's own failure with no pull request", end)
	}
}

func TestAE5AStoppedSessionIsRecordedWithoutUsage(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("9", 1, ready))
	d.send(core.StopRequested{})
	d.send(core.SessionEnded{IssueID: issueID("9"), Action: "development", Outcome: failed("stopped by crew")})

	cmds, _ := d.send(core.PullRequestFound{IssueID: issueID("9"), Action: "development", PullRequest: noPR})
	end := records(cmds)
	if len(end) != 1 || end[0].Usage.HasCost || end[0].Usage.HasTokens || end[0].Reason != "stopped by crew" {
		t.Fatalf("records = %#v, want a stopped end with no usage", end)
	}
}

func TestWithoutAFinderNothingIsLookedUp(t *testing.T) {
	d := &driver{t: t, m: core.New(draft(), 2, core.RecordingRuns(nil)), now: t0}
	d.running(issue("9", 1, ready))

	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "development", Outcome: succeeded, Usage: spent})
	end := records(cmds)
	if len(lookups(cmds)) != 0 || len(end) != 1 || end[0].PullRequest.Lookup != crew.PullRequestNotLookedUp {
		t.Fatalf("commands = %#v, want an end at once, not looked up", cmds)
	}
}

func TestAFreshWorkspaceLooksUpFromItsCreationAndAResumedOneFromAnyTime(t *testing.T) {
	t.Run("fresh", func(t *testing.T) {
		d := usageDriver(t, draft())
		cmds, _ := d.poll(issue("9", 1, ready))
		d.send(core.CallResult{ID: moveID(t, cmds, "9"), Result: core.ResultDone})
		d.send(space("9", "acceptance"))
		made := d.now
		d.send(core.SessionStarted{IssueID: issueID("9"), Action: "acceptance"})
		cmds, _ = d.send(core.SessionEnded{IssueID: issueID("9"), Action: "acceptance", Outcome: succeeded})
		if got := lookups(cmds); len(got) != 1 || !got[0].Since.Equal(made) {
			t.Fatalf("lookups = %#v, want since %v", got, made)
		}
	})
	t.Run("resumed", func(t *testing.T) {
		past := endedRun(startedRun("9", "development", "lfg", "lfg"), failed("broke"))
		d := &driver{t: t, m: core.New(crewRules(), 2,
			core.RecordingRuns([]core.RunRecord{past}), core.Reopening(), core.FindingPullRequests()), now: t0}
		d.takeIssue(issue("9", 1, readyForDev))
		d.send(reopened("9", "lfg", "lfg"))
		d.send(core.SessionStarted{IssueID: issueID("9"), Action: "lfg"})
		cmds, _ := d.send(core.SessionEnded{IssueID: issueID("9"), Action: "lfg", Outcome: succeeded})
		if got := lookups(cmds); len(got) != 1 || !got[0].Since.IsZero() || got[0].Branch != "crew/issue-9-lfg" {
			t.Fatalf("lookups = %#v, want one from crew/issue-9-lfg with no since", got)
		}
	})
}

func TestAE4ARuleMissingACostShowsTheKnownCostAsPartial(t *testing.T) {
	d := usageDriver(t, draft())
	d.running(issue("5", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("5"), Action: "acceptance", Outcome: failed("killed")})
	d.send(core.PullRequestFound{IssueID: issueID("5"), Action: "acceptance", PullRequest: noPR})
	d.send(core.SessionEnded{IssueID: issueID("5"), Action: "development", Outcome: succeeded, Usage: spent})
	verdict, _ := d.send(core.PullRequestFound{IssueID: issueID("5"), Action: "development", PullRequest: pr45})
	d.settle(verdict)

	if got := onlyEntry(t, d).Spend(); got != partialSpend {
		t.Fatalf("handled spend = %#v, want the known cost of two sessions, %#v", got, partialSpend)
	}
}

func TestAnActionWithoutASessionAddsNothingAndMakesNothingPartial(t *testing.T) {
	d := usageDriver(t, draft())
	cmds, _ := d.poll(issue("5", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "5"), Result: core.ResultDone})
	d.send(core.WorkspaceFailed{IssueID: issueID("5"), Action: "acceptance", Reason: "no space left"})
	d.send(space("5", "development"))
	d.send(core.SessionStarted{IssueID: issueID("5"), Action: "development"})
	d.send(core.SessionEnded{IssueID: issueID("5"), Action: "development", Outcome: succeeded, Usage: spent})
	verdict, _ := d.send(core.PullRequestFound{IssueID: issueID("5"), Action: "development", PullRequest: pr45})
	d.settle(verdict)

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
	for _, action := range []crew.ActionName{"acceptance", "development"} {
		d.send(core.SessionEnded{IssueID: issueID("7"), Action: action, Outcome: succeeded, Usage: spent})
		verdict, _ := d.send(core.PullRequestFound{IssueID: issueID("7"), Action: action, PullRequest: noPR})
		d.settle(verdict)
	}

	d.running(issue("7", 1, readyToReview))
	d.send(core.SessionEnded{IssueID: issueID("7"), Action: "custom_review", Outcome: succeeded, Usage: tokens})
	verdict, _ := d.send(core.PullRequestFound{IssueID: issueID("7"), Action: "custom_review", PullRequest: noPR})
	d.settle(verdict)

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
		Name:    "merge",
		Labels:  crew.Labels{Ready: readyToMerge, Running: "merging", Success: "merged", Failure: needsAttention},
		Actions: []crew.Action{{Name: "merge_it", Prompt: "Merge issue {{.Issue.Ref}}"}},
	})
	d := usageDriver(t, rules)
	cost := func(dollars float64, output int64) crew.Usage {
		return crew.Usage{Cost: dollars, HasCost: true, Tokens: crew.Tokens{Output: output}, HasTokens: true}
	}
	end := func(action crew.ActionName, u crew.Usage) {
		d.send(core.SessionEnded{IssueID: issueID("8"), Action: action, Outcome: succeeded, Usage: u})
		verdict, _ := d.send(core.PullRequestFound{IssueID: issueID("8"), Action: action, PullRequest: noPR})
		d.settle(verdict)
	}

	d.running(issue("8", 1, ready))
	end("acceptance", cost(1, 10))
	end("development", cost(2, 20))
	if got := onlyEntry(t, d); got.Rule != "implement" || got.Earlier != (crew.Spend{}) {
		t.Fatalf("entry after implement: rule %q, earlier %#v; want implement with no earlier spend", got.Rule, got.Earlier)
	}

	d.running(issue("8", 1, readyToReview))
	end("custom_review", cost(4, 40))
	implement := crew.Spend{Sessions: 2, Cost: 3, WithCost: 2, Tokens: crew.Tokens{Output: 30}, WithTokens: 2}
	if got := onlyEntry(t, d); got.Rule != "review" || got.Earlier != implement {
		t.Fatalf("entry after review: rule %q, earlier %#v; want review with implement's spend %#v",
			got.Rule, got.Earlier, implement)
	}

	d.running(issue("8", 1, readyToMerge))
	end("merge_it", cost(8, 80))
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
		d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded, Usage: spent})
		d.send(core.PullRequestFound{IssueID: issueID("1"), Action: "acceptance", PullRequest: pr45})
		cmds, _ := d.send(core.Tick{})
		return statusOf(t, cmds, "1")
	}

	off := run()
	if off.Actions[0].Spend != (crew.Spend{}) || off.Actions[0].PullRequest != (crew.PullRequest{}) {
		t.Fatalf("status without the setting holds %#v", off.Actions[0])
	}
	on := run(core.ReportingUsage())
	if on.Actions[0].Spend != spent.Spend() || on.Actions[0].PullRequest != pr45 {
		t.Fatalf("ended action's status = %#v, want its spend and #45", on.Actions[0])
	}
	if on.Actions[1].Spend != (crew.Spend{}) || on.Actions[1].PullRequest != (crew.PullRequest{}) {
		t.Fatalf("running action's status = %#v, want no spend", on.Actions[1])
	}
}

func TestRunningAndFinishingActionsShowNoSpend(t *testing.T) {
	d := usageDriver(t, draft(), core.ReportingStatus(), core.ReportingUsage())
	d.runAll(d.take(issue("1", 1, ready)))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded, Usage: spent})

	cmds, _ := d.send(core.Tick{})
	st := statusOf(t, cmds, "1")
	if st.Actions[0].State != crew.ActionRunning || st.Actions[0].Spend != (crew.Spend{}) {
		t.Fatalf("finishing action's status = %#v, want running with no spend", st.Actions[0])
	}
	if got := d.m.View().Spent; got != (crew.Spend{}) {
		t.Fatalf("run spend = %#v before any action ended", got)
	}
}
