package config_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// answerer writes questions: with octocat as the answerer, on line 1.
const answerer = "questions: {answerer: octocat}\n"

// askRoute is a route ask of one question step, unsure, whose text is text
// and whose return label is ret, after passed and failed, for routeRule.
// With answerer as routeRule's head, ask is on line 13 and its step on
// line 14.
func askRoute(text, ret string) string {
	return failedRoutes + "      ask:\n        - question: {id: unsure, text: \"" + text + "\", return: \"" + ret + "\"}\n"
}

// askOf returns the question step holds, and fails t when it holds none.
func askOf(t *testing.T, step crew.Step) crew.Ask {
	t.Helper()
	q, ok := step.(crew.QuestionStep)
	if !ok {
		t.Fatalf("step = %T, want a question", step)
	}
	return q.Question
}

// checkAsk checks that q has the id, text and return label given.
func checkAsk(t *testing.T, q crew.Ask, id crew.QuestionID, text string, ret crew.State) {
	t.Helper()
	if q.ID != id || q.Text.Text() != text || q.Return != ret {
		t.Errorf("question = %s %q to %s, want %s %q to %s", q.ID, q.Text.Text(), q.Return, id, text, ret)
	}
}

// R1, KTD2: a question step is written last, and crew moves the item to
// crew:question after it.
func TestLoadReadsAQuestionStep(t *testing.T) {
	cfg := load(t, routeRule(answerer, ", on: {unsure: ask}", askRoute("Is {{.Issue.Ref}} done?", "ready")))
	ask, ok := cfg.Rules[2].Route("ask")
	if !ok || len(ask.Steps) != 2 {
		t.Fatalf("route ask = %+v, want a question and a move", ask)
	}
	checkAsk(t, askOf(t, ask.Steps[0]), "unsure", "Is {{.Issue.Ref}} done?", "ready")
	if ask.Steps[1] != (crew.MoveStep{To: "crew:question"}) {
		t.Errorf("last step = %+v, want the move to crew:question", ask.Steps[1])
	}
}

// KTD3: a question action sends asked to a route of its name, which crew
// adds: the question, then the move to crew:question.
func TestLoadReadsAQuestionAction(t *testing.T) {
	items := "      - prompt: go\n" +
		"      - question: {id: blocks, text: \"Does {{.Issue.Ref}} block #281?\", return: ready}\n" +
		"        name: confirm\n"
	r := load(t, sequenceRule(answerer+oneAgent, items, "")).Rules[2]
	a := r.Action("confirm")
	spec, ok := a.Kind.(crew.QuestionSpec)
	if !ok {
		t.Fatalf("action confirm = %+v, want a question", a)
	}
	checkAsk(t, spec.Question, "blocks", "Does {{.Issue.Ref}} block #281?", "ready")
	if want := (crew.On{crew.Asked: crew.ToRoute{Route: "confirm"}}); !reflect.DeepEqual(a.On, want) {
		t.Errorf("on = %+v, want %+v", a.On, want)
	}
	route, ok := r.Route("confirm")
	if !ok || len(route.Steps) != 2 || route.Steps[1] != (crew.MoveStep{To: "crew:question"}) {
		t.Fatalf("route confirm = %+v, want the question and the move to crew:question", route)
	}
	checkAsk(t, askOf(t, route.Steps[0]), "blocks", "Does {{.Issue.Ref}} block #281?", "ready")
}

// KTD3: a question action without name is called question, and so is its
// route.
func TestLoadNamesAQuestionActionQuestion(t *testing.T) {
	r := load(t, sequenceRule(answerer+oneAgent, "      - question: {id: blocks, text: hi, return: ready}\n", "")).Rules[2]
	if _, ok := r.Action("question").Kind.(crew.QuestionSpec); !ok {
		t.Errorf("actions = %+v, want a question called question", r.Actions)
	}
	if _, ok := r.Route("question"); !ok {
		t.Errorf("routes = %+v, want a route called question", r.Routes)
	}
}

