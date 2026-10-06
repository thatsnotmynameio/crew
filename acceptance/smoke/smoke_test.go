// Package smoke holds the developer's smoke runs: they show that the crew
// binary built for release runs its loop against the doubles without a call
// the doubles do not know. They assert nothing that crew's README promises;
// the acceptance scenarios do.
package smoke

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

// config has one rule whose action runs a Claude Code session with the
// issue's title in its prompt. crew polls every second and stops by itself
// after a few seconds.
const config = `poll_interval_seconds: 1
run_time_limit_seconds: 5
tracker:
  name: github
agents:
  worker:
    harness:
      name: claude
rules:
  smoke:
    labels:
      ready: "smoke:ready"
      running: "smoke:in progress"
      success: "smoke:done"
      failure: "smoke:failed"
    actions:
      work:
        agent: worker
        prompt: "Work on {{.Issue.Title}}."
`

// The titles of the two issues, each the key of its session's script.
const (
	succeeds = "the issue whose session succeeds"
	fails    = "the issue whose session fails"
)

// timeout bounds every wait: crew stops itself well within it.
const timeout = 60 * time.Second

// sessions are the two scripted sessions and whether each was invoked.
type sessions struct {
	succeeded, failed atomic.Bool
}

// invoked reports whether both sessions were invoked.
func (s *sessions) invoked() bool {
	return s.succeeded.Load() && s.failed.Load()
}

// setUp adds the two issues in the rule's ready label and scripts their
// sessions.
func setUp(sc *harness.Scenario) *sessions {
	s := &sessions{}
	for _, title := range []string{succeeds, fails} {
		sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Labels: []string{"smoke:ready"}})
	}
	sc.Claude.Script(succeeds, marked(&s.succeeded, fakeclaude.Succeed("Done.")))
	sc.Claude.Script(fails, marked(&s.failed, fakeclaude.Fail("Could not finish.")))
	return s
}

// marked returns fn, setting flag when it runs.
func marked(flag *atomic.Bool, fn fakeclaude.ScriptFunc) fakeclaude.ScriptFunc {
	return func(ctx context.Context, s *fakeclaude.Session) int {
		flag.Store(true)
		return fn(ctx, s)
	}
}

func TestSmokePlain(t *testing.T) {
	sc := harness.New(t, harness.Options{Config: config, Args: []string{"--plain"}})
	s := setUp(sc)
	sc.Start()
	sc.Wait(s.invoked, timeout)
	if exited := sc.Exit(timeout); exited.Code != 0 {
		t.Fatalf("crew exited %d, want 0\nstdout:\n%s\nstderr:\n%s", exited.Code, exited.Stdout, exited.Stderr)
	}
}

func TestSmokeScreen(t *testing.T) {
	sc := harness.New(t, harness.Options{Config: config, Screen: true})
	s := setUp(sc)
	sc.Start()
	sc.Screen().WaitForText(t, harness.RepositoryName, timeout)
	sc.Wait(s.invoked, timeout)
	sc.Screen().Send(t, "q")
	if exited := sc.Exit(timeout); exited.Code != 0 {
		t.Fatalf("crew exited %d, want 0\nscreen:\n%s", exited.Code, sc.Screen().Text())
	}
}
