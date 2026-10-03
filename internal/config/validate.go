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
	Check  located[string] `yaml:"check"`
}

// What each kind of list item must be, said when an item is not a mapping.
const (
	stageShape = "must be a stage with name, label, moves_to, on_success, on_failure, actions, " +
		"and optionally description and issue_template"
	actionShape = "must be an action with name and prompt"
	extraShape  = "must be an extra label with label, and optionally description and issue_template"
)

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
	var doc stageDoc
	if err := decodeItem(n, path, stageShape, &doc); err != nil {
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
	var errs []error
	out := make([]crew.Action, 0, len(n.Content))
	firstPath := make(map[string]string, len(n.Content))
	for i, item := range n.Content {
		action, err := parseAction(item, fmt.Sprintf("%s[%d]", path, i), firstPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, action)
	}
	return out, errors.Join(errs...)
}

// parseAction decodes and checks the action n at path. firstPath holds the
// path of each action name the stage already has; a valid name is added to
// it.
func parseAction(n *yaml.Node, path string, firstPath map[string]string) (crew.Action, error) {
	var doc actionDoc
	if err := decodeItem(n, path, actionShape, &doc); err != nil {
		return crew.Action{}, err
	}
	name, nameErr := required(doc.Name, path+".name", n.Line)
	prompt, promptErr := required(doc.Prompt, path+".prompt", n.Line)
	if nameErr != nil || promptErr != nil {
		return crew.Action{}, errors.Join(nameErr, promptErr)
	}
	if first, ok := firstPath[name]; ok {
		return crew.Action{}, keyError(path+".name", doc.Name.line, fmt.Sprintf("action %q is already %s", name, first))
	}
	firstPath[name] = path
	// A check present but blank would run nothing and pass every time.
	if doc.Check.line != 0 && strings.TrimSpace(doc.Check.value) == "" {
		return crew.Action{}, keyError(path+".check", doc.Check.line, "must not be empty")
	}
	action := crew.Action{Name: name, Prompt: prompt, Check: doc.Check.value}
	if _, err := action.Render(sampleIssue()); err != nil {
		return crew.Action{}, keyError(path+".prompt", doc.Prompt.line, err.Error())
	}
	return action, nil
}

// decodeItem decodes the list item n at path into the struct target points
// to. An item that is not a mapping is reported with shape, what it must be.
func decodeItem(n *yaml.Node, path, shape string, target any) error {
	if n.Kind != yaml.MappingNode {
		return keyError(path, n.Line, shape)
	}
	return decodeFields(entries(n, path), reflect.ValueOf(target).Elem())
}

// sampleIssue is the issue every prompt is rendered for at load, so a bad
// template stops crew before polling rather than when an issue is taken.
func sampleIssue() crew.Issue {
	return crew.Issue{Key: "42", Ref: "#42", Title: "Sample issue", URL: "https://example.com/issues/42"}
}

// prompts checks the top-level prompts: a mapping from a name to a prompt
// that a skill, such as /cw-brainstorm, runs in the user's own session. crew
// does not run them, so they are checked but not kept: each must be
// non-empty text that renders as an action's prompt does.
func prompts(n *yaml.Node) error {
	section, err := mapping(n, "prompts")
	if err != nil {
		return err
	}
	var errs []error
	seen := make(map[string]int, len(section))
	for _, e := range section {
		name := e.key.Value
		if first, ok := seen[name]; ok {
			errs = append(errs, keyError(e.path, e.key.Line, fmt.Sprintf("duplicate key, first set on line %d", first)))
			continue
		}
		seen[name] = e.key.Line
		var prompt located[string]
		if err := decodeValue(e.value, e.path, reflect.ValueOf(&prompt).Elem()); err != nil {
			errs = append(errs, err)
			continue
		}
		if prompt.value == "" {
			errs = append(errs, keyError(e.path, e.key.Line, "must not be empty"))
			continue
		}
		action := crew.Action{Name: name, Prompt: prompt.value}
		if _, err := action.Render(sampleIssue()); err != nil {
			errs = append(errs, keyError(e.path, e.key.Line, err.Error()))
		}
	}
	return errors.Join(errs...)
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
	taken := lowerStates(workflow)
	firstPath := make(map[string]string, len(n.Content))
	var errs []error
	out := make([]crew.State, 0, len(n.Content))
	for i, item := range n.Content {
		path := fmt.Sprintf("extra_labels[%d]", i)
		var doc extraDoc
		if err := decodeItem(item, path, extraShape, &doc); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := described(doc.Description, doc.IssueTemplate, path); err != nil {
			errs = append(errs, err)
		}
		label, err := extraLabel(doc.Label, path, item.Line, taken, firstPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, label)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// lowerStates returns the workflow's states, lowercased.
func lowerStates(workflow []crew.Stage) map[string]bool {
	states := map[string]bool{}
	for _, s := range crew.WorkflowStates(workflow) {
		states[strings.ToLower(string(s))] = true
	}
	return states
}

// extraLabel checks the label of the extra at path, whose mapping is on
// itemLine. taken holds the workflow's states and firstPath the path of
// each earlier extra, both by lowercased label; a valid label is added to
// firstPath.
func extraLabel(l located[string], path string, itemLine int, taken map[string]bool,
	firstPath map[string]string,
) (crew.State, error) {
	label, err := state(l, path+".label", itemLine)
	if err != nil {
		return "", err
	}
	key := strings.ToLower(string(label))
	if taken[key] {
		return "", keyError(path+".label", l.line,
			fmt.Sprintf("%q is a label the workflow names; an extra label must be one no stage names", label))
	}
	if first, ok := firstPath[key]; ok {
		return "", keyError(path+".label", l.line, fmt.Sprintf("%q is already %s.label", label, first))
	}
	firstPath[key] = path
	return label, nil
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
