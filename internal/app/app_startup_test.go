package app_test

import (
	"context"
	"errors"
	"fmt"
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
	r := options(t, strings.Replace(oneAction, "harness: fake", "harness: codex", 1), tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	stderr := r.stderr.String()
	for _, want := range []string{"harness", `"codex"`, "fake"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not name %s", stderr, want)
		}
	}
	if out := r.stdout.String(); out != "" {
		t.Errorf("stdout = %q, want nothing", out)
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
	if stderr := r.stderr.String(); !strings.Contains(stderr, "tracker.labels (line 6): unknown key") {
		t.Errorf("stderr = %q, want it to name tracker.labels and its line", stderr)
	}
}

// Covers AE4 of #92.
func TestAStageTakingNeitherKindExitsTwoBeforeAnyListingNamingTheKey(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	body := strings.Replace(oneAction, "    on_failure: needs attention\n",
		"    on_failure: needs attention\n    takes: prs\n", 1)
	r := options(t, body, tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	want := `workflow[0].takes (line 12): "prs" must be issues or pull_requests`
	if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to name workflow[0].takes and its line", stderr)
	}
}

func TestAFailingEnvironmentCheckExitsTwoBeforeAnyListing(t *testing.T) {
	tr := fake.NewPreparingTracker(issue("1", ready))
	counter := &listCounter{Tracker: tr.Tracker}
	tr.Fail(errors.New("gh is not logged in"))
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
	if out := r.stdout.String(); out != "" {
		t.Errorf("stdout = %q, want nothing", out)
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
	if out := r.stdout.String(); out != "" {
		t.Errorf("stdout = %q, want nothing", out)
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
}
