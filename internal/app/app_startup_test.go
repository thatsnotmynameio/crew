package app_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// Covers AE4.
func TestAnUnregisteredHarnessExitsTwoBeforeAnyListingNamingTheRegisteredOnes(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	r := options(t, strings.Replace(oneAction, "      name: fake\n", "      name: codex\n", 1), tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	stderr := r.stderr.String()
	for _, want := range []string{"agents.developer.harness.name", `"codex"`, "fake"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not name %s", stderr, want)
		}
	}
	if got, want := unstamped(t, r.stdout.String()), []string{"loading .crew/config.yaml"}; !slices.Equal(got, want) {
		t.Errorf("stdout = %q, want only the config's boot line", got)
	}
}

// Covers AE3.
func TestAConfigWithTrackerLabelsExitsTwoNamingTheKey(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	body := strings.Replace(oneAction, "  name: fake\n", "  name: fake\n  labels:\n    ready: ready\n", 1)
	r := options(t, body, tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	if stderr := r.stderr.String(); !strings.Contains(stderr, "tracker.labels (line 4): unknown key") {
		t.Errorf("stderr = %q, want it to name tracker.labels and its line", stderr)
	}
}

// Covers AE4 of #92.
func TestARuleTakingNeitherKindExitsTwoBeforeAnyListingNamingTheKey(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	body := strings.Replace(oneAction, "  implement:\n", "  implement:\n    takes: prs\n", 1)
	r := options(t, body, tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	want := `rules.implement.takes (line 10): "prs" must be issues or pull_requests`
	if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to name rules.implement.takes and its line", stderr)
	}
}

func TestAFailingEnvironmentCheckExitsTwoBeforeAnyListing(t *testing.T) {
	tr := fake.NewPreparingTracker(issue("1", ready))
	counter := &listCounter{Tracker: tr.Tracker}
	tr.Fail(errors.New("gh is not logged in"))
	tr.ReportStep("checking the gh login")
	r := options(t, oneAction, struct {
		*listCounter
		*fake.Preparation
	}{counter, tr.Preparation}, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := counter.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	stderr := r.stderr.String()
	if !strings.Contains(stderr, "gh is not logged in") || !strings.Contains(stderr, "tracker") {
		t.Errorf("stderr = %q, want the tracker's failed check", stderr)
	}
	// Covers AE3 of #113: the boot log ends with the step that failed.
	want := []string{"loading .crew/config.yaml", "checking the gh login"}
	if got := unstamped(t, r.stdout.String()); !slices.Equal(got, want) {
		t.Errorf("stdout = %q, want the boot log %q", got, want)
	}
}

// hangingTracker is a fake tracker whose environment check runs until its
// context ends, as a hung gh would. atCheck, when set, is the first thing
// the check calls; ignoreEnd makes the check succeed all the same.
type hangingTracker struct {
	*listCounter

	checking  chan struct{}
	atCheck   func()
	ignoreEnd bool
}

func newHangingTracker(issues ...crew.Issue) *hangingTracker {
	return &hangingTracker{listCounter: &listCounter{Tracker: fake.NewTracker(issues...)}, checking: make(chan struct{})}
}

// Prepare implements port.Preparer.
func (h *hangingTracker) Prepare(ctx context.Context, _ []crew.State) error {
	close(h.checking)
	if h.atCheck != nil {
		h.atCheck()
	}
	<-ctx.Done()
	if h.ignoreEnd {
		return nil
	}
	return fmt.Errorf("the check ended: %w", ctx.Err())
}

func TestASignalDuringTheEnvironmentChecksKillsEveryProcessAndExitsTwo(t *testing.T) {
	tr := newHangingTracker(issue("1", ready))
	r := options(t, oneAction, tr, fake.NewHarness())
	sleeper := child(t, r.opts.Group) // a check's process, still running
	r.start()
	<-tr.checking

	r.signals <- syscall.SIGINT

	if code := r.exitCode(t); code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, r.stderr)
	}
	killed(t, sleeper)
	if stderr := r.stderr.String(); !strings.Contains(stderr, "stopped during the environment checks") {
		t.Errorf("stderr = %q, want it to say crew stopped during the environment checks", stderr)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	if got, want := unstamped(t, r.stdout.String()), []string{"loading .crew/config.yaml"}; !slices.Equal(got, want) {
		t.Errorf("stdout = %q, want only the boot log before the stop", got)
	}
}

func TestHungEnvironmentChecksTimeOutAfterTenMinutesAndExitTwo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := newHangingTracker(issue("1", ready))
		r := options(t, oneAction, tr, fake.NewHarness())
		start := time.Now()
		r.start()

		code := <-r.code

		if code != 2 {
			t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, r.stderr)
		}
		if took := time.Since(start); took != 10*time.Minute {
			t.Errorf("the checks ended after %v, want 10m0s", took)
		}
		if stderr := r.stderr.String(); !strings.Contains(stderr, "timed out") {
			t.Errorf("stderr = %q, want it to say the environment checks timed out", stderr)
		}
		if n := tr.listed(); n != 0 {
			t.Errorf("the tracker listed %d times, want none", n)
		}
	})
}

func TestASignalAsTheEnvironmentChecksSucceedStillStopsCrew(t *testing.T) {
	tr := newHangingTracker(issue("1", ready))
	r := options(t, oneAction, tr, fake.NewHarness())
	tr.atCheck = func() { r.signals <- syscall.SIGTERM }
	tr.ignoreEnd = true // the checks finish as the signal arrives
	r.start()

	if code := r.exitCode(t); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	if !strings.Contains(r.stdout.String(), "crew: stopped") {
		t.Errorf("stdout lacks the stop; it is:\n%s", r.stdout)
	}
	// The journal is read after the signal, so its step does not print.
	if strings.Contains(r.stdout.String(), "reading the run journal") {
		t.Errorf("stdout shows a step that started after the stop:\n%s", r.stdout)
	}
}
