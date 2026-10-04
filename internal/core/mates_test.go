package core_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// draftPairs are the draft workflow's actions, as a mates entry lists them.
var draftPairs = []string{"implement/acceptance", "implement/development", "review/custom_review"}

// mated is a workflow whose triage acts as clerk, whose development acts as
// developer and whose review acts as reviewer.
func mated() []crew.Stage {
	return []crew.Stage{
		{
			Name: "triage", Label: needsTriage, MovesTo: triaging, OnSuccess: ready, OnFailure: needsAttention,
			Actions: []crew.Action{{Name: "triage", Prompt: "Triage {{.Issue.Ref}}", Mate: "clerk"}},
		},
		{
			Name: "implement", Label: ready, MovesTo: inProgress, OnSuccess: readyToReview, OnFailure: needsAttention,
			Actions: []crew.Action{{Name: "development", Prompt: "Develop {{.Issue.Ref}}", Mate: "developer"}},
		},
		{
			Name: "review", Label: readyToReview, MovesTo: inReview, OnSuccess: readyToMerge, OnFailure: needsAttention,
			Actions: []crew.Action{{Name: "review", Prompt: "Review {{.Issue.Ref}}", Mate: "reviewer"}},
		},
	}
}

// crewMates configures mated's mates, clerk the default, with unable the
// short reasons of those that cannot act at startup.
func crewMates(unable map[string]string) core.MatesConfig {
	return core.MatesConfig{
		Default: "clerk", Names: []string{"clerk", "developer", "reviewer"}, Unable: unable, Login: "octocat",
	}
}

// matesDriver is a driver for workflow with mates c.
func matesDriver(t *testing.T, workflow []crew.Stage, c core.MatesConfig) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(workflow, 4, core.WithMates(c)), now: t0}
}

// entry returns the view's entry for name, "you" for the boss's.
func entry(t *testing.T, d *driver, name string) core.MateView {
	t.Helper()
	for _, e := range d.m.View().Mates {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no mates entry %s in %#v", name, d.m.View().Mates)
	return core.MateView{}
}

// names returns the names of the view's entries, in order.
func names(d *driver) []string {
	entries := d.m.View().Mates
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

// endedAs runs action of issue i to a successful end with usage, and
// settles its verdict.
func endedAs(d *driver, i crew.Issue, action string, usage crew.Usage) {
	d.t.Helper()
	d.running(i)
	verdict, _ := d.send(core.SessionEnded{IssueKey: i.Key, Action: action, Outcome: succeeded, Usage: usage})
	d.settle(verdict)
}

func TestAE1AMateShowsItsStateWritesPairsTotalsAndRunningActions(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(nil))
	for _, key := range []string{"2", "3", "4"} {
		endedAs(d, issue(key, 2, needsTriage), "triage", spent)
	}
	d.running(issue("1", 1, ready))

	if got := names(d); !slices.Equal(got, []string{"clerk", "developer", "reviewer", "you"}) {
		t.Fatalf("entries = %v, want the mates in config order, then you", got)
	}
	triaged := spent.Spend().Add(spent.Spend()).Add(spent.Spend())
	want := core.MateView{
		Name: "clerk", Acting: true, State: "acting", Writes: true, Pairs: []string{"triage/triage"}, Spend: triaged,
	}
	if got := entry(t, d, "clerk"); !reflect.DeepEqual(got, want) {
		t.Fatalf("clerk:\n got %#v\nwant %#v", got, want)
	}
	want = core.MateView{
		Name: "developer", Acting: true, State: "acting", Pairs: []string{"implement/development"},
		Running: []core.RunningAction{{IssueRef: "#1", Stage: "implement", Action: "development"}},
	}
	if got := entry(t, d, "developer"); !reflect.DeepEqual(got, want) {
		t.Fatalf("developer:\n got %#v\nwant %#v", got, want)
	}
	want = core.MateView{Name: "you", You: true, Login: "octocat"}
	if got := entry(t, d, "you"); !reflect.DeepEqual(got, want) {
		t.Fatalf("you:\n got %#v\nwant %#v", got, want)
	}
}

func TestAE2WithoutMatesOnlyYouActsAndCounts(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded, Usage: spent})

	if got := names(d); !slices.Equal(got, []string{"you"}) {
		t.Fatalf("entries = %v, want only you", got)
	}
	want := core.MateView{
		Name: "you", You: true, Writes: true,
		Pairs:   draftPairs,
		Running: []core.RunningAction{{IssueRef: "#1", Stage: "implement", Action: "development"}},
		Spend:   spent.Spend(),
	}
	if got := entry(t, d, "you"); !reflect.DeepEqual(got, want) {
		t.Fatalf("you:\n got %#v\nwant %#v", got, want)
	}
}

