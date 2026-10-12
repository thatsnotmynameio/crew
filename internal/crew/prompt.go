package crew

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"text/template"
)

// Prompt is an action's prompt: a text/template over PromptData, parsed
// once when the config loads. A Prompt that ParsePrompt returned renders
// for any data whose fields render; the zero Prompt renders the empty
// string.
type Prompt struct {
	action ActionName
	text   string
	tmpl   *template.Template
}

// PromptData is what a session's prompt is rendered for: the run's issue,
// its base and its stack (KTD8).
type PromptData struct {
	// Issue is the run's issue.
	Issue Issue
	// Base is the branch the run's work goes on top of: its stack's branch
	// when stacked, else origin's default branch; empty while the run does
	// not know it.
	Base string
	// Stack is the blocker's pull request the run was taken on, when its
	// take stacked it.
	Stack Optional[Stack]
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

// newPromptIssue returns the template data of issue.
func newPromptIssue(issue Issue) promptIssue {
	return promptIssue{Ref: issue.Ref(), Key: issue.ID().Key, Title: issue.Title(), URL: issue.URL()}
}

// promptData is the only data a prompt template can reach: a struct, not
// PromptData itself, so templates depend on exactly these fields.
type promptData struct {
	Issue promptIssue
	Base  string
	Stack promptStack
}

// promptStack is the only stack data a prompt template can reach, all of
// it empty when the run is unstacked. A struct, not the Stack itself, so
// any other name, such as {{.Stack.Blocker.Number}}, fails to render.
type promptStack struct {
	Stacked     bool
	Blocker     promptLink
	PullRequest promptLink
	Branch      string
}

// promptLink is an issue or a pull request in a prompt's stack.
type promptLink struct {
	Ref string
	URL string
}

// newPromptData returns the template data of d.
func newPromptData(d PromptData) promptData {
	data := promptData{Issue: newPromptIssue(d.Issue), Base: d.Base}
	if s, ok := d.Stack.Get(); ok {
		data.Stack = promptStack{
			Stacked: true, Blocker: promptLink{Ref: s.BlockerRef, URL: s.BlockerURL},
			PullRequest: promptLink{Ref: s.PullRequestRef, URL: s.PullRequestURL}, Branch: s.Branch,
		}
	}
	return data
}

// sampleIssue is the issue every prompt is rendered for when it is parsed,
// so a bad template stops crew before polling rather than when an issue is
// taken.
func sampleIssue() Issue {
	return NewIssue(IssueData{
		ID: IssueID{Key: "42"}, Ref: "#42", Title: "Sample issue", URL: "https://example.com/issues/42",
	})
}

// samplePromptData are the data every prompt is rendered for when it is
// parsed: an unstacked and a stacked run of the sample issue, so a bad
// template inside {{if .Stack.Stacked}} stops crew too.
func samplePromptData() []PromptData {
	stack := Stack{
		Blocker: IssueID{Key: "41"}, BlockerRef: "#41", BlockerURL: "https://example.com/issues/41",
		PullRequestRef: "#45", PullRequestURL: "https://example.com/pull/45", Branch: "crew/issue-41-implement",
	}
	return []PromptData{
		{Issue: sampleIssue(), Base: "main"},
		{Issue: sampleIssue(), Base: stack.Branch, Stack: Some(stack)},
	}
}

// ParsePrompt parses text, the prompt of the action named action, and
// renders it for an unstacked and a stacked sample run. The template's data
// is .Issue with the fields Ref, Key, Title and URL, .Base, and .Stack with
// the fields Stacked, Blocker.Ref, Blocker.URL, PullRequest.Ref,
// PullRequest.URL and Branch; a template that does not parse, or that names
// any other field, is an error naming the action.
func ParsePrompt(action ActionName, text string) (Prompt, error) {
	tmpl, err := template.New(string(action)).Parse(text)
	if err != nil {
		return Prompt{}, fmt.Errorf("parse prompt of action %q: %w", action, err)
	}
	p := Prompt{action: action, text: text, tmpl: tmpl}
	for _, d := range samplePromptData() {
		if _, err := p.Render(d); err != nil {
			return Prompt{}, err
		}
	}
	return p, nil
}

// Render renders the prompt for d. An error names the action.
func (p Prompt) Render(d PromptData) (string, error) {
	if p.tmpl == nil {
		return "", nil
	}
	var out strings.Builder
	if err := p.tmpl.Execute(&out, newPromptData(d)); err != nil {
		return "", fmt.Errorf("render prompt of action %q: %w", p.action, err)
	}
	return out.String(), nil
}

// Text returns the prompt as the config wrote it.
func (p Prompt) Text() string { return p.text }

// Hash returns the SHA-256, in hex, of the prompt as the config wrote it:
// the same template in two actions hashes the same, and an edited one
// differently. The rendered prompt is not in it (KTD8).
func (p Prompt) Hash() string {
	sum := sha256.Sum256([]byte(p.text))
	return hex.EncodeToString(sum[:])
}
