package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// twoRules is a config of two rules, implement and review, of one session
// each, whose labels and routes are a and b, each a labels key and a routes
// key, YAML flow mappings on lines 6 and 7, and 11 and 12.
func twoRules(a, b string) string {
	return oneAgent + `rules:
  implement:
    ` + a + `
    actions:
      - {name: development, prompt: "Implement {{.Issue.Ref}}"}
  review:
    ` + b + `
    actions:
      - {name: custom_review, prompt: "Review {{.Issue.Ref}}"}
`
}

// ruleWith is oneRule with key added to its rule, on line 6.
func ruleWith(key string) string {
	return strings.Replace(oneRule, "  implement:\n", "  implement:\n    "+key+"\n", 1)
}

// The labels and routes of twoRules' implement, and of review, which
// follows it.
const (
	implementRule = "labels: {ready: ready, running: in progress}\n" +
		"    routes: {passed: ready to review, failed: needs attention}"
	reviewRule = "labels: {ready: ready to review, running: in review}\n" +
		"    routes: {passed: ready to merge, failed: needs attention}"
)

// ruleLabelCases are rules Load reads, with the states of each rule: its
// ready and running labels, then the labels its routes move to.
var ruleLabelCases = []struct {
	name string
	body string
	want [][]crew.State
}{
	{
		name: "any label text, kept as written",
		body: twoRules(implementRule, reviewRule),
		want: [][]crew.State{
			{"ready", "in progress", "ready to review", "needs attention"},
			{"ready to review", "in review", "ready to merge", "needs attention"},
		},
	},
	{
		// A failed review goes back to implement.
		name: "failed moves to another rule's ready label",
		body: twoRules(implementRule, "labels: {ready: ready to review, running: in review}\n"+
			"    routes: {passed: done, failed: ready}"),
		want: [][]crew.State{
			{"ready", "in progress", "ready to review", "needs attention"},
			{"ready to review", "in review", "done", "ready"},
		},
	},
	{
		// A label is spelled everywhere as it is first written.
		name: "a label written in two cases takes its first spelling",
		body: twoRules(implementRule, "labels: {ready: Ready To Review, running: in review}\n"+
			"    routes: {passed: done, failed: [report, move: NEEDS attention]}"),
		want: [][]crew.State{
			{"ready", "in progress", "ready to review", "needs attention"},
			{"ready to review", "in review", "done", "needs attention"},
		},
	},
	{
		name: "a rule without actions moves through passed",
		body: "rules:\n  promote:\n    labels: {ready: done, running: promoting}\n    routes: {passed: next}\n",
		want: [][]crew.State{{"done", "promoting", "next"}},
	},
}

func TestLoadReadsRuleLabels(t *testing.T) {
	tests := ruleLabelCases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := load(t, tt.body).Rules
			got := make([][]crew.State, len(rules))
			for i, r := range rules {
				got[i] = crew.RuleStates([]crew.Rule{r})
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("states = %q\nwant %q", got, tt.want)
			}
		})
	}
}

// Rules and their actions keep file order, which breaks ties when crew
// takes items and orders the actions of a rule.
func TestLoadKeepsRulesAndActionsInFileOrder(t *testing.T) {
	cfg := load(t, oneAgent+`rules:
  zeta:
    labels: {ready: z, running: z running}
    actions:
      - {name: second, prompt: "2"}
      - {name: first, prompt: "1"}
    routes: {passed: z done, failed: z failed}
  alpha:
    labels: {ready: a, running: a running}
    actions:
      - {name: only, prompt: "a"}
    routes: {passed: a done, failed: a failed}
`)
	var got []string
	for _, r := range cfg.Rules {
		for _, a := range r.Actions {
			got = append(got, string(r.Name)+"/"+string(a.Name))
		}
	}
	if want := []string{"zeta/second", "zeta/first", "alpha/only"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rules/actions = %q, want %q", got, want)
	}
}

func TestLoadGivesEveryRuleWhetherItNotifies(t *testing.T) {
	const promote = "rules:\n  promote:\n    labels: {ready: done, running: promoting}\n    routes: {passed: next}\n"
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
			cfg := load(t, tt.body)
			if got, ok := cfg.Notify[cfg.Rules[0].Name]; !ok || got != tt.want || len(cfg.Notify) != len(cfg.Rules) {
				t.Errorf("Notify = %v, want %s %v and one entry per rule", cfg.Notify, cfg.Rules[0].Name, tt.want)
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
