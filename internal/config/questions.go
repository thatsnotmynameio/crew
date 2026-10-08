package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Questions is questions: who answers every question a rule asks, and the
// queue crew's question rule runs in.
type Questions struct {
	// Answerer is questions.answerer: the login of the person, or of the
	// App as <slug>[bot], crew mentions on every question. It is never
	// github-actions[bot].
	Answerer string
	// Queue is the queue of crew's question rule: the one questions.queue
	// names, or default.
	Queue crew.Queue
}

// questionsDoc is questions:.
type questionsDoc struct {
	Answerer located[string] `yaml:"answerer"`
	Queue    located[string] `yaml:"queue"`
}

// questionDoc is a question a rule asks, as a route's step or as one of
// its actions.
type questionDoc struct {
	ID     located[string] `yaml:"id"`
	Text   located[string] `yaml:"text"`
	Return located[string] `yaml:"return"`
}

// questionItemDoc is a question action of a rule's actions.
type questionItemDoc struct {
	Question questionDoc     `yaml:"question"`
	Name     located[string] `yaml:"name"`
}

// What questions: and a question must be, said when one is not a mapping.
const (
	questionsShape = "must be a mapping with answerer, and optionally queue"
	questionShape  = "must be a mapping with id, text and return"
)

// questionWord is the key of a question, as a route's step or one of a
// rule's actions, and the name of a question action without name.
const questionWord = "question"

// The name and labels of crew's question rule, the same in every
// repository (KTD7).
const (
	questionRule    crew.RuleName = "question"
	questionLabel   crew.State    = "crew:question"
	questionRunning crew.State    = "crew:question:in progress"
	questionWaiting crew.State    = "crew:question:waiting answer"
)

// The name, labels and action of crew's answered rule, the same in every
// repository (KTD1 of #311).
const (
	answeredRule    crew.RuleName   = "answered"
	answeredLabel   crew.State      = "crew:answered"
	answeredRunning crew.State      = "crew:answered:in progress"
	answeredFailed  crew.State      = "crew:answered:failed"
	answerAction    crew.ActionName = "answer"
)

// builtinRule is one of crew's rules questions adds: its name, and the
// labels it takes, runs in or moves an item to, which no other rule may.
type builtinRule struct {
	name   crew.RuleName
	labels []crew.State
}

// builtinRules returns crew's rules questions adds, the question rule
// first.
func builtinRules() []builtinRule {
	return []builtinRule{
		{questionRule, []crew.State{questionLabel, questionRunning, questionWaiting}},
		{answeredRule, []crew.State{answeredLabel, answeredRunning, answeredFailed}},
	}
}

// reservedLabel rejects label, written at path and line, when it is one of
// the labels of crew's built-in rules, ignoring case.
func reservedLabel(path string, line int, label crew.State) error {
	for _, b := range builtinRules() {
		if slices.ContainsFunc(b.labels, func(l crew.State) bool { return strings.EqualFold(string(label), string(l)) }) {
			return keyError(path, line, fmt.Sprintf(
				"%q is a label of crew's %s rule, which questions adds, so no other rule takes it, "+
					"runs in it or moves an item to it", label, b.name))
		}
	}
	return nil
}

// askAt is where a question is written: its step or action, and its return
// label.
type askAt struct {
	step, ret keyAt
}

// questions reads the optional questions n, whose queue is one of table,
// into cfg. Left out, cfg keeps none; written, cfg holds it even with
// errors, so that a question is not also reported as lacking it.
func questions(n *yaml.Node, table queueTable, cfg *Config) error {
	const path = "questions"
	if n.Kind == 0 {
		return nil
	}
	q := &Questions{}
	cfg.Questions = q
	var doc questionsDoc
	if err := decodeItem(n, path, questionsShape, &doc); err != nil {
		return err
	}
	var answererErr, queueErr error
	q.Answerer, answererErr = required(doc.Answerer, path+".answerer", n.Line)
	if strings.EqualFold(q.Answerer, githubActions) {
		answererErr = keyError(path+".answerer", doc.Answerer.line, githubActionsRefusal)
	}
	q.Queue, queueErr = ruleQueue(doc.Queue, path, table)
	return errors.Join(answererErr, queueErr)
}

// parseAsk returns the question doc the entry e writes, in the route
// called route, and where its return label is written.
func parseAsk(doc questionDoc, e entry, route crew.RouteName) (crew.Ask, keyAt, error) {
	line := resolve(e.value).Line
	ret := keyAt{e.path + ".return", doc.Return.line}
	id, idErr := required(doc.ID, e.path+".id", line)
	text, textErr := required(doc.Text, e.path+".text", line)
	to, retErr := state(doc.Return, ret.path, line)
	if err := errors.Join(idErr, textErr, retErr); err != nil {
		return crew.Ask{}, ret, err
	}
	q := crew.Ask{Return: to}
	q.ID, idErr = crew.ParseQuestionID(id)
	if idErr != nil {
		idErr = keyError(e.path+".id", doc.ID.line, idErr.Error())
	}
	q.Text, textErr = crew.ParseCommentTemplate(route, text)
	if textErr != nil {
		textErr = keyError(e.path+".text", doc.Text.line, textErr.Error())
	}
	return q, ret, errors.Join(idErr, textErr)
}

