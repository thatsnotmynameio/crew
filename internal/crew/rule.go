package crew

import (
	"fmt"
	"strings"
	"text/template"
)

// Rule is one of the config's rules. It takes an item of its Takes kind in
// its Labels.Ready state, moves it to Labels.Running while its actions run,
// and moves it to Labels.Success once every action has succeeded, or to
// Labels.Failure when any failed.
type Rule struct {
	// Name identifies the rule in events and the TUI.
	Name string
	// Labels are the states the rule takes an item from and moves it to.
	Labels Labels
	// Actions run in parallel, each in its own workspace and session. A
	// rule may have none.
	Actions []Action
	// Queue is the queue the rule runs in: the share of the global limit
	// its issues may hold. The zero Queue is no queue: the rule is limited
	// only by the global limit, which it shares with every other such rule.
	Queue Queue
	// Takes is the kind of item the rule takes: it takes only the items of
	// that kind in its Labels.Ready state. The zero Kind takes issues.
	Takes Kind
	// Notify tells whether the live view sends a desktop notification when
	// the rule ends for an item.
	Notify bool
}

// Labels are a rule's states, one for each point of its run.
type Labels struct {
	// Ready is the state an item must be in for the rule to take it.
	Ready State
	// Running is the state the item is in while the rule's actions run.
	Running State
	// Success is the state the item moves to after every action succeeded.
	Success State
	// Failure is the state the item moves to when any action failed, with
	// a failure report. It is empty only on a rule without actions, which
	// never fails.
	Failure State
}

// DefaultQueue gets the slots the other queues leave, and runs every rule
// that names no queue.
const DefaultQueue = "default"

// Queue is a fixed share of the global limit on the issues crew holds at
// once. Only the rules in a queue use its slots, and it never lends an idle
// one to another queue.
type Queue struct {
	// Name identifies the queue: DefaultQueue or a queue the config
	// declares.
	Name string
	// Slots is how many issues the queue's rules may hold at once, possibly
	// zero.
	Slots int
}

// Action is one session a rule runs for an issue.
type Action struct {
	// Name identifies the action within its rule, in workspace names, logs
	// and failure reports.
	Name string
	// Prompt is a text/template over the issue; see Render.
	Prompt string
	// Agent is the name of the agent whose harness runs the action's
	// session.
	Agent string
	// Checks run in the action's workspace once its session succeeded, one
	// after another in this order, until one does not pass; empty when the
	// action has none. A check that does not pass fails the action.
	Checks []Check
	// Bot is the name of the bot that acts for the action's session and
	// check on the tracker: its agent's, or the tracker's when the agent
	// names none. Empty means you.
	Bot string
}

// Check is one of an action's checks.
type Check struct {
	// Name is the check's name in the config's checks.
	Name string
	// Script is a shell command. It is never a template: it reads the
	// issue, the session's prompt and its last message from environment
	// variables and the files they name, so no issue or session text
	// becomes part of the command.
	Script string
}

// CheckResult is how one check of an action ended.
type CheckResult struct {
	// Name is the check's name.
	Name string
	// Passed is true when the check exited 0.
	Passed bool
	// Reason is crew's one line on how it ended, naming the check, followed
	// by the last line the check printed when it printed one.
	Reason string
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
	Reason SessionText
}

// FailureReport is what the engine asks a tracker to post on an issue whose
// rule had failed actions. The tracker adapter formats it in its own markup.
type FailureReport struct {
	// IssueKey and IssueRef identify the issue, as in Issue.
	IssueKey string
	IssueRef string
	// Failures lists each failed action, in the rule's action order.
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
