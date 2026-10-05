package engine_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// lastStatus returns the last status the tracker holds for issue 1, failing
// without one.
func lastStatus(t *testing.T, tr fake.ReportingTracker) crew.Status {
	t.Helper()
	got := tr.Statuses("1")
	if len(got) == 0 {
		t.Fatal("no status written for issue 1")
	}
	return got[len(got)-1]
}

func TestAE2AE6RunningStatusCarriesTheSessionsWordsWithLocalPathsShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Harnesses = harnesses(fake.NewNarratingHarness())
		r := start(t, cfg)
		s := r.sessions(1)["issue-1-development"]
		begun := time.Now()

		s.Say(fmt.Sprintf("Edited %s/internal/core/update.go for @someone", cfg.Root))
		time.Sleep(poll)
		synctest.Wait()

		got := lastStatus(t, tr)
		if got.Kind != crew.StatusRunning || len(got.Actions) != 1 {
			t.Fatalf("status = %#v, want development running", got)
		}
		a := got.Actions[0]
		if want := "Edited ./internal/core/update.go for @someone"; a.Said != want {
			t.Errorf("Said = %q, want %q", a.Said, want)
		}
		if !a.Started.Equal(begun) || !got.Updated.Equal(begun.Add(poll)) {
			t.Errorf("started %v and updated %v, want %v and a poll later", a.Started, got.Updated, begun)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestR9SessionThatCannotNarrateGivesAStatusWithoutWords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.sessions(1)
		time.Sleep(poll)
		synctest.Wait()

		got := lastStatus(t, tr)
		if a := got.Actions[0]; a.State != crew.ActionRunning || a.Started.IsZero() || a.Said != "" {
			t.Errorf("action = %#v, want running with a start time and no words", a)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestAE3AE4StopLeavesTheMoveOnTheStatusAndTheFailureReportApart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		r.sessions(1)

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := crew.Status{
			IssueKey: "1", IssueRef: "#1", Rule: "implement", Kind: crew.StatusEnded,
			Actions: []crew.ActionStatus{{
				Name: "development", State: crew.ActionFailed, Cause: crew.CauseStopped,
				Log: ".crew/logs/issue-1-development.log",
			}},
			To: needsAttention, Move: crew.MoveDone,
		}
		got := lastStatus(t, tr)
		if got.Run == "" {
			t.Errorf("last status has no run: %#v", got)
		}
		got.Updated, got.Run = time.Time{}, ""
		if !reflect.DeepEqual(got, want) {
			t.Errorf("last status = %#v, want %#v", got, want)
		}
		if len(tr.Reports()) != 1 {
			t.Errorf("reports = %+v, want the failure report as well", tr.Reports())
		}
	})
}

// statusCounter is a reporting fake tracker that counts its status writes.
type statusCounter struct {
	fake.ReportingTracker

	mu     sync.Mutex
	writes int
}

func (c *statusCounter) ReportStatus(ctx context.Context, s crew.Status) error {
	c.mu.Lock()
	c.writes++
	c.mu.Unlock()
	return c.ReportingTracker.ReportStatus(ctx, s)
}

func (c *statusCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

func TestARefusedEndedStatusIsNotRetriedAndStopDoesNotWaitForIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &statusCounter{ReportingTracker: fake.NewReportingTracker(issue(1, ready))}
		r := start(t, config(t, tr, develop))
		s := r.sessions(1)["issue-1-development"]
		synctest.Wait()

		locked := fmt.Errorf("issue is locked: %w", port.ErrRefused)
		tr.FailStatuses("1", locked, locked)
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		writes := tr.count()

		time.Sleep(poll)
		synctest.Wait()
		if got := tr.count(); got != writes {
			t.Errorf("%d status writes after the next tick, want still %d", got, writes)
		}
		for _, st := range tr.Statuses("1") {
			if st.Kind == crew.StatusEnded {
				t.Errorf("an ended status was written: %#v", st)
			}
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
	})
}

func TestALongSaidTextIsCutOnlyAfterItsLocalPathsAreShortened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Harnesses = harnesses(fake.NewNarratingHarness())
		r := start(t, cfg)
		s := r.sessions(1)["issue-1-development"]

		// The repository's path starts before the last 200 characters, so a
		// cut before shortening would leave the end of it in the text.
		tail := "/internal/core/update.go " + strings.Repeat("x", 190)
		s.Say("Edited " + cfg.Root + tail)
		time.Sleep(poll)
		synctest.Wait()

		got := lastStatus(t, tr).Actions[0].Said
		if want := "…" + string([]rune("." + tail)[len([]rune("."+tail))-199:]); got != want {
			t.Errorf("Said = %q, want %q", got, want)
		}
		if strings.Contains(got, filepath.Base(cfg.Root)) || strings.Contains(got, "home") {
			t.Errorf("Said = %q names part of a local path", got)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}
