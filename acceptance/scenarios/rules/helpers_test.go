package rules

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// timeout bounds every wait and every exit: generous, since a wait
	// returns as soon as its condition holds.
	timeout = time.Minute

	// owner is the account gh is logged in as on the fake GitHub, and the
	// repository's code owner in every scenario of this package.
	owner = "boss"

	// The labels of the development rule.
	ready   = "dev:ready"
	running = "dev:running"
	success = "dev:done"
	failure = "dev:failed"
)

// developmentConfig has one rule, development, with one action, implement,
// whose prompt names the issue's title so a script can key on it.
const developmentConfig = `poll_interval_seconds: 1
agents:
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

// newScenario builds a scenario for config whose repository has owner as its
// code owner and the given labels already created.
func newScenario(t *testing.T, config string, labels ...string) *harness.Scenario {
	t.Helper()
	sc := harness.New(t, harness.Options{Config: config, Args: []string{"--plain"}})
	sc.GitHub.SetFile(".github/CODEOWNERS", "* @"+owner+"\n")
	if len(labels) > 0 {
		sc.GitHub.AddLabel(labels...)
	}
	return sc
}

// addIssue adds an open issue that the code owner opened, carrying labels.
func addIssue(sc *harness.Scenario, title string, labels ...string) int {
	return sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Author: owner, Labels: labels})
}

// searchBox is the title of the issue a scenario expects crew to take, and
// implementSearchBox the implement action's prompt for it, the key of its
// script.
const (
	searchBox          = "Add a search box"
	implementSearchBox = `Implement "` + searchBox + `".`
)

// hasLabel reports whether labels holds name, ignoring case as GitHub does.
func hasLabel(labels []string, name string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return strings.EqualFold(l, name) })
}

// labelsOf returns the labels the issue number carries now.
func labelsOf(sc *harness.Scenario, number int) []string {
	issue, _ := sc.GitHub.Issue(number)
	return issue.Labels
}

// waitForAny waits until the issue number carries one of names.
func waitForAny(sc *harness.Scenario, number int, names ...string) {
	sc.Wait(func() bool {
		labels := labelsOf(sc, number)
		return slices.ContainsFunc(names, func(name string) bool { return hasLabel(labels, name) })
	}, timeout)
}

// stop stops crew as Ctrl+C does and waits for it to exit.
func stop(sc *harness.Scenario) {
	sc.Stop()
	sc.Exit(timeout)
}

// wantOnly fails the test unless the issue number carries want and none of
// the other labels in states.
func wantOnly(t *testing.T, sc *harness.Scenario, number int, want string, states ...string) {
	t.Helper()
	labels := labelsOf(sc, number)
	if !hasLabel(labels, want) {
		t.Errorf("issue #%d carries %q, want %q", number, labels, want)
	}
	for _, other := range states {
		if other != want && hasLabel(labels, other) {
			t.Errorf("issue #%d still carries %q: %q", number, other, labels)
		}
	}
}

// commentHolding reports whether a comment on the issue number contains text.
func commentHolding(sc *harness.Scenario, number int, text string) bool {
	return slices.ContainsFunc(sc.GitHub.Comments(number), func(c fakegithub.Comment) bool {
		return strings.Contains(c.Body, text)
	})
}

// failedWord is the word failed on its own, ignoring case.
var failedWord = regexp.MustCompile(`(?i)\bfailed\b`)

// commentSaysFailed reports whether a comment on the issue number says that
// action failed: one of its lines names action and holds the word failed once
// the rule's failure label is taken out of it, so a line that only names the
// label an issue moves to on failure does not count.
func commentSaysFailed(sc *harness.Scenario, number int, action string) bool {
	return slices.ContainsFunc(sc.GitHub.Comments(number), func(c fakegithub.Comment) bool {
		for line := range strings.SplitSeq(c.Body, "\n") {
			line = strings.ReplaceAll(strings.ToLower(line), failure, "")
			if strings.Contains(line, action) && failedWord.MatchString(line) {
				return true
			}
		}
		return false
	})
}
