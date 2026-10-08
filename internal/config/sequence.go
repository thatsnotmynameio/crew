package config

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// sessionDoc is a session of a rule's actions: a mapping with prompt.
type sessionDoc struct {
	Agent  located[string] `yaml:"agent"`
	Prompt located[string] `yaml:"prompt"`
	Name   located[string] `yaml:"name"`
	Wait   located[string] `yaml:"wait"`
	On     yaml.Node       `yaml:"on"`
}

// defaultWait is how long a session waits for an answer when its wait is
// left out (R20).
const defaultWait = 10 * time.Minute

// referenceDoc is the keys of a reference to one of actions written as a
// mapping, besides the one key that names the action.
type referenceDoc struct {
	Name located[string] `yaml:"name"`
	On   yaml.Node       `yaml:"on"`
}

// itemShape is what an item of a rule's actions must be, said when it is
// none of its forms.
const itemShape = "must be the name of one of actions or of a function, a session with prompt, " +
	"a question with question: {id, text, return} and optionally name, " +
	"or a mapping whose one key names one of actions or a function, with optionally on and name"

// The keys of an item of a rule's actions that are not a session's alone.
const (
	nameKey = "name"
	onKey   = "on"
)

// parsedAction is an action of a rule's actions that passed its own checks,
// with what the rule's checks need to name its keys.
type parsedAction struct {
	crew.Action

	path string
	line int
	on   onEntries
	ask  *askAt
}

// onEntry is one entry of an action's on, with where its target is written.
type onEntry struct {
	verdict crew.Verdict
	target  crew.Target
	path    string
	line    int
}

// sequence decodes the optional actions n of a rule at path: a list of
// actions, in the order they run. It reports every error it finds.
func sequence(n *yaml.Node, path string, env ruleEnv) ([]parsedAction, error) {
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.SequenceNode:
	default:
		return nil, keyError(path, n.Line, "must be a list of actions")
	}
	var errs []error
	items := sequenceItems(entry{value: n, path: path})
	out := make([]parsedAction, 0, len(items))
	for _, e := range items {
		a, err := parseItem(e, env)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, a)
	}
	return out, errors.Join(errs...)
}

// parseItem decodes the item e of a rule's actions: the name of one of
// actions, a session, a question, or a reference to one of actions with
// its on and name beside it.
func parseItem(e entry, env ruleEnv) (parsedAction, error) {
	n := resolve(e.value)
	switch {
	case n.Kind == yaml.ScalarNode:
		c, err := env.callee(n, e.path)
		if err != nil {
			return parsedAction{}, err
		}
		kind, err := env.call(c, nil, e.path, n.Line)
		return parsedAction{Name: c.name, Kind: kind, path: e.path, line: n.Line}, err
	case n.Kind != yaml.MappingNode:
		return parsedAction{}, keyError(e.path, n.Line, itemShape)
	case hasKey(n, "prompt") || hasKey(n, "agent"):
		return parseSession(entry{key: e.key, value: n, path: e.path}, env)
	case hasKey(n, questionWord):
		return parseQuestion(entry{key: e.key, value: n, path: e.path})
	}
	return parseReference(entry{key: e.key, value: n, path: e.path}, env)
}

// hasKey tells whether the mapping n has the key key.
func hasKey(n *yaml.Node, key string) bool {
	for i := 0; i < len(n.Content); i += nodesPerEntry {
		if n.Content[i].Value == key {
			return true
		}
	}
	return false
}

// parseSession decodes the session e, resolving its agent and its bot, its
// agent's or else tracker.bot, in env, and reading its wait. It is named
// after its agent unless its name says otherwise (KTD16).
func parseSession(e entry, env ruleEnv) (parsedAction, error) {
	var doc sessionDoc
	if err := decodeItem(e.value, e.path, itemShape, &doc); err != nil {
		return parsedAction{}, err
	}
	line := e.value.Line
	text, promptErr := required(doc.Prompt, e.path+".prompt", line)
	agent, agentErr := env.agent(doc.Agent, e.path+".agent", line)
	byAgent := doc.Agent.value
	if byAgent == "" {
		byAgent = string(agent.Name)
	}
	name, nameErr := actionName(doc.Name, e.path+".name", byAgent)
	wait, waitErr := sessionWait(doc.Wait, e.path+".wait")
	on, onErr := parseOn(&doc.On, e.path+".on")
	if err := errors.Join(promptErr, agentErr, nameErr, waitErr, onErr); err != nil {
		return parsedAction{}, err
	}
	prompt, err := crew.ParsePrompt(name, text)
	if err != nil {
		return parsedAction{}, keyError(e.path+".prompt", doc.Prompt.line, err.Error())
	}
	if err := retiredVariables(text, e.path+".prompt", doc.Prompt.line); err != nil {
		return parsedAction{}, err
	}
	bot := env.bot
	if agent.Bot != "" {
		bot = crew.Bot{Name: agent.Bot}
	}
	spec := crew.SessionSpec{Agent: agent.Agent, Prompt: prompt, Bot: bot, Wait: wait}
	a := crew.Action{Name: name, Kind: spec, On: on.targets()}
	return parsedAction{Action: a, path: e.path, line: line, on: on}, nil
}

