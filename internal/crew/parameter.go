package crew

import (
	"fmt"
	"strings"
	"text/template"
)

// TextParameter is one of a function use's text parameters: its name, and
// its value as a template over the issue.
type TextParameter struct {
	Name     string
	Template ParameterTemplate
}

// ParameterTemplate is the value of a function's text parameter: a
// text/template over the issue, parsed once when the config loads. A
// ParameterTemplate that ParseParameterTemplate returned renders for any
// issue whose fields render; the zero ParameterTemplate renders the empty
// string.
type ParameterTemplate struct {
	name string
	tmpl *template.Template
}

// ParseParameterTemplate parses text, the value of the parameter named
// name, and renders it for a sample issue. The template's data is .Issue
// with the fields Ref, Key, Title and URL, as a prompt's; a template that
// does not parse, or that names any other field, is an error naming the
// parameter.
func ParseParameterTemplate(name, text string) (ParameterTemplate, error) {
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		return ParameterTemplate{}, fmt.Errorf("parse parameter %q: %w", name, err)
	}
	p := ParameterTemplate{name: name, tmpl: tmpl}
	if _, err := p.Render(sampleIssue()); err != nil {
		return ParameterTemplate{}, err
	}
	return p, nil
}

// Render renders the parameter's value for issue. An error names the
// parameter.
func (p ParameterTemplate) Render(issue Issue) (string, error) {
	if p.tmpl == nil {
		return "", nil
	}
	data := struct{ Issue promptIssue }{newPromptIssue(issue)}
	var out strings.Builder
	if err := p.tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render parameter %q: %w", p.name, err)
	}
	return out.String(), nil
}

// RenderTexts returns each of s's text parameters rendered for issue, by
// name, or the error of the first that does not render, which names it.
func (s FunctionSpec) RenderTexts(issue Issue) (map[string]string, error) {
	out := make(map[string]string, len(s.Texts))
	for _, p := range s.Texts {
		text, err := p.Template.Render(issue)
		if err != nil {
			return nil, err
		}
		out[p.Name] = text
	}
	return out, nil
}

// SampleTexts returns each of s's text parameters rendered for the sample
// issue every template is checked against when the config loads. It cannot
// fail: ParseParameterTemplate rendered each one for that issue already.
func (s FunctionSpec) SampleTexts() map[string]string {
	out, _ := s.RenderTexts(sampleIssue())
	return out
}
