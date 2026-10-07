package crew

import (
	"fmt"
	"strings"
	"text/template"
)

// CommentTemplate is the text of a route's comment step: a text/template
// over CommentData, parsed once when the config loads. A CommentTemplate
// that ParseCommentTemplate returned renders for any data whose issue
// renders; the zero CommentTemplate renders the empty string.
type CommentTemplate struct {
	route RouteName
	text  string
	tmpl  *template.Template
}

// CommentData is what a comment template is rendered for: only what crew
// itself knows of a run, never a session's or a script's words.
type CommentData struct {
	// Issue is the run's issue.
	Issue Issue
	// Rule is the run's rule.
	Rule RuleName
	// Action is the action that ended the run's sequence.
	Action ActionName
	// Verdict is that action's verdict.
	Verdict Verdict
	// Route is the route the run ends through.
	Route RouteName
	// Log is the repository-relative path of the run's log.
	Log string
}

// commentData is the only data a comment template can reach: a struct,
// not CommentData itself, so templates depend on exactly these fields and
// any other name, such as {{.Reason}}, fails to render.
type commentData struct {
	Issue   promptIssue
	Rule    RuleName
	Action  ActionName
	Verdict Verdict
	Route   RouteName
	Log     string
}

// sampleCommentData is the data every comment template is rendered for when
// it is parsed, so a bad template stops crew before polling rather than
// when a run ends.
func sampleCommentData() CommentData {
	return CommentData{
		Issue: sampleIssue(), Rule: "rule", Action: "action", Verdict: Failed, Route: FailedRoute,
		Log: ".crew/logs/issue-42-rule.log",
	}
}

// ParseCommentTemplate parses text, the comment of the route named route,
// and renders it for sample data. The template's data is .Issue with the
// fields Ref, Key, Title and URL, and .Rule, .Action, .Verdict, .Route and
// .Log; a template that does not parse, or that names any other field, is
// an error naming the route.
func ParseCommentTemplate(route RouteName, text string) (CommentTemplate, error) {
	tmpl, err := template.New(string(route)).Parse(text)
	if err != nil {
		return CommentTemplate{}, fmt.Errorf("parse comment of route %q: %w", route, err)
	}
	t := CommentTemplate{route: route, text: text, tmpl: tmpl}
	if _, err := t.Render(sampleCommentData()); err != nil {
		return CommentTemplate{}, err
	}
	return t, nil
}

// Render renders the comment for d. An error names the route.
func (t CommentTemplate) Render(d CommentData) (string, error) {
	if t.tmpl == nil {
		return "", nil
	}
	data := commentData{
		Issue: newPromptIssue(d.Issue), Rule: d.Rule, Action: d.Action, Verdict: d.Verdict, Route: d.Route, Log: d.Log,
	}
	var out strings.Builder
	if err := t.tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render comment of route %q: %w", t.route, err)
	}
	return out.String(), nil
}

// Text returns the comment as the config wrote it.
func (t CommentTemplate) Text() string { return t.text }