// R3: a return label is compared ignoring case and takes the spelling of
// the ready label it names.
func TestLoadSpellsAReturnLabelAsTheReadyLabel(t *testing.T) {
	cfg := load(t, routeRule(answerer, ", on: {unsure: ask}", askRoute("Is it?", "READY")))
	ask, _ := cfg.Rules[2].Route("ask")
	if got := askOf(t, ask.Steps[0]).Return; got != "ready" {
		t.Errorf("return = %q, want ready", got)
	}
}

// KTD7: with questions:, the rules start with crew's question rule, which
// takes crew:question, delegates the question and moves the item to
// crew:question:waiting answer, without actions and without notifying.
func TestLoadAddsTheQuestionRuleFirst(t *testing.T) {
	cfg := load(t, answerer+oneRule)
	want := crew.Rule{
		Name: "question", Queue: crew.Queue{Name: "default", Slots: 2},
		Labels: crew.Labels{Ready: "crew:question", Running: "crew:question:in progress"},
		Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{
			crew.DelegateStep{}, crew.MoveStep{To: "crew:question:waiting answer"},
		}}},
	}
	if len(cfg.Rules) != 3 || !reflect.DeepEqual(cfg.Rules[0], want) || cfg.Rules[2].Name != "implement" {
		t.Fatalf("rules = %+v\nwant %+v, then answered, then implement", cfg.Rules, want)
	}
	if notify, ok := cfg.Notify["question"]; !ok || notify {
		t.Errorf("question rule's notify = %v (set %v), want false", notify, ok)
	}
	if cfg.Questions == nil || cfg.Questions.Answerer != "octocat" {
		t.Errorf("Questions = %+v, want octocat as the answerer", cfg.Questions)
	}
}

// KTD1, KTD4 of #311: with questions:, crew's answered rule comes right
// after the question rule, in questions.queue. It takes crew:answered,
// runs its one check, answer, in crew:answered:in progress, returns the
// item on passed and fails it to crew:answered:failed with a report, and
// does not notify.
func TestLoadAddsTheAnsweredRuleAfterTheQuestionRule(t *testing.T) {
	cfg := load(t, "max_parallel_issues: 3\nqueues: {clerk: 1}\n"+
		"questions: {answerer: octocat, queue: clerk}\n"+oneRule)
	want := crew.Rule{
		Name: "answered", Queue: crew.Queue{Name: "clerk", Slots: 1},
		Labels:  crew.Labels{Ready: "crew:answered", Running: "crew:answered:in progress"},
		Actions: []crew.Action{{Name: "answer", Kind: crew.ReturnSpec{}}},
		Routes: []crew.Route{
			{Name: crew.PassedRoute, Steps: []crew.Step{crew.ReturnStep{}}},
			{Name: crew.FailedRoute, Steps: []crew.Step{crew.ReportStep{}, crew.MoveStep{To: "crew:answered:failed"}}},
		},
	}
	names := make([]crew.RuleName, len(cfg.Rules))
	for i, r := range cfg.Rules {
		names[i] = r.Name
	}
	if wantNames := []crew.RuleName{"question", "answered", "implement"}; !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("rules = %q, want %q", names, wantNames)
	}
	if !reflect.DeepEqual(cfg.Rules[1], want) {
		t.Errorf("answered rule = %+v\nwant %+v", cfg.Rules[1], want)
	}
	if notify, ok := cfg.Notify["answered"]; !ok || notify {
		t.Errorf("answered rule's notify = %v (set %v), want false", notify, ok)
	}
}

// KTD6: without questions:, crew adds no rule.
func TestLoadWithoutQuestionsAddsNoRule(t *testing.T) {
	cfg := load(t, oneRule)
	if len(cfg.Rules) != 1 || cfg.Questions != nil {
		t.Errorf("rules = %+v, Questions = %+v; want implement alone and none", cfg.Rules, cfg.Questions)
	}
}

// KTD6: the answerer is a user or an App, and the question rule runs in
// the queue questions names.
func TestLoadReadsQuestions(t *testing.T) {
	cfg := load(t, "max_parallel_issues: 3\nqueues: {clerk: 1}\n"+
		"questions: {answerer: \"claude[bot]\", queue: clerk}\n"+oneRule)
	clerk := crew.Queue{Name: "clerk", Slots: 1}
	if want := (config.Questions{Answerer: "claude[bot]", Queue: clerk}); cfg.Questions == nil || *cfg.Questions != want {
		t.Errorf("Questions = %+v, want %+v", cfg.Questions, want)
	}
	if cfg.Rules[0].Queue != clerk {
		t.Errorf("question rule's queue = %+v, want %+v", cfg.Rules[0].Queue, clerk)
	}
}

