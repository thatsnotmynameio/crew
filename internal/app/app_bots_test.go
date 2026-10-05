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

// botAction is oneAction with the default bot ops, and the development
// action acting as developer and running a check.
const botAction = `
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
	opsWriter = port.Identity{Bot: "ops", Login: "crew-ops[bot]", Env: []string{"GH_CONFIG_DIR=/run/ops/crew"}}
	opsID     = port.Identity{Bot: "ops", Login: "crew-ops[bot]", Env: []string{"GH_CONFIG_DIR=/run/ops/sessions"}}
	devID     = port.Identity{Bot: "developer", Login: "crew-developer[bot]",
		Env: []string{"GH_CONFIG_DIR=/run/developer/sessions"}, Unset: []string{"GH_TOKEN"}}
)

// resolver is a scripted Options.Bots: it returns bots or err, records
// each call and counts the calls to the Close it returns.
type resolver struct {
	bots app.Bots
	err  error
	// entered, when not nil, is closed as the resolver starts; it then
	// waits for its context to end.
	entered chan struct{}

	mu     sync.Mutex
	calls  [][]string // the default, then the names
	closes int
}

func (r *resolver) resolve(ctx context.Context, def string, names []string) (app.Bots, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{def}, names...))
	r.mu.Unlock()
	if r.entered != nil {
		close(r.entered)
		<-ctx.Done()
		return app.Bots{}, fmt.Errorf("resolve the mates: %w", ctx.Err())
	}
	if r.err != nil {
		return app.Bots{}, r.err
	}
	m := r.bots
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
	return got.Bot == want.Bot && got.Login == want.Login &&
		slices.Equal(got.Env, want.Env) && slices.Equal(got.Unset, want.Unset)
}

// Covers F1: crew's writes go as the default bot, and the action's session
// and check as its own, with the code owners' and the bots' logins.
func TestEachActionActsAsItsBotAndCrewAsTheDefault(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		tr.SetCodeOwners("mguilarducci")
		h := fake.NewHarness()
		checker := fake.NewChecker()
		res := &resolver{bots: app.Bots{Writer: opsWriter,
			Identities: map[string]port.Identity{"ops": opsID, "developer": devID},
			Logins:     []string{"crew-ops[bot]", "crew-developer[bot]"}}}
		r := options(t, botAction, tr, h)
		r.opts.Plain, r.opts.Checker, r.opts.Bots = true, checker, res.resolve
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
		if len(calls) != 1 || !sameIdentity(calls[0].Writer, opsWriter) || !slices.Equal(calls[0].Bots, logins) {
			t.Errorf("ActAs calls = %+v, want one with ops and both logins", calls)
		}
		run := session.Run()
		if !sameIdentity(run.Identity, devID) || !slices.Equal(run.CodeOwners, []string{"mguilarducci"}) ||
			!slices.Equal(run.Bots, logins) {
			t.Errorf("session ran as %+v for %q with mates %q, want developer", run.Identity, run.CodeOwners, run.Bots)
		}
		checks := checker.Checks()
		if len(checks) != 1 || !sameIdentity(checks[0].Identity, devID) ||
			!slices.Equal(checks[0].CodeOwners, []string{"mguilarducci"}) || !slices.Equal(checks[0].Bots, logins) {
			t.Errorf("checks = %+v, want one as developer", checks)
		}
		resolved, closes := res.counts()
		if !slices.EqualFunc(resolved, [][]string{{"ops", "ops", "developer"}}, slices.Equal) || closes != 1 {
			t.Errorf("resolver calls = %q and %d closes, want ops then ops and developer, closed once",
				resolved, closes)
		}
	})
}

// Covers AE4: a config without bots resolves none and hands the tracker
// no writer.
func TestWithoutBotsNothingActsAsABot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		tr.SetCodeOwners("me")
		h := fake.NewHarness()
		res := &resolver{}
		r := options(t, oneAction, tr, h)
		r.opts.Bots = res.resolve
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
		if !sameIdentity(run.Identity, port.Identity{}) || !slices.Equal(run.CodeOwners, []string{"me"}) || run.Bots != nil {
			t.Errorf("session ran as %+v for %q with mates %q, want you", run.Identity, run.CodeOwners, run.Bots)
		}
	})
}

// Covers AE10: a default bot that cannot act leaves crew acting as
// you, with the warning shown.
func TestABotThatCannotActWarnsAndCrewActsAsYou(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		h := fake.NewHarness()
		warning := "mate ops has no key on this machine for thatsnotmynameio; " +
			"run `crew mates create ops` in this repository"
		res := &resolver{bots: app.Bots{Warnings: []string{warning}}}
		r := options(t, strings.Replace(oneAction, "config:\n", "config:\n  mate: ops\n", 1), tr, h)
		r.opts.Bots = res.resolve
		r.start()

		session := next(t, h)
		session.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		calls := tr.ActAsCalls()
		if len(calls) != 1 || !sameIdentity(calls[0].Writer, port.Identity{}) || calls[0].Bots != nil {
			t.Errorf("ActAs calls = %+v, want one with you and no mate", calls)
		}
		if run := session.Run(); !sameIdentity(run.Identity, port.Identity{}) {
			t.Errorf("session ran as %+v, want you", run.Identity)
		}
		lines := unstamped(t, r.stdout.String())
		if i := slices.Index(lines, "reading the run journal"); i < 0 || i+1 >= len(lines) ||
			lines[i+1] != "warning: "+warning {
			t.Errorf("stdout = %q, want the warning right after the boot log", lines)
		}
	})
}

// A bot stored on this machine that cannot act this run, such as one not
// installed on the repository, still has the issues it opened taken.
func TestABotThatCannotActStillHasItsIssuesTaken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		h := fake.NewHarness()
		res := &resolver{bots: app.Bots{Logins: []string{"crew-ops[bot]"},
			Warnings: []string{"mate ops is not installed on thatsnotmynameio/crew"}}}
		r := options(t, strings.Replace(oneAction, "config:\n", "config:\n  mate: ops\n", 1), tr, h)
		r.opts.Bots = res.resolve
		r.start()

		session := next(t, h)
		session.End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		calls := tr.ActAsCalls()
		if len(calls) != 1 || !sameIdentity(calls[0].Writer, port.Identity{}) ||
			!slices.Equal(calls[0].Bots, []string{"crew-ops[bot]"}) {
			t.Errorf("ActAs calls = %+v, want one with you and ops's login", calls)
		}
		if run := session.Run(); !sameIdentity(run.Identity, port.Identity{}) ||
			!slices.Equal(run.Bots, []string{"crew-ops[bot]"}) {
			t.Errorf("session ran as %+v with mates %q, want you and ops's login", run.Identity, run.Bots)
		}
	})
}

func TestAResolverErrorExitsTwoBeforeAnythingRuns(t *testing.T) {
	tr := fake.NewActingTracker(issue("1", ready))
	res := &resolver{err: errors.New("mate Ops: a name is lowercase letters, digits and hyphens")}
	r := options(t, botAction, tr, fake.NewHarness())
	r.opts.Bots = res.resolve
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

func TestBotsWithoutAResolverExitTwo(t *testing.T) {
	r := options(t, botAction, fake.NewActingTracker(issue("1", ready)), fake.NewHarness())
	r.start()

	if code := r.exitCode(t); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr := r.stderr.String(); !strings.Contains(stderr, "cannot make them act") {
		t.Errorf("stderr = %q, want it to say crew cannot make the mates act", stderr)
	}
}

func TestASignalWhileTheBotsResolveExitsTwo(t *testing.T) {
	res := &resolver{entered: make(chan struct{})}
	r := options(t, botAction, fake.NewActingTracker(issue("1", ready)), fake.NewHarness())
	r.opts.Bots = res.resolve
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

func TestAForcedExitStillClosesTheBots(t *testing.T) {
	tr := fake.NewActingTracker(issue("1", ready))
	h := fake.NewHarness()
	h.IgnoreStop(true) // the stop sequence would wait 10 seconds for it
	res := &resolver{bots: app.Bots{Writer: opsWriter, Identities: map[string]port.Identity{"ops": opsID}}}
	r := options(t, strings.Replace(oneAction, "config:\n", "config:\n  mate: ops\n", 1), tr, h)
	r.opts.Bots = res.resolve
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

// runOnce runs crew on r until its one session, from h, succeeded, then
// stops it, and returns what it printed.
func runOnce(t *testing.T, r *crewRun, h *fake.Harness) string {
	t.Helper()
	r.start()
	next(t, h).End(success)
	synctest.Wait()
	r.signals <- syscall.SIGTERM
	if code := <-r.code; code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	return r.stdout.String()
}

// The engine learns which bots cannot act at startup and reads the bots'
// renewals: a bot that cannot act is never said to stop acting, while one
// that acts is.
func TestABotThatCannotActIsNeverSaidToStopAndOneThatActsIs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		h := fake.NewHarness()
		devWarning := "mate developer could not renew its token: GitHub is down"
		res := &resolver{bots: app.Bots{
			Identities: map[string]port.Identity{"developer": devID},
			Unable:     map[string]string{"ops": "no key"},
			Failing: func() map[string]string {
				return map[string]string{"ops": "mate ops could not renew its token", "developer": devWarning}
			},
		}}
		r := options(t, botAction, tr, h)
		r.opts.Plain, r.opts.Checker, r.opts.Bots = true, fake.NewChecker(), res.resolve

		out := runOnce(t, r, h)

		containsAll(t, out, "mate developer stopped acting: "+devWarning)
		if strings.Contains(out, "mate ops stopped acting") {
			t.Errorf("stdout says ops stopped acting, which never acted:\n%s", out)
		}
	})
}

// crew's writes as the default bot going back to you is said once.
func TestTheDefaultBotsWritesGoingBackToYouIsSaid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		warning := "mate ops lacks a permission: run `crew mates create ops`; crew writes as you until it restarts"
		tr.SetWriterLost(warning)
		h := fake.NewHarness()
		res := &resolver{bots: app.Bots{Writer: opsWriter,
			Identities: map[string]port.Identity{"ops": opsID, "developer": devID}}}
		r := options(t, botAction, tr, h)
		r.opts.Plain, r.opts.Checker, r.opts.Bots = true, fake.NewChecker(), res.resolve

		out := runOnce(t, r, h)

		if n := strings.Count(out, "mate ops stopped acting: "+warning); n != 1 {
			t.Errorf("stdout says ops stopped acting %d times, want once:\n%s", n, out)
		}
	})
}

// Without bots, nothing acts as a bot, so nothing stops acting.
func TestWithoutBotsNoBotStopsActing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker(issue("1", ready))
		tr.SetWriterLost("crew writes as you until it restarts")
		h := fake.NewHarness()
		r := options(t, oneAction, tr, h)
		r.opts.Plain = true

		if out := runOnce(t, r, h); strings.Contains(out, "stopped acting") {
			t.Errorf("stdout says a mate stopped acting, without mates:\n%s", out)
		}
	})
}
