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

// FunctionUse is one place of the config that calls a function: a rule's
// action or a route's step whose name is a function's, or one of actions'
// presets of it. It holds what building the function and calling it need.
type FunctionUse struct {
	// Use is the place's key path, such as rules.development.actions[2] or
	// rules.development.routes.failed[0]: the Use of its crew.FunctionSpec.
	Use crew.FunctionUse
	// Function is the function's registered name.
	Function crew.FunctionName
	// Section decodes the use's parameters, its preset's each replaced by
	// its own of the same key, with every text parameter rendered for a
	// sample issue: what the function's factory checks when crew builds it.
	// Its errors name each parameter's file, key path and line.
	Section Decode
	// Bind returns a Decode over the same parameters with each text
	// parameter's value replaced by texts[name], its rendering for the
	// issue a call is for. Every Decode it returns has nodes of its own,
	// so calls may decode at the same time.
	Bind func(texts map[string]string) Decode
	// Refused returns the config error for the function refusing its
	// parameter for reason: at the parameter's file, key path and line, or
	// at the use's own when the use does not write it.
	Refused func(parameter, reason string) error
	// Failed returns the config error for err, an error building the
	// function that names no parameter, at the use's file, key path and
	// line. An error from decoding Section, which names its key, line and
	// file already, it returns as it is.
	Failed func(err error) error
}

// named returns u with every error it makes named by its file in o.
func (u FunctionUse) named(o origin) FunctionUse {
	bindTexts, refused, failed := u.Bind, u.Refused, u.Failed
	u.Section = o.decode(u.Section)
	u.Bind = func(texts map[string]string) Decode { return o.decode(bindTexts(texts)) }
	u.Refused = func(parameter, reason string) error { return o.name(refused(parameter, reason)) }
	u.Failed = func(err error) error {
		if k := (*keyPathError)(nil); errors.As(err, &k) {
			return err // Section's decode named its key, line and file already
		}
		return o.name(failed(err))
	}
	return u
}

// presetDoc is crew's keys of a function preset of actions; every other
// key is one of the function's parameters.
type presetDoc struct {
	Name   located[string] `yaml:"name"`
	Resume located[string] `yaml:"resume"`
}

// The shapes a function's parameters must have, said when they do not.
const (
	parameterShape  = "a function's parameter is text, a number or a boolean"
	parametersShape = "a function's parameters are a mapping of their names to their values; leave it empty for none"
)

// strTag is the tag of a YAML string.
const strTag = "!!str"

// preset is a function preset of actions, or a function named by its own
// name, which presets nothing: the function, where a resume starts, and
// its parameters, each checked.
type preset struct {
	function   crew.FunctionName
	resumeSelf bool
	params     []parameter
}

// parameter is one of a function use's parameters, written at its entry's
// key path and line: a scalar, with its template when it is text.
type parameter struct {
	entry

	text *crew.ParameterTemplate
}

// definedName reports the name of the action of actions e when it is a
// word of the rules' grammar or a registered function's (R32), so that a
// rule's items and a route's steps read one way only.
func definedName(e entry, functions map[string][]crew.Verdict) error {
	if slices.Contains(reservedNames(), e.key.Value) {
		return keyError(e.path, e.key.Line, fmt.Sprintf(
			"%q is a word of the rules' grammar, so it cannot name an action", e.key.Value))
	}
	if _, ok := functions[e.key.Value]; ok {
		return keyError(e.path, e.key.Line, fmt.Sprintf(
			"%q is the name of a registered function, so it cannot name an action", e.key.Value))
	}
	return nil
}

// isPreset tells whether the action of actions e is a function preset: a
// mapping with name.
func isPreset(e entry) bool {
	v := resolve(e.value)
	return v.Kind == yaml.MappingNode && hasKey(v, nameKey)
}

