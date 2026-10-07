package app_test

import (
	"context"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// withFailedRoute is oneAction whose failed route is steps, a YAML flow
// list.
func withFailedRoute(steps string) string {
	return strings.Replace(oneAction, "      failed:\n        - report\n        - move: needs attention\n",
		"      failed: "+steps+"\n", 1)
}

// The failed routes of the route capability tests: one closes the issue,
// the other comments on it.
const (
	closing    = "[report, close]"
	commenting = `[comment: "{{.Action}} failed", move: needs attention]`
)

// KTD7: crew refuses at startup a route whose step its tracker cannot
// take, before any listing, naming the rule and the route, with the
// config's exit code.
func TestARouteStepTheTrackerCannotTakeExitsTwoNamingTheRuleAndTheRoute(t *testing.T) {
	tests := []struct {
		name, steps, want string
	}{
		{"close without a Closer", closing, `rules.implement.routes.failed: tracker "fake" cannot close issues`},
		{
			"comment without a Commenter", commenting,
			`rules.implement.routes.failed: tracker "fake" cannot comment on issues`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
			r := options(t, withFailedRoute(tt.steps), tr, fake.NewHarness())

			if code := app.Run(context.Background(), r.opts); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if n := tr.listed(); n != 0 {
				t.Errorf("the tracker listed %d times, want none", n)
			}
			if stderr := r.stderr.String(); !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.want)
			}
		})
	}
}

// KTD7: crew starts when its tracker can take every step of the routes.
func TestARouteStepTheTrackerCanTakeStarts(t *testing.T) {
	for name, steps := range map[string]string{"comment with a Commenter": commenting, "close with a Closer": closing} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := fake.NewHarness()
				runOnce(t, options(t, withFailedRoute(steps), fake.NewRoutingTracker(issue("1", ready)), h), h)
			})
		})
	}
}

// R14: the labels a route moves to are crew's states, which the tracker
// prepares, as it does the rules' ready and running labels. A session that
// may wait needs a tracker that lists comments (KTD-W5).
func TestTheTrackerPreparesTheLabelsTheRoutesMoveTo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewRoutingTracker()
		r := options(t, mayWait(), tr, fake.NewHarness())
		r.start()
		synctest.Wait()
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}

		calls := tr.Calls()
		if len(calls) == 0 {
			t.Fatal("the tracker was never prepared")
		}
		for _, want := range []crew.State{ready, inProgress, readyToReview, needsAttention, "waiting answer"} {
			if !slices.Contains(calls[0], want) {
				t.Errorf("Prepare got %v, want it to hold %q", calls[0], want)
			}
		}
	})
}
