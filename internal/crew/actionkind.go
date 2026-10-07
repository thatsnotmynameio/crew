package crew

// ActionKind is what an action runs: SessionSpec or ShellSpec.
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

func (SessionSpec) actionKind() {}
func (ShellSpec) actionKind()   {}
