package app_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/adapter/sqlite"
	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// withStores registers stores in r beside its tracker and harness, as
// fake.
func withStores(r *crewRun, tracker port.Tracker, harness port.Harness, stores map[string]port.StatisticsFactory) {
	r.opts.Registry = registry.New(
		map[string]port.TrackerFactory{"fake": fake.TrackerFactory(tracker)},
		map[string]port.HarnessFactory{"fake": fake.HarnessFactory(harness)},
		nil,
		stores,
	)
}

// runToReview runs #1 through oneAction's one session, stops crew, checks
// it exited 0 with #1 in ready to review, and returns its stdout.
func runToReview(t *testing.T, r *crewRun, tr *fake.Tracker, h *fake.Harness) string {
	t.Helper()
	r.start()
	next(t, h).End(success)
	synctest.Wait()
	r.signals <- syscall.SIGTERM
	if code := <-r.code; code != app.ExitClean {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
		t.Errorf("#1 is in %v, want ready to review", got)
	}
	return r.stdout.String()
}

// R5, R6: the store the config names records the crew process once, with
// crew's version and the repository's root, then the repository on the
// tracker the config names, and is closed once the engine has stopped.
func TestACrewRunRecordsItselfInTheStoreTheConfigNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, h := fake.NewTracker(issue("1", ready)), fake.NewHarness()
		r := options(t, "statistics: {store: fake}\n"+oneAction, tr, h)
		stats := fake.NewStatistics()
		withStores(r, tr, h, map[string]port.StatisticsFactory{"fake": fake.StatisticsFactory(stats)})
		r.opts.Version, r.opts.DataDir = "v1.2.3", t.TempDir()

		runToReview(t, r, tr, h)

		got := stats.Recorded()
		if len(got) < 2 {
			t.Fatalf("records = %+v, want a process, then the repository", got)
		}
		p, ok := got[0].(crew.Process)
		if !ok || p.Version != "v1.2.3" || p.Folder != r.opts.Root {
			t.Errorf("record = %+v, want a process of v1.2.3 in %s", got[0], r.opts.Root)
		}
		if repo, ok := got[1].(crew.RepositoryRecord); !ok || repo.Tracker != "fake" {
			t.Errorf("record = %+v, want the repository on the tracker fake", got[1])
		}
		if n := stats.Closes(); n != 1 {
			t.Errorf("the store was closed %d times, want once", n)
		}
	})
}

// Covers AE12: with recording off, crew builds no store, so the store's
// factory is never called, and runs its rules as before.
func TestAE12RecordingOffBuildsNoStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, h := fake.NewTracker(issue("1", ready)), fake.NewHarness()
		r := options(t, "statistics: {store: off}\n"+oneAction, tr, h)
		built := 0
		withStores(r, tr, h, map[string]port.StatisticsFactory{
			"sqlite": func(port.Decode, string) (port.Statistics, error) {
				built++
				return fake.NewStatistics(), nil
			},
		})

		out := runToReview(t, r, tr, h)

		if built != 0 {
			t.Errorf("the store was built %d times, want never", built)
		}
		if strings.Contains(out, "statistics") {
			t.Errorf("stdout mentions the statistics:\n%s", out)
		}
	})
}

// Covers AE12 with the real store: with recording off, nothing appears in
// the data folder.
func TestAE12RecordingOffCreatesNoFile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, h := fake.NewTracker(issue("1", ready)), fake.NewHarness()
		r := options(t, "statistics: {store: off}\n"+oneAction, tr, h)
		withStores(r, tr, h, map[string]port.StatisticsFactory{"sqlite": sqlite.Factory()})
		r.opts.DataDir = filepath.Join(t.TempDir(), "crew")

		runToReview(t, r, tr, h)

		if _, err := os.Stat(r.opts.DataDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stat %s = %v, want no data folder", r.opts.DataDir, err)
		}
	})
}

// KTD2: a store no adapter has is a config error, before anything polls.
func TestAnUnknownStoreIsAConfigError(t *testing.T) {
	r := options(t, "statistics: {store: postgres}\n"+oneAction, fake.NewTracker(), fake.NewHarness())
	r.start()

	select {
	case code := <-r.code:
		if code != app.ExitConfig {
			t.Fatalf("exit code = %d, want %d", code, app.ExitConfig)
		}
	case <-time.After(5 * time.Second):
		r.signals <- syscall.SIGTERM
		t.Fatalf("crew ran, want a config error; exit code %d", <-r.code)
	}
	want := `statistics.store: no store is named "postgres"; the registered stores are: sqlite`
	if !strings.Contains(r.stderr.String(), want) {
		t.Errorf("stderr lacks %q; it is:\n%s", want, r.stderr)
	}
}

// Covers AE8 with the real store: without a data folder, crew runs every
// rule and prints one warning that the process was not recorded.
func TestWithoutADataFolderCrewRunsAndWarnsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, h := fake.NewTracker(issue("1", ready)), fake.NewHarness()
		r := options(t, oneAction, tr, h)
		withStores(r, tr, h, map[string]port.StatisticsFactory{"sqlite": sqlite.Factory()})
		r.opts.Plain = true

		out := runToReview(t, r, tr, h)

		const want = "crew: warning: could not record this crew process in the statistics store: " +
			"no data folder: set XDG_DATA_HOME or HOME\n"
		if n := strings.Count(out, want); n != 1 {
			t.Errorf("stdout holds %q %d times, want once; it is:\n%s", want, n, out)
		}
	})
}

// closeFailing is a store whose Close fails with an error shaped like the
// sqlite store's.
type closeFailing struct{ *fake.Statistics }

func (closeFailing) Close() error { return errors.New("close the statistics store: disk I/O error") }

// A store that fails to close is a warning: the run still exits 0.
// A failed engine never ended its writer, which may be inside a record, so
// crew leaves the store open, as a forced exit does. A real one-second
// tick, not synctest: the writer the panic left behind outlives the test.
func TestAFailingEngineLeavesTheStoreOpen(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h := panickingHarness{fake.NewHarness()}
	r := options(t, "poll_interval_seconds: 1\nstatistics: {store: fake}\n"+oneAction, tr, h)
	stats := fake.NewStatistics()
	withStores(r, tr, h, map[string]port.StatisticsFactory{"fake": fake.StatisticsFactory(stats)})
	r.opts.DataDir = t.TempDir()
	r.start()
	session := next(t, h.Harness)

	code := r.exitCode(t)
	session.End(port.SessionEnd{Reason: "released by the test"})
	sessionKept(t, r.opts.Root, "issue-1-implement")

	if code != app.ExitFailure {
		t.Errorf("exit code = %d, want 1", code)
	}
	if n := stats.Closes(); n != 0 {
		t.Errorf("the store was closed %d times, want none", n)
	}
}

func TestAStoreThatFailsToCloseIsAWarning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, h := fake.NewTracker(issue("1", ready)), fake.NewHarness()
		r := options(t, oneAction, tr, h)
		withStores(r, tr, h, map[string]port.StatisticsFactory{
			"sqlite": fake.StatisticsFactory(closeFailing{fake.NewStatistics()}),
		})

		runToReview(t, r, tr, h)

		want := "crew: warning: close the statistics store: disk I/O error"
		if !strings.Contains(r.stderr.String(), want) {
			t.Errorf("stderr lacks %q; it is:\n%s", want, r.stderr)
		}
	})
}
