package core_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// draftPairs are the draft rules' actions, as a bots entry lists them.
var draftPairs = []string{"implement/acceptance", "implement/development", "review/custom_review"}

// botRules is a set of rules whose triage acts as clerk, whose development
// acts as developer and whose review acts as reviewer.
func botRules() []crew.Rule {
	return []crew.Rule{
		{
			Name: "triage", Labels: crew.Labels{Ready: needsTriage, Running: triaging, Success: ready, Failure: needsAttention},
			Actions: []crew.Action{{Name: "triage", Prompt: "Triage {{.Issue.Ref}}", Bot: "clerk"}},
		},
		{
			Name:    "implement",
			Labels:  crew.Labels{Ready: ready, Running: inProgress, Success: readyToReview, Failure: needsAttention},
			Actions: []crew.Action{{Name: "development", Prompt: "Develop {{.Issue.Ref}}", Bot: "developer"}},
		},
		{
			Name:    "review",
			Labels:  crew.Labels{Ready: readyToReview, Running: inReview, Success: readyToMerge, Failure: needsAttention},
			Actions: []crew.Action{{Name: "review", Prompt: "Review {{.Issue.Ref}}", Bot: "reviewer"}},
		},
	}
}

// crewBots configures botRules' bots, clerk the default, with unable the
// short reasons of those that cannot act at startup.
func crewBots(unable map[string]string) core.BotsConfig {
	return core.BotsConfig{
		Default: "clerk", Names: []string{"clerk", "developer", "reviewer"}, Unable: unable, Login: "octocat",
	}
}

// botsDriver is a driver for rules with bots c.
func botsDriver(t *testing.T, rules []crew.Rule, c core.BotsConfig) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(rules, 4, core.WithBots(c)), now: t0}
}

// entry returns the view's entry for name, "you" for yours.
func entry(t *testing.T, d *driver, name string) core.BotView {
	t.Helper()
	for _, e := range d.m.View().Bots {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no bots entry %s in %#v", name, d.m.View().Bots)
	return core.BotView{}
}

// names returns the names of the view's entries, in order.
func names(d *driver) []string {
	entries := d.m.View().Bots
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

func TestAE1ABotShowsItsStateWritesPairsTotalsAndRunningActions(t *testing.T) {
	d := botsDriver(t, botRules(), crewBots(nil))
	for _, key := range []string{"2", "3", "4"} {
		endedAs(d, issue(key, 2, needsTriage), "triage", spent)
	}
	d.running(issue("1", 1, ready))

	if got := names(d); !slices.Equal(got, []string{"clerk", "developer", "reviewer", "you"}) {
		t.Fatalf("entries = %v, want the bots in config order, then you", got)
	}
	triaged := spent.Spend().Add(spent.Spend()).Add(spent.Spend())
	want := core.BotView{
		Name: "clerk", Acting: true, State: "acting", Writes: true, Pairs: []string{"triage/triage"}, Spend: triaged,
	}
	if got := entry(t, d, "clerk"); !reflect.DeepEqual(got, want) {
		t.Fatalf("clerk:\n got %#v\nwant %#v", got, want)
	}
	want = core.BotView{
		Name: "developer", Acting: true, State: "acting", Pairs: []string{"implement/development"},
		Running: []core.RunningAction{{IssueRef: "#1", Rule: "implement", Action: "development"}},
	}
	if got := entry(t, d, "developer"); !reflect.DeepEqual(got, want) {
		t.Fatalf("developer:\n got %#v\nwant %#v", got, want)
	}
	want = core.BotView{Name: "you", You: true, Login: "octocat"}
	if got := entry(t, d, "you"); !reflect.DeepEqual(got, want) {
		t.Fatalf("you:\n got %#v\nwant %#v", got, want)
	}
}

func TestAE2WithoutBotsOnlyYouActsAndCounts(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: succeeded, Usage: spent})

	if got := names(d); !slices.Equal(got, []string{"you"}) {
		t.Fatalf("entries = %v, want only you", got)
	}
	want := core.BotView{
		Name: "you", You: true, Writes: true,
		Pairs:   draftPairs,
		Running: []core.RunningAction{{IssueRef: "#1", Rule: "implement", Action: "development"}},
		Spend:   spent.Spend(),
	}
	if got := entry(t, d, "you"); !reflect.DeepEqual(got, want) {
		t.Fatalf("you:\n got %#v\nwant %#v", got, want)
	}
}