// askShape reports the question e when its value is not a mapping.
func askShape(e entry) error {
	if v := resolve(e.value); v.Kind != yaml.MappingNode {
		return keyError(e.path, v.Line, questionShape)
	}
	return nil
}

// questionStep decodes the question e, a step of the route called route.
// Its keyAt is where its return label is written.
func questionStep(e entry, route crew.RouteName) (crew.Step, keyAt, error) {
	if err := askShape(e); err != nil {
		return nil, keyAt{}, err
	}
	var doc stepDoc
	if err := decodeFields([]entry{e}, reflect.ValueOf(&doc).Elem()); err != nil {
		return nil, keyAt{}, err
	}
	q, ret, err := parseAsk(doc.Question, e, route)
	return crew.QuestionStep{Question: q}, ret, err
}

// parseQuestion decodes the question action e: its question, and its name
// beside it, question by default, which also names its route. Its on sends
// asked to that route (KTD3).
func parseQuestion(e entry) (parsedAction, error) {
	section := entries(e.value, e.path)
	key := section[slices.IndexFunc(section, func(k entry) bool { return k.key.Value == questionWord })]
	if err := askShape(key); err != nil {
		return parsedAction{}, err
	}
	var doc questionItemDoc
	if err := decodeItem(e.value, e.path, itemShape, &doc); err != nil {
		return parsedAction{}, err
	}
	name, err := actionName(doc.Name, e.path+".name", questionWord)
	if err != nil {
		return parsedAction{}, err
	}
	route, err := crew.ParseRouteName(string(name))
	if err != nil {
		return parsedAction{}, keyError(e.path+".name", doc.Name.line, err.Error())
	}
	q, ret, err := parseAsk(doc.Question, key, route)
	if err != nil {
		return parsedAction{}, err
	}
	line := e.value.Line
	on := onEntries{{verdict: crew.Asked, target: crew.ToRoute{Route: route}, path: e.path, line: line}}
	a := crew.Action{Name: name, Kind: crew.QuestionSpec{Question: q}, On: on.targets()}
	return parsedAction{Action: a, path: e.path, line: line, on: on, ask: &askAt{keyAt{e.path, line}, ret}}, nil
}

// endWithQuestion rejects a step after a question, which ends its route,
// and appends to a route that asks the move to crew:question (KTD2).
func endWithQuestion(r *parsedRoute) error {
	last := len(r.Steps) - 1
	for i, step := range r.Steps[:max(last, 0)] {
		if _, ok := step.(crew.QuestionStep); ok {
			at := r.steps[i+1]
			return keyError(at.path, at.line, fmt.Sprintf(
				"a question ends its route: crew moves the item to %s after it, so no step comes after it", questionLabel))
		}
	}
	if r.ask != nil {
		r.Steps, r.steps = append(r.Steps, crew.MoveStep{To: questionLabel}), append(r.steps, r.ask.ret)
	}
	return nil
}

// questionRoutes returns p's routes, then the route of each of its question
// actions, which crew declares: the question, then the move to
// crew:question (KTD3). It rejects a declared route named like a question
// action.
func questionRoutes(p parsedRule) ([]parsedRoute, error) {
	out := p.routes
	var errs []error
	for _, a := range p.actions {
		spec, ok := a.Kind.(crew.QuestionSpec)
		if !ok {
			continue
		}
		name := crew.RouteName(a.Name)
		if i := slices.IndexFunc(p.routes, func(r parsedRoute) bool { return r.Name == name }); i >= 0 {
			errs = append(errs, keyError(p.routes[i].path, p.routes[i].line, fmt.Sprintf(
				"route %q is the route of question action %q (%s, line %d), which crew declares itself; "+
					"name the action or the route another way", name, a.Name, a.path, a.line)))
			continue
		}
		steps := []crew.Step{crew.QuestionStep(spec), crew.MoveStep{To: questionLabel}}
		out = append(out, parsedRoute{
			Name: name, Steps: steps, path: a.path, line: a.line,
			steps: []keyAt{a.ask.step, a.ask.ret}, ask: a.ask,
		})
	}
	return out, errors.Join(errs...)
}

