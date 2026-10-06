package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/captain"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// sessionID is a Claude Code session id, as crew runs them.
const sessionID = "0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10"

// decodeTask decodes stdout as exactly one line holding one task, with no
// key beyond the three crew sessions prints.
func decodeTask(t *testing.T, stdout string) printedTask {
	t.Helper()
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout = %q, want one line", stdout)
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.DisallowUnknownFields()
	var task printedTask
	if err := dec.Decode(&task); err != nil {
		t.Fatalf("stdout = %q: %v", stdout, err)
	}
	return task
}

// stubCaptain answers with task and err, and counts the questions.
type stubCaptain struct {
	task  crew.Task
	err   error
	asked int
}

func (s *stubCaptain) Task(_ context.Context, session uuid.UUID) (crew.Task, error) {
	s.asked++
	task := s.task
	task.Session = session
	return task, s.err
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout is closed") }

func TestSessionsTasksNextPrintsTheTaskAsJSON(t *testing.T) {
	// Covers AE1.
	got := runCaptured(t, "sessions", sessionID, "tasks", "next")
	if got.code != app.ExitClean {
		t.Fatalf("code = %d, want %d (stderr %q)", got.code, app.ExitClean, got.stderr)
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want nothing", got.stderr)
	}
	task := decodeTask(t, got.stdout)
	if task.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", task.SessionID, sessionID)
	}
	if _, err := uuid.Parse(task.ID); err != nil {
		t.Errorf("id = %q, want a UUID: %v", task.ID, err)
	}
	if task.Prompt != captain.Placeholder {
		t.Errorf("prompt = %q, want %q", task.Prompt, captain.Placeholder)
	}
}

func TestSessionsTasksCurrentAndNextAnswerTheSame(t *testing.T) {
	// Covers AE2.
	current := runCaptured(t, "sessions", sessionID, "tasks", "current")
	next := runCaptured(t, "sessions", sessionID, "tasks", "next")
	if current.code != app.ExitClean || next.code != app.ExitClean {
		t.Fatalf("codes = %d, %d, want %d", current.code, next.code, app.ExitClean)
	}
	a, b := decodeTask(t, current.stdout), decodeTask(t, next.stdout)
	if a.SessionID != sessionID || b.SessionID != sessionID {
		t.Errorf("session_ids = %q, %q, want %q", a.SessionID, b.SessionID, sessionID)
	}
	if a.Prompt != b.Prompt {
		t.Errorf("prompts = %q, %q, want the same", a.Prompt, b.Prompt)
	}
	if a.ID == b.ID {
		t.Errorf("both tasks have the id %q, want one each", a.ID)
	}
}

func TestSessionsRefusesAnIDThatIsNotAUUID(t *testing.T) {
	// Covers AE3.
	got := runCaptured(t, "sessions", "not-a-uuid", "tasks", "next")
	if got.code != app.ExitConfig {
		t.Errorf("code = %d, want %d", got.code, app.ExitConfig)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want nothing", got.stdout)
	}
	if !strings.Contains(got.stderr, `crew: session id "not-a-uuid" is not a UUID`) {
		t.Errorf("stderr = %q, want the id named", got.stderr)
	}
	if !strings.Contains(got.stderr, sessionsUsage) {
		t.Errorf("stderr = %q, want the usage", got.stderr)
	}
}

