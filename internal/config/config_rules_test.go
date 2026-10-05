package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// twoRules is a config of two rules, implement and review, of one action
// each, whose labels are a and b, YAML flow mappings on lines 6 and 10.
func twoRules(a, b string) string {
	return oneAgent + `rules:
  implement:
    labels: ` + a + `
    actions:
      development: {prompt: "Implement {{.Issue.Ref}}"}
  review:
    labels: ` + b + `
    actions:
      custom_review: {prompt: "Review {{.Issue.Ref}}"}
`
}

// ruleWith is oneRule with key added to its rule, on line 6.
func ruleWith(key string) string {
	return strings.Replace(oneRule, "  implement:\n", "  implement:\n    "+key+"\n", 1)
}

// The labels of twoRules' implement, and of review, which follows it.
const (
	implementLabels = "{ready: ready, running: in progress, success: ready to review, failure: needs attention}"
	reviewLabels    = "{ready: ready to review, running: in review, success: ready to merge, failure: needs attention}"
)

// ruleLabelCases are rules Load reads, with the labels of each rule.
var ruleLabelCases = []struct {
	name string
	body string
	want []crew.Labels
}{
	{
		name: "any label text, kept as written",
		body: twoRules(implementLabels, reviewLabels),
		want: []crew.Labels{
			{Ready: "ready", Running: "in progress", Success: "ready to review", Failure: "needs attention"},
			{Ready: "ready to review", Running: "in review", Success: "ready to merge", Failure: "needs attention"},
		},
	},
	{
		// A failed review goes back to implement.
		name: "failure is another rule's ready label",
		body: twoRules(implementLabels, "{ready: ready to review, running: in review, success: done, failure: ready}"),
		want: []crew.Labels{
			{Ready: "ready", Running: "in progress", Success: "ready to review", Failure: "needs attention"},
			{Ready: "ready to review", Running: "in review", Success: "done", Failure: "ready"},
		},
	},
	{
		// A label is spelled everywhere as it is first written.
		name: "a label written in two cases takes its first spelling",
		body: twoRules(implementLabels,
			"{ready: Ready To Review, running: in review, success: done, failure: NEEDS attention}"),
		want: []crew.Labels{
			{Ready: "ready", Running: "in progress", Success: "ready to review", Failure: "needs attention"},
			{Ready: "ready to review", Running: "in review", Success: "done", Failure: "needs attention"},
		},
	},
	{
		// The item stays in running when the rule fails.
		name: "failure equals the rule's own running",
		body: twoRules("{ready: ready, running: stuck, success: ready to review, failure: stuck}", reviewLabels),
		want: []crew.Labels{
			{Ready: "ready", Running: "stuck", Success: "ready to review", Failure: "stuck"},
			{Ready: "ready to review", Running: "in review", Success: "ready to merge", Failure: "needs attention"},
		},
	},
	{
		// A rule without actions never fails.
		name: "a rule without actions and without failure",
		body: "rules:\n  promote:\n    labels: {ready: done, running: promoting, success: next}\n",
		want: []crew.Labels{{Ready: "done", Running: "promoting", Success: "next"}},
	},
}

func TestLoadReadsRuleLabels(t *testing.T) {
	tests := ruleLabelCases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := load(t, tt.body).Rules
			got := make([]crew.Labels, len(rules))
			for i, r := range rules {
				got[i] = r.Labels
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("labels = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// Rules and their actions keep file order, which breaks ties when crew
// takes items.
func TestLoadKeepsRulesAndActionsInFileOrder(t *testing.T) {
	cfg := load(t, oneAgent+`rules:
  zeta:
    labels: {ready: z, running: z running, success: z done, failure: z failed}
    actions:
      second: {prompt: "2"}
      first: {prompt: "1"}
  alpha:
    labels: {ready: a, running: a running, success: a done, failure: a failed}
    actions:
      only: {prompt: "a"}
`)
	var got []string
	for _, r := range cfg.Rules {
		for _, a := range r.Actions {
			got = append(got, r.Name+"/"+a.Name)
		}
	}
	if want := []string{"zeta/second", "zeta/first", "alpha/only"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rules/actions = %q, want %q", got, want)
	}
}

func TestLoadGivesEveryRuleWhetherItNotifies(t *testing.T) {
	const promote = "rules:\n  promote:\n    labels: {ready: done, running: promoting, success: next}\n"
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "left out on a rule with actions", body: oneRule, want: true},
		{name: "left out on a rule without actions", body: promote, want: false},
		{name: "off on a rule with actions", body: ruleWith("notify: false"), want: false},
		{
			name: "written on a rule without actions",
			body: strings.Replace(promote, "  promote:\n", "  promote:\n    notify: true\n", 1), want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := load(t, tt.body).Rules[0].Notify; got != tt.want {
				t.Errorf("Notify = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadGivesEveryRuleTheKindItTakes(t *testing.T) {
	tests := []struct {
		name  string
		takes string
		want  crew.Kind
	}{
		{name: "a rule without the key takes issues", takes: "", want: crew.KindIssue},
		{name: "takes: issues", takes: "takes: issues", want: crew.KindIssue},
		{name: "takes: pull_requests", takes: "takes: pull_requests", want: crew.KindPullRequest},
		{name: "takes with no value takes issues", takes: "takes:", want: crew.KindIssue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := load(t, ruleWith(tt.takes)).Rules[0].Takes; got != tt.want {
				t.Errorf("Takes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadReadsActionCheck(t *testing.T) {
	withCheck := func(script string) string {
		return "checks:\n  branch: " + script + "\n" +
			strings.Replace(oneRule, "        prompt:", "        check: branch\n        prompt:", 1)
	}
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "left out", body: oneRule, want: ""},
		{name: "a command", body: withCheck(`"test -n \"$CREW_BRANCH\""`), want: `test -n "$CREW_BRANCH"`},
		// A check is a shell command, never a template: braces stay as written.
		{name: "not a template", body: withCheck(`"echo '{{.Issue.Title}}'"`), want: "echo '{{.Issue.Title}}'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			if checks := load(t, tt.body).Rules[0].Actions[0].Checks; len(checks) > 0 {
				got = checks[0].Script
			}
			if got != tt.want {
				t.Errorf("check script = %q, want %q", got, tt.want)
			}
		})
	}
}
