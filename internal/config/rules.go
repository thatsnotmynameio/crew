package config

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// ruleDoc is one rule of rules, keyed by its name.
type ruleDoc struct {
	Takes   located[string] `yaml:"takes"`
	Queue   located[string] `yaml:"queue"`
	Notify  located[bool]   `yaml:"notify"`
	Labels  yaml.Node       `yaml:"labels"`
	Actions yaml.Node       `yaml:"actions"`
}

// labelsDoc is a rule's labels.
type labelsDoc struct {
	Ready   located[string] `yaml:"ready"`
	Running located[string] `yaml:"running"`
	Success located[string] `yaml:"success"`
	Failure located[string] `yaml:"failure"`
}

// actionDoc is one action of a rule's actions, keyed by its name.
type actionDoc struct {
	Agent  located[string] `yaml:"agent"`
	Prompt located[string] `yaml:"prompt"`
	Check  located[string] `yaml:"check"`
}

// What each kind of named item must be, said when an item is not a mapping.
const (
	ruleShape   = "must be a rule with labels, and optionally takes, queue, notify and actions"
	labelsShape = "must be a mapping with ready, running, success and failure"
	actionShape = "must be an action with prompt, and optionally agent and check"
)

// parsedRule is a rule that passed its own checks, with what the
// rule-graph checks need to name its keys.
type parsedRule struct {
	crew.Rule

	path   string
	labels labelsDoc
}

// ruleEnv is what the rules' names resolve against: the queues, the agents,
// the checks' scripts by name, and tracker.bot.
type ruleEnv struct {
	queues queueTable
	agents []Agent
	checks map[string]string
	bot    string
}

// rules decodes and validates rules:, resolving each rule's queue and each
// action's agent, check and bot in env. It reports every error it finds.
func rules(n *yaml.Node, env ruleEnv) ([]crew.Rule, error) {
	if n.Kind == 0 {
		return nil, errors.New("rules: missing; write at least one rule")
	}
	section, err := named(n, "rules")
	if err != nil {
		return nil, err
	}
	if len(section) == 0 {
		return nil, keyError("rules", n.Line, "write at least one rule")
	}
	var errs []error
	parsed := make([]parsedRule, 0, len(section))
	for _, e := range section {
		rule, err := parseRule(e, env)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		parsed = append(parsed, rule)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	spellOnce(parsed)
	if err := checkGraph(parsed); err != nil {
		return nil, err
	}
	out := make([]crew.Rule, len(parsed))
	for i, p := range parsed {
		out[i] = p.Rule
	}
	return out, nil
}

// parseRule decodes and checks the rule e, reporting every error it finds.
// A rule notifies by default when it has actions.
func parseRule(e entry, env ruleEnv) (parsedRule, error) {
	var doc ruleDoc
	if err := decodeItem(e.value, e.path, ruleShape, &doc); err != nil {
		return parsedRule{}, err
	}
	p := parsedRule{Name: e.key.Value, path: e.path}
	hasActions := doc.Actions.Kind == yaml.MappingNode && len(doc.Actions.Content) > 0
	var labelsErr, queueErr, takesErr, actionsErr error
	p.Labels, p.labels, labelsErr = ruleLabels(&doc.Labels, e.path+".labels", e.key.Line, hasActions)
	p.Queue, queueErr = ruleQueue(doc.Queue, e.path, env.queues)
	p.Takes, takesErr = ruleTakes(doc.Takes, e.path)
	p.Actions, actionsErr = actions(&doc.Actions, e.path+".actions", env)
	p.Notify = hasActions
	if doc.Notify.line != 0 {
		p.Notify = doc.Notify.value
	}
	return p, errors.Join(labelsErr, queueErr, takesErr, actionsErr)
}

// ruleLabels decodes the labels n of a rule at path, whose key is on
// ruleLine. ready, running and success are required, and failure too when
// the rule has actions: a rule without actions never fails.
func ruleLabels(n *yaml.Node, path string, ruleLine int, hasActions bool) (crew.Labels, labelsDoc, error) {
	var doc labelsDoc
	switch n.Kind {
	case 0:
		return crew.Labels{}, doc, keyError(path, ruleLine, "required")
	case yaml.MappingNode:
		if err := decodeItem(n, path, labelsShape, &doc); err != nil {
			return crew.Labels{}, doc, err
		}
	default:
		return crew.Labels{}, doc, keyError(path, n.Line, labelsShape)
	}
	var labels crew.Labels
	var errs [4]error
	labels.Ready, errs[0] = state(doc.Ready, path+".ready", n.Line)
	labels.Running, errs[1] = state(doc.Running, path+".running", n.Line)
	labels.Success, errs[2] = state(doc.Success, path+".success", n.Line)
	if hasActions || doc.Failure.line != 0 {
		labels.Failure, errs[3] = state(doc.Failure, path+".failure", n.Line)
	}
	return labels, doc, errors.Join(errs[:]...)
}

// The values of a rule's takes, one per kind.
const (
	takesIssues       = "issues"
	takesPullRequests = "pull_requests"
)

// ruleTakes returns the kind of item the rule at path takes: issues when
// takes is left out, and otherwise the kind its value names.
func ruleTakes(l located[string], path string) (crew.Kind, error) {
	switch {
	case l.line == 0, l.value == takesIssues:
		return crew.KindIssue, nil
	case l.value == takesPullRequests:
		return crew.KindPullRequest, nil
	}
	return crew.KindIssue, keyError(path+".takes", l.line,
		fmt.Sprintf("%q must be %s or %s", l.value, takesIssues, takesPullRequests))
}

// actions decodes and checks the optional actions n of a rule at path, in
// file order. It reports every error it finds.
func actions(n *yaml.Node, path string, env ruleEnv) ([]crew.Action, error) {
	section, err := named(n, path)
	errs := []error{err}
	out := make([]crew.Action, 0, len(section))
	for _, e := range section {
		action, err := parseAction(e, env)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, action)
	}
	return out, errors.Join(errs...)
}

