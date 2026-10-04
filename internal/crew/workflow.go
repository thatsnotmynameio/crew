package crew

import (
	"fmt"
	"strings"
	"text/template"
)

// Stage is one step of the workflow. It takes an item of its Takes kind in
// its Label state, moves it to MovesTo while its actions run, and moves it to
// OnSuccess once every action has succeeded, or to OnFailure when any failed.
type Stage struct {
	// Name identifies the stage in events and the TUI.
	Name string
	// Label is the state an issue must be in for the stage to take it.
	Label State
	// MovesTo is the state the issue is in while the stage's actions run.
	MovesTo State
	// OnSuccess is the state the issue moves to after every action succeeded.
	OnSuccess State
	// OnFailure is the state the issue moves to when any action failed, with
	// a failure report.
	OnFailure State
	// Actions run in parallel, each in its own workspace and session.
	Actions []Action
	// Queue is the queue the stage runs in: the share of the global limit
	// its issues may hold. The zero Queue is no queue: the stage is limited
	// only by the global limit, which it shares with every other such stage.
	Queue Queue
	// Takes is the kind of item the stage takes: it takes only the items of
	// that kind in its Label state. The zero Kind takes issues.
	Takes Kind
	// OffBoard hides the stage from the live view's board: no column, and
	// no card for an issue it holds. Only the live view reads it; the zero
	// value shows the stage.
	OffBoard bool
}

// The queues every workflow has.
const (
	// ClerkQueue is crew's queue for bookkeeping work.
	ClerkQueue = "clerk"
	// DefaultQueue gets the slots the other queues leave, and runs every
	// stage that names no queue.
	DefaultQueue = "default"
)

// Queue is a fixed share of the global limit on the issues crew holds at
// once. Only the stages in a queue use its slots, and it never lends an idle
// one to another queue.
type Queue struct {
	// Name identifies the queue: ClerkQueue, DefaultQueue or a queue the
	// config declares.
	Name string
	// Slots is how many issues the queue's stages may hold at once, possibly
	// zero.
	Slots int
}

// Action is one session a stage runs for an issue.
type Action struct {
	// Name identifies the action within its stage, in workspace names, logs
	// and failure reports.
	Name string
	// Prompt is a text/template over the issue; see Render.
	Prompt string
	// Check is a shell command run in the action's workspace once its
	// session succeeded; empty when the action has none. It is never a
	// template: it reads the issue from environment variables, so no issue
	// text becomes part of the command. A failing check fails the action.
	Check string
	// Mate is the name of the mate that acts for the action's session and
	// check on the tracker: the action's own or the config's default. Empty
	// means the boss.
	Mate string
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

// Render renders the action's prompt for issue. The template's data is
// .Issue with the fields Ref, Key, Title and URL; a reference to any other
// field, or a template that does not parse, is an error naming the action.
func (a Action) Render(issue Issue) (string, error) {
	tmpl, err := template.New(a.Name).Parse(a.Prompt)
	if err != nil {
		return "", fmt.Errorf("parse prompt of action %q: %w", a.Name, err)
	}
	data := struct{ Issue promptIssue }{promptIssue{Ref: issue.Ref, Key: issue.Key, Title: issue.Title, URL: issue.URL}}
	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render prompt of action %q: %w", a.Name, err)
	}
	return out.String(), nil
}

// Outcome is how an action's session ended, as its harness judged it.
type Outcome struct {
	// Succeeded is true when the session ended cleanly.
	Succeeded bool
	// Reason is one line saying why, such as the session's last message.
	Reason string
}

// FailureReport is what the engine asks a tracker to post on an issue whose
// stage had failed actions. The tracker adapter formats it in its own markup.
type FailureReport struct {
	// IssueKey and IssueRef identify the issue, as in Issue.
	IssueKey string
	IssueRef string
	// Failures lists each failed action, in the stage's action order.
	Failures []ActionFailure
}

// ActionFailure is one failed action in a FailureReport.
type ActionFailure struct {
	// Action is the action's name.
	Action string
	// Reason is the one-line reason from the action's Outcome.
	Reason string
	// Workspace is the name of the workspace the action ran in.
	Workspace string
	// Log is the repository-relative path of the session's log file.
	Log string
}