// parsePreset decodes the function preset e, whose function must be one
// of functions.
func parsePreset(e entry, functions map[string][]crew.Verdict) (preset, error) {
	v := resolve(e.value)
	if hasKey(v, "script") {
		return preset{}, keyError(e.path, v.Line, shellShape)
	}
	section, sectionErr := named(v, e.path)
	own, rest := split(section, nameKey, "resume")
	var doc presetDoc
	if err := decodeFields(own, reflect.ValueOf(&doc).Elem()); err != nil {
		return preset{}, errors.Join(sectionErr, err)
	}
	p := preset{function: crew.FunctionName(doc.Name.value), resumeSelf: doc.Resume.value == resumeSelf}
	nameErr := registered(doc.Name, e.path+".name", v.Line, functions)
	var resumeErr error
	if doc.Resume.line != 0 && !p.resumeSelf {
		resumeErr = keyError(e.path+".resume", doc.Resume.line, fmt.Sprintf(
			"%q must be %s, the only start a function can choose", doc.Resume.value, resumeSelf))
	}
	var paramsErr error
	p.params, paramsErr = parameters(rest)
	return p, errors.Join(sectionErr, nameErr, resumeErr, paramsErr)
}

// registered reports the function name l at path, in a mapping on line,
// when it is left out or is not one of functions, listing them.
func registered(l located[string], path string, line int, functions map[string][]crew.Verdict) error {
	name, err := required(l, path, line)
	if err != nil {
		return err
	}
	if _, ok := functions[name]; ok {
		return nil
	}
	names := "none"
	if len(functions) > 0 {
		names = strings.Join(sortedKeys(functions), ", ")
	}
	return keyError(path, l.line, fmt.Sprintf(
		"no function is named %q; the registered functions are %s", name, names))
}

