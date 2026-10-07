package engine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// listCounter is a preparing fake tracker that counts its listings.
type listCounter struct {
	fake.PreparingTracker

	mu    sync.Mutex
	lists int
}

func (l *listCounter) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	l.mu.Lock()
	l.lists++
	l.mu.Unlock()
	return l.PreparingTracker.List(ctx, states)
}

func TestAFailingPreparerStopsTheEngineBeforeAnyListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker(issue(1, ready))}
		notLoggedIn := errors.New("gh is not logged in")
		tr.Fail(notLoggedIn)
		tr.ReportStep("checking the gh login")
		harness := fake.NewPreparingHarness()
		harness.ReportStep("checking claude")
		cfg := config(t, tr, develop)
		cfg.Harnesses = harnesses(harness)
		var steps []string
		ctx := port.WithSteps(context.Background(), func(step string) { steps = append(steps, step) })

		err := engine.New(cfg).Run(ctx)

		if !errors.Is(err, notLoggedIn) {
			t.Fatalf("Run = %v, want the preparer's error", err)
		}
		if !strings.Contains(err.Error(), "tracker") {
			t.Errorf("Run = %q, want it to name the tracker", err)
		}
		if tr.lists != 0 {
			t.Errorf("tracker listed %d times, want none", tr.lists)
		}
		want := [][]crew.State{{ready, inProgress, readyToReview, needsAttention}}
		if got := tr.Calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("tracker prepared for %v, want the rules' states %v", got, want)
		}
		if got := harness.Calls(); len(got) != 0 {
			t.Errorf("harness prepared for %v, want it never prepared after the tracker failed", got)
		}
		if want := []string{"checking the gh login"}; !reflect.DeepEqual(steps, want) {
			t.Errorf("steps = %q, want %q: the failed step last, and no journal step", steps, want)
		}
	})
}

func TestPrepareReportsTheJournalStepAfterEveryPortPrepared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker()}
		tr.ReportStep("checking the gh login")
		harness := fake.NewPreparingHarness()
		harness.ReportStep("checking claude")
		cfg := config(t, tr, develop)
		cfg.Harnesses = harnesses(harness)
		e := engine.New(cfg)
		var steps []string
		ctx := port.WithSteps(context.Background(), func(step string) { steps = append(steps, step) })

		if err := e.Prepare(ctx); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		want := []string{"checking the gh login", "checking claude", "reading the run journal"}
		if !reflect.DeepEqual(steps, want) {
			t.Errorf("steps = %q, want %q", steps, want)
		}

		e.Stop()
		if err := e.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if tr.lists != 1 {
			t.Errorf("Run listed %d times, want the first poll's listing from the built core", tr.lists)
		}
	})
}

func TestPrepareGetsOnlyTheStatesTheRulesName(t *testing.T) {
	blocked := develop
	blocked.Routes = routes("blocked")
	tr := fake.NewPreparingTracker()

	if err := engine.New(config(t, tr, blocked)).Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	want := [][]crew.State{{ready, inProgress, readyToReview, "blocked"}}
	if got := tr.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("tracker prepared for %v, want %v and no needs attention", got, want)
	}
}

func TestRunAfterPrepareDoesNotPrepareAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker()}
		e := engine.New(config(t, tr, develop))

		if err := e.Prepare(context.Background()); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if got := len(tr.Calls()); got != 1 {
			t.Fatalf("Prepare ran the tracker's Preparer %d times, want 1", got)
		}
		if tr.lists != 0 {
			t.Fatalf("Prepare listed %d times, want none", tr.lists)
		}

		e.Stop()
		if err := e.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := len(tr.Calls()); got != 1 {
			t.Errorf("the tracker's Preparer ran %d times in all, want once", got)
		}
		if tr.lists != 1 {
			t.Errorf("Run listed %d times, want the first poll's listing", tr.lists)
		}
	})
}

// Each agent's harness is prepared once, in config order, and a failing one
// is named after its agent.
func TestPrepareRunsEveryAgentsHarnessAndNamesTheOneThatFails(t *testing.T) {
	developer, reviewer := fake.NewPreparingHarness(), fake.NewPreparingHarness()
	notInstalled := errors.New("codex is not on PATH")
	reviewer.Fail(notInstalled)
	cfg := config(t, fake.NewTracker(), develop)
	cfg.Harnesses = []engine.AgentHarness{{Agent: "developer", Harness: developer}, {Agent: "reviewer", Harness: reviewer}}

	err := engine.New(cfg).Prepare(context.Background())

	if !errors.Is(err, notInstalled) || !strings.Contains(err.Error(), "prepare the harness of agent reviewer") {
		t.Errorf("Prepare = %v, want the reviewer's harness error, naming the agent", err)
	}
	want := [][]crew.State{{ready, inProgress, readyToReview, needsAttention}}
	if got := developer.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("developer's harness prepared for %v, want once for %v", got, want)
	}
	if got := reviewer.Calls(); len(got) != 1 {
		t.Errorf("reviewer's harness prepared %d times, want once", len(got))
	}
}

// repositoryTracker is a fake tracker that names its repository.
type repositoryTracker struct {
	*fake.Tracker

	repository crew.Repository
}

func (r repositoryTracker) Repository() crew.Repository { return r.repository }

// The engine works on the repository its tracker names in Prepare, and on
// one named after the root directory when the tracker names none (KTD6).
func TestPrepareReadsTheRepository(t *testing.T) {
	widgets := crew.Repository{ID: "R_kgDOWidgets", Name: "acme/widgets"}
	for name, tc := range map[string]struct {
		tracker port.Tracker
		want    crew.Repository
	}{
		"the tracker's":      {repositoryTracker{Tracker: fake.NewTracker(), repository: widgets}, widgets},
		"the root directory": {fake.NewTracker(), crew.Repository{ID: "repo", Name: "repo"}},
	} {
		t.Run(name, func(t *testing.T) {
			e := engine.New(config(t, tc.tracker, develop))
			var steps []string
			ctx := port.WithSteps(context.Background(), func(step string) { steps = append(steps, step) })
			if err := e.Prepare(ctx); err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := e.Repository(); got != tc.want {
				t.Errorf("Repository = %+v, want %+v", got, tc.want)
			}
			if want := []string{"reading the run journal"}; !reflect.DeepEqual(steps, want) {
				t.Errorf("steps = %q, want %q and no step of the repository's", steps, want)
			}
		})
	}
}

// unreadableJournal is a run journal whose events cannot be loaded.
type unreadableJournal struct{ *fake.Journal }

func (unreadableJournal) Load(crew.RepositoryID) ([]crew.RunEvent, error) {
	return nil, errors.New("read the run journal .crew/logs/runs.jsonl: is a directory")
}

func TestAJournalThatCannotBeReadFailsPrepareNamingIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &listCounter{PreparingTracker: fake.NewPreparingTracker()}
		cfg := config(t, tr, develop)
		cfg.Journal = unreadableJournal{fake.NewJournal()}
		var steps []string
		ctx := port.WithSteps(context.Background(), func(step string) { steps = append(steps, step) })

		err := engine.New(cfg).Run(ctx)

		if err == nil || !strings.Contains(err.Error(), ".crew/logs/runs.jsonl") {
			t.Fatalf("Run = %v, want an error naming .crew/logs/runs.jsonl", err)
		}
		if want := []string{"reading the run journal"}; !reflect.DeepEqual(steps, want) {
			t.Errorf("steps = %q, want the journal's step before its error", steps)
		}
		if tr.lists != 0 {
			t.Errorf("tracker listed %d times, want none", tr.lists)
		}
	})
}