func TestAE3ABotThatCannotActLetsItsActionsActAndCountAsYou(t *testing.T) {
	d := botsDriver(t, botRules(), crewBots(map[string]string{"reviewer": "no key"}))
	endedAs(d, issue("5", 1, readyToReview), "review", spent)

	want := core.BotView{
		Name: "reviewer", State: "cannot act: no key", ActsAsYou: true, Pairs: []string{"review/review"},
	}
	if got := entry(t, d, "reviewer"); !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewer:\n got %#v\nwant %#v", got, want)
	}
	want = core.BotView{
		Name: "you", You: true, Login: "octocat", Pairs: []string{"review/review"}, Spend: spent.Spend(),
	}
	if got := entry(t, d, "you"); !reflect.DeepEqual(got, want) {
		t.Fatalf("you:\n got %#v\nwant %#v", got, want)
	}
}

func TestADefaultBotThatCannotActPutsTheWritesOnYou(t *testing.T) {
	d := botsDriver(t, botRules(), crewBots(map[string]string{"clerk": "not installed"}))
	if got := entry(t, d, "clerk"); got.Writes || got.State != "cannot act: not installed" {
		t.Fatalf("clerk = %#v, want cannot act, without the writes", got)
	}
	if got := entry(t, d, "you"); !got.Writes || !slices.Equal(got.Pairs, []string{"triage/triage"}) {
		t.Fatalf("you = %#v, want the writes and triage/triage", got)
	}
}

func TestAE4WritesThatFallBackStopTheDefaultBotForGood(t *testing.T) {
	d := botsDriver(t, botRules(), crewBots(nil))
	_, events := d.send(core.BotsChecked{WritesLost: "clerk was refused"})
	wantEvents(t, events, core.BotStopped{
		At: d.now, Bot: "clerk", Reason: "writes as you", Warning: "clerk was refused",
	})
	clerk := entry(t, d, "clerk")
	if clerk.State != "writes as you" || clerk.Acting || clerk.Writes ||
		!slices.Equal(clerk.Warnings, []string{"clerk was refused"}) {
		t.Fatalf("clerk = %#v, want writes as you with the warning, without the writes", clerk)
	}
	if !entry(t, d, "you").Writes {
		t.Fatalf("you does not carry the writes after the fallback")
	}

	_, events = d.send(core.BotsChecked{WritesLost: "clerk was refused"})
	wantEvents(t, events)
	_, events = d.send(core.BotsChecked{})
	wantEvents(t, events)
	if got := entry(t, d, "clerk"); !reflect.DeepEqual(got, clerk) {
		t.Fatalf("clerk after a reading without the fallback = %#v, want %#v", got, clerk)
	}
}

func TestAE5ATokenNotRenewedStopsABotUntilItRenews(t *testing.T) {
	d := botsDriver(t, botRules(), crewBots(nil))
	_, events := d.send(core.BotsChecked{NotRenewed: map[string]string{"developer": "could not renew"}})
	wantEvents(t, events, core.BotStopped{
		At: d.now, Bot: "developer", Reason: "token not renewed", Warning: "could not renew",
	})
	if got := entry(t, d, "developer"); got.State != "token not renewed" || got.Acting ||
		!slices.Equal(got.Warnings, []string{"could not renew"}) {
		t.Fatalf("developer = %#v, want token not renewed with the warning", got)
	}

	_, events = d.send(core.BotsChecked{NotRenewed: map[string]string{"developer": "could not renew"}})
	wantEvents(t, events)

	_, events = d.send(core.BotsChecked{})
	wantEvents(t, events, core.BotActsAgain{At: d.now, Bot: "developer"})
	if got := entry(t, d, "developer"); got.State != "acting" || !got.Acting || got.Warnings != nil {
		t.Fatalf("developer = %#v, want acting without warnings", got)
	}
}

