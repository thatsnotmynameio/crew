package crew

import "uuid"

// Task is what a coding-agent session is asked to do next, as its captain
// answers it.
type Task struct {
	// ID identifies the task itself; each task the captain hands out has its
	// own.
	ID uuid.UUID
	// Session is the id of the coding-agent session the task belongs to: a
	// Claude Code or Codex session crew runs. Nothing checks that the
	// session exists.
	Session uuid.UUID
	// Prompt is what the session is asked to do, in free text.
	Prompt string
}
