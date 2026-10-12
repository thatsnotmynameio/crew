package engine_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// notRecorded returns the StatisticNotRecorded events r's engine published,
// once Run has returned. It drains r's queue, as events does.
func (r *rig) notRecorded() []core.StatisticNotRecorded {
	var out []core.StatisticNotRecorded
	for _, e := range r.events() {
		if n, ok := e.(core.StatisticNotRecorded); ok {
			out = append(out, n)
		}
	}
	return out
}

// developOnce runs issue 1's development session in r to a success and
// checks that the issue moved to ready to review.
func developOnce(t *testing.T, r *rig, tr *fake.Tracker) {
	t.Helper()
	r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})
	synctest.Wait()
	if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
		t.Fatalf("issue 1 is in %v, want ready to review", got)
	}
}

// widgets is the repository a repositoryTracker names in these tests.
var widgets = crew.Repository{ID: "R_kgDOWidgets", Name: "acme/widgets"}

// recordingConfig returns a config over tr for rules that records its
// statistics in a fresh store, as crew 1.2.3 on GitHub.
func recordingConfig(t *testing.T, tr port.Tracker, rules ...crew.Rule) (engine.Config, *fake.Statistics) {
	t.Helper()
	cfg := config(t, tr, rules...)
	store := fake.NewStatistics()
	cfg.Statistics, cfg.Version, cfg.TrackerName = store, "1.2.3", "github"
	return cfg, store
}

// routedUse is the place of the config that calls the function pr-open in
// routed's passed route.
const routedUse crew.FunctionUse = "rules.implement.routes.passed[1]"

// routed is develop whose passed route runs the shell step notify and the
// function step check before it moves the issue to ready to review.
var routed = crew.Rule{
	Name: develop.Name, Labels: develop.Labels, Actions: develop.Actions,
	Routes: []crew.Route{
		{Name: crew.PassedRoute, Steps: []crew.Step{
			crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}},
			crew.FunctionStep{Name: "check", Function: prOpen(routedUse)},
			crew.MoveStep{To: readyToReview},
		}},
		develop.Routes[1],
	},
}

// routedConfig is recordingConfig for routed, with a harness whose
// sessions report their usage, a shell whose scripts exit 0 and a function
// that returns passed.
func routedConfig(t *testing.T, tr port.Tracker) (engine.Config, *fake.Statistics) {
	t.Helper()
	cfg, store := recordingConfig(t, tr, routed)
	cfg.Harnesses = harnesses(fake.NewUsageHarness())
	cfg.Shell = fake.NewShell()
	cfg.Functions = map[crew.FunctionUse]engine.Function{routedUse: {Function: fake.NewFunction(), Bind: bind}}
	return cfg, store
}

// sessionTime is how long issue 1's development session runs in
// developRouted.
const sessionTime = 90 * time.Second

// usage is what issue 1's development session reports in developRouted:
// its turns and its tokens by model.
var usage = crew.Usage{
	Turns:  crew.Some(7),
	Tokens: crew.Some(crew.Tokens{Input: 30, Output: 50, CacheRead: 700, CacheWrite: 90}),
	Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
	ByModel: []crew.ModelTokens{
		{Model: "claude-opus-5-5", Tokens: crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}},
		{Model: "claude-sonnet-5-5", Tokens: crew.Tokens{Input: 20, Output: 30, CacheRead: 400, CacheWrite: 50}},
	},
}

// developRouted runs issue 1's development session in r for sessionTime,
// reporting usage, to a success, and checks that its route moved the issue
// to ready to review.
func developRouted(t *testing.T, r *rig, tr *fake.Tracker) {
	t.Helper()
	s := r.session()
	time.Sleep(sessionTime)
	s.SetUsage(usage)
	s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
	synctest.Wait()
	if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
		t.Fatalf("issue 1 is in %v, want ready to review", got)
	}
}