func TestAE3AMateThatCannotActLetsItsActionsActAndCountAsYou(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(map[string]string{"reviewer": "no key"}))
	endedAs(d, issue("5", 1, readyToReview), "review", spent)

	want := core.MateView{
		Name: "reviewer", State: "cannot act: no key", ActsAsYou: true, Pairs: []string{"review/review"},
	}
	if got := entry(t, d, "reviewer"); !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewer:\n got %#v\nwant %#v", got, want)
	}
	want = core.MateView{
		Name: "you", You: true, Login: "octocat", Pairs: []string{"review/review"}, Spend: spent.Spend(),
	}
	if got := entry(t, d, "you"); !reflect.DeepEqual(got, want) {
		t.Fatalf("you:\n got %#v\nwant %#v", got, want)
	}
}

func TestADefaultMateThatCannotActPutsTheWritesOnYou(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(map[string]string{"clerk": "not installed"}))
	if got := entry(t, d, "clerk"); got.Writes || got.State != "cannot act: not installed" {
		t.Fatalf("clerk = %#v, want cannot act, without the writes", got)
	}
	if got := entry(t, d, "you"); !got.Writes || !slices.Equal(got.Pairs, []string{"triage/triage"}) {
		t.Fatalf("you = %#v, want the writes and triage/triage", got)
	}
}

func TestAE4WritesThatFallBackStopTheDefaultMateForGood(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(nil))
	_, events := d.send(core.MatesChecked{WritesLost: "clerk was refused"})
	wantEvents(t, events, core.MateStopped{
		At: d.now, Mate: "clerk", Reason: "writes as you", Warning: "clerk was refused",
	})
	clerk := entry(t, d, "clerk")
	if clerk.State != "writes as you" || clerk.Acting || clerk.Writes ||
		!slices.Equal(clerk.Warnings, []string{"clerk was refused"}) {
		t.Fatalf("clerk = %#v, want writes as you with the warning, without the writes", clerk)
	}
	if !entry(t, d, "you").Writes {
		t.Fatalf("you does not carry the writes after the fallback")
	}

	_, events = d.send(core.MatesChecked{WritesLost: "clerk was refused"})
	wantEvents(t, events)
	_, events = d.send(core.MatesChecked{})
	wantEvents(t, events)
	if got := entry(t, d, "clerk"); !reflect.DeepEqual(got, clerk) {
		t.Fatalf("clerk after a reading without the fallback = %#v, want %#v", got, clerk)
	}
}

func TestAE5ATokenNotRenewedStopsAMateUntilItRenews(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(nil))
	_, events := d.send(core.MatesChecked{NotRenewed: map[string]string{"developer": "could not renew"}})
	wantEvents(t, events, core.MateStopped{
		At: d.now, Mate: "developer", Reason: "token not renewed", Warning: "could not renew",
	})
	if got := entry(t, d, "developer"); got.State != "token not renewed" || got.Acting ||
		!slices.Equal(got.Warnings, []string{"could not renew"}) {
		t.Fatalf("developer = %#v, want token not renewed with the warning", got)
	}

	_, events = d.send(core.MatesChecked{NotRenewed: map[string]string{"developer": "could not renew"}})
	wantEvents(t, events)

	_, events = d.send(core.MatesChecked{})
	wantEvents(t, events, core.MateActsAgain{At: d.now, Mate: "developer"})
	if got := entry(t, d, "developer"); got.State != "acting" || !got.Acting || got.Warnings != nil {
		t.Fatalf("developer = %#v, want acting without warnings", got)
	}
}

func TestTheDefaultMateWithBothProblemsKeepsWritingAsYouOnceItsTokenRenews(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(nil))
	token := map[string]string{"clerk": "could not renew"}
	_, events := d.send(core.MatesChecked{NotRenewed: token})
	wantEvents(t, events, core.MateStopped{
		At: d.now, Mate: "clerk", Reason: "token not renewed", Warning: "could not renew",
	})
	_, events = d.send(core.MatesChecked{WritesLost: "refused", NotRenewed: token})
	wantEvents(t, events, core.MateStopped{At: d.now, Mate: "clerk", Reason: "writes as you", Warning: "refused"})
	if got := entry(t, d, "clerk"); got.State != "writes as you" ||
		!slices.Equal(got.Warnings, []string{"refused", "could not renew"}) {
		t.Fatalf("clerk = %#v, want writes as you with both warnings", got)
	}

	_, events = d.send(core.MatesChecked{WritesLost: "refused"})
	wantEvents(t, events)
	if got := entry(t, d, "clerk"); got.State != "writes as you" || !slices.Equal(got.Warnings, []string{"refused"}) {
		t.Fatalf("clerk = %#v, want writes as you with only the writes warning", got)
	}
}