// KTD7: the question rule's name and labels are anyone's without
// questions:.
func TestLoadWithoutQuestionsTakesTheQuestionRulesNameAndLabels(t *testing.T) {
	cfg := load(t, "rules:\n  question:\n    labels: {ready: crew:question, running: crew:question:waiting answer}\n"+
		"    routes: {passed: crew:question:in progress}\n")
	if len(cfg.Rules) != 1 || cfg.Rules[0].Name != "question" {
		t.Errorf("rules = %+v, want the file's rule question alone", cfg.Rules)
	}
}

func TestLoadRejectsInvalidQuestions(t *testing.T) {
	testRejects(t, invalidQuestions)
}

func TestLoadRejectsInvalidQuestionSteps(t *testing.T) {
	testRejects(t, invalidQuestionSteps)
}

func TestLoadRejectsInvalidQuestionActions(t *testing.T) {
	testRejects(t, invalidQuestionActions)
}

// KTD1 of #311: the answered rule's name and labels are anyone's without
// questions:.
func TestLoadWithoutQuestionsTakesTheAnsweredRulesNameAndLabels(t *testing.T) {
	cfg := load(t, "rules:\n  answered:\n    labels: {ready: crew:answered, running: crew:answered:in progress}\n"+
		"    routes: {passed: crew:answered:failed}\n")
	if len(cfg.Rules) != 1 || cfg.Rules[0].Name != "answered" {
		t.Errorf("rules = %+v, want the file's rule answered alone", cfg.Rules)
	}
}

// invalidQuestions are errors in questions:, and rules that take the name
// or labels of its rules.
var invalidQuestions = []rejectCase{
	{
		name:  "questions written as a list",
		body:  "questions: [octocat]\n" + oneRule,
		wants: []string{".crew/config.yaml: questions (line 1)", "must be a mapping"},
	},
	{
		name:  "no answerer",
		body:  "questions: {queue: default}\n" + oneRule,
		wants: []string{".crew/config.yaml: questions.answerer (line 1)", "required"},
	},
	{
		name:  "github-actions[bot] as the answerer, in any case",
		body:  "questions: {answerer: \"GitHub-Actions[bot]\"}\n" + oneRule,
		wants: []string{"questions.answerer (line 1)", "github-actions[bot] never answers"},
	},
	{
		name:  "a queue that does not exist",
		body:  "questions: {answerer: octocat, queue: clerk}\n" + oneRule,
		wants: []string{"questions.queue (line 1)", `queue "clerk" does not exist`},
	},
	{
		name:  "a rule named question",
		body:  answerer + "rules:\n  question:\n    labels: {ready: q, running: qr}\n    routes: {passed: done}\n",
		wants: []string{"rules.question (line 3)", "crew's question rule"},
	},
	{
		name: "a rule that takes the waiting label, in any case",
		body: answerer + "rules:\n  wait:\n    labels:\n      ready: Crew:Question:Waiting Answer\n      running: x\n" +
			"    routes: {passed: done}\n",
		wants: []string{"rules.wait.labels.ready (line 5)", "crew's question rule"},
	},
	{
		name: "a rule that runs in the question rule's running label",
		body: answerer + "rules:\n  wait:\n    labels: {ready: x, running: crew:question:in progress}\n" +
			"    routes: {passed: done}\n",
		wants: []string{"rules.wait.labels.running (line 4)", "crew's question rule"},
	},
	{
		name:  "a written move to crew:question",
		body:  routeRule(answerer, "", "      passed: crew:question\n      failed: failed\n"),
		wants: []string{"rules.implement.routes.passed (line 11)", "crew's question rule"},
	},
	{
		name:  "a rule named answered",
		body:  answerer + "rules:\n  answered:\n    labels: {ready: a, running: ar}\n    routes: {passed: done}\n",
		wants: []string{"rules.answered (line 3)", "crew's answered rule"},
	},
	{
		name: "a rule that takes crew:answered, in any case",
		body: answerer + "rules:\n  wait:\n    labels:\n      ready: Crew:Answered\n      running: x\n" +
			"    routes: {passed: done}\n",
		wants: []string{"rules.wait.labels.ready (line 5)", "crew's answered rule"},
	},
	{
		name: "a rule that runs in the answered rule's running label, in any case",
		body: answerer + "rules:\n  wait:\n    labels: {ready: x, running: CREW:ANSWERED:IN PROGRESS}\n" +
			"    routes: {passed: done}\n",
		wants: []string{"rules.wait.labels.running (line 4)", "crew's answered rule"},
	},
	{
		name:  "a written move to crew:answered:failed, in any case",
		body:  routeRule(answerer, "", "      passed: done\n      failed: crew:Answered:Failed\n"),
		wants: []string{"rules.implement.routes.failed (line 12)", "crew's answered rule"},
	},
}

