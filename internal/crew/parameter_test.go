package crew

import (
	"reflect"
	"strings"
	"testing"
)

func TestAParameterTemplateRendersTheIssue(t *testing.T) {
	p, err := ParseParameterTemplate("title", "Fixes {{.Issue.Ref}}: {{.Issue.Title}} ({{.Issue.Key}}, {{.Issue.URL}})")
	if err != nil {
		t.Fatalf("ParseParameterTemplate: %v", err)
	}
	got, err := p.Render(NewIssue(testIssue()))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	issue := NewIssue(testIssue())
	want := "Fixes " + issue.Ref() + ": " + issue.Title() + " (" + issue.ID().Key + ", " + issue.URL() + ")"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestParseParameterTemplateRefusesABadTemplate(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"does not parse", "{{.Issue.Ref", `parse parameter "title": `},
		{"unknown field", "Fixes {{.Issue.Number}}", `render parameter "title": `},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseParameterTemplate("title", tt.text)
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("ParseParameterTemplate(%q) = %v, want an error starting %q", tt.text, err, tt.want)
			}
		})
	}
}

func TestZeroParameterTemplateRendersNothing(t *testing.T) {
	got, err := ParameterTemplate{}.Render(NewIssue(IssueData{Ref: "#1"}))
	if err != nil || got != "" {
		t.Fatalf("ParameterTemplate{}.Render = %q, %v; want \"\", nil", got, err)
	}
}

// parameter returns the text parameter name, parsed from text.
func parameter(t *testing.T, name, text string) TextParameter {
	t.Helper()
	tmpl, err := ParseParameterTemplate(name, text)
	if err != nil {
		t.Fatalf("ParseParameterTemplate(%q): %v", text, err)
	}
	return TextParameter{Name: name, Template: tmpl}
}

func TestAFunctionRendersItsTextsForTheIssue(t *testing.T) {
	spec := FunctionSpec{Function: "check", Texts: []TextParameter{
		parameter(t, "title", "Fixes {{.Issue.Ref}}"), parameter(t, "base", "main"),
	}}
	got, err := spec.RenderTexts(NewIssue(IssueData{Ref: "#7"}))
	if want := map[string]string{"title": "Fixes #7", "base": "main"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("RenderTexts = %#v, %v; want %#v, nil", got, err, want)
	}
	none, err := FunctionSpec{Function: "check"}.RenderTexts(NewIssue(IssueData{Ref: "#7"}))
	if err != nil || len(none) != 0 {
		t.Errorf("RenderTexts without texts = %#v, %v; want none", none, err)
	}
}

func TestAFunctionsTextThatDoesNotRenderNamesItsParameter(t *testing.T) {
	// Renders for the sample issue's title, and fails on a shorter one.
	spec := FunctionSpec{Function: "check", Texts: []TextParameter{
		parameter(t, "base", "main"), parameter(t, "title", "{{slice .Issue.Title 0 10}}"),
	}}
	_, err := spec.RenderTexts(NewIssue(IssueData{Title: "short"}))
	if err == nil || !strings.HasPrefix(err.Error(), `render parameter "title": `) {
		t.Fatalf("RenderTexts = %v, want an error naming the parameter", err)
	}
}