// checkQuestions rejects, in rules after spellOnce, every question without
// q, and with q every question in a rule that takes pull requests, every
// return label that is no rule's ready label (R3) or the ready label of a
// rule that takes pull requests (KTD4 of #311), and every rule that takes
// the name or labels of crew's question or answered rule.
func checkQuestions(rules []parsedRule, q *Questions) error {
	var errs []error
	for _, r := range rules {
		if q != nil {
			errs = append(errs, checkReserved(r))
		}
		for _, route := range r.routes {
			if route.ask != nil {
				errs = append(errs, checkAsk(r, route, q, rules))
			}
		}
	}
	return errors.Join(errs...)
}

// checkAsk rejects the question route of r asks, as checkQuestions does,
// against the ready labels of rules.
func checkAsk(r parsedRule, route parsedRoute, q *Questions, rules []parsedRule) error {
	at := route.ask.step
	switch {
	case q == nil:
		return keyError(at.path, at.line, "a question needs questions.answerer, who answers it, and questions is missing")
	case r.Takes == crew.KindPullRequest:
		return keyError(at.path, at.line,
			fmt.Sprintf("rule %q takes pull requests, and crew's question rule takes issues, so it cannot ask", r.Name))
	}
	// A question route ends with its question, then crew's move to
	// crew:question: endWithQuestion and questionRoutes see to it.
	s, _ := route.Steps[len(route.Steps)-2].(crew.QuestionStep)
	ret, to := route.ask.ret, s.Question.Return
	i := slices.IndexFunc(rules, func(o parsedRule) bool { return o.Labels.Ready == to })
	switch {
	case i < 0:
		ready := make([]string, len(rules))
		for j, o := range rules {
			ready[j] = string(o.Labels.Ready)
		}
		return keyError(ret.path, ret.line, fmt.Sprintf(
			"%q is not the ready label of a rule, so none would take the item back once its question is answered; "+
				"the ready labels are %s", to, strings.Join(ready, ", ")))
	case rules[i].Takes == crew.KindPullRequest:
		return keyError(ret.path, ret.line, fmt.Sprintf(
			"%q is the ready label of rule %q, which takes pull requests, and the item that asks is an issue, "+
				"so that rule would never take it back once its question is answered", to, rules[i].Name))
	}
	return nil
}

// checkReserved rejects r when it is named like one of crew's built-in
// rules, when it takes or runs in one of their labels, or when one of its
// written moves leads to one. Labels are compared ignoring case.
func checkReserved(r parsedRule) error {
	errs := []error{
		reservedLabel(r.path+".labels.ready", r.labels.Ready.line, r.Labels.Ready),
		reservedLabel(r.path+".labels.running", r.labels.Running.line, r.Labels.Running),
	}
	for _, b := range builtinRules() {
		if r.Name == b.name {
			errs = append(errs, keyError(r.path, r.ruleLine, fmt.Sprintf(
				"%q is the name of crew's %s rule, which questions adds; name this rule another way", r.Name, b.name)))
		}
	}
	for _, route := range r.routes {
		written := len(route.Steps)
		if route.ask != nil {
			written-- // crew appended the move to crew:question
		}
		for i, step := range route.Steps[:written] {
			if m, ok := step.(crew.MoveStep); ok {
				errs = append(errs, reservedLabel(route.steps[i].path, route.steps[i].line, m.To))
			}
		}
	}
	return errors.Join(errs...)
}

// withQuestionRule returns rules and notify with crew's question rule
// first, then its answered rule, both in q's queue, when q is set (KTD7).
// The question rule takes crew:question, delegates the item's open
// question and moves the item to crew:question:waiting answer, which no
// rule takes; it has no actions. The answered rule takes crew:answered,
// checks the answer with its one action, answer, and returns the item to
// the label the check found, or reports and moves it to
// crew:answered:failed (KTD1, KTD4 of #311). Neither notifies. At equal
// priority the scheduler takes later rules first, so the file's rules take
// first.
func withQuestionRule(q *Questions, rules []crew.Rule, notify map[crew.RuleName]bool,
) ([]crew.Rule, map[crew.RuleName]bool) {
	if q == nil {
		return rules, notify
	}
	question := crew.Rule{
		Name: questionRule, Queue: q.Queue,
		Labels: crew.Labels{Ready: questionLabel, Running: questionRunning},
		Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{
			crew.DelegateStep{}, crew.MoveStep{To: questionWaiting},
		}}},
	}
	answered := crew.Rule{
		Name: answeredRule, Queue: q.Queue,
		Labels:  crew.Labels{Ready: answeredLabel, Running: answeredRunning},
		Actions: []crew.Action{{Name: answerAction, Kind: crew.ReturnSpec{}}},
		Routes: []crew.Route{
			{Name: crew.PassedRoute, Steps: []crew.Step{crew.ReturnStep{}}},
			{Name: crew.FailedRoute, Steps: []crew.Step{crew.ReportStep{}, crew.MoveStep{To: answeredFailed}}},
		},
	}
	notify[questionRule], notify[answeredRule] = false, false
	return append([]crew.Rule{question, answered}, rules...), notify
}
