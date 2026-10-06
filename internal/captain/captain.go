// Package captain answers what a coding-agent session crew runs should do
// next. Its only captain today, Dumb, decides nothing: it hands every session
// the same placeholder task.
package captain

import (
	"context"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Placeholder is the prompt of every task Dumb hands out. A captain that
// decides nothing can only tell a session to go on with what it was given.
const Placeholder = "Carry on with the work your session was started with."

// Dumb is a port.Captain that decides nothing. For any session it returns a
// new task, with a time-ordered id of its own, for that session and with
// the Placeholder prompt. It never fails.
type Dumb struct{}

// Task returns a new placeholder task for session.
func (Dumb) Task(_ context.Context, session uuid.UUID) (crew.Task, error) {
	return crew.Task{ID: uuid.NewV7(), Session: session, Prompt: Placeholder}, nil
}