// invalidQuestionSteps are errors in a question step. With answerer as
// routeRule's head, the route ask is on line 13 and its first step on
// line 14.
var invalidQuestionSteps = []rejectCase{
	{
		// Covers AE6.
		name: "a return label that is no rule's ready label",
		body: routeRule(answerer, ", on: {unsure: ask}", failedRoutes+
			"      ask:\n        - question:\n            id: unsure\n            text: Is it?\n"+
			"            return: crew:nowhere\n"),
		wants: []string{
			".crew/config.yaml: rules.implement.routes.ask[0].question.return (line 17)",
			`"crew:nowhere" is not the ready label of a rule`,
		},
	},
	{
		name:  "a return label to crew:question",
		body:  routeRule(answerer, ", on: {unsure: ask}", askRoute("Is it?", "crew:question")),
		wants: []string{"rules.implement.routes.ask[0].question.return (line 14)", "not the ready label of a rule"},
	},
	{
		// KTD4 of #311.
		name: "a return label that is the ready label of a rule that takes pull requests",
		body: routeRule(answerer, ", on: {unsure: ask}", failedRoutes+
			"      ask:\n        - question:\n            id: unsure\n            text: Is it?\n"+
			"            return: review\n") +
			"  review:\n    takes: pull_requests\n    labels: {ready: review, running: reviewing}\n" +
			"    routes: {passed: reviewed}\n",
		wants: []string{
			".crew/config.yaml: rules.implement.routes.ask[0].question.return (line 17)",
			`"review" is the ready label of rule "review", which takes pull requests`,
		},
	},
	{
		name: "a step after the question",
		body: routeRule(answerer, ", on: {unsure: ask}", askRoute("Is it?", "ready")+
			"        - move: elsewhere\n"),
		wants: []string{"rules.implement.routes.ask[1].move (line 15)", "a question ends its route"},
	},
	{
		name: "an id that is not a name",
		body: routeRule(answerer, ", on: {unsure: ask}", failedRoutes+
			"      ask:\n        - question: {id: two words, text: hi, return: ready}\n"),
		wants: []string{"rules.implement.routes.ask[0].question.id (line 14)", `question id "two words"`},
	},
	{
		name:  "a text that names the reason",
		body:  routeRule(answerer, ", on: {unsure: ask}", askRoute("{{.Reason}}", "ready")),
		wants: []string{"rules.implement.routes.ask[0].question.text (line 14)", "Reason"},
	},
	{
		name: "a question without its text",
		body: routeRule(answerer, ", on: {unsure: ask}", failedRoutes+
			"      ask:\n        - question: {id: unsure, return: ready}\n"),
		wants: []string{"rules.implement.routes.ask[0].question.text (line 14)", "required"},
	},
	{
		name:  "a question that is not a mapping",
		body:  routeRule(answerer, ", on: {unsure: ask}", failedRoutes+"      ask:\n        - question: unsure\n"),
		wants: []string{"rules.implement.routes.ask[0].question (line 14)", "must be a mapping with id, text and return"},
	},
	{
		name: "an unknown key in a question",
		body: routeRule(answerer, ", on: {unsure: ask}", failedRoutes+
			"      ask:\n        - question: {id: unsure, text: hi, return: ready, to: octocat}\n"),
		wants: []string{"rules.implement.routes.ask[0].question.to (line 14)", "unknown key"},
	},
	{
		name:  "a question without questions",
		body:  routeRule("", ", on: {unsure: ask}", askRoute("Is it?", "ready")),
		wants: []string{"rules.implement.routes.ask[0] (line 13)", "questions.answerer", "missing"},
	},
	{
		name: "a question in a rule that takes pull requests",
		body: answerer + "rules:\n  review:\n    takes: pull_requests\n    labels: {ready: review, running: reviewing}\n" +
			"    routes:\n      passed:\n        - question: {id: a, text: hi, return: review}\n",
		wants: []string{"rules.review.routes.passed[0] (line 8)", "takes pull requests"},
	},
}

