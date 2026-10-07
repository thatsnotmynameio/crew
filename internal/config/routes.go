package config

import (
	"errors"
	"reflect"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// stepDoc is a step of a route written as a mapping of one key, besides a
// reference to one of actions.
type stepDoc struct {
	Move    located[string] `yaml:"move"`
	Comment located[string] `yaml:"comment"`
}

// What a route and its steps must be, said when one is none of its forms.
const (
	routeShape = "must be a label to move the item to, or a list of steps"
	stepShape  = "must be report, close, move: <label>, comment: <text>, or the name of one of actions"
)

// The steps' words: report and close are written alone, move and comment
// as the key of a step's mapping.
const (
	reportWord  = "report"
	closeWord   = "close"
	moveWord    = "move"
	commentWord = "comment"
)

// parsedRoute is a route of a rule that passed its own checks, with what
// the rule's checks need to name its keys.
type parsedRoute struct {
	crew.Route

	path  string
	line  int
	steps []keyAt
}

// keyAt is where a step is written: its key path and its line. For a move,
// it is where its label is written.
type keyAt struct {
	path string
	line int
}

// routes decodes the optional routes n of a rule at path, in file order:
// a mapping from a route's name to a label, or to a list of steps. It
// reports every error it finds.
func routes(n *yaml.Node, path string, env ruleEnv) ([]parsedRoute, error) {
	section, err := named(n, path)
	errs := []error{err}
	out := make([]parsedRoute, 0, len(section))
	for _, e := range section {
		name, err := crew.ParseRouteName(e.key.Value)
		if err != nil {
			errs = append(errs, keyError(e.path, e.key.Line, err.Error()))
			continue
		}
		r, err := parseRoute(e, name, env)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, r)
	}
	return out, errors.Join(errs...)
}

// parseRoute decodes the route e, called name: a label, which is one move,
// or a list of steps.
func parseRoute(e entry, name crew.RouteName, env ruleEnv) (parsedRoute, error) {
	r := parsedRoute{Name: name, path: e.path, line: e.key.Line}
	v := resolve(e.value)
	switch v.Kind {
	case yaml.ScalarNode:
		to, err := state(located[string]{value: v.Value, line: v.Line}, e.path, e.key.Line)
		r.Steps, r.steps = []crew.Step{crew.MoveStep{To: to}}, []keyAt{{e.path, v.Line}}
		return r, err
	case yaml.SequenceNode:
	default:
		return r, keyError(e.path, v.Line, routeShape)
	}
	var errs []error
	for _, item := range sequenceItems(entry{value: v, path: e.path}) {
		step, at, err := parseStep(item, name, env)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		r.Steps, r.steps = append(r.Steps, step), append(r.steps, at)
	}
	return r, errors.Join(errs...)
}

// parseStep decodes the step e of the route called route.
func parseStep(e entry, route crew.RouteName, env ruleEnv) (crew.Step, keyAt, error) {
	n := resolve(e.value)
	at := keyAt{e.path, n.Line}
	switch {
	case n.Kind == yaml.ScalarNode && n.Value == reportWord:
		return crew.ReportStep{}, at, nil
	case n.Kind == yaml.ScalarNode && n.Value == closeWord:
		return crew.CloseStep{}, at, nil
	case n.Kind == yaml.ScalarNode:
		name, spec, err := env.shell(n, e.path)
		return crew.ShellStep{Name: name, Shell: spec}, at, err
	case n.Kind != yaml.MappingNode || len(n.Content) != nodesPerEntry:
		return nil, at, keyError(e.path, n.Line, stepShape)
	}
	key := entries(n, e.path)[0]
	switch key.key.Value {
	case moveWord, commentWord:
		return effectStep(key, route, at)
	}
	name, spec, err := env.shell(key.key, key.path)
	if err == nil {
		err = noParameters(key)
	}
	return crew.ShellStep{Name: name, Shell: spec}, at, err
}

// effectStep decodes the move or the comment e, a step of the route called
// route, which the step's own key path and line, in at, name.
func effectStep(e entry, route crew.RouteName, at keyAt) (crew.Step, keyAt, error) {
	var doc stepDoc
	if err := decodeFields([]entry{e}, reflect.ValueOf(&doc).Elem()); err != nil {
		return nil, at, err
	}
	if e.key.Value == moveWord {
		to, err := state(doc.Move, e.path, e.key.Line)
		return crew.MoveStep{To: to}, keyAt{e.path, e.key.Line}, err
	}
	text, err := required(doc.Comment, e.path, e.key.Line)
	if err != nil {
		return nil, at, err
	}
	tmpl, err := crew.ParseCommentTemplate(route, text)
	if err != nil {
		return nil, at, keyError(e.path, doc.Comment.line, err.Error())
	}
	return crew.CommentStep{Template: tmpl}, at, nil
}