// developedRun is what run, which process took at at, records as it
// develops issue id under routed at once: the open of its span, its take's
// move, the span of its session development, which ran for sessionTime
// and reported usage, the open of its route's span, chosen by
// development's verdict, the spans of its route's shell and function
// steps, its route's move, then the end of its route's span and of its own
// through passed.
func developedRun(id crew.IssueID, run crew.RuleRunID, process crew.ProcessID, at time.Time) []crew.Statistic {
	ended := at.Add(sessionTime)
	move := func(from, to crew.State, seen time.Time) crew.LabelMove {
		return crew.LabelMove{Tracker: "github", Issue: id, From: from, To: to, Seen: seen, Run: crew.Some(run)}
	}
	open := crew.RuleRunSpan{Tracker: "github", Issue: id, Run: run, Process: process, Rule: routed.Name, Start: at}
	spec, _ := routed.Actions[0].Kind.(crew.SessionSpec)
	development := crew.ActionSpan{
		Issue: id, Rule: routed.Name, Run: run, Action: "development", Process: process, Start: at, End: ended,
		Outcome: crew.WorkSucceeded, Verdict: crew.Passed,
		Work: crew.SessionWork{
			PromptHash: spec.Prompt.Hash(), Started: crew.Some(at), Turns: crew.Some(7), Tokens: usage.ByModel,
		},
	}
	route := crew.RouteSpan{
		Issue: id, Rule: routed.Name, Run: run, Process: process, Route: crew.PassedRoute,
		ChosenBy: crew.Some(crew.RouteChoice{Action: "development", Verdict: crew.Passed}), Start: ended,
	}
	step := func(index int, name crew.ActionName, work crew.Work) crew.StepSpan {
		return crew.StepSpan{
			Issue: id, Rule: routed.Name, Run: run, Index: index, Name: name, Process: process,
			Start: ended, End: ended, Outcome: crew.WorkSucceeded, Work: work,
		}
	}
	routeEnded := route
	routeEnded.End = crew.Some(crew.RouteEnd{At: ended, Outcome: crew.OutcomeRouted})
	end := open
	end.End = crew.Some(crew.RuleRunEnd{At: ended, Outcome: crew.OutcomeRouted, Route: crew.Some(crew.PassedRoute)})
	return []crew.Statistic{
		open, move(ready, inProgress, at), development, route,
		step(0, "notify", crew.ShellWork{ExitStatus: crew.Some(0)}),
		step(1, "check", crew.FunctionWork{Function: "pr-open", Verdict: crew.Some(crew.Passed)}),
		move(inProgress, readyToReview, ended), routeEnded, end,
	}
}

// Covers R5, R6, R7, R8, R9, R10, R11, R12, F1, F2, AE3: a crew that
// develops issue 1 records the process, the repository on the configured
// tracker, the sighting of issue 1 in ready, the open of the run's span,
// then the take's move, the span of its session with its turns and its
// tokens by model, the open of the route's span, the spans of the route's
// shell and function steps, the route's move, made by the run, the end of
// the route's span, then the end of the run's span with its outcome and
// route, in that order.
func TestRunRecordsTheProcessTheRepositoryTheIssueItsMovesAndItsRunsSpanInOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg, store := routedConfig(t, repositoryTracker{Tracker: tr, repository: widgets})
		r := start(t, cfg)

		developRouted(t, r, tr)
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		recorded := store.Recorded()
		if len(recorded) != 12 {
			t.Fatalf("recorded %#v, want twelve records", recorded)
		}
		p, ok := recorded[0].(crew.Process)
		if !ok || p.ID == "" {
			t.Fatalf("recorded %#v first, want a process with an id", recorded[0])
		}
		take, ok := recorded[4].(crew.LabelMove)
		run, byRun := take.Run.Get()
		if !ok || !byRun || run == "" {
			t.Fatalf("recorded %#v fifth, want a move made by a run", recorded[4])
		}
		started := final.Snapshot.Started
		id := crew.IssueID{Repository: widgets.ID, Key: "1"}
		want := append([]crew.Statistic{
			crew.Process{ID: p.ID, Version: "1.2.3", Folder: cfg.Root, Start: started},
			crew.RepositoryRecord{Tracker: "github", Repository: widgets},
			crew.IssueSighting{
				Tracker: "github", Issue: id, Ref: "#1", Created: crew.Some(issue(1).Created()), Seen: started,
				State: crew.Some(ready),
			},
		}, developedRun(id, run, p.ID, started)...)
		if !reflect.DeepEqual(recorded, want) {
			t.Errorf("recorded\n %#v\nwant\n %#v", recorded, want)
		}
		if n := r.notRecorded(); len(n) != 0 {
			t.Errorf("StatisticNotRecorded = %+v, want none", n)
		}
	})
}

// Covers AE2, R8: a person's move of a blocked issue 1 from ready to ready
// to review, made on the tracker between two ticks, is recorded at the
// second tick's listing, made outside crew.
func TestAMoveMadeOutsideCrewIsRecordedAtTheNextListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked := issue(1, ready).Data()
		blocked.Blocked = true
		tr := fake.NewTracker(crew.NewIssue(blocked))
		review := crew.Rule{
			Name: "review", Labels: crew.Labels{Ready: readyToReview, Running: "in review"},
			Actions: develop.Actions, Routes: routes(needsAttention),
		}
		cfg, store := recordingConfig(t, tr, develop, review)
		r := start(t, cfg)
		synctest.Wait()

		tr.SetStates("1", readyToReview)
		time.Sleep(poll)
		synctest.Wait()
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		recorded := store.Recorded()
		want := crew.LabelMove{
			Tracker: "github", Issue: crew.IssueID{Repository: "repo", Key: "1"}, From: ready, To: readyToReview,
			Seen: final.Snapshot.Started.Add(poll),
		}
		if len(recorded) != 4 || !reflect.DeepEqual(recorded[3], want) {
			t.Errorf("recorded %#v, want the process, the repository, the sighting, then %#v", recorded, want)
		}
	})
}