// sessionWait returns how long the wait l at path has a session wait for an
// answer: a positive Go duration, such as 10m, or defaultWait when l is left
// out (KTD-W3).
func sessionWait(l located[string], path string) (time.Duration, error) {
	if l.line == 0 {
		return defaultWait, nil
	}
	d, err := time.ParseDuration(l.value)
	if err != nil {
		return 0, keyError(path, l.line, fmt.Sprintf("%q must be a duration, such as 10m", l.value))
	}
	if d <= 0 {
		return 0, keyError(path, l.line, "must be positive")
	}
	return d, nil
}

// parseReference decodes the reference e: a mapping whose one key besides
// on and name names one of actions or a function, with an empty value for
// a shell action, which takes no parameters, and an empty value or the
// parameters for a function. A function's action is named after the key
// written unless its name says otherwise.
func parseReference(e entry, env ruleEnv) (parsedAction, error) {
	own, rest := split(entries(e.value, e.path), nameKey, onKey)
	if len(rest) != 1 {
		return parsedAction{}, keyError(e.path, e.value.Line, itemShape)
	}
	var doc referenceDoc
	if err := decodeFields(own, reflect.ValueOf(&doc).Elem()); err != nil {
		return parsedAction{}, err
	}
	ref := rest[0]
	c, refErr := env.callee(ref.key, ref.path)
	var kind crew.ActionKind
	if refErr == nil {
		kind, refErr = env.call(c, &ref, e.path, e.value.Line)
	}
	name, nameErr := actionName(doc.Name, e.path+".name", string(c.name))
	on, onErr := parseOn(&doc.On, e.path+".on")
	if err := errors.Join(refErr, nameErr, onErr); err != nil {
		return parsedAction{}, err
	}
	a := crew.Action{Name: name, Kind: kind, On: on.targets()}
	return parsedAction{Action: a, path: e.path, line: e.value.Line, on: on}, nil
}

// noParameters reports the reference e, whose key names a shell action,
// when its value is not empty.
func noParameters(e entry) error {
	v := e.value
	if v.ShortTag() == nullTag || v.Kind == yaml.MappingNode && len(v.Content) == 0 {
		return nil
	}
	return keyError(e.path, v.Line, "a shell action takes no parameters; leave its value empty")
}

// actionName returns the name an action's name l at path gives it, or
// byDefault when l is left out.
func actionName(l located[string], path, byDefault string) (crew.ActionName, error) {
	if l.line == 0 {
		return crew.ActionName(byDefault), nil
	}
	name, err := required(l, path, l.line)
	return crew.ActionName(name), err
}

// onEntries are an action's on, in file order.
type onEntries []onEntry

// targets returns the targets of on by verdict, or nil when on is empty.
func (on onEntries) targets() crew.On {
	if len(on) == 0 {
		return nil
	}
	out := make(crew.On, len(on))
	for _, o := range on {
		out[o.verdict] = o.target
	}
	return out
}

// parseOn decodes the optional on n of an action at path: a mapping from a
// verdict to next or the name of one of the rule's routes.
func parseOn(n *yaml.Node, path string) (onEntries, error) {
	section, err := named(n, path)
	errs := []error{err}
	out := make(onEntries, 0, len(section))
	for _, e := range section {
		v, err := crew.ParseVerdict(e.key.Value)
		if err != nil {
			errs = append(errs, keyError(e.path, e.key.Line, err.Error()))
			continue
		}
		if e.value.Kind != yaml.ScalarNode {
			errs = append(errs, keyError(e.path, e.value.Line, "must be next or the name of one of the rule's routes"))
			continue
		}
		t, err := crew.ParseTarget(e.value.Value)
		if err != nil {
			errs = append(errs, keyError(e.path, e.value.Line, err.Error()))
			continue
		}
		out = append(out, onEntry{verdict: v, target: t, path: e.path, line: e.value.Line})
	}
	return out, errors.Join(errs...)
}

// sequenceItems lists the items of e's value when it is a sequence, each
// without a key and with its index in its path, and nothing otherwise.
func sequenceItems(e entry) []entry {
	if e.value.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]entry, len(e.value.Content))
	for i, item := range e.value.Content {
		out[i] = entry{value: item, path: fmt.Sprintf("%s[%d]", e.path, i)}
	}
	return out
}