// parameters checks each of a function's parameters in section.
func parameters(section []entry) ([]parameter, error) {
	out := make([]parameter, 0, len(section))
	var errs []error
	for _, e := range section {
		p, err := parseParameter(e)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	return out, errors.Join(errs...)
}

// parseParameter checks the parameter e: a scalar, and when it is text, a
// template over the issue that renders for the sample issue (R29).
func parseParameter(e entry) (parameter, error) {
	v := resolve(e.value)
	if v.Kind != yaml.ScalarNode {
		return parameter{}, keyError(e.path, v.Line, parameterShape)
	}
	p := parameter{key: e.key, value: v, path: e.path}
	if v.ShortTag() != strTag {
		return p, nil
	}
	tmpl, err := crew.ParseParameterTemplate(e.key.Value, v.Value)
	if err != nil {
		return parameter{}, keyError(e.path, v.Line, err.Error())
	}
	p.text = &tmpl
	return p, nil
}

// useParameters checks the parameters of the reference ref to a function:
// its value, empty for none, or a mapping of parameters.
func useParameters(ref *entry) ([]parameter, error) {
	if ref == nil {
		return nil, nil
	}
	v := resolve(ref.value)
	switch {
	case v.ShortTag() == nullTag:
		return nil, nil
	case v.Kind != yaml.MappingNode:
		return nil, keyError(ref.path, v.Line, parametersShape)
	}
	section, sectionErr := named(v, ref.path)
	params, err := parameters(section)
	return params, errors.Join(sectionErr, err)
}

// mergeParameters returns a use's parameters: the preset's, each replaced by the
// use's of the same key, then the use's keys the preset does not write
// (KTD-F9). Each keeps where it is written.
func mergeParameters(preset, use []parameter) []parameter {
	out := make([]parameter, 0, len(preset)+len(use))
	for _, p := range preset {
		if i := slices.IndexFunc(use, p.sameKey); i >= 0 {
			p = use[i]
		}
		out = append(out, p)
	}
	for _, u := range use {
		if !slices.ContainsFunc(preset, u.sameKey) {
			out = append(out, u)
		}
	}
	return out
}

// sameKey tells whether p and other are the same parameter.
func (p parameter) sameKey(other parameter) bool {
	return p.key.Value == other.key.Value
}

// functionUse returns the call of the function p presets, with params as
// its parameters, written at path on line, and the use crew builds it
// from (KTD-F7).
func functionUse(p preset, params []parameter, path string, line int, functions map[string][]crew.Verdict,
) (crew.FunctionSpec, FunctionUse) {
	spec := crew.FunctionSpec{
		Function: p.function, Use: crew.FunctionUse(path),
		Verdicts: functions[string(p.function)], ResumeSelf: p.resumeSelf,
	}
	for _, param := range params {
		if param.text != nil {
			spec.Texts = append(spec.Texts, crew.TextParameter{Name: param.key.Value, Template: *param.text})
		}
	}
	bindTexts := bindParameters(path, params)
	// Every text parameter rendered for the sample issue when it was parsed,
	// so this renders them all.
	sample, _ := spec.RenderTexts(sampleIssue())
	use := FunctionUse{
		Use: spec.Use, Function: p.function, Section: bindTexts(sample), Bind: bindTexts,
		Refused: func(name, reason string) error {
			for _, param := range params {
				if param.key.Value == name {
					return keyError(param.path, param.key.Line, reason)
				}
			}
			return keyError(path, line, fmt.Sprintf("parameter %q: %s", name, reason))
		},
		Failed: func(err error) error { return keyError(path, line, err.Error()) },
	}
	return spec, use
}

// bindParameters returns what builds the Decode of params, the parameters
// of the use at path, with each text parameter's value replaced by its
// text. Each Decode gets copies of the nodes, so none is shared.
func bindParameters(path string, params []parameter) func(texts map[string]string) Decode {
	return func(texts map[string]string) Decode {
		section := make([]entry, len(params))
		for i, p := range params {
			key, value := *p.key, *p.value
			if p.text != nil {
				value.Value, value.Tag = texts[p.key.Value], strTag
			}
			section[i] = entry{key: &key, value: &value, path: p.path}
		}
		return bind(path, section)
	}
}

// sampleIssue is the issue a use's text parameters are rendered for when
// the config loads, as the domain renders prompts and parameters to check
// them.
func sampleIssue() crew.Issue {
	return crew.NewIssue(crew.IssueData{
		ID: crew.IssueID{Key: "42"}, Ref: "#42", Title: "Sample issue", URL: "https://example.com/issues/42",
	})
}

// callee is what a name in a rule's actions or a route's steps calls: a
// shell action of actions, or a function, through one of actions' presets
// or by its own name.
type callee struct {
	name     crew.ActionName
	shell    crew.ShellSpec
	function *preset
}

// callee returns what the scalar n, an item or a step at path, names: one
// of actions, which wins, or a registered function.
func (env ruleEnv) callee(n *yaml.Node, path string) (callee, error) {
	n = resolve(n)
	if n.Kind != yaml.ScalarNode {
		return callee{}, keyError(path, n.Line, itemShape)
	}
	name := crew.ActionName(n.Value)
	if spec, ok := env.actions[name]; ok {
		return callee{name: name, shell: spec}, nil
	}
	if p, ok := env.presets[name]; ok {
		return callee{name: name, function: &p}, nil
	}
	if _, ok := env.functions[n.Value]; ok {
		return callee{name: name, function: &preset{function: crew.FunctionName(n.Value)}}, nil
	}
	return callee{}, keyError(path, n.Line, env.unknownAction(n.Value))
}

// unknownAction says that no action of actions and no registered function
// is called name, listing those that are.
func (env ruleEnv) unknownAction(name string) string {
	actions := slices.Concat(sortedKeys(env.actions), sortedKeys(env.presets))
	slices.Sort(actions)
	msg := fmt.Sprintf("action %q does not exist; actions declares none", name)
	if len(actions) > 0 {
		msg = fmt.Sprintf("action %q does not exist; the actions are %s", name, strings.Join(actions, ", "))
	}
	if len(env.functions) > 0 {
		msg += "; the registered functions are " + strings.Join(sortedKeys(env.functions), ", ")
	}
	return msg
}

// call returns what c runs when an item or a step at path, on line, names
// it: its shell action, which takes no parameters, or its function's call
// with the parameters of ref, the reference whose key names c, nil when
// the name is written alone. A function's use is added to env's uses.
func (env ruleEnv) call(c callee, ref *entry, path string, line int) (crew.ActionKind, error) {
	if c.function == nil {
		if ref != nil {
			return c.shell, noParameters(*ref)
		}
		return c.shell, nil
	}
	params, err := useParameters(ref)
	if err != nil {
		return nil, err
	}
	spec, use := functionUse(*c.function, mergeParameters(c.function.params, params), path, line, env.functions)
	*env.uses = append(*env.uses, use)
	return spec, nil
}

// step returns the route step that calls c, as call does.
func (env ruleEnv) step(c callee, ref *entry, path string, line int) (crew.Step, error) {
	kind, err := env.call(c, ref, path, line)
	if spec, ok := kind.(crew.FunctionSpec); ok {
		return crew.FunctionStep{Name: c.name, Function: spec}, err
	}
	return crew.ShellStep{Name: c.name, Shell: c.shell}, err
}
