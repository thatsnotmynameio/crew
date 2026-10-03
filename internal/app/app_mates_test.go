package app_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// matedAction is oneAction with the default mate ops, and the development
// action acting as developer and running a check.
const matedAction = `
config:
  harness: fake
  mate: ops
tracker:
  name: fake
workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement development for issue {{.Issue.Ref}}"
        mate: developer
        check: gh pr list
`

// The identities the fake resolver hands out: ops for crew's own writes
// and developer for the action's session and check.
var (
	opsWriter = port.Identity{Mate: "ops", Login: "crew-ops[bot]", Env: []string{"GH_CONFIG_DIR=/run/ops/crew"}}
	opsID     = port.Identity{Mate: "ops", Login: "crew-ops[bot]", Env: []string{"GH_CONFIG_DIR=/run/ops/sessions"}}
	devID     = port.Identity{Mate: "developer", Login: "crew-developer[bot]",
		Env: []string{"GH_CONFIG_DIR=/run/developer/sessions"}, Unset: []string{"GH_TOKEN"}}
)

// resolver is a scripted Options.Mates: it returns mates or err, records
// each call and counts the calls to the Close it returns.
type resolver struct {
	mates app.Mates
	err   error
	// entered, when not nil, is closed as the resolver starts; it then
	// waits for its context to end.
	entered chan struct{}

	mu     sync.Mutex
	calls  [][]string // the default, then the names
	closes int
}

func (r *resolver) resolve(ctx context.Context, def string, names []string) (app.Mates, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{def}, names...))
	r.mu.Unlock()
	if r.entered != nil {
		close(r.entered)
		<-ctx.Done()
		return app.Mates{}, fmt.Errorf("resolve the mates: %w", ctx.Err())
	}
	if r.err != nil {
		return app.Mates{}, r.err
	}
	m := r.mates
	m.Close = func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.closes++
	}
	return m, nil
}

// counts returns the resolver's calls and how many times Close ran.
func (r *resolver) counts() ([][]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls), r.closes
}

// sameIdentity reports whether got is want, Renew aside.
func sameIdentity(got, want port.Identity) bool {
	return got.Mate == want.Mate && got.Login == want.Login &&
		slices.Equal(got.Env, want.Env) && slices.Equal(got.Unset, want.Unset)
}

// Covers F1: crew's writes go as the default mate, and the action's session
// and check as its own, with the boss's and the mates' logins.
func TestEachActionActsAsItsMateAndCrewAsTheDefault(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		tr.SetBoss("mguilarducci")
		h := fake.NewHarness()
		checker := fake.NewChecker()
		res := &resolver{mates: app.Mates{Writer: opsWriter,
			Identities: map[string]port.Identity{"ops": opsID, "developer": devID}}}
		r := options(t, matedAction, tr, h)
		r.opts.Plain, r.opts.Checker, r.opts.Mates = true, checker, res.resolve
		r.start()

		session := next(t, h)
		session.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}

		logins := []string{"crew-ops[bot]", "crew-developer[bot]"}
		calls := tr.ActAsCalls()
		if len(calls) != 1 || !sameIdentity(calls[0].Writer, opsWriter) || !slices.Equal(calls[0].Mates, logins) {
			t.Errorf("ActAs calls = %+v, want one with ops and both logins", calls)
		}
		run := session.Run()
		if !sameIdentity(run.Identity, devID) || !slices.Equal(run.Boss, []string{"mguilarducci"}) ||
			!slices.Equal(run.Mates, logins) {
			t.Errorf("session ran as %+v for %q with mates %q, want developer", run.Identity, run.Boss, run.Mates)
		}
		checks := checker.Checks()
		if len(checks) != 1 || !sameIdentity(checks[0].Identity, devID) ||
			!slices.Equal(checks[0].Boss, []string{"mguilarducci"}) || !slices.Equal(checks[0].Mates, logins) {
			t.Errorf("checks = %+v, want one as developer", checks)
		}
		resolved, closes := res.counts()
		if !slices.EqualFunc(resolved, [][]string{{"ops", "ops", "developer"}}, slices.Equal) || closes != 1 {
			t.Errorf("resolver calls = %q and %d closes, want ops then ops and developer, closed once",
				resolved, closes)
		}
	})
}

