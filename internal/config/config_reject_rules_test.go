package config_test

import (
	"strings"
	"testing"
)

func TestLoadRejectsInvalidRules(t *testing.T) {
	testRejects(t, invalidRules)
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
		body:  strings.Replace(ruleWith("takes: prs"), "      running: in progress\n", "", 1),
		wants: []string{"rules.implement.takes", `"prs"`, "rules.implement.labels.running", "required"},
	},
	{
		name:  "a rule that is not a mapping",
		body:  "rules:\n  implement: yes\n",
		wants: []string{"rules.implement", "line 2", "must be a rule with labels and routes"},
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
		wants: []string{"rules.implement", "line 17", "duplicate key, first set on line 5"},
	},
	{
		name:  "a rule with an empty name",
		body:  `rules: {"": {labels: {ready: a, running: b}, routes: {passed: c}}}`,
		wants: []string{"rules", "line 1", "a name must not be empty"},
	},
	{
		name:  "a rule without labels",
		body:  "rules:\n  implement:\n    routes: {passed: done}\n",
		wants: []string{"rules.implement.labels", "line 2", "required"},
	},
	{
		name:  "labels that are not a mapping",
		body:  "rules:\n  implement:\n    labels: ready\n    routes: {passed: done}\n",
		wants: []string{"rules.implement.labels", "line 3", "must be a mapping with ready and running"},
	},
	{
		// R1, R33: a rule's success and failure are its routes now.
		name:  "a success label",
		body:  "rules:\n  implement:\n    labels: {ready: a, running: b, success: c}\n    routes: {passed: done}\n",
		wants: []string{"rules.implement.labels.success", "line 3", "unknown key"},
	},
	{
		name: "labels without ready and running",
		body: "rules:\n  implement:\n    labels: {}\n    routes: {passed: done}\n",
		wants: []string{
			"rules.implement.labels.ready", "rules.implement.labels.running", "required",
		},
	},
	{
		name:  "an empty label",
		body:  "rules:\n  implement:\n    labels: {ready: a, running: \"\"}\n    routes: {passed: done}\n",
		wants: []string{"rules.implement.labels.running", "line 3", "required"},
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
