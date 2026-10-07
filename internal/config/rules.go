package config

import (
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
	Routes  yaml.Node       `yaml:"routes"`
}

// labelsDoc is a rule's labels.
type labelsDoc struct {
	Ready   located[string] `yaml:"ready"`
	Running located[string] `yaml:"running"`
}

// What a rule and its labels must be, said when one is not a mapping.
const (
	ruleShape   = "must be a rule with labels and routes, and optionally takes, queue, notify and actions"
	labelsShape = "must be a mapping with ready and running"
)

// parsedRule is a rule that passed its own checks, with its actions and
// routes as they were parsed and what the rule-graph checks need to name
// its keys.
type parsedRule struct {
	crew.Rule

	path       string
	ruleLine   int
	routesLine int
	labels     labelsDoc
	notify     bool
	actions    []parsedAction
	routes     []parsedRoute
}

// rule returns the domain's rule p describes.
func (p parsedRule) rule() crew.Rule {
	r := p.Rule
	r.Actions, r.Routes = nil, nil
	for _, a := range p.actions {
		r.Actions = append(r.Actions, a.Action)
	}
	for _, route := range p.routes {
		r.Routes = append(r.Routes, route.Route)
	}
	return r
}

// ruleEnv is what the rules' names resolve against: the queues, the agents,
// the shell actions by name, and tracker.bot.
type ruleEnv struct {
	queues  queueTable
	agents  []Agent
	actions map[crew.ActionName]crew.ShellSpec
	bot     crew.Bot
}

// rules decodes and validates rules:, resolving each rule's queue, each
// session's agent and bot, and each shell action in env. It returns the
// rules, and whether each notifies, by name. It reports every error it
// finds.
func rules(n *yaml.Node, env ruleEnv) ([]crew.Rule, map[crew.RuleName]bool, error) {
	if n.Kind == 0 {
		return nil, nil, errors.New("rules: missing; write at least one rule")
	}
	section, err := named(n, "rules")
	if err != nil {
		return nil, nil, err
	}
	if len(section) == 0 {
		return nil, nil, keyError("rules", n.Line, "write at least one rule")
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
		return nil, nil, errors.Join(errs...)
	}
	spellOnce(parsed)
	if err := checkGraph(parsed); err != nil {
		return nil, nil, err
	}
	out := make([]crew.Rule, len(parsed))
	notify := make(map[crew.RuleName]bool, len(parsed))
	for i, p := range parsed {
		out[i], notify[p.Name] = p.rule(), p.notify
	}
	return out, notify, nil
}

// parseRule decodes and checks the rule e, reporting every error it finds.
// A rule notifies by default when it has actions.
func parseRule(e entry, env ruleEnv) (parsedRule, error) {
	var doc ruleDoc
	if err := decodeItem(e.value, e.path, ruleShape, &doc); err != nil {
		return parsedRule{}, err
	}
	p := parsedRule{Name: crew.RuleName(e.key.Value), path: e.path, ruleLine: e.key.Line}
	var labelsErr, queueErr, takesErr, actionsErr, routesErr error
	p.Labels, p.labels, labelsErr = ruleLabels(&doc.Labels, e.path+".labels", e.key.Line)
	p.Queue, queueErr = ruleQueue(doc.Queue, e.path, env.queues)
	p.Takes, takesErr = ruleTakes(doc.Takes, e.path)
	p.actions, actionsErr = sequence(&doc.Actions, e.path+".actions", env)
	p.routes, routesErr = routes(&doc.Routes, e.path+".routes", env)
	p.routesLine = doc.Routes.Line
	p.notify = doc.Actions.Kind == yaml.SequenceNode && len(doc.Actions.Content) > 0
	if doc.Notify.line != 0 {
		p.notify = doc.Notify.value
	}
	if err := errors.Join(labelsErr, queueErr, takesErr, actionsErr, routesErr); err != nil {
		return p, err
	}
	return p, checkRoutes(p)
}

// ruleLabels decodes the labels n of a rule at path, whose key is on
// ruleLine. ready and running are required.
func ruleLabels(n *yaml.Node, path string, ruleLine int) (crew.Labels, labelsDoc, error) {
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
	var readyErr, runningErr error
	labels.Ready, readyErr = state(doc.Ready, path+".ready", n.Line)
	labels.Running, runningErr = state(doc.Running, path+".running", n.Line)
	return labels, doc, errors.Join(readyErr, runningErr)
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

// agent returns the agent a session names in l, at path, on line. A
// session may leave its agent out only when one agent is declared.
// Without agents it returns no agent and no error: the missing agents are
// reported once, by agentsInUse.
func (env ruleEnv) agent(l located[string], path string, line int) (Agent, error) {
	switch {
	case len(env.agents) == 0:
		return Agent{}, nil
	case l.line == 0 && len(env.agents) == 1:
		return env.agents[0], nil
	case l.line == 0:
		return Agent{}, keyError(path, line,
			"required, since agents declares more than one agent: "+env.agentNames())
	}
	for _, a := range env.agents {
		if a.Name == crew.AgentName(l.value) {
			return a, nil
		}
	}
	return Agent{}, keyError(path, l.line, fmt.Sprintf(
		"agent %q does not exist; the agents are %s", l.value, env.agentNames()))
}

// agentNames returns the names of the agents, in file order, joined by
// commas, for an error that lists them.
func (env ruleEnv) agentNames() string {
	names := make([]string, len(env.agents))
	for i, a := range env.agents {
		names[i] = string(a.Name)
	}
	return strings.Join(names, ", ")
}

// spellOnce gives every label the spelling it first has in the rules, in
// rule order and then ready, running and each move of the routes, in route
// order. GitHub does not tell labels apart by case, so "In Review" and
// "in review" are one label; after this, comparing states exactly compares
// them as GitHub does.
func spellOnce(rules []parsedRule) {
	first := map[string]crew.State{}
	spell := func(state crew.State) crew.State {
		key := strings.ToLower(string(state))
		if spelling, ok := first[key]; ok {
			return spelling
		}
		if state != "" {
			first[key] = state
		}
		return state
	}
	for i := range rules {
		l := &rules[i].Labels
		l.Ready, l.Running = spell(l.Ready), spell(l.Running)
		for _, route := range rules[i].routes {
			for j, step := range route.Steps {
				if m, ok := step.(crew.MoveStep); ok {
					route.Steps[j] = crew.MoveStep{To: spell(m.To)}
				}
			}
		}
	}
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
