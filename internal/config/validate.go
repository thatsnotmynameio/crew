package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

type stageDoc struct {
	Name      located[string] `yaml:"name"`
	Label     located[string] `yaml:"label"`
	MovesTo   located[string] `yaml:"moves_to"`
	OnSuccess located[string] `yaml:"on_success"`
	Actions   yaml.Node       `yaml:"actions"`
}

type actionDoc struct {
	Name   located[string] `yaml:"name"`
	Prompt located[string] `yaml:"prompt"`
}

// parsedStage is a stage that passed its own checks, with what the
// workflow-graph checks need to name its keys.
type parsedStage struct {
	crew.Stage
	path string
	doc  stageDoc
}

// workflow decodes and validates workflow:, reporting every error it finds.
func workflow(n *yaml.Node) ([]crew.Stage, error) {
	if n.Kind == 0 {
		return nil, errors.New("workflow: missing; list at least one stage")
	}
	if n.Kind != yaml.SequenceNode || len(n.Content) == 0 {
		return nil, keyError("workflow", n.Line, "list at least one stage")
	}
	var errs []error
	parsed := make([]parsedStage, 0, len(n.Content))
	for i, item := range n.Content {
		stage, err := parseStage(item, fmt.Sprintf("workflow[%d]", i))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		parsed = append(parsed, stage)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if err := checkGraph(parsed); err != nil {
		return nil, err
	}
	stages := make([]crew.Stage, len(parsed))
	for i, p := range parsed {
		stages[i] = p.Stage
	}
	return stages, nil
}

func parseStage(n *yaml.Node, path string) (parsedStage, error) {
	if n.Kind != yaml.MappingNode {
		return parsedStage{}, keyError(path, n.Line, "must be a stage with name, label, moves_to, on_success and actions")
	}
	var doc stageDoc
	if err := decodeFields(entries(n, path), reflect.ValueOf(&doc).Elem()); err != nil {
		return parsedStage{}, err
	}
	p := parsedStage{path: path, doc: doc}
	var errs []error
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	var err error
	p.Name, err = required(doc.Name, path+".name", n.Line)
	collect(err)
	p.Label, err = state(doc.Label, path+".label", n.Line)
	collect(err)
	p.MovesTo, err = state(doc.MovesTo, path+".moves_to", n.Line)
	collect(err)
	p.OnSuccess, err = state(doc.OnSuccess, path+".on_success", n.Line)
	collect(err)
	p.Actions, err = actions(&doc.Actions, path+".actions", n.Line)
	collect(err)
	return p, errors.Join(errs...)
}

func actions(n *yaml.Node, path string, stageLine int) ([]crew.Action, error) {
	switch {
	case n.Kind == 0:
		return nil, keyError(path, stageLine, "list at least one action")
	case n.Kind != yaml.SequenceNode || len(n.Content) == 0:
		return nil, keyError(path, n.Line, "list at least one action")
	}
	// Prompts are rendered for this issue at load, so a bad template stops
	// crew before polling rather than when an issue is taken.
	sample := crew.Issue{Key: "42", Ref: "#42", Title: "Sample issue", URL: "https://example.com/issues/42"}
	var errs []error
	out := make([]crew.Action, 0, len(n.Content))
	firstPath := make(map[string]string, len(n.Content))
	for i, item := range n.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if item.Kind != yaml.MappingNode {
			errs = append(errs, keyError(itemPath, item.Line, "must be an action with name and prompt"))
			continue
		}
		var doc actionDoc
		if err := decodeFields(entries(item, itemPath), reflect.ValueOf(&doc).Elem()); err != nil {
			errs = append(errs, err)
			continue
		}
		name, nameErr := required(doc.Name, itemPath+".name", item.Line)
		prompt, promptErr := required(doc.Prompt, itemPath+".prompt", item.Line)
		if nameErr != nil || promptErr != nil {
			errs = append(errs, nameErr, promptErr)
			continue
		}
		if first, ok := firstPath[name]; ok {
			errs = append(errs, keyError(itemPath+".name", doc.Name.line, fmt.Sprintf("action %q is already %s", name, first)))
			continue
		}
		firstPath[name] = itemPath
		action := crew.Action{Name: name, Prompt: prompt}
		if _, err := action.Render(sample); err != nil {
			errs = append(errs, keyError(itemPath+".prompt", doc.Prompt.line, err.Error()))
			continue
		}
		out = append(out, action)
	}
	return out, errors.Join(errs...)
}

// checkGraph rejects a workflow that would loop or take an issue twice.
func checkGraph(stages []parsedStage) error {
	var errs []error
	byLabel := make(map[crew.State]parsedStage, len(stages))
	byName := make(map[string]parsedStage, len(stages))
	for _, s := range stages {
		if other, ok := byName[s.Name]; ok {
			errs = append(errs, keyError(s.path+".name", s.doc.Name.line,
				fmt.Sprintf("stage %q is already %s", s.Name, other.path)))
		} else {
			byName[s.Name] = s
		}
		if other, ok := byLabel[s.Label]; ok {
			errs = append(errs, keyError(s.path+".label", s.doc.Label.line,
				fmt.Sprintf("stage %q takes %q, as stage %q (%s) does; two stages cannot take the same state",
					s.Name, s.Label, other.Name, other.path)))
		} else {
			byLabel[s.Label] = s
		}
		if s.OnSuccess == s.Label {
			errs = append(errs, keyError(s.path+".on_success", s.doc.OnSuccess.line,
				fmt.Sprintf("%q is the stage's own label, so stage %q would take the issue again", s.OnSuccess, s.Name)))
		}
	}
	for _, s := range stages {
		if other, ok := byLabel[s.MovesTo]; ok {
			errs = append(errs, keyError(s.path+".moves_to", s.doc.MovesTo.line,
				fmt.Sprintf("%q is the label of stage %q (%s.label), which would take the issue while stage %q runs",
					s.MovesTo, other.Name, other.path, s.Name)))
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

// state returns a key's value as one of the eight crew states.
func state(l located[string], path string, parentLine int) (crew.State, error) {
	v, err := required(l, path, parentLine)
	if err != nil {
		return "", err
	}
	s := crew.State(v)
	if !s.Valid() {
		names := make([]string, 0, len(crew.States()))
		for _, valid := range crew.States() {
			names = append(names, string(valid))
		}
		return "", keyError(path, l.line, fmt.Sprintf("%q is not a crew state; use one of %s", v, strings.Join(names, ", ")))
	}
	return s, nil
}
