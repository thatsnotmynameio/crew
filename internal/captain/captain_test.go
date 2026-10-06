package captain_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/captain"
	"github.com/thatsnotmynameio/crew/internal/port"
)

var _ port.Captain = captain.Dumb{}

// session is a Claude Code session id, as crew runs them.
var session = uuid.MustParse("0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10")

func TestDumbAnswersTheSessionWithThePlaceholder(t *testing.T) {
	task, err := captain.Dumb{}.Task(context.Background(), session)
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if task.Session != session {
		t.Errorf("task.Session = %v, want %v", task.Session, session)
	}
	if task.Prompt != captain.Placeholder {
		t.Errorf("task.Prompt = %q, want the placeholder %q", task.Prompt, captain.Placeholder)
	}
	if task.ID == uuid.Nil() || task.ID == session {
		t.Errorf("task.ID = %v, want a new id of its own", task.ID)
	}
}

func TestDumbGivesEachTaskItsOwnTimeOrderedID(t *testing.T) {
	first, err := captain.Dumb{}.Task(context.Background(), session)
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	second, err := captain.Dumb{}.Task(context.Background(), session)
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if first.ID == second.ID {
		t.Errorf("two tasks share the id %v", first.ID)
	}
	for _, id := range []uuid.UUID{first.ID, second.ID} {
		if version := id[6] >> 4; version != 7 {
			t.Errorf("task id %v is version %d, want 7", id, version)
		}
	}
}

func TestDumbNeverFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A session crew never ran, the nil id, is still answered: nothing
	// checks that a session exists.
	task, err := captain.Dumb{}.Task(ctx, uuid.Nil())
	if err != nil {
		t.Fatalf("Task with a cancelled context: %v", err)
	}
	if task.Session != uuid.Nil() || task.Prompt != captain.Placeholder {
		t.Errorf("Task = %+v, want the placeholder for the nil session", task)
	}
}
