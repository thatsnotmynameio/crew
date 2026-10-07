package config_test

import (
	"strings"
	"testing"
)

func TestLoadRejectsInvalidRules(t *testing.T) {
	testRejects(t, invalidRules)
}

func TestLoadRejectsRulesThatTakeAnItemTwice(t *testing.T) {
	testRejects(t, invalidRuleGraphs)
}

func TestLoadRejectsInvalidActions(t *testing.T) {
	testRejects(t, invalidActions)
}

// invalidRules are errors in one rule, or in rules as a whole.
var invalidRules = []rejectCase{
	{
		name:  "takes names neither kind",
		body:  ruleWith("takes: prs"),
		wants: []string{"rules.implement.takes", "line 6", `"prs"`, "issues", "pull_requests"},
	},
	{
		// Values are matched exactly, like queue names.
		name:  "takes in another case",
		body:  ruleWith("takes: Issues"),
		wants: []string{"rules.implement.takes", "line 6", `"Issues"`},
	},
	{
		// A rule takes one kind, never both.
		name:  "takes is a list",
		body:  ruleWith("takes: [issues, pull_requests]"),
		wants: []string{"rules.implement.takes", "line 6"},
	},
	{
		name:  "notify not a boolean",
		body:  ruleWith("notify: maybe"),
		wants: []string{"rules.implement.notify", "line 6", "maybe"},
	},
	{
		// A bad takes is reported with the rule's other errors.
		name:  "takes and a label wrong in one rule",
		body:  strings.Replace(ruleWith("takes: prs"), "      success: ready to review\n", "", 1),
		wants: []string{"rules.implement.takes", `"prs"`, "rules.implement.labels.success", "required"},
	},
	{
		name:  "a rule that is not a mapping",
		body:  "rules:\n  implement: yes\n",
		wants: []string{"rules.implement", "line 2", "must be a rule with labels"},
	},
	{
		name:  "rules written as a list",
		body:  "rules:\n  - implement\n",
		wants: []string{"rules", "line 2", "must be a mapping"},
	},
	{
		name:  "no rules",
		body:  "max_parallel_issues: 2\n",
		wants: []string{"rules", "missing", "at least one rule"},
	},
	{
		name:  "rules without a rule",
		body:  "rules: {}\n",
		wants: []string{"rules", "line 1", "at least one rule"},
	},
	{
		name:  "two rules share a name",
		body:  oneRule + strings.TrimPrefix(oneRule, oneAgent+"rules:\n"),
		wants: []string{"rules.implement", "line 14", "duplicate key, first set on line 5"},
	},
	{
		name:  "a rule with an empty name",
		body:  `rules: {"": {labels: {ready: a, running: b, success: c}}}`,
		wants: []string{"rules", "line 1", "a name must not be empty"},
	},
	{
		name:  "a rule without labels",
		body:  oneAgent + "rules:\n  implement:\n    actions:\n      development: {prompt: go}\n",
		wants: []string{"rules.implement.labels", "line 5", "required"},
	},
	{
		name:  "labels that are not a mapping",
		body:  twoRules("ready", reviewLabels),
		wants: []string{"rules.implement.labels", "line 6", "must be a mapping with ready"},
	},
	{
		name:  "a label key no rule has",
		body:  twoRules("{ready: a, running: b, success: c, failure: d, done: e}", reviewLabels),
		wants: []string{"rules.implement.labels.done", "line 6", "unknown key"},
	},
	{
		name: "labels without ready, running and success",
		body: twoRules("{failure: d}", reviewLabels),
		wants: []string{
			"rules.implement.labels.ready", "rules.implement.labels.running", "rules.implement.labels.success", "required",
		},
	},
	{
		// A rule with actions can fail, so it needs a label to fail to.
		name:  "a rule with actions without failure",
		body:  twoRules("{ready: a, running: b, success: c}", reviewLabels),
		wants: []string{"rules.implement.labels.failure", "line 6", "required"},
	},
	{
		name:  "an empty label",
		body:  twoRules(`{ready: a, running: "", success: c, failure: d}`, reviewLabels),
		wants: []string{"rules.implement.labels.running", "line 6", "required"},
	},
	{
		name:  "a rule names a queue that does not exist",
		body:  "queues: {developer: 1}\n" + ruleWith("queue: nope"),
		wants: []string{"rules.implement.queue", "line 7", `queue "nope" does not exist`, "developer, default"},
	},
	{
		name:  "an empty queue",
		body:  ruleWith(`queue: ""`),
		wants: []string{"rules.implement.queue", "line 6", "must not be empty"},
	},
}