// parseAction decodes and checks the action e, resolving its agent, its
// check and its bot, its agent's or else tracker.bot, in env.
func parseAction(e entry, env ruleEnv) (crew.Action, error) {
	var doc actionDoc
	if err := decodeItem(e.value, e.path, actionShape, &doc); err != nil {
		return crew.Action{}, err
	}
	prompt, promptErr := required(doc.Prompt, e.path+".prompt", e.key.Line)
	agent, agentErr := env.agent(doc.Agent, e.path+".agent", e.key.Line)
	check, checkErr := env.check(doc.Check, e.path+".check")
	if err := errors.Join(promptErr, agentErr, checkErr); err != nil {
		return crew.Action{}, err
	}
	action := crew.Action{
		Name: e.key.Value, Prompt: prompt, Agent: agent.Name, Check: check, Bot: cmp.Or(agent.Bot, env.bot),
	}
	if _, err := action.Render(sampleIssue()); err != nil {
		return crew.Action{}, keyError(e.path+".prompt", doc.Prompt.line, err.Error())
	}
	if err := retiredVariables(prompt, e.path+".prompt", doc.Prompt.line); err != nil {
		return crew.Action{}, err
	}
	return action, nil
}

// agent returns the agent an action names in l, at path, whose key is on
// line. An action may leave its agent out only when one agent is declared.
// Without agents it returns no agent and no error: the missing agents are
// reported once, by agentsInUse.
func (env ruleEnv) agent(l located[string], path string, line int) (Agent, error) {
	names := make([]string, len(env.agents))
	for i, a := range env.agents {
		names[i] = a.Name
	}
	switch {
	case len(env.agents) == 0:
		return Agent{}, nil
	case l.line == 0 && len(env.agents) == 1:
		return env.agents[0], nil
	case l.line == 0:
		return Agent{}, keyError(path, line,
			"required, since agents declares more than one agent: "+strings.Join(names, ", "))
	}
	for _, a := range env.agents {
		if a.Name == l.value {
			return a, nil
		}
	}
	return Agent{}, keyError(path, l.line, fmt.Sprintf(
		"agent %q does not exist; the agents are %s", l.value, strings.Join(names, ", ")))
}