// invalidQuestionActions are errors in a question action. With answerer
// and oneAgent as sequenceRule's head, the first item is on line 9.
var invalidQuestionActions = []rejectCase{
	{
		// Covers AE6.
		name: "a return label that is no rule's ready label",
		body: sequenceRule(answerer+oneAgent,
			"      - question:\n          id: blocks\n          text: hi\n          return: crew:nowhere\n", ""),
		wants: []string{
			".crew/config.yaml: rules.implement.actions[0].question.return (line 12)",
			`"crew:nowhere" is not the ready label of a rule`,
		},
	},
	{
		name: "two questions without names",
		body: sequenceRule(answerer+oneAgent,
			"      - question: {id: a, text: hi, return: ready}\n      - question: {id: b, text: hi, return: ready}\n", ""),
		wants: []string{"rules.implement.actions[1] (line 10)", `action "question" is also`},
	},
	{
		name:  "a question that is not a mapping",
		body:  sequenceRule(answerer+oneAgent, "      - question: blocks\n", ""),
		wants: []string{"rules.implement.actions[0].question (line 9)", "must be a mapping with id, text and return"},
	},
	{
		name: "an empty name",
		body: sequenceRule(answerer+oneAgent,
			"      - question: {id: a, text: hi, return: ready}\n        name: \"\"\n", ""),
		wants: []string{"rules.implement.actions[0].name (line 10)", "required"},
	},
	{
		name:  "a question without its id",
		body:  sequenceRule(answerer+oneAgent, "      - question: {text: hi, return: ready}\n", ""),
		wants: []string{"rules.implement.actions[0].question.id (line 9)", "required"},
	},
	{
		name: "a question with on",
		body: sequenceRule(answerer+oneAgent,
			"      - question: {id: a, text: hi, return: ready}\n        on: {failed: next}\n", ""),
		wants: []string{"rules.implement.actions[0].on (line 10)", "unknown key"},
	},
	{
		name: "a declared route named like a question action",
		body: sequenceRule(answerer+oneAgent,
			"      - question: {id: a, text: hi, return: ready}\n        name: confirm\n", "      confirm: elsewhere\n"),
		wants: []string{"rules.implement.routes.confirm (line 14)", `question action "confirm"`},
	},
	{
		name: "a name that cannot name a route",
		body: sequenceRule(answerer+oneAgent,
			"      - question: {id: a, text: hi, return: ready}\n        name: Confirm It\n", ""),
		wants: []string{"rules.implement.actions[0].name (line 10)", `route "Confirm It"`},
	},
	{
		name:  "a question action without questions",
		body:  sequenceRule(oneAgent, "      - question: {id: a, text: hi, return: ready}\n", ""),
		wants: []string{"rules.implement.actions[0] (line 8)", "questions.answerer", "missing"},
	},
	{
		name: "a question action in a rule that takes pull requests",
		body: answerer + "rules:\n  review:\n    takes: pull_requests\n    labels: {ready: review, running: reviewing}\n" +
			"    actions:\n      - question: {id: a, text: hi, return: review}\n" +
			"    routes: {passed: reviewed, failed: failed}\n",
		wants: []string{"rules.review.actions[0] (line 7)", "takes pull requests"},
	},
}
