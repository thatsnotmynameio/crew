package app_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// twoHarnesses is a config of the agents developer, on the harness fake,
// and reviewer, on fake2, each running one rule's session.
const twoHarnesses = `
tracker:
  name: fake
agents:
  developer:
    harness: {name: fake}
  reviewer:
    harness: {name: fake2}
rules:
  development:
    labels: {ready: ready, running: in progress}
    actions:
      - {agent: developer, name: lfg, prompt: "Develop {{.Issue.Ref}}"}
    routes: {passed: ready to review, failed: [report, move: needs attention]}
  review:
    labels: {ready: ready to review, running: in review}
    actions:
      - {agent: reviewer, name: review, prompt: "Review {{.Issue.Ref}}"}
    routes: {passed: ready to merge, failed: [report, move: needs attention]}
`

// withHarnesses registers each of harnesses under its name in r, beside
// tracker as fake.
func withHarnesses(r *crewRun, tracker port.Tracker, harnesses map[string]port.Harness) {
	factories := map[string]port.HarnessFactory{}
	for name, h := range harnesses {
		factories[name] = fake.HarnessFactory(h)
	}
	r.opts.Registry = registry.New(map[string]port.TrackerFactory{"fake": fake.TrackerFactory(tracker)}, factories)
}

// Covers AE4: actions on two harnesses run at once, each on its agent's.
func TestAE4ActionsOnTwoHarnessesRunAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready), issue("2", readyToReview))
		developer, reviewer := fake.NewHarness(), fake.NewHarness()
		r := options(t, twoHarnesses, tr, developer)
		withHarnesses(r, tr, map[string]port.Harness{"fake": developer, "fake2": reviewer})
		r.start()

		developing, reviewing := next(t, developer), next(t, reviewer)
		synctest.Wait()
		if got := developing.Run().Prompt; got != "Develop #1" {
			t.Errorf("the developer's harness runs %q, want #1's development", got)
		}
		if got := reviewing.Run().Prompt; got != "Review #2" {
			t.Errorf("the reviewer's harness runs %q, want #2's review", got)
		}
		if len(developer.Sessions()) != 1 || len(reviewer.Sessions()) != 1 {
			t.Errorf("sessions = %d on fake and %d on fake2, want one each",
				len(developer.Sessions()), len(reviewer.Sessions()))
		}
		developing.End(success)
		reviewing.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
	})
}

// unusedAgent is oneAction with the agent idle, which no action names, on
// the harness name.
func unusedAgent(name string) string {
	body := strings.Replace(oneAction, "rules:\n", "  idle:\n    harness:\n      name: "+name+"\nrules:\n", 1)
	return strings.Replace(body, "        prompt:", "        agent: developer\n        prompt:", 1)
}

// R13: an agent's harness name no adapter has stops crew, even when no
// action names the agent.
func TestAnUnusedAgentOnAnUnregisteredHarnessExitsTwo(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	r := options(t, unusedAgent("nosuch"), tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	want := `agents.idle.harness.name: no harness is named "nosuch"; the registered harness adapters are: fake`
	if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
}

// An agent no action names is never prepared, so a harness it cannot run
// does not stop crew (KTD4).
func TestAnUnusedAgentIsNeverPrepared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h, broken := fake.NewHarness(), fake.NewPreparingHarness()
		broken.Fail(errors.New("codex is not on PATH"))
		r := options(t, unusedAgent("broken"), tr, h)
		withHarnesses(r, tr, map[string]port.Harness{"fake": h, "broken": broken})
		r.start()

		next(t, h).End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if got := broken.Calls(); len(got) != 0 {
			t.Errorf("the unused agent's harness was prepared for %v, want never", got)
		}
	})
}

// An agent's bot needs no tracker.bot: its actions act as it, and crew's own
// writes go as the gh login (KTD7).
func TestAnAgentsBotActsWithoutATrackerBot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		h := fake.NewHarness()
		res := &resolver{bots: app.Bots{
			Identities: map[crew.BotName]port.Identity{"developer": devID}, Logins: []string{"crew-developer[bot]"},
		}}
		body := strings.Replace(oneAction, "      name: fake\n", "      name: fake\n    bot: developer\n", 1)
		r := options(t, body, tr, h)
		r.opts.Bots = res.resolve
		r.start()

		session := next(t, h)
		session.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if calls, _ := res.counts(); !slices.EqualFunc(calls, [][]crew.BotName{{"", "developer"}}, slices.Equal) {
			t.Errorf("resolver calls = %q, want no default and developer", calls)
		}
		calls := tr.ActAsCalls()
		if len(calls) != 1 || !sameIdentity(calls[0].Writer, port.Identity{}) {
			t.Errorf("ActAs calls = %+v, want one with crew's writes as you", calls)
		}
		if run := session.Run(); !sameIdentity(run.Identity, devID) {
			t.Errorf("session ran as %+v, want developer", run.Identity)
		}
	})
}
