package crew

// Rule is one of the config's rules. It takes an item of its Takes kind in
// its Labels.Ready state, moves it to Labels.Running while its actions run,
// and moves it to Labels.Success once every action has succeeded, or to
// Labels.Failure when any failed.
type Rule struct {
	// Name identifies the rule in events and the TUI.
	Name RuleName
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
const DefaultQueue QueueName = "default"

// Queue is a fixed share of the global limit on the issues crew holds at
// once. Only the rules in a queue use its slots, and it never lends an idle
// one to another queue.
type Queue struct {
	// Name identifies the queue: DefaultQueue or a queue the config
	// declares.
	Name QueueName
	// Slots is how many issues the queue's rules may hold at once, possibly
	// zero.
	Slots int
}

// Action is one session a rule runs for an issue.
type Action struct {
	// Name identifies the action within its rule, in workspace names, logs
	// and failure reports.
	Name ActionName
	// Prompt is the action's prompt, parsed when the config loaded.
	Prompt Prompt
	// Agent is the agent whose harness runs the action's session.
	Agent Agent
	// Checks run in the action's workspace once its session succeeded, one
	// after another in this order, until one does not pass; empty when the
	// action has none. A check that does not pass fails the action.
	Checks []Check
	// Bot is the bot that acts for the action's session and check on the
	// tracker: its agent's, or the tracker's when the agent names none. The
	// zero Bot is you.
	Bot Bot
}

// Check is one of an action's checks.
type Check struct {
	// Name is the check's name in the config's checks.
	Name CheckName
	// Script is a shell command. It is never a template: it reads the
	// issue, the session's prompt and its last message from environment
	// variables and the files they name, so no issue or session text
	// becomes part of the command.
	Script string
}

// CheckResult is how one check of an action ended.
type CheckResult struct {
	// Name is the check's name.
	Name CheckName
	// Passed is true when the check exited 0.
	Passed bool
	// Reason is crew's one line on how it ended, naming the check, followed
	// by the last line the check printed when it printed one.
	Reason CheckReason
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
// It carries no reason: an outcome's reason is a session's or a tool's last
// words, which a tracker comment must not show.
type FailureReport struct {
	// IssueID and IssueRef identify the issue, as ID and Ref in Issue.
	IssueID  IssueID
	IssueRef string
	// Failures lists each failed action, in the rule's action order.
	Failures []ActionFailure
}

// ActionFailure is one failed action in a FailureReport: where to read why
// it failed, never the reason itself.
type ActionFailure struct {
	// Action is the action's name.
	Action ActionName
	// Workspace is the workspace the action ran in.
	Workspace WorkspaceName
	// Log is the repository-relative path of the session's log file.
	Log string
}
