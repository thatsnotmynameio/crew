package crew

import (
	"fmt"
	"strings"
	"text/template"
)

// Prompt is an action's prompt: a text/template over the issue, parsed once
// when the config loads. A Prompt that ParsePrompt returned renders for any
// issue whose fields render; the zero Prompt renders the empty string.
type Prompt struct {
	action ActionName
	text   string
	tmpl   *template.Template
}

// promptIssue is the only issue data a prompt template can reach. A struct,
// not the Issue itself, so templates depend on exactly these four fields and
// any other name, such as {{.Issue.Number}}, fails to render.
type promptIssue struct {
	Ref   string
	Key   string
	Title string
	URL   string
}

// sampleIssue is the issue every prompt is rendered for when it is parsed,
// so a bad template stops crew before polling rather than when an issue is
// taken.
func sampleIssue() Issue {
	return NewIssue(IssueData{
		ID: IssueID{Key: "42"}, Ref: "#42", Title: "Sample issue", URL: "https://example.com/issues/42",
	})
}

// ParsePrompt parses text, the prompt of the action named action, and
// renders it for a sample issue. The template's data is .Issue with the
// fields Ref, Key, Title and URL; a template that does not parse, or that
// names any other field, is an error naming the action.
func ParsePrompt(action ActionName, text string) (Prompt, error) {
	tmpl, err := template.New(string(action)).Parse(text)
	if err != nil {
		return Prompt{}, fmt.Errorf("parse prompt of action %q: %w", action, err)
	}
	p := Prompt{action: action, text: text, tmpl: tmpl}
	if _, err := p.Render(sampleIssue()); err != nil {
		return Prompt{}, err
	}
	return p, nil
}

// Render renders the prompt for issue. An error names the action.
func (p Prompt) Render(issue Issue) (string, error) {
	if p.tmpl == nil {
		return "", nil
	}
	data := struct{ Issue promptIssue }{
		promptIssue{Ref: issue.Ref(), Key: issue.ID().Key, Title: issue.Title(), URL: issue.URL()},
	}
	var out strings.Builder
	if err := p.tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render prompt of action %q: %w", p.action, err)
	}
	return out.String(), nil
}

// Text returns the prompt as the config wrote it.
func (p Prompt) Text() string { return p.text }