// check returns the script of the check an action names in l, at path, or
// no script when it names none.
func (env ruleEnv) check(l located[string], path string) (string, error) {
	if l.line == 0 {
		return "", nil
	}
	if script, ok := env.checks[l.value]; ok {
		return script, nil
	}
	if len(env.checks) == 0 {
		return "", keyError(path, l.line, fmt.Sprintf("check %q does not exist; checks declares none", l.value))
	}
	return "", keyError(path, l.line, fmt.Sprintf("check %q does not exist; the checks are %s",
		l.value, strings.Join(sortedKeys(env.checks), ", ")))
}

// sampleIssue is the issue every prompt is rendered for at load, so a bad
// template stops crew before polling rather than when an issue is taken.
func sampleIssue() crew.Issue {
	return crew.Issue{Key: "42", Ref: "#42", Title: "Sample issue", URL: "https://example.com/issues/42"}
}

// spellOnce gives every label the spelling it first has in the rules, in
// rule order and then ready, running, success, failure. GitHub does not tell
// labels apart by case, so "In Review" and "in review" are one label; after
// this, comparing states exactly compares them as GitHub does.
func spellOnce(rules []parsedRule) {
	first := map[string]crew.State{}
	for i := range rules {
		l := &rules[i].Labels
		for _, state := range []*crew.State{&l.Ready, &l.Running, &l.Success, &l.Failure} {
			key := strings.ToLower(string(*state))
			if spelling, ok := first[key]; ok {
				*state = spelling
			} else if *state != "" {
				first[key] = *state
			}
		}
	}
}

// checkGraph rejects a set of rules that would take an item twice or take
// back what one of them moved. It runs after spellOnce, so it compares
// labels ignoring case.
func checkGraph(rules []parsedRule) error {
	var errs []error
	byReady := make(map[crew.State]parsedRule, len(rules))
	for _, r := range rules {
		l := r.Labels
		if other, ok := byReady[l.Ready]; ok {
			errs = append(errs, keyError(r.path+".labels.ready", r.labels.Ready.line,
				fmt.Sprintf("rule %q takes %q, as rule %q (%s) does; two rules cannot take the same label",
					r.Name, l.Ready, other.Name, other.path)))
		} else {
			byReady[l.Ready] = r
		}
		if l.Success == l.Ready {
			errs = append(errs, keyError(r.path+".labels.success", r.labels.Success.line,
				fmt.Sprintf("%q is the rule's own ready label, so rule %q would take the item again", l.Success, r.Name)))
		}
		if l.Failure == l.Ready {
			errs = append(errs, keyError(r.path+".labels.failure", r.labels.Failure.line,
				fmt.Sprintf("%q is the rule's own ready label, so rule %q would take the failed item again",
					l.Failure, r.Name)))
		}
	}
	for _, r := range rules {
		if other, ok := byReady[r.Labels.Running]; ok {
			errs = append(errs, keyError(r.path+".labels.running", r.labels.Running.line,
				fmt.Sprintf("%q is the ready label of rule %q (%s.labels.ready), which would take the item while rule %q runs",
					r.Labels.Running, other.Name, other.path, r.Name)))
		}
	}
	return errors.Join(errs...)
}

// required returns a key's non-empty value. A missing key is reported on the
// line of the mapping that lacks it.
func required(l located[string], path string, parentLine int) (string, error) {
	if l.value != "" {
		return l.value, nil
	}
	line := l.line
	if line == 0 {
		line = parentLine
	}
	return "", keyError(path, line, "required")
}

// state returns a key's value as a state: any non-empty text.
func state(l located[string], path string, parentLine int) (crew.State, error) {
	v, err := required(l, path, parentLine)
	return crew.State(v), err
}