// Covers AE4: a config without mates resolves none and hands the tracker
// no writer.
func TestWithoutMatesNothingActsAsAMate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		tr.SetBoss("me")
		h := fake.NewHarness()
		res := &resolver{}
		r := options(t, oneAction, tr, h)
		r.opts.Mates = res.resolve
		r.start()

		session := next(t, h)
		session.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		if calls, _ := res.counts(); len(calls) != 0 {
			t.Errorf("resolver calls = %q, want none", calls)
		}
		if calls := tr.ActAsCalls(); len(calls) != 0 {
			t.Errorf("ActAs calls = %+v, want none", calls)
		}
		run := session.Run()
		if !sameIdentity(run.Identity, port.Identity{}) || !slices.Equal(run.Boss, []string{"me"}) || run.Mates != nil {
			t.Errorf("session ran as %+v for %q with mates %q, want the boss", run.Identity, run.Boss, run.Mates)
		}
	})
}

// Covers AE10: a default mate that cannot act leaves crew acting as the
// boss, with the warning shown.
func TestAMateThatCannotActWarnsAndCrewActsAsTheBoss(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		h := fake.NewHarness()
		warning := "mate ops has no key on this machine for thatsnotmynameio; " +
			"run `crew mates create ops` in this repository"
		res := &resolver{mates: app.Mates{Warnings: []string{warning}}}
		r := options(t, strings.Replace(oneAction, "config:\n", "config:\n  mate: ops\n", 1), tr, h)
		r.opts.Mates = res.resolve
		r.start()

		session := next(t, h)
		session.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		calls := tr.ActAsCalls()
		if len(calls) != 1 || !sameIdentity(calls[0].Writer, port.Identity{}) || calls[0].Mates != nil {
			t.Errorf("ActAs calls = %+v, want one with the boss and no mate", calls)
		}
		if run := session.Run(); !sameIdentity(run.Identity, port.Identity{}) {
			t.Errorf("session ran as %+v, want the boss", run.Identity)
		}
		first, _, _ := strings.Cut(r.stdout.String(), "\n")
		if !stamped.MatchString(first) || !strings.HasSuffix(first, "crew: warning: "+warning) {
			t.Errorf("first line = %q, want the stamped warning", first)
		}
	})
}

func TestAResolverErrorExitsTwoBeforeAnythingRuns(t *testing.T) {
	tr := fake.NewActingTracker(issue("1", ready))
	res := &resolver{err: errors.New("mate Ops: a name is lowercase letters, digits and hyphens")}
	r := options(t, matedAction, tr, fake.NewHarness())
	r.opts.Mates = res.resolve
	r.start()

	if code := r.exitCode(t); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr := r.stderr.String(); !strings.Contains(stderr, "make the mates act: mate Ops") {
		t.Errorf("stderr = %q, want the resolver's error", stderr)
	}
	if _, closes := res.counts(); closes != 0 || len(tr.ActAsCalls()) != 0 {
		t.Errorf("closed %d times and ActAs ran %d times, want neither", closes, len(tr.ActAsCalls()))
	}
}

func TestMatesWithoutAResolverExitTwo(t *testing.T) {
	r := options(t, matedAction, fake.NewActingTracker(issue("1", ready)), fake.NewHarness())
	r.start()

	if code := r.exitCode(t); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr := r.stderr.String(); !strings.Contains(stderr, "cannot make them act") {
		t.Errorf("stderr = %q, want it to say crew cannot make the mates act", stderr)
	}
}

func TestASignalWhileTheMatesResolveExitsTwo(t *testing.T) {
	res := &resolver{entered: make(chan struct{})}
	r := options(t, matedAction, fake.NewActingTracker(issue("1", ready)), fake.NewHarness())
	r.opts.Mates = res.resolve
	r.start()
	<-res.entered

	r.signals <- syscall.SIGINT

	if code := r.exitCode(t); code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, r.stderr)
	}
	if stderr := r.stderr.String(); !strings.Contains(stderr, "stopped during the environment checks") {
		t.Errorf("stderr = %q, want it to say crew stopped during the environment checks", stderr)
	}
}

func TestAForcedExitStillClosesTheMates(t *testing.T) {
	tr := fake.NewActingTracker(issue("1", ready))
	h := fake.NewHarness()
	h.IgnoreStop(true) // the stop sequence would wait 10 seconds for it
	res := &resolver{mates: app.Mates{Writer: opsWriter, Identities: map[string]port.Identity{"ops": opsID}}}
	r := options(t, strings.Replace(oneAction, "config:\n", "config:\n  mate: ops\n", 1), tr, h)
	r.opts.Mates = res.resolve
	r.start()
	session := next(t, h)

	r.signals <- syscall.SIGINT
	r.signals <- syscall.SIGINT

	if code := r.exitCode(t); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if _, closes := res.counts(); closes != 1 {
		t.Errorf("closed %d times, want once", closes)
	}
	r.release(session)
}
