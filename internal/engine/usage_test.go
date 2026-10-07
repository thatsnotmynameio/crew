package engine_test

import (
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// ended returns the action runs' ends cfg's fake journal holds, in the
// order they were appended.
func ended(t *testing.T, cfg engine.Config) []crew.ActionEnded {
	t.Helper()
	j, ok := cfg.Journal.(*fake.Journal)
	if !ok {
		t.Fatalf("journal is %T, want the fake one", cfg.Journal)
	}
	var out []crew.ActionEnded
	for _, e := range j.Appended() {
		if end, ok := e.(crew.ActionEnded); ok {
			out = append(out, end)
		}
	}
	return out
}

func TestAE1AnEndedActionsLineHoldsItsUsageAndPullRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewFindingTracker(issue(31, ready))
		tr.ScriptLookup("crew/issue-31-development", fake.LookupScript{
			Found: crew.PullRequestFound{Ref: "#45", URL: "https://example.test/pull/45"},
		})
		cfg := config(t, tr, develop)
		cfg.Harnesses = harnesses(fake.NewUsageHarness())
		r := start(t, cfg)

		s := r.session()
		time.Sleep(90 * time.Second)
		s.SetUsage(crew.Usage{
			Cost: crew.Some(12.40), Turns: crew.Some(7),
			Tokens: crew.Some(crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}),
			Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
		})
		s.End(port.Verdict{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		end := ended(t, cfg)
		if len(end) != 1 {
			t.Fatalf("ends = %#v, want one", end)
		}
		want := crew.Usage{
			Cost: crew.Some(12.40), Turns: crew.Some(7),
			Tokens: crew.Some(crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}),
			Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
		}
		started, _ := end[0].SessionStarted.Get()
		if !end[0].End.Outcome().Succeeded || !reflect.DeepEqual(end[0].Usage, want) ||
			end[0].PullRequest != (crew.PullRequestFound{Ref: "#45", URL: "https://example.test/pull/45"}) ||
			end[0].At.Sub(started) != 90*time.Second {
			t.Errorf("end = %#v, want a success after 90s with its usage and #45", end[0])
		}
		if got := tr.Lookups(); len(got) != 1 || got[0].Branch != "crew/issue-31-development" || got[0].Since.IsZero() {
			t.Errorf("lookups = %#v, want one from crew/issue-31-development since its worktree was made", got)
		}
		if got := states(t, tr, "31"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Errorf("issue 31 is in %v, want ready to review", got)
		}
	})
}

func TestAE6AHarnessAndTrackerThatCannotTellLeaveTheValuesOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		r.session().End(port.Verdict{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		end := ended(t, cfg)
		if len(end) != 1 || end[0].PullRequest != nil || !reflect.DeepEqual(end[0].Usage, crew.Usage{}) {
			t.Fatalf("ends = %#v, want one with no usage, whose pull request was not looked up", end)
		}
	})
}

func TestALookupThatHangsGivesUpAfterFifteenSecondsAndChangesNoOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewFindingTracker(issue(1, ready))
		tr.ScriptLookup("crew/issue-1-development", fake.LookupScript{Block: true})
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		r.session().End(port.Verdict{Succeeded: true, Reason: "done"})
		synctest.Wait()
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{inProgress}) {
			t.Fatalf("issue 1 is in %v while its lookup runs, want in progress", got)
		}
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Fatalf("issue 1 is in %v after the lookup gave up, want ready to review", got)
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if end := ended(t, cfg); len(end) != 1 || end[0].PullRequest != (crew.PullRequestNotLookedUp{}) {
			t.Fatalf("ends = %#v, want one whose pull request was not looked up", end)
		}
	})
}

func TestAStopDuringALookupWaitsForItAndWritesTheLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewFindingTracker(issue(1, ready))
		tr.ScriptLookup("crew/issue-1-development", fake.LookupScript{Block: true})
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		r.session().End(port.Verdict{Reason: "tests fail"})
		synctest.Wait()
		stopped := time.Now()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if waited := time.Since(stopped); waited > 15*time.Second {
			t.Errorf("the stop waited %v, want at most 15s", waited)
		}
		end := ended(t, cfg)
		if len(end) != 1 || end[0].End.Outcome().Reason.String() != "tests fail" {
			t.Fatalf("ends = %#v, want one with the session's own reason", end)
		}
	})
}

func TestAE8UsageInStatusPutsTheSpendAndPullRequestOnTheEndedStatus(t *testing.T) {
	pr := crew.PullRequestFound{Ref: "#45", URL: "https://example.test/pull/45"}
	used := crew.Usage{Cost: crew.Some(1.5)}
	for _, on := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			tr := fake.NewFindingTracker(issue(1, ready))
			tr.ScriptLookup("crew/issue-1-development", fake.LookupScript{Found: pr})
			cfg := config(t, tr, develop)
			cfg.Harnesses = harnesses(fake.NewUsageHarness())
			cfg.UsageInStatus = on
			r := start(t, cfg)

			s := r.session()
			s.SetUsage(used)
			s.End(port.Verdict{Succeeded: true, Reason: "done"})
			synctest.Wait()
			r.engine.Stop()
			if _, err := r.wait(); err != nil {
				t.Fatalf("Run: %v", err)
			}

			got := lastStatus(t, tr.ReportingTracker).Actions()[0]
			want := crew.ActionStatus{Name: "development", State: crew.ActionSucceeded{}}
			if on {
				want.State = crew.ActionSucceeded{Usage: crew.Some(crew.ShownUsage{Spend: used.Spend(), PullRequest: pr})}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("usage_in_status %v: action status = %#v, want %#v", on, got, want)
			}
		})
	}
}
