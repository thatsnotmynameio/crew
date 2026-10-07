// Package smoke holds the developer's smoke runs: they show that the crew
// binary built for release runs its loop against the doubles without a call
// the doubles do not know. They assert nothing that crew's README promises;
// the acceptance scenarios do.
package smoke

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

// config has one rule that runs a Claude Code session with the issue's title
// in its prompt, then the shell action smoke-file, which passes when the
// session left the file smoke.txt in the run's worktree. The passed route
// moves the issue to smoke:done; the failed route posts crew's report and a
// comment, then moves the issue to smoke:failed. crew polls every second.
const config = `poll_interval_seconds: 1
tracker:
  name: github
agents:
  worker:
    harness:
      name: claude
actions:
  smoke-file: '[ -f smoke.txt ] || { echo "no smoke.txt"; exit 1; }'
rules:
  smoke:
    labels:
      ready: "smoke:ready"
      running: "smoke:in progress"
    actions:
      - agent: worker
        prompt: "Work on {{.Issue.Title}}."
      - smoke-file
    routes:
      passed: "smoke:done"
      failed:
        - report
        - comment: "` + comment + `"
        - move: "smoke:failed"
`

// comment is the failed route's comment, the start of what it posts.
const comment = "Smoke failed at {{.Action}}."

// The titles of the two issues, each the key of its session's script.
const (
	passes = "the issue whose shell action passes"
	fails  = "the issue whose shell action fails"
)

// timeout bounds every wait.
const timeout = 60 * time.Second

// issues are the numbers of the two issues.
type issues struct {
	passes, fails int
}

// setUp adds the two issues in the rule's ready label and scripts their
// sessions: both succeed, and only the first leaves smoke.txt behind.
func setUp(sc *harness.Scenario) issues {
	n := issues{
		passes: sc.GitHub.AddIssue(fakegithub.Issue{Title: passes, Labels: []string{"smoke:ready"}}),
		fails:  sc.GitHub.AddIssue(fakegithub.Issue{Title: fails, Labels: []string{"smoke:ready"}}),
	}
	sc.Claude.Script(passes, writesSmokeFile)
	sc.Claude.Script(fails, fakeclaude.Succeed("Done."))
	return n
}

// writesSmokeFile is a session that writes smoke.txt in its directory, the
// run's worktree, and succeeds.
func writesSmokeFile(ctx context.Context, s *fakeclaude.Session) int {
	if err := os.WriteFile(filepath.Join(s.Dir, "smoke.txt"), []byte("smoke\n"), 0o600); err != nil {
		_ = s.Emit(s.Init(), s.Failure(err.Error()))
		return 1
	}
	return fakeclaude.Succeed("Done.")(ctx, s)
}

// ended reports whether both runs ended: the first issue in smoke:done, the
// second in smoke:failed with the failed route's comment.
func ended(gh *fakegithub.GitHub, n issues) func() bool {
	return func() bool {
		return hasLabel(gh, n.passes, "smoke:done") && hasLabel(gh, n.fails, "smoke:failed") &&
			slices.ContainsFunc(gh.Comments(n.fails), func(c fakegithub.Comment) bool {
				return strings.HasPrefix(c.Body, "Smoke failed at smoke-file.")
			})
	}
}

// hasLabel reports whether the issue number carries label.
func hasLabel(gh *fakegithub.GitHub, number int, label string) bool {
	issue, ok := gh.Issue(number)
	return ok && slices.Contains(issue.Labels, label)
}

func TestSmokePlain(t *testing.T) {
	sc := harness.New(t, harness.Options{Config: config, Args: []string{"--plain"}})
	n := setUp(sc)
	sc.Start()
	sc.Wait(ended(sc.GitHub, n), timeout)
	sc.Stop()
	if exited := sc.Exit(timeout); exited.Code != 0 {
		t.Fatalf("crew exited %d, want 0\nstdout:\n%s\nstderr:\n%s", exited.Code, exited.Stdout, exited.Stderr)
	}
}

func TestSmokeScreen(t *testing.T) {
	sc := harness.New(t, harness.Options{Config: config, Screen: true})
	n := setUp(sc)
	sc.Start()
	sc.Screen().WaitForText(t, harness.RepositoryName, timeout)
	sc.Wait(ended(sc.GitHub, n), timeout)
	sc.Screen().Send(t, "qq")
	if exited := sc.Exit(timeout); exited.Code != 0 {
		t.Fatalf("crew exited %d, want 0\nscreen:\n%s", exited.Code, sc.Screen().Text())
	}
}
