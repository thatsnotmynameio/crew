package config_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// routeRule is a config of the top-level keys in head, written as is,
// oneAgent and one rule, implement, of one session, lfg, whose on is the
// flow mapping's entries in on, after a comma, and whose routes are routes,
// indented under routes. Without head, its session is on line 8 and its
// first route on line 10.
func routeRule(head, on, routes string) string {
	return head + oneAgent + `rules:
  implement:
    labels: {ready: ready, running: in progress}
    actions:
      - {name: lfg, prompt: go` + on + `}
    routes:
` + routes
}

// failedRoutes are the routes passed and failed of routeRule, each a
// single move.
const failedRoutes = "      passed: done\n      failed: failed\n"

// R13, R14: a route is a label, a single move, or a list of steps: report,
// close, move, comment, and shell actions named alone or as a mapping of
// one empty key.
func TestLoadReadsEveryStepOfARoute(t *testing.T) {
	routes := failedRoutes + `      blocked:
        - report
        - comment: "{{.Action}} stopped as {{.Verdict}}. Its log is {{.Log}}."
        - lint
        - lint:
        - close
`
	cfg := load(t, routeRule("actions:\n  lint: make lint\n", ", on: {blocked: blocked}", routes))
	got := cfg.Rules[0].Routes
	want := []crew.Route{passedRoute("done"), {Name: crew.FailedRoute, Steps: []crew.Step{crew.MoveStep{To: "failed"}}}}
	if !reflect.DeepEqual(got[:2], want) {
		t.Errorf("routes passed and failed = %+v, want %+v", got[:2], want)
	}
	blocked := got[2]
	lint := crew.ShellStep{Name: "lint", Shell: crew.ShellSpec{Script: "make lint"}}
	if blocked.Name != "blocked" || len(blocked.Steps) != 5 || blocked.Steps[0] != (crew.ReportStep{}) ||
		!reflect.DeepEqual(blocked.Steps[2], lint) || !reflect.DeepEqual(blocked.Steps[3], lint) ||
		blocked.Steps[4] != (crew.CloseStep{}) {
		t.Fatalf("route blocked = %+v, want report, a comment, lint twice and close", blocked)
	}
	comment, ok := blocked.Steps[1].(crew.CommentStep)
	if !ok {
		t.Fatalf("step 1 = %T, want a comment", blocked.Steps[1])
	}
	text, err := comment.Template.Render(crew.CommentData{Action: "lfg", Verdict: "blocked", Log: ".crew/logs/x.log"})
	if want := "lfg stopped as blocked. Its log is .crew/logs/x.log."; err != nil || text != want {
		t.Errorf("comment = %q, %v; want %q", text, err, want)
	}
}

// A label written in two cases takes its first spelling, in a route's move
// as in a rule's labels.
func TestLoadSpellsARoutesLabelAsItIsFirstWritten(t *testing.T) {
	cfg := load(t, oneRule+`  review:
    labels: {ready: Ready To Review, running: reviewing}
    routes:
      passed: NEEDS ATTENTION
`)
	if got := cfg.Rules[1].Labels.Ready; got != "ready to review" {
		t.Errorf("review's ready = %q, want ready to review", got)
	}
	if got, want := cfg.Rules[1].Routes, []crew.Route{passedRoute("needs attention")}; !reflect.DeepEqual(got, want) {
		t.Errorf("review's routes = %+v, want %+v", got, want)
	}
}

func TestLoadRejectsInvalidRoutes(t *testing.T) {
	testRejects(t, invalidRoutes)
}

// invalidRoutes are errors in one route or one of its steps.
var invalidRoutes = []rejectCase{
	{
		name:  "routes written as a list",
		body:  routeRule("", "", "      - passed\n"),
		wants: []string{"rules.implement.routes", "line 10", "must be a mapping"},
	},
	{
		name:  "a route that is a mapping",
		body:  routeRule("", "", failedRoutes+"      other: {move: x}\n"),
		wants: []string{"rules.implement.routes.other", "line 12", "must be a label to move the item to, or a list of steps"},
	},
	{
		name:  "an empty label",
		body:  routeRule("", "", "      passed: \"\"\n      failed: failed\n"),
		wants: []string{"rules.implement.routes.passed", "line 10", "required"},
	},
	{
		name:  "a route named next",
		body:  routeRule("", ", on: {failed: next}", failedRoutes+"      next: elsewhere\n"),
		wants: []string{"rules.implement.routes.next", "line 12", `"next" names the next action, not a route`},
	},
	{
		name:  "a route's name that is not a verdict's name",
		body:  routeRule("", "", failedRoutes+"      Needs Person: elsewhere\n"),
		wants: []string{"rules.implement.routes.Needs Person", "line 12", `route "Needs Person"`},
	},
	{
		name:  "a step that names no action",
		body:  routeRule("", "", "      passed: done\n      failed: [report, vet, move: failed]\n"),
		wants: []string{"rules.implement.routes.failed[1]", "line 11", `action "vet" does not exist; actions declares none`},
	},
	{
		name: "a step's reference with parameters",
		body: routeRule("actions:\n  lint: make lint\n", "",
			"      passed: done\n      failed: [lint: {fix: true}, move: failed]\n"),
		wants: []string{"rules.implement.routes.failed[0].lint", "line 13", "takes no parameters"},
	},
	{
		name:  "a step of two keys",
		body:  routeRule("", "", "      passed: done\n      failed: [{move: failed, comment: hi}]\n"),
		wants: []string{"rules.implement.routes.failed[0]", "line 11", "must be report, close, move: <label>"},
	},
	{
		name:  "a step that is a list",
		body:  routeRule("", "", "      passed: done\n      failed: [[report], move: failed]\n"),
		wants: []string{"rules.implement.routes.failed[0]", "line 11", "must be report, close, move: <label>"},
	},
	{
		name:  "a move without its label",
		body:  routeRule("", "", "      passed: done\n      failed: [move: ]\n"),
		wants: []string{"rules.implement.routes.failed[0].move", "line 11", "required"},
	},
	{
		name:  "a comment that does not parse",
		body:  routeRule("", "", "      passed: done\n      failed: [comment: \"{{.Issue.Ref\", move: failed]\n"),
		wants: []string{"rules.implement.routes.failed[0].comment", "line 11", `parse comment of route "failed"`},
	},
	{
		// Covers AE12: a comment never quotes what a session printed.
		name: "a comment that names the reason",
		body: routeRule("", "",
			"      passed: done\n      failed:\n        - comment: \"{{.Reason}}\"\n        - move: failed\n"),
		wants: []string{"rules.implement.routes.failed[0].comment", "line 12", `render comment of route "failed"`, "Reason"},
	},
}
