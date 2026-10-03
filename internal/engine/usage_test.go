package engine_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// journal returns the run journal of root, one decoded object per line.
func journal(t *testing.T, root string) []map[string]any {
	t.Helper()
	data, err := fs.ReadFile(os.DirFS(root), ".crew/logs/runs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for line := range strings.Lines(string(data)) {
		var l map[string]any
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("journal line %q: %v", line, err)
		}
		out = append(out, l)
	}
	return out
}

// ended returns the journal's ended lines.
func ended(lines []map[string]any) []map[string]any {
	return slices.DeleteFunc(slices.Clone(lines), func(l map[string]any) bool { return l["event"] != "ended" })
}

// lacks fails when line has any of keys.
func lacks(t *testing.T, line map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := line[k]; ok {
			t.Errorf("line has %s = %v, want none: %v", k, v, line)
		}
	}
}

var usageKeys = []string{
	"cost_usd", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "turns", "models",
}

func TestAE1AnEndedActionsLineHoldsItsUsageAndPullRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewFindingTracker(issue(31, ready))
		tr.ScriptLookup("crew/issue-31-development", fake.LookupScript{
			Found: crew.PullRequest{Lookup: crew.PullRequestFound, Ref: "#45", URL: "https://example.test/pull/45"},
		})
		cfg := config(t, tr, develop)
		cfg.Harness = fake.NewUsageHarness()
		r := start(t, cfg)

		s := r.session()
		time.Sleep(90 * time.Second)
		s.SetUsage(crew.Usage{
			Cost: 12.40, HasCost: true, HasTokens: true, HasTurns: true, Turns: 7,
			Tokens: crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40},
			Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
		})
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		lines := journal(t, cfg.Root)
		end := ended(lines)
		if len(end) != 1 {
			t.Fatalf("ended lines = %v, want one", end)
		}
		want := map[string]any{
			"cost_usd": 12.4, "input_tokens": 10.0, "output_tokens": 20.0, "cache_read_tokens": 300.0,
			"cache_write_tokens": 40.0, "turns": 7.0, "models": []any{"claude-opus-5-5", "claude-sonnet-5-5"},
			"pull_request": "#45", "pull_request_url": "https://example.test/pull/45", "pull_request_lookup": "found",
			"duration_ms": 90_000.0, "succeeded": true,
		}
		for k, v := range want {
			if !reflect.DeepEqual(end[0][k], v) {
				t.Errorf("%s = %#v, want %#v", k, end[0][k], v)
			}
		}
		if run := lines[0]["run"]; run == nil || run == "" || end[0]["run"] != run {
			t.Errorf("run = %v then %v, want one run id on every line", run, end[0]["run"])
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

		r.session().End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		end := ended(journal(t, cfg.Root))
		if len(end) != 1 || end[0]["pull_request_lookup"] != "not looked up" {
			t.Fatalf("ended lines = %v, want one whose pull request was not looked up", end)
		}
		lacks(t, end[0], append(usageKeys, "pull_request", "pull_request_url")...)
	})
}

func TestALookupThatHangsGivesUpAfterFifteenSecondsAndChangesNoOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewFindingTracker(issue(1, ready))
		tr.ScriptLookup("crew/issue-1-development", fake.LookupScript{Block: true})
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		r.session().End(crew.Outcome{Succeeded: true, Reason: "done"})
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
		if end := ended(journal(t, cfg.Root)); len(end) != 1 || end[0]["pull_request_lookup"] != "not looked up" {
			t.Fatalf("ended lines = %v, want one whose pull request was not looked up", end)
		}
	})
}

func TestAStopDuringALookupWaitsForItAndWritesTheLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewFindingTracker(issue(1, ready))
		tr.ScriptLookup("crew/issue-1-development", fake.LookupScript{Block: true})
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		r.session().End(crew.Outcome{Reason: "tests fail"})
		synctest.Wait()
		stopped := time.Now()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if waited := time.Since(stopped); waited > 15*time.Second {
			t.Errorf("the stop waited %v, want at most 15s", waited)
		}
		end := ended(journal(t, cfg.Root))
		if len(end) != 1 || end[0]["reason"] != "tests fail" {
			t.Fatalf("ended lines = %v, want one with the session's own reason", end)
		}
	})
}

func TestAE7NewLinesAreAppendedAndEarlierOnesKeptAsTheyWere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const earlier = `{"v":1,"event":"ended","time":"2026-01-01T10:00:00Z","issue":"9","ref":"#9","stage":"implement",` +
			`"action":"development","workspace":"issue-9-development","branch":"crew/issue-9-development",` +
			`"log":".crew/logs/issue-9-development.log","succeeded":true,"reason":"done","cost_usd":6.89}`
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		writeJournal(t, cfg.Root, earlier)
		r := start(t, cfg)

		r.session().End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		data, err := fs.ReadFile(os.DirFS(cfg.Root), ".crew/logs/runs.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(data), earlier+"\n") {
			t.Fatalf("journal = %q, want the earlier line first and unchanged", data)
		}
		lines := journal(t, cfg.Root)
		if len(lines) != 3 || lines[1]["run"] == nil || lines[2]["run"] != lines[1]["run"] {
			t.Fatalf("journal = %v, want two new lines of one run after the earlier one", lines)
		}
	})
}

func TestAE8UsageInStatusPutsTheSpendAndPullRequestOnTheEndedStatus(t *testing.T) {
	pr := crew.PullRequest{Lookup: crew.PullRequestFound, Ref: "#45", URL: "https://example.test/pull/45"}
	used := crew.Usage{Cost: 1.5, HasCost: true}
	for _, on := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			tr := fake.NewFindingTracker(issue(1, ready))
			tr.ScriptLookup("crew/issue-1-development", fake.LookupScript{Found: pr})
			cfg := config(t, tr, develop)
			cfg.Harness = fake.NewUsageHarness()
			cfg.UsageInStatus = on
			r := start(t, cfg)

			s := r.session()
			s.SetUsage(used)
			s.End(crew.Outcome{Succeeded: true, Reason: "done"})
			synctest.Wait()
			r.engine.Stop()
			if _, err := r.wait(); err != nil {
				t.Fatalf("Run: %v", err)
			}

			got := lastStatus(t, tr.ReportingTracker, "1").Actions[0]
			want := crew.ActionStatus{Name: "development", State: crew.ActionSucceeded}
			if on {
				want.Spend, want.PullRequest = used.Spend(), pr
			}
			if got != want {
				t.Errorf("usage_in_status %v: action status = %#v, want %#v", on, got, want)
			}
		})
	}
}