func TestSessionsRefusesAMalformedCommandBeforeAskingTheCaptain(t *testing.T) {
	// Covers AE4.
	tests := []struct {
		name string
		args []string
	}{
		{name: "nothing", args: nil},
		{name: "the id alone", args: []string{sessionID}},
		{name: "no task word", args: []string{sessionID, "tasks"}},
		{name: "unknown task word", args: []string{sessionID, "tasks", "later"}},
		{name: "task instead of tasks", args: []string{sessionID, "task", "next"}},
		{name: "upper-case word", args: []string{sessionID, "tasks", "NEXT"}},
		{name: "extra argument", args: []string{sessionID, "tasks", "next", "extra"}},
		{name: "words out of order", args: []string{"tasks", sessionID, "next"}},
		{name: "empty id", args: []string{"", "tasks", "next"}},
		{name: "not a UUID", args: []string{"not-a-uuid", "tasks", "next"}},
		{name: "help flag", args: []string{"-h"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			stub := &stubCaptain{}
			if code := runSessions(tt.args, &stdout, &stderr, stub); code != app.ExitConfig {
				t.Errorf("runSessions(%q) = %d, want %d", tt.args, code, app.ExitConfig)
			}
			if !strings.Contains(stderr.String(), "crew: "+sessionsUsage) {
				t.Errorf("stderr = %q, want the usage", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
			if stub.asked != 0 {
				t.Errorf("the captain was asked %d times, want none", stub.asked)
			}
		})
	}
}

func TestSessionsRunsOutsideAnyRepository(t *testing.T) {
	// Covers AE5.
	outsideGit(t)
	got := runCaptured(t, "sessions", sessionID, "tasks", "next")
	if got.code != app.ExitClean {
		t.Fatalf("code = %d, want %d (stderr %q)", got.code, app.ExitClean, got.stderr)
	}
	if task := decodeTask(t, got.stdout); task.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", task.SessionID, sessionID)
	}
}

func TestSessionsPrintsTheCanonicalSessionID(t *testing.T) {
	for _, id := range []string{
		strings.ToUpper(sessionID),
		"{" + sessionID + "}",
		"urn:uuid:" + sessionID,
		strings.ReplaceAll(sessionID, "-", ""),
	} {
		t.Run(id, func(t *testing.T) {
			got := runCaptured(t, "sessions", id, "tasks", "next")
			if got.code != app.ExitClean {
				t.Fatalf("code = %d, want %d (stderr %q)", got.code, app.ExitClean, got.stderr)
			}
			if task := decodeTask(t, got.stdout); task.SessionID != sessionID {
				t.Errorf("session_id = %q, want %q", task.SessionID, sessionID)
			}
		})
	}
}

func TestSessionsLeavesHTMLCharactersInThePrompt(t *testing.T) {
	var stdout, stderr bytes.Buffer
	stub := &stubCaptain{task: crew.Task{ID: uuid.NewV7(), Prompt: "fix <b> & </b>"}}
	if code := runSessions([]string{sessionID, "tasks", "next"}, &stdout, &stderr, stub); code != app.ExitClean {
		t.Fatalf("code = %d, want %d (stderr %q)", code, app.ExitClean, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"prompt":"fix <b> & </b>"`) {
		t.Errorf("stdout = %q, want the prompt unescaped", stdout.String())
	}
}

func TestSessionsFailsWhenTheCaptainFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	stub := &stubCaptain{err: errors.New("the captain is away")}
	if code := runSessions([]string{sessionID, "tasks", "next"}, &stdout, &stderr, stub); code != app.ExitFailure {
		t.Errorf("code = %d, want %d", code, app.ExitFailure)
	}
	if stderr.String() != "crew: the captain is away\n" {
		t.Errorf("stderr = %q, want the captain's error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", stdout.String())
	}
}

func TestSessionsFailsWhenStdoutFails(t *testing.T) {
	var stderr bytes.Buffer
	code := runSessions([]string{sessionID, "tasks", "next"}, failingWriter{}, &stderr, captain.Dumb{})
	if code != app.ExitFailure {
		t.Errorf("code = %d, want %d", code, app.ExitFailure)
	}
	if !strings.Contains(stderr.String(), "crew: stdout is closed") {
		t.Errorf("stderr = %q, want the write error", stderr.String())
	}
}

func TestHelpListsSessions(t *testing.T) {
	got := runCaptured(t, "-h")
	if got.code != app.ExitClean {
		t.Errorf("code = %d, want %d", got.code, app.ExitClean)
	}
	if !strings.Contains(got.stderr, "crew sessions <session-id> tasks next|current") {
		t.Errorf("help = %q, want crew sessions listed", got.stderr)
	}
}
