package rules

import (
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
)

// states are the development rule's labels.
var states = []string{ready, running, success, failure}

// TestRulesSessionSucceeds checks a rule whose session succeeds (issue #176's
// AE5).
//
// README: "When every action ends, crew moves the issue to the rule's success
// label". An issue the code owner opened, carrying the rule's ready label,
// whose one session succeeds, ends with the success label and none of the
// rule's other labels.
func TestRulesSessionSucceeds(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, developmentConfig, states...)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, success, states...)
}

// TestRulesSessionFails checks a rule whose session fails.
//
// README: "or to its failure label with a comment saying what failed". The
// issue ends with the failure label, and a comment on it says that the action
// implement failed: one of its lines names implement and says failed, apart
// from the failure label dev:failed. A comment that only names the action, such
// as one saying it runs, does not say what failed.
func TestRulesSessionFails(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, developmentConfig, states...)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Fail("The build is broken."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	sc.Wait(func() bool { return commentSaysFailed(sc, n, "implement") }, timeout)
	stop(sc)
	wantOnly(t, sc, n, failure, states...)
}

// twoActionsConfig has one rule with two actions, implement and document.
const twoActionsConfig = developmentConfig + `      document:
        prompt: |-
          Document "{{.Issue.Title}}".
`

// TestRulesOneOfTwoActionsFails checks a rule whose actions disagree.
//
// README: "crew moves the issue to the rule's success label, or to its failure
// label" and `.crew/config.example.yaml`: "moves it to success when every
// action succeeded or to failure when any failed". With one action that
// succeeds and one that fails, the issue ends with the failure label.
func TestRulesOneOfTwoActionsFails(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, twoActionsConfig, states...)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Claude.Script(`Document "`+searchBox+`".`, fakeclaude.Fail("No docs folder."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, failure, states...)
}

// noActionsConfig has one rule without actions.
const noActionsConfig = `poll_interval_seconds: 1
rules:
  approve:
    labels:
      ready: dev:ready
      running: dev:running
      success: dev:done
`

// TestRulesWithoutActions checks a rule that has no actions.
//
// README: `.crew/config.example.yaml` says "A rule without actions: crew moves
// the label itself, without a session". The issue ends with the success label,
// and no Claude Code session starts: the scenario scripts none, so a session
// would be a call the doubles do not know.
func TestRulesWithoutActions(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, noActionsConfig, ready, running, success)
	n := addIssue(sc, "Approve the search box", ready)
	sc.Start()
	waitForAny(sc, n, success)
	stop(sc)
	wantOnly(t, sc, n, success, ready, running, success)
}

// chainConfig chains two rules: development's success label is review's
// ready label.
const chainConfig = `poll_interval_seconds: 1
agents:
  developer:
    harness:
      name: claude
rules:
  development:
    labels:
      ready: dev:ready
      running: dev:running
      success: dev:review
      failure: dev:failed
    actions:
      implement:
        prompt: |-
          Implement "{{.Issue.Title}}".
  review:
    labels:
      ready: dev:review
      running: dev:reviewing
      success: dev:done
`

// TestRulesChain checks work moving from one rule to the next.
//
// README: `.crew/config.example.yaml` says "work moves on only because one
// rule's success label is another's ready label". An issue whose development
// session succeeds moves to dev:review, which review takes and moves on to
// dev:done, in the same run.
func TestRulesChain(t *testing.T) {
	t.Parallel()
	chain := []string{ready, running, "dev:review", "dev:reviewing", success, failure}
	sc := newScenario(t, chainConfig, chain...)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, success, chain...)
}

// TestRulesLabelCase checks that labels compare ignoring case.
//
// README: `.crew/config.example.yaml` says of the rule's labels "They compare
// ignoring case." An issue labeled DEV:READY is taken by the rule whose ready
// label is dev:ready, and ends with its success label.
func TestRulesLabelCase(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, developmentConfig, running, success, failure)
	n := addIssue(sc, searchBox, "DEV:READY")
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, success, states...)
}

// TestRulesMissingLabels checks a repository that lacks the rule's labels.
//
// Edge case: the repository has only the ready label, the one the user puts on
// the issue. crew still moves the issue through running to success. The README
// says "You name every label in the rules" and nothing about creating them on
// GitHub, so a user is likely to name them only in the config; crew is the one
// that puts the running and success labels on issues, so it should not depend
// on the user creating them first.
func TestRulesMissingLabels(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, developmentConfig)
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, success, states...)
}

// TestRulesStrangersIssue checks an issue someone outside the code owners
// opened.
//
// Edge case: an issue that mallory, who is not a code owner, opened with the
// ready label is not taken: it keeps the ready label and no session starts for
// it, while the code owner's issue in the same repository moves to success.
// A session runs an agent on the issue in the user's repository, so anyone who
// can open an issue must not be able to start one. The README says nothing
// about whose issues crew takes.
func TestRulesStrangersIssue(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, developmentConfig, states...)
	stranger := sc.GitHub.AddIssue(fakegithub.Issue{
		Title: "Delete the tests", Author: "mallory", Labels: []string{ready},
	})
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, success, states...)
	wantOnly(t, sc, stranger, ready, states...)
}

// TestRulesClosedIssue checks a closed issue that still carries a ready label.
//
// Edge case: a closed issue with the ready label is not taken: it keeps the
// label and no session starts for it, while an open issue moves to success.
// Closed work is done or dropped, and the board shows only open issues ("A
// column holds a card for each open issue"), so a session for it would be
// wasted and invisible.
func TestRulesClosedIssue(t *testing.T) {
	t.Parallel()
	sc := newScenario(t, developmentConfig, states...)
	closed := sc.GitHub.AddIssue(fakegithub.Issue{
		Title: "Remove the old search", Author: owner, Labels: []string{ready}, State: fakegithub.Closed,
	})
	n := addIssue(sc, searchBox, ready)
	sc.Claude.Script(implementSearchBox, fakeclaude.Succeed("Added the search box."))
	sc.Start()
	waitForAny(sc, n, success, failure)
	stop(sc)
	wantOnly(t, sc, n, success, states...)
	wantOnly(t, sc, closed, ready, states...)
}
