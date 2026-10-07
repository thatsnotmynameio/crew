package config_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// R12: a rule with actions declares passed and failed; a rule without
// actions declares only passed, which runs as soon as crew takes the item.
func TestLoadARuleWithoutActionsAndOnlyPassed(t *testing.T) {
	cfg := load(t, "rules:\n  promote:\n    labels: {ready: done, running: promoting}\n    routes: {passed: next}\n")
	r := cfg.Rules[0]
	if len(r.Actions) != 0 || len(r.Routes) != 1 || r.Routes[0].Name != crew.PassedRoute || cfg.Notify["promote"] {
		t.Errorf("rule = %+v, notify %v; want no action, passed alone, no notify", r, cfg.Notify["promote"])
	}
}

// R32: a declared route that only an on: leads to loads; passed and failed
// need none.
func TestLoadARouteAnOnLeadsTo(t *testing.T) {
	cfg := load(t, routeRule("", ", on: {blocked: blocked}", failedRoutes+"      blocked: blocked\n"))
	if got := len(cfg.Rules[0].Routes); got != 3 {
		t.Errorf("routes = %+v, want passed, failed and blocked", cfg.Rules[0].Routes)
	}
}

func TestLoadRejectsRoutesNoRunCanTake(t *testing.T) {
	testRejects(t, invalidRouteGraphs)
}

func TestLoadRejectsRulesThatTakeAnItemTwice(t *testing.T) {
	testRejects(t, invalidRuleGraphs)
}

// invalidRouteGraphs are routes and on: entries that do not fit together
// (R12, R15, R32). routeRule's session is on line 8, its routes on line 9
// and its first route on line 10.
var invalidRouteGraphs = []rejectCase{
	{
		// Covers AE8.
		name:  "a route ending with comment",
		body:  routeRule("", "", "      passed: done\n      failed: [report, comment: failed]\n"),
		wants: []string{"rules.implement.routes.failed", "line 11", "must end with move or close"},
	},
	{
		name:  "an empty route",
		body:  routeRule("", "", "      passed: done\n      failed: []\n"),
		wants: []string{"rules.implement.routes.failed", "line 11", "must end with move or close"},
	},
	{
		name:  "a move before the last step",
		body:  routeRule("", "", "      passed: done\n      failed: [move: failed, report, close]\n"),
		wants: []string{"rules.implement.routes.failed[0]", "line 11", "move and close end a route"},
	},
	{
		name:  "a close before the last step",
		body:  routeRule("", "", "      passed: done\n      failed: [close, move: failed]\n"),
		wants: []string{"rules.implement.routes.failed[0]", "line 11", "move and close end a route"},
	},
	{
		// Covers AE8.
		name: "an on that names an undeclared route",
		body: routeRule("", ", on: {blocked: blocked}", failedRoutes),
		wants: []string{
			"rules.implement.actions[0].on.blocked", "line 8",
			`route "blocked" is not one of rule "implement"'s routes: passed, failed`,
		},
	},
	{
		// Covers AE8.
		name:  "a route nothing leads to",
		body:  routeRule("", "", failedRoutes+"      blocked: blocked\n"),
		wants: []string{"rules.implement.routes.blocked", "line 12", `no action's on leads to route "blocked"`},
	},
	{
		name:  "a rule with actions without failed",
		body:  routeRule("", "", "      passed: done\n"),
		wants: []string{"rules.implement.routes", "line 10", "must declare failed"},
	},
	{
		name:  "a rule without passed",
		body:  routeRule("", "", "      failed: failed\n"),
		wants: []string{"rules.implement.routes", "line 10", "must declare passed"},
	},
	{
		name:  "a rule without routes",
		body:  "rules:\n  promote:\n    labels: {ready: done, running: promoting}\n",
		wants: []string{"rules.promote.routes", "line 2", "required"},
	},
	{
		name: "a rule without actions with failed",
		body: "rules:\n  promote:\n    labels: {ready: done, running: promoting}\n" +
			"    routes: {passed: next, failed: oops}\n",
		wants: []string{"rules.promote.routes.failed", "line 4", "a rule without actions ends only through passed"},
	},
	{
		name: "two actions share a name",
		body: sequenceRule(oneAgent,
			"      - {name: lfg, prompt: go}\n      - {agent: claude, name: lfg, prompt: again}\n", ""),
		wants: []string{
			"rules.implement.actions[1]", "line 9", `action "lfg" is also rules.implement.actions[0] (line 8)`,
			"set name: on one of them",
		},
	},
	{
		name: "a session named after a shell action it follows",
		body: sequenceRule("actions:\n  claude: ./claude\n"+oneAgent, "      - claude\n      - prompt: go\n", ""),
		wants: []string{
			"rules.implement.actions[1]", "line 11", `action "claude" is also rules.implement.actions[0] (line 10)`,
		},
	},
	{
		name:  "a move to the rule's own ready label",
		body:  routeRule("", "", "      passed: Ready\n      failed: failed\n"),
		wants: []string{"rules.implement.routes.passed", "line 10", `"ready" is the rule's own ready label`},
	},
	{
		name: "a move to the rule's own running label",
		body: routeRule("", "", "      passed: done\n      failed: [report, move: In Progress]\n"),
		wants: []string{
			"rules.implement.routes.failed[1].move", "line 11", `"in progress" is the running label of rule "implement"`,
		},
	},
	{
		name: "a move to another rule's running label",
		body: oneRule + "  review:\n    labels: {ready: review, running: reviewing}\n    routes: {passed: in progress}\n",
		wants: []string{
			"rules.review.routes.passed", "line 19",
			`"in progress" is the running label of rule "implement" (rules.implement.labels.running)`,
		},
	},
}

// invalidRuleGraphs are rules that would take an item twice (R7).
var invalidRuleGraphs = []rejectCase{
	{
		name: "two rules take the same label in different cases",
		body: oneRule + "  review:\n    labels: {ready: READY, running: sorting}\n    routes: {passed: sorted}\n",
		wants: []string{
			"rules.review.labels.ready", "line 18", `rule "review" takes "ready", as rule "implement" (rules.implement) does`,
			"two rules cannot take the same label",
		},
	},
	{
		name: "running is another rule's ready",
		body: oneRule + "  review:\n    labels: {ready: done, running: Ready}\n    routes: {passed: sorted}\n",
		wants: []string{
			"rules.review.labels.running", "line 18",
			`"ready" is the ready label of rule "implement" (rules.implement.labels.ready)`,
			`while rule "review" runs`,
		},
	},
	{
		name:  "running is its own rule's ready",
		body:  "rules:\n  promote:\n    labels: {ready: done, running: done}\n    routes: {passed: next}\n",
		wants: []string{"rules.promote.labels.running", "line 3", `of rule "promote"`},
	},
}
