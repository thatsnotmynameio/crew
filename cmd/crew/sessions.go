package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// sessionsUsage is the one form crew sessions takes.
const sessionsUsage = "usage: crew sessions <session-id> tasks next|current"

// printedTask is a task as crew sessions prints it.
type printedTask struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
}

// runSessions runs crew sessions with args, the arguments after
// "sessions", and returns crew's exit code. It takes exactly
// <session-id> tasks next or <session-id> tasks current, asks captain for
// the session's task, and prints it on stdout as one JSON line. It needs no
// repository or config.
func runSessions(args []string, stdout, stderr io.Writer, captain port.Captain) int {
	if len(args) != 3 || args[1] != "tasks" || (args[2] != "next" && args[2] != "current") {
		_, _ = fmt.Fprintln(stderr, "crew: "+sessionsUsage)
		return app.ExitConfig
	}
	session, err := uuid.Parse(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "crew: session id %q is not a UUID\ncrew: %s\n", args[0], sessionsUsage)
		return app.ExitConfig
	}
	task, err := captain.Task(context.Background(), session)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "crew: %v\n", err)
		return app.ExitFailure
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	printed := printedTask{ID: task.ID.String(), SessionID: task.Session.String(), Prompt: task.Prompt}
	if err := enc.Encode(printed); err != nil {
		_, _ = fmt.Fprintf(stderr, "crew: %v\n", err)
		return app.ExitFailure
	}
	return app.ExitClean
}