func TestTheDefaultBotWithBothProblemsKeepsWritingAsYouOnceItsTokenRenews(t *testing.T) {
	d := botsDriver(t, botRules(), crewBots(nil))
	token := map[string]string{"clerk": "could not renew"}
	_, events := d.send(core.BotsChecked{NotRenewed: token})
	wantEvents(t, events, core.BotStopped{
		At: d.now, Bot: "clerk", Reason: "token not renewed", Warning: "could not renew",
	})
	_, events = d.send(core.BotsChecked{WritesLost: "refused", NotRenewed: token})
	wantEvents(t, events, core.BotStopped{At: d.now, Bot: "clerk", Reason: "writes as you", Warning: "refused"})
	if got := entry(t, d, "clerk"); got.State != "writes as you" ||
		!slices.Equal(got.Warnings, []string{"refused", "could not renew"}) {
		t.Fatalf("clerk = %#v, want writes as you with both warnings", got)
	}

	_, events = d.send(core.BotsChecked{WritesLost: "refused"})
	wantEvents(t, events)
	if got := entry(t, d, "clerk"); got.State != "writes as you" || !slices.Equal(got.Warnings, []string{"refused"}) {
		t.Fatalf("clerk = %#v, want writes as you with only the writes warning", got)
	}
}

func TestReadingsForBotsThatCannotActOrAreNotConfiguredAreIgnored(t *testing.T) {
	d := botsDriver(t, botRules(), crewBots(map[string]string{"clerk": "no key", "reviewer": "no key"}))
	before := d.m.View().Bots
	_, events := d.send(core.BotsChecked{
		WritesLost: "refused", NotRenewed: map[string]string{"reviewer": "x", "stranger": "y"},
	})
	wantEvents(t, events)
	if got := d.m.View().Bots; !reflect.DeepEqual(got, before) {
		t.Fatalf("entries after the reading:\n got %#v\nwant %#v", got, before)
	}
}

func TestAnActionRunsOnItsEntryFromItsSessionUntilItsSpendLands(t *testing.T) {
	development := core.RunningAction{IssueRef: "#74", Rule: "implement", Action: "development"}
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
	d := botsDriver(t, botRules(), crewBots(nil))
	cmds, _ := d.poll(issue("2", 1, needsTriage))
	d.send(core.CallResult{ID: moveID(t, cmds, "2"), Result: core.ResultDone})
	d.send(core.WorkspaceFailed{IssueKey: "2", Action: "triage", Reason: crew.NewSessionText("disk full")})
	for _, e := range d.m.View().Bots {
		if e.Spend != (crew.Spend{}) || e.Running != nil {
			t.Fatalf("entry %s = %#v, want no spend and nothing running", e.Name, e)
		}
	}
}

func TestTheEntriesSpendSumsToTheViewsSpent(t *testing.T) {
	rules := draft()
	rules[0].Actions[1].Bot = "developer"
	rules[1].Actions[0].Bot = "reviewer"
	d := botsDriver(t, rules, core.BotsConfig{
		Names: []string{"developer", "reviewer"}, Unable: map[string]string{"reviewer": "bad key file"},
	})
	reviewed(d, failed("changes requested"))

	v := d.m.View()
	var sum crew.Spend
	for _, e := range v.Bots {
		sum = sum.Add(e.Spend)
	}
	if sum != v.Spent || v.Spent.Sessions != 3 {
		t.Fatalf("entries sum %#v, view spent %#v: want equal, with 3 sessions", sum, v.Spent)
	}
	if got := entry(t, d, "developer").Spend.Sessions; got != 1 {
		t.Fatalf("developer's sessions = %d, want 1", got)
	}
}

func TestTheBotsViewSharesNoMemoryWithTheModel(t *testing.T) {
	c := crewBots(nil)
	d := botsDriver(t, botRules(), c)
	c.Names[1], c.Default = "stranger", "stranger"
	d.running(issue("1", 1, ready))
	d.send(core.BotsChecked{NotRenewed: map[string]string{"developer": "could not renew"}})

	v := d.m.View()
	developer := v.Bots[1]
	developer.Pairs[0], developer.Warnings[0], developer.Running[0].IssueRef = "x", "x", "x"
	if got := entry(t, d, "developer"); got.Pairs[0] != "implement/development" ||
		got.Warnings[0] != "could not renew" || got.Running[0].IssueRef != "#1" {
		t.Fatalf("developer after changing a view = %#v", got)
	}
	if !entry(t, d, "clerk").Writes {
		t.Fatalf("changing the config after New changed the default bot")
	}
}
