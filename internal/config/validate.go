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
	Name          located[string] `yaml:"name"`
	Description   located[string] `yaml:"description"`
	IssueTemplate located[string] `yaml:"issue_template"`
	Label         located[string] `yaml:"label"`
	MovesTo       located[string] `yaml:"moves_to"`
	OnSuccess     located[string] `yaml:"on_success"`
	OnFailure     located[string] `yaml:"on_failure"`
	Actions       yaml.Node       `yaml:"actions"`
}

// extraDoc is one item of extra_labels: a label no stage takes.
type extraDoc struct {
	Label         located[string] `yaml:"label"`
	Description   located[string] `yaml:"description"`
	IssueTemplate located[string] `yaml:"issue_template"`
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
	spellOnce(parsed)
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
		return parsedStage{}, keyError(path, n.Line, "must be a stage with name, label, moves_to, on_success, on_failure, actions, and optionally description and issue_template")
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
	collect(described(doc.Description, doc.IssueTemplate, path))
	p.Label, err = state(doc.Label, path+".label", n.Line)
	collect(err)
	p.MovesTo, err = state(doc.MovesTo, path+".moves_to", n.Line)
	collect(err)
	p.OnSuccess, err = state(doc.OnSuccess, path+".on_success", n.Line)
	collect(err)
	p.OnFailure, err = state(doc.OnFailure, path+".on_failure", n.Line)
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

// extraLabels decodes and validates extra_labels: labels for parked work
// that no stage takes. Each must be none of workflow's states and no earlier
// extra, ignoring case as GitHub does. It reports every error it finds.
func extraLabels(n *yaml.Node, workflow []crew.Stage) ([]crew.State, error) {
	switch {
	case n.Kind == 0:
		return nil, nil
	case n.Kind != yaml.SequenceNode:
		return nil, keyError("extra_labels", n.Line, "must be a list of labels")
	}
	taken := map[string]bool{}
	for _, s := range crew.WorkflowStates(workflow) {
		taken[strings.ToLower(string(s))] = true
	}
	firstPath := make(map[string]string, len(n.Content))
	var errs []error
	out := make([]crew.State, 0, len(n.Content))
	for i, item := range n.Content {
		path := fmt.Sprintf("extra_labels[%d]", i)
		if item.Kind != yaml.MappingNode {
			errs = append(errs, keyError(path, item.Line, "must be an extra label with label, and optionally description and issue_template"))
			continue
		}
		var doc extraDoc
		if err := decodeFields(entries(item, path), reflect.ValueOf(&doc).Elem()); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := described(doc.Description, doc.IssueTemplate, path); err != nil {
			errs = append(errs, err)
		}
		label, err := state(doc.Label, path+".label", item.Line)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		key := strings.ToLower(string(label))
		if taken[key] {
			errs = append(errs, keyError(path+".label", doc.Label.line,
				fmt.Sprintf("%q is a label the workflow names; an extra label must be one no stage names", label)))
			continue
		}
		if first, ok := firstPath[key]; ok {
			errs = append(errs, keyError(path+".label", doc.Label.line,
				fmt.Sprintf("%q is already %s.label", label, first)))
			continue
		}
		firstPath[key] = path
		out = append(out, label)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// described checks the optional description and issue_template that a stage
// or an extra label may have. Nothing in crew reads them, the
// /cw-create-issue skill does, so they are checked but not kept.
func described(description, template located[string], path string) error {
	var errs []error
	if description.line > 0 && description.value == "" {
		errs = append(errs, keyError(path+".description", description.line, "must not be empty"))
	}
	if v := template.value; template.line > 0 && (strings.ContainsAny(v, `/\`) || !strings.HasSuffix(v, ".md")) {
		errs = append(errs, keyError(path+".issue_template", template.line,
			fmt.Sprintf("%q must be a file name in .github/ISSUE_TEMPLATE/ ending in .md, such as bug.md", v)))
	}
	return errors.Join(errs...)
}

// spellOnce gives every label the spelling it first has in the workflow, in
// stage order and then label, moves_to, on_success, on_failure. GitHub does
// not tell labels apart by case, so "In Review" and "in review" are one
// label; after this, comparing states exactly compares them as GitHub does.
func spellOnce(stages []parsedStage) {
	first := map[string]crew.State{}
	for i := range stages {
		s := &stages[i]
		for _, state := range []*crew.State{&s.Label, &s.MovesTo, &s.OnSuccess, &s.OnFailure} {
			key := strings.ToLower(string(*state))
			if spelling, ok := first[key]; ok {
				*state = spelling
			} else {
				first[key] = *state
			}
		}
	}
}

// checkGraph rejects a workflow that would loop or take an issue twice. It
// runs after spellOnce, so it compares labels ignoring case.
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
				fmt.Sprintf("stage %q takes %q, as stage %q (%s) does; two stages cannot take the same label",
					s.Name, s.Label, other.Name, other.path)))
		} else {
			byLabel[s.Label] = s
		}
		if s.OnSuccess == s.Label {
			errs = append(errs, keyError(s.path+".on_success", s.doc.OnSuccess.line,
				fmt.Sprintf("%q is the stage's own label, so stage %q would take the issue again", s.OnSuccess, s.Name)))
		}
		if s.OnFailure == s.Label {
			errs = append(errs, keyError(s.path+".on_failure", s.doc.OnFailure.line,
				fmt.Sprintf("%q is the stage's own label, so stage %q would take the failed issue again", s.OnFailure, s.Name)))
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

// state returns a key's value as a state: any non-empty text.
func state(l located[string], path string, parentLine int) (crew.State, error) {
	v, err := required(l, path, parentLine)
	return crew.State(v), err
}
