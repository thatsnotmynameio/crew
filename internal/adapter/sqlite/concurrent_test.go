package sqlite_test

import (
	"sync"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// recordAll records n processes from offset into a store of its own on dir
// in each of stores goroutines at once, and returns the first error each
// goroutine met.
func recordAll(t *testing.T, dir string, stores, n int) []error {
	t.Helper()
	errs := make([]error, stores)
	var wg sync.WaitGroup
	for g := range stores {
		s := open(t, dir)
		wg.Go(func() {
			for i := range n {
				if err := s.Record(t.Context(), process(g*n+i)); err != nil {
					errs[g] = err
					return
				}
			}
		})
	}
	wg.Wait()
	return errs
}

// distinct returns how many distinct ids ps holds.
func distinct(ps []crew.Process) int {
	ids := map[crew.ProcessID]bool{}
	for _, p := range ps {
		ids[p.ID] = true
	}
	return len(ids)
}

func TestTwoStoresRecordIntoTheSameFileAtOnce(t *testing.T) {
	dir := t.TempDir()
	record(t, open(t, dir), process(1000))

	for g, err := range recordAll(t, dir, 2, 50) {
		if err != nil {
			t.Errorf("store %d: %v", g, err)
		}
	}

	if got := processes(t, dir); len(got) != 101 || distinct(got) != 101 {
		t.Errorf("processes = %d rows, %d distinct, want 101 of each", len(got), distinct(got))
	}
}

func TestStoresOpeningANewFileAtOnceAllRecord(t *testing.T) {
	dir := t.TempDir()

	for g, err := range recordAll(t, dir, 4, 1) {
		if err != nil {
			t.Errorf("store %d: %v", g, err)
		}
	}

	if got := processes(t, dir); len(got) != 4 || distinct(got) != 4 {
		t.Errorf("processes = %d rows, %d distinct, want 4 of each", len(got), distinct(got))
	}
	if v := userVersion(t, dir); v != migrations {
		t.Errorf("user_version = %d, want %d", v, migrations)
	}
}

// recordAtOnce records each of sts into a store of its own on dir, each in
// its own goroutine at once, and returns the error each met.
func recordAtOnce(t *testing.T, dir string, sts ...crew.Statistic) []error {
	t.Helper()
	errs := make([]error, len(sts))
	var wg sync.WaitGroup
	for g, st := range sts {
		s := open(t, dir)
		wg.Go(func() { errs[g] = s.Record(t.Context(), st) })
	}
	wg.Wait()
	return errs
}

func TestTwoStoresRecordingTheSameMoveOutsideCrewKeepOne(t *testing.T) {
	dir := t.TempDir()
	record(t, open(t, dir), sighting(1, "ready"))

	for g, err := range recordAtOnce(t, dir, outside(2, "ready", "done"), outside(3, "ready", "done")) {
		if err != nil {
			t.Errorf("store %d: %v", g, err)
		}
	}

	if got := moves(t, dir); len(got) != 1 || got[0].to != "done" {
		t.Errorf("moves = %+v, want one to done", got)
	}
}

func TestTwoStoresRecordingTheSameSightingAndRepositoryKeepOneOfEach(t *testing.T) {
	dir := t.TempDir()

	errs := recordAtOnce(t, dir,
		repository("acme/app"), repository("acme/app"), sighting(1, "ready"), sighting(2, "ready"))
	for g, err := range errs {
		if err != nil {
			t.Errorf("store %d: %v", g, err)
		}
	}

	if got := repositories(t, dir); len(got) != 1 {
		t.Errorf("repositories = %+v, want one", got)
	}
	if got := issues(t, dir); len(got) != 1 {
		t.Errorf("issues = %+v, want one", got)
	}
	if got := moves(t, dir); len(got) != 0 {
		t.Errorf("moves = %+v, want none", got)
	}
}

// Covers KTD5: two stores writing the same open and end at once leave one
// span with one end.
func TestTwoStoresRecordingTheSameSpanAtOnceKeepOne(t *testing.T) {
	dir := t.TempDir()

	errs := recordAtOnce(t, dir, opened(), opened(),
		ended(2, crew.OutcomeRouted, crew.PassedRoute), ended(2, crew.OutcomeRouted, crew.PassedRoute))
	for g, err := range errs {
		if err != nil {
			t.Errorf("store %d: %v", g, err)
		}
	}

	wantSpan(t, dir, endedRow("routed"), ruleRunOf("passed"))
}
