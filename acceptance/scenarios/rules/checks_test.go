package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
)

const (
	// redReason is the last line the red check prints.
	redReason = "the widget tests are red"

	// markerLabel is the label the marker check creates on GitHub, which shows
	// that it ran.
	markerLabel = "check-ran"
)

// checks declares two checks: red, which fails with redReason as its last
// line, and marker, which passes after creating markerLabel through gh.
const checks = `checks:
  red: |-
    echo "running the widget tests"
    echo "the widget tests are red"
    exit 1
  marker: gh label create check-ran
`

// checkConfig is developmentConfig whose implement action names check, a
// check or a list of checks, with the checks declared.
func checkConfig(check string) string {
	return developmentConfig + "        check: " + check + "\n" + checks
}

// TestRulesCheckPasses checks an action whose session and check pass.
//
// README: a check "runs in the action's worktree after the session succeeded,
// and decides whether the action succeeded". The marker check runs, which
// creates its label, and passes, so the issue ends with the success label.
func TestRulesCheckPasses(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, checkConfig("marker"), states...)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, success, states...)
	if !slices.Contains(sc.GitHub.Labels(), markerLabel) {
		t.Errorf("the check did not run: the repository's labels are %q", sc.GitHub.Labels())
	}
}

// TestRulesCheckReasonInComment checks the reason of a failing check.
//
// README: "The issue's status comment shows each check that ran, with crew's
// words and the last line the check printed". After a session that succeeds
// and a check that fails, the issue ends with the failure label, and a comment
// on it holds the check's last line, not its first.
func TestRulesCheckReasonInComment(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, checkConfig("red"), states...)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	sc.Wait(func() bool { return commentHolding(sc, n, redReason) }, timeout)
	stop(sc)
	wantOnly(t, sc, n, failure, states...)
	for _, c := range sc.GitHub.Comments(n) {
		if strings.Contains(c.Body, "running the widget tests") {
			t.Errorf("a comment shows the check's first line, not only its last: %q", c.Body)
		}
	}
}

// TestRulesChecksStopAtFirstFailure checks a list of checks whose first fails.
//
// README: "The checks run in that order, each with its own ten minutes. The
// first that fails, cannot start, runs out of time or is stopped fails the
// action, and the rest do not run." With check: [red, marker], the issue ends
// with the failure label and marker never runs, so its label does not exist.
func TestRulesChecksStopAtFirstFailure(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, checkConfig("[red, marker]"), states...)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, failure, states...)
	if slices.Contains(sc.GitHub.Labels(), markerLabel) {
		t.Error("the check after the failing one ran")
	}
}