// invalidRuleGraphs are rules that would take an item twice, or take back
// what one of them moved (R7).
var invalidRuleGraphs = []rejectCase{
	{
		name: "two rules take the same label in different cases",
		body: twoRules(implementLabels, "{ready: READY, running: sorting, success: sorted, failure: failed}"),
		wants: []string{
			"rules.review.labels.ready", "line 10", `rule "review" takes "ready", as rule "implement" (rules.implement) does`,
			"two rules cannot take the same label",
		},
	},
	{
		name:  "success equals the rule's own ready",
		body:  twoRules("{ready: ready, running: in progress, success: Ready, failure: failed}", reviewLabels),
		wants: []string{"rules.implement.labels.success", "line 6", `"ready" is the rule's own ready label`},
	},
	{
		name: "failure equals the rule's own ready in another case",
		body: twoRules(implementLabels,
			"{ready: ready to review, running: in review, success: done, failure: READY TO REVIEW}"),
		wants: []string{"rules.review.labels.failure", "line 10", "would take the failed item again"},
	},
	{
		name: "running is another rule's ready",
		body: twoRules("{ready: ready, running: Ready To Review, success: done, failure: failed}", reviewLabels),
		wants: []string{
			"rules.implement.labels.running", "line 6",
			`"Ready To Review" is the ready label of rule "review" (rules.review.labels.ready)`,
			`while rule "implement" runs`,
		},
	},
	{
		name:  "running is its own rule's ready",
		body:  twoRules("{ready: ready, running: ready, success: done, failure: failed}", reviewLabels),
		wants: []string{"rules.implement.labels.running", "line 6", `of rule "implement"`},
	},
}

// invalidActions are errors in a rule's actions.
var invalidActions = []rejectCase{
	{
		name:  "actions written as a list",
		body:  oneAgent + "rules:\n  implement:\n    labels: " + implementLabels + "\n    actions:\n      - development\n",
		wants: []string{"rules.implement.actions", "line 8", "must be a mapping"},
	},
	{
		name:  "an action that is not a mapping",
		body:  oneAgent + "rules:\n  implement:\n    labels: " + implementLabels + "\n    actions:\n      development: go\n",
		wants: []string{"rules.implement.actions.development", "line 8", "must be an action with prompt"},
	},
	{
		name:  "action without prompt",
		body:  strings.Replace(oneRule, `        prompt: "Implement {{.Issue.Ref}}"`, "        check:", 1),
		wants: []string{"rules.implement.actions.development.prompt", "line 12", "required"},
	},
	{
		name:  "an action key crew does not know",
		body:  strings.Replace(oneRule, "        prompt:", "        model: opus\n        prompt:", 1),
		wants: []string{"rules.implement.actions.development.model", "line 13", "unknown key"},
	},
	{
		name:  "two actions of a rule share a name",
		body:  oneRule + "      development: {prompt: again}\n",
		wants: []string{"rules.implement.actions.development", "line 14", "duplicate key, first set on line 12"},
	},
	{
		name: "prompt references an unknown issue field",
		body: strings.Replace(oneRule, "{{.Issue.Ref}}", "{{.Issue.Number}}", 1),
		wants: []string{
			"rules.implement.actions.development.prompt", "line 13", `render prompt of action "development"`, "Number",
		},
	},
	{
		name:  "prompt does not parse",
		body:  strings.Replace(oneRule, "{{.Issue.Ref}}", "{{.Issue.Ref", 1),
		wants: []string{"rules.implement.actions.development.prompt", "line 13", `parse prompt of action "development"`},
	},
	{
		name:  "a prompt that reads CREW_BOSS",
		body:  strings.Replace(oneRule, "Implement {{.Issue.Ref}}", "Ask $CREW_BOSS about {{.Issue.Ref}}", 1),
		wants: []string{"rules.implement.actions.development.prompt", "line 13", "CREW_BOSS is now CREW_CODE_OWNERS"},
	},
}
