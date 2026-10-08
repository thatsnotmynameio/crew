package crew

import "time"

// ActionKind is what an action runs: SessionSpec, ShellSpec, FunctionSpec
// or QuestionSpec.
//
//sumtype:decl
type ActionKind interface {
	actionKind()
}

// SessionSpec is an action that runs one coding-agent session.
type SessionSpec struct {
	// Agent is the agent whose harness runs the session.
	Agent Agent
	// Prompt is the session's prompt, parsed when the config loaded.
	Prompt Prompt
	// Bot is the bot the session acts as on the tracker: its agent's, or
	// the tracker's when the agent names none. The zero Bot is you.
	Bot Bot
	// Wait is how long the session waits for an answer to a question it
	// asks on the issue before it ends as Waiting: its wait, 10 minutes by
	// default (R20).
	Wait time.Duration
}

// ShellSpec is an action that runs one shell script.
type ShellSpec struct {
	// Script is a shell command. It is never a template: it reads the
	// issue, the latest session's prompt and its last message from
	// environment variables and the files they name, so no issue or
	// session text becomes part of the command.
	Script string
	// Verdicts maps an exit status to the verdict it gives; empty when
	// the definition names none. A status it does not name gives Passed
	// for 0 and Failed for any other.
	Verdicts map[int]Verdict
	// ResumeSelf is true when a resume that restarts at the action starts
	// at the action itself rather than at the latest session before it,
	// for a script that checks something outside the worktree, such as CI.
	ResumeSelf bool
}

// FunctionName is the name a crew function is registered under, which a
// rule's action or a route's step calls it by.
type FunctionName string

// FunctionUse identifies one place of the config that calls a function: its
// key path, such as rules.development.actions[2], unique across the config.
type FunctionUse string

// FunctionSpec is an action that calls one of crew's functions.
type FunctionSpec struct {
	// Function is the function's registered name.
	Function FunctionName
	// Use is the place of the config this call was built for.
	Use FunctionUse
	// Texts are the use's text parameters, as templates over the issue, in
	// the order the config writes them.
	Texts []TextParameter
	// Verdicts are the verdicts the function declares it can return, beside
	// Passed and Failed, which any function can.
	Verdicts []Verdict
	// ResumeSelf is true when a resume that restarts at the action starts
	// at the action itself rather than at the latest session before it.
	ResumeSelf bool
}

func (SessionSpec) actionKind()  {}
func (ShellSpec) actionKind()    {}
func (FunctionSpec) actionKind() {}
func (QuestionSpec) actionKind() {}
