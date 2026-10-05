package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestTheRepositorysIssueTemplatesMatchItsConfig keeps this repository's
// example config and .github/ISSUE_TEMPLATE/ in step: every rule and
// extra label names a template that exists, and that template's frontmatter
// gives exactly the type's label, so an issue opened on the web lands where
// the /cw-create-issue skill would put it. Config keeps no template fields,
// so the test decodes the file itself once Load has validated it.
func TestTheRepositorysIssueTemplatesMatchItsConfig(t *testing.T) {
	root := filepath.Join("..", "..")
	loadExample(t)
	// Several types may share a template, such as a rule and an extra that
	// both hold a brainstormed feature. Its labels name one of them.
	templates, labelsOf := templateLabels(t, repositoryIssueTypes(t))
	for _, name := range templates {
		checkIssueTemplate(t, root, name, labelsOf[name])
	}
}

// issueType is a rule or an extra label of .crew/config.yaml, with the keys
// an issue template is chosen and labelled by.
type issueType struct {
	Name          string `yaml:"name"`
	Label         string `yaml:"label"`
	Description   string `yaml:"description"`
	IssueTemplate string `yaml:"issue_template"`
}

// namedIssueType is an issueType with what the test calls it in its errors.
type namedIssueType struct {
	issueType

	what string
}

// repositoryIssueTypes reads the rules and extra labels of the repository's
// example config.
func repositoryIssueTypes(t *testing.T) []namedIssueType {
	t.Helper()
	data, err := os.ReadFile(exampleConfig)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Rules       []issueType `yaml:"workflow"`
		ExtraLabels []issueType `yaml:"extra_labels"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	types := make([]namedIssueType, 0, len(cfg.Rules)+len(cfg.ExtraLabels))
	for _, s := range cfg.Rules {
		types = append(types, namedIssueType{issueType: s, what: "workflow stage " + s.Name})
	}
	for _, e := range cfg.ExtraLabels {
		types = append(types, namedIssueType{issueType: e, what: "extra label " + e.Label})
	}
	return types
}

// templateLabels checks that every type has a description and a template,
// and returns the templates in the order first named, with the labels of the
// types that name each.
func templateLabels(t *testing.T, types []namedIssueType) ([]string, map[string][]string) {
	t.Helper()
	var templates []string
	labelsOf := map[string][]string{}
	for _, typ := range types {
		what := typ.what
		if typ.Description == "" {
			t.Errorf("%s has no description", what)
		}
		if typ.IssueTemplate == "" {
			t.Errorf("%s has no issue_template", what)
			continue
		}
		if _, seen := labelsOf[typ.IssueTemplate]; !seen {
			templates = append(templates, typ.IssueTemplate)
		}
		labelsOf[typ.IssueTemplate] = append(labelsOf[typ.IssueTemplate], typ.Label)
	}
	return templates, labelsOf
}

// checkIssueTemplate checks that the issue template name under root has what
// GitHub needs to list it and one label, one of labels.
func checkIssueTemplate(t *testing.T, root, name string, labels []string) {
	t.Helper()
	path := filepath.Join(root, ".github", "ISSUE_TEMPLATE", name)
	front, err := templateFrontmatter(path)
	if err != nil {
		t.Errorf("issue_template %s: %v", name, err)
		return
	}
	if front.Name == "" || front.About == "" {
		t.Errorf("%s: GitHub needs name and about to list it; got name %q, about %q", path, front.Name, front.About)
	}
	if len(front.Labels) != 1 || !slices.Contains(labels, front.Labels[0]) {
		t.Errorf("%s: labels = %q, want one of the labels of the types that name it, %q", path, front.Labels, labels)
	}
}

// issueTemplateFront is the frontmatter of a GitHub Markdown issue template.
type issueTemplateFront struct {
	Name   string
	About  string
	Labels []string
}

// templateFrontmatter reads the frontmatter between the leading "---" lines
// of a Markdown issue template. GitHub takes labels as a list or as one
// comma-separated string, so both are read.
func templateFrontmatter(path string) (issueTemplateFront, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return issueTemplateFront{}, fmt.Errorf("read: %w", err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return issueTemplateFront{}, errors.New(`does not start with a "---" frontmatter line`)
	}
	front, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return issueTemplateFront{}, errors.New(`has no closing "---" frontmatter line`)
	}
	var doc struct {
		Name   string    `yaml:"name"`
		About  string    `yaml:"about"`
		Labels yaml.Node `yaml:"labels"`
	}
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return issueTemplateFront{}, fmt.Errorf("frontmatter: %w", err)
	}
	out := issueTemplateFront{Name: doc.Name, About: doc.About}
	switch doc.Labels.Kind {
	case yaml.SequenceNode:
		if err := doc.Labels.Decode(&out.Labels); err != nil {
			return issueTemplateFront{}, fmt.Errorf("labels: %w", err)
		}
	case yaml.ScalarNode:
		for l := range strings.SplitSeq(doc.Labels.Value, ",") {
			if l = strings.TrimSpace(l); l != "" {
				out.Labels = append(out.Labels, l)
			}
		}
	default:
		// No labels, or labels in a shape GitHub does not read: none.
	}
	return out, nil
}