func TestReadingsForMatesThatCannotActOrAreNotConfiguredAreIgnored(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(map[string]string{"clerk": "no key", "reviewer": "no key"}))
	before := d.m.View().Mates
	_, events := d.send(core.MatesChecked{
		WritesLost: "refused", NotRenewed: map[string]string{"reviewer": "x", "stranger": "y"},
	})
	wantEvents(t, events)
	if got := d.m.View().Mates; !reflect.DeepEqual(got, before) {
		t.Fatalf("entries after the reading:\n got %#v\nwant %#v", got, before)
	}
}

func TestAnActionRunsOnItsEntryFromItsSessionUntilItsSpendLands(t *testing.T) {
	development := core.RunningAction{IssueRef: "#74", Stage: "implement", Action: "development"}
	tests := []struct {
		name    string
		to      func(d *driver)
		running bool
	}{
		{"starting", func(d *driver) {
			cmds, _ := d.poll(issue("74", 1, ready))
			take, _ := d.send(core.CallResult{ID: moveID(d.t, cmds, "74"), Result: core.ResultDone})
			for _, c := range take {
				if w, ok := c.(core.CreateWorkspace); ok {
					d.send(space(w.Issue.Key, w.Action))
				}
			}
		}, false},
		{"running", func(d *driver) { d.running(issue("74", 1, ready)) }, true},
		{"checking", func(d *driver) {
			d.running(issue("74", 1, ready))
			d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
		}, true},
		{"finishing", func(d *driver) {
			d.running(issue("74", 1, ready))
			d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
			d.send(core.CheckEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
		}, true},
		{"ended", func(d *driver) {
			d.running(issue("74", 1, ready))
			d.send(core.SessionEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
			d.send(core.CheckEnded{IssueKey: "74", Action: "development", Outcome: succeeded})
			d.send(core.PullRequestFound{IssueKey: "74", Action: "development", PullRequest: noPR})
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := usageDriver(t, checked())
			tt.to(d)
			if got := slices.Contains(entry(t, d, "you").Running, development); got != tt.running {
				t.Fatalf("development running on you = %v, want %v", got, tt.running)
			}
		})
	}
}

func TestAnActionThatEndedWithoutASessionAddsNothing(t *testing.T) {
	d := matesDriver(t, mated(), crewMates(nil))
	cmds, _ := d.poll(issue("2", 1, needsTriage))
	d.send(core.CallResult{ID: moveID(t, cmds, "2"), Result: core.ResultDone})
	d.send(core.WorkspaceFailed{IssueKey: "2", Action: "triage", Reason: "disk full"})
	for _, e := range d.m.View().Mates {
		if e.Spend != (crew.Spend{}) || e.Running != nil {
			t.Fatalf("entry %s = %#v, want no spend and nothing running", e.Name, e)
		}
	}
}

func TestTheEntriesSpendSumsToTheViewsSpent(t *testing.T) {
	workflow := hiddenReview()
	workflow[0].Actions[1].Mate = "developer"
	workflow[1].Actions[0].Mate = "reviewer"
	d := matesDriver(t, workflow, core.MatesConfig{
		Names: []string{"developer", "reviewer"}, Unable: map[string]string{"reviewer": "bad key file"},
	})
	reviewed(d, failed("changes requested"))

	v := d.m.View()
	var sum crew.Spend
	for _, e := range v.Mates {
		sum = sum.Add(e.Spend)
	}
	if sum != v.Spent || v.Spent.Sessions != 3 {
		t.Fatalf("entries sum %#v, view spent %#v: want equal, with 3 sessions", sum, v.Spent)
	}
	if got := entry(t, d, "developer").Spend.Sessions; got != 1 {
		t.Fatalf("developer's sessions = %d, want 1", got)
	}
}

func TestTheMatesViewSharesNoMemoryWithTheModel(t *testing.T) {
	c := crewMates(nil)
	d := matesDriver(t, mated(), c)
	c.Names[1], c.Default = "stranger", "stranger"
	d.running(issue("1", 1, ready))
	d.send(core.MatesChecked{NotRenewed: map[string]string{"developer": "could not renew"}})

	v := d.m.View()
	developer := v.Mates[1]
	developer.Pairs[0], developer.Warnings[0], developer.Running[0].IssueRef = "x", "x", "x"
	if got := entry(t, d, "developer"); got.Pairs[0] != "implement/development" ||
		got.Warnings[0] != "could not renew" || got.Running[0].IssueRef != "#1" {
		t.Fatalf("developer after changing a view = %#v", got)
	}
	if !entry(t, d, "clerk").Writes {
		t.Fatalf("changing the config after New changed the default mate")
	}
}