// wantLostSpan fails the test unless lost is the warning of a lost record
// of a run's span, its end when ended, else its open.
func wantLostSpan(t *testing.T, lost core.StatisticNotRecorded, ended bool) {
	t.Helper()
	sp, ok := lost.Statistic.(crew.RuleRunSpan)
	if _, hasEnd := sp.End.Get(); !ok || hasEnd != ended {
		t.Errorf("lost %#v, want the span of the run, ended %v", lost.Statistic, ended)
	}
}

// wantLost fails the test unless lost is the warning of a lost record of
// the same kind as want.
func wantLost(t *testing.T, lost core.StatisticNotRecorded, want crew.Statistic) {
	t.Helper()
	if reflect.TypeOf(lost.Statistic) != reflect.TypeOf(want) {
		t.Errorf("lost %#v, want a %T", lost.Statistic, want)
	}
}

func TestAE8AStoreThatCannotBeWrittenIsWarnedAboutAndTheRunGoesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg, store := routedConfig(t, tr)
		db := filepath.Join(cfg.Home, ".local", "share", "crew", "statistics.db")
		store.FailRecords(fmt.Errorf("open %s: database or disk is full", db))
		r := start(t, cfg)

		developRouted(t, r, tr)
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		// The process, the repository, issue 1's sighting, the open of its
		// run's span, its take's move, its session's span, the open of its
		// route's span, its two steps' spans, its route's move, the end of
		// its route's span and of its run's span.
		n := r.notRecorded()
		if len(n) != 12 {
			t.Fatalf("StatisticNotRecorded = %+v, want twelve", n)
		}
		if _, ok := n[0].Statistic.(crew.Process); !ok {
			t.Errorf("the first lost record is %T, want the process", n[0].Statistic)
		}
		wantLostSpan(t, n[3], false)
		wantLost(t, n[5], crew.ActionSpan{})
		wantLost(t, n[6], crew.RouteSpan{})
		wantLost(t, n[7], crew.StepSpan{})
		wantLost(t, n[8], crew.StepSpan{})
		wantLost(t, n[10], crew.RouteSpan{})
		wantLostSpan(t, n[11], true)
		want := "open ~/.local/share/crew/statistics.db: database or disk is full"
		for _, lost := range n {
			if lost.Reason != want {
				t.Errorf("reason of the lost %T = %q, want %q", lost.Statistic, lost.Reason, want)
			}
		}
		if got := store.Recorded(); len(got) != 0 {
			t.Errorf("recorded %#v, want nothing", got)
		}
	})
}

func TestABlockedStoreDelaysNoTickAndRunWaitsForItsRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		store := fake.NewStatistics()
		store.Block()
		cfg.Statistics = store
		r := start(t, cfg)

		developOnce(t, r, tr)
		// A later tick lists the issue again and runs it again.
		tr.SetStates("1", ready)
		time.Sleep(poll)
		developOnce(t, r, tr)

		r.engine.Stop()
		synctest.Wait()
		select {
		case err := <-r.done:
			t.Fatalf("Run returned %v while its record was still being written", err)
		default:
		}
		if got := store.Recorded(); len(got) != 0 {
			t.Fatalf("recorded %#v before the release, want nothing", got)
		}

		store.Release()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		// The process, the repository, issue 1's sighting, the spans and
		// moves of its first run, the move back to ready made outside crew,
		// and the spans and moves of its second run.
		if got := store.Recorded(); len(got) != 18 {
			t.Errorf("recorded %#v, want eighteen records", got)
		}
	})
}

func TestAE12WithoutAStoreTheRunGoesOnAndNothingIsRecorded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		developOnce(t, r, tr)
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if n := r.notRecorded(); len(n) != 0 {
			t.Errorf("StatisticNotRecorded = %+v, want none", n)
		}
	})
}

func TestTheWriterWritesItsRecordsInTheOrderTheyCame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := fake.NewStatistics()
		a := crew.Process{ID: "a", Version: "1"}
		b := crew.Process{ID: "b", Version: "1"}

		errs := engine.WriteStatistics(context.Background(), store, a, b)

		if got := store.Recorded(); !slices.Equal(got, []crew.Statistic{a, b}) {
			t.Errorf("recorded %#v, want a then b", got)
		}
		if !slices.Equal(errs, []error{nil, nil}) {
			t.Errorf("errors = %v, want two nils", errs)
		}
	})
}
