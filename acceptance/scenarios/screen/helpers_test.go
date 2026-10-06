package screen

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// timeout bounds every wait and every exit: generous, since a wait
	// returns as soon as its condition holds.
	timeout = time.Minute

	// settle is how long a masked screen must stay the same to be stable.
	settle = 3 * time.Second

	// cols and rows are the terminal's size: far taller than the layout
	// needs, so no section loses rows to the height.
	cols = 160
	rows = 80

	// owner is the account gh is logged in as, and the repository's code
	// owner.
	owner = "boss"

	// The labels of the development rule.
	ready   = "dev:ready"
	running = "dev:running"
	success = "dev:done"
	failure = "dev:failed"

	// title is the title of every scenario's issue, and said what its
	// session says while it works.
	title = "Add a search box"
	said  = "Writing the search box."
)

// config has one rule, development, with one action, implement, and no board,
// so the board has one column for the rule. It keeps the default poll interval,
// so a poll's event does not scroll the screen while a scenario waits for it to
// hold still: crew takes the issue at its first poll.
const config = `agents:
  developer:
    harness:
      name: claude
rules:
  development:
    labels:
      ready: dev:ready
      running: dev:running
      success: dev:done
      failure: dev:failed
    actions:
      implement:
        prompt: |-
          Implement "{{.Issue.Title}}".
`

// prompt is the implement action's prompt for the scenarios' issue.
const prompt = `Implement "` + title + `".`

// newScenario builds a screen scenario whose repository has owner as its code
// owner, the rule's labels, and one issue titled title with the ready label.
// It returns the scenario and the issue's number.
func newScenario(t *testing.T) (*harness.Scenario, int) {
	t.Helper()
	sc := harness.New(t, harness.Options{Config: config, Screen: true, Size: harness.Size{Cols: cols, Rows: rows}})
	sc.GitHub.SetFile(".github/CODEOWNERS", "* @"+owner+"\n")
	sc.GitHub.AddLabel(ready, running, success, failure)
	n := sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Author: owner, Labels: []string{ready}})
	return sc, n
}

// heldSession is a session that says said and then works until release is
// closed, when it succeeds, or until crew stops it.
func heldSession(release <-chan struct{}) fakeclaude.ScriptFunc {
	return func(ctx context.Context, s *fakeclaude.Session) int {
		_ = s.Emit(s.Init(), s.Said(said))
		select {
		case <-release:
			_ = s.Emit(s.Success("Added the search box."))
			return 0
		case <-ctx.Done():
			return 1
		}
	}
}

// masks are the default masks, then one for a clock that a box drawn over
// the Events section cuts after its first digits, such as 01:2.
func masks() []harness.Mask {
	return append(harness.DefaultMasks(), harness.Mask{
		Pattern: regexp.MustCompile(`\b[0-9]{2}:[0-9]\b`), Placeholder: "HH:M",
	})
}

// waitForLabel waits until the issue number carries label.
func waitForLabel(sc *harness.Scenario, number int, label string) {
	sc.Wait(func() bool {
		issue, _ := sc.GitHub.Issue(number)
		return slices.Contains(issue.Labels, label)
	}, timeout)
}

// stop stops crew as Ctrl+C does and waits for it to exit.
func stop(sc *harness.Scenario) {
	sc.Stop()
	sc.Exit(timeout)
}

// wantText fails the test for each of wants the screen text does not show.
func wantText(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("the screen does not show %q:\n%s", want, text)
		}
	}
}

// inColumn reports whether a line below the column header holds text,
// ignoring case, starting at or right of the header's first cell: a card in
// that column of the board.
func inColumn(screen, header, text string) bool {
	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		x := cell(line, header)
		if x < 0 {
			continue
		}
		for _, below := range lines[i+1:] {
			if y := cell(strings.ToLower(below), strings.ToLower(text)); y >= x {
				return true
			}
		}
	}
	return false
}

// cell is the column, counted in runes, where line holds text, or -1.
func cell(line, text string) int {
	before, _, found := strings.Cut(line, text)
	if !found {
		return -1
	}
	return len([]rune(before))
}
