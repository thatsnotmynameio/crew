package fakeclaude

import (
	"context"
	"encoding/json"
	"fmt"
)

// The fixed usage of every result event Success and Failure build: what the
// session cost in US dollars, its turns, and its tokens, all for the model
// the invocation named.
const (
	Cost                = 0.25
	Turns               = 3
	InputTokens         = 1200
	OutputTokens        = 340
	CacheReadTokens     = 5000
	CacheCreationTokens = 800
)

// sessionID is the session id every event carries, under keySessionID.
const (
	sessionID    = "00000000-0000-4000-8000-000000000001"
	keySessionID = "session_id"
	keyType      = "type"
)

// Event is one stream-json event: a JSON object that claude prints on one
// line, such as {"type":"result",...}.
type Event map[string]any

// Emit prints events, one line each, in order, as the session goes. It
// returns an error when the program that ran claude is no longer reading,
// such as after it stopped claude.
func (s *Session) Emit(events ...Event) error {
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("encode a stream-json event: %w", err)
		}
		if _, err := s.out.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write a stream-json event: %w", err)
		}
	}
	return nil
}

// Init returns the system init event that opens every session: its working
// directory, model and permission mode.
func (s *Session) Init() Event {
	return Event{
		keyType: "system", "subtype": "init", "cwd": s.Dir, keySessionID: sessionID,
		"model": s.Model, "permissionMode": s.PermissionMode, "tools": []string{"Bash", "Read", "Edit", "Write"},
	}
}

// Said returns an assistant event whose message is text: what the session
// says as it works. The last text a session said is the one its reader
// shows.
func (s *Session) Said(text string) Event {
	return Event{
		keyType: "assistant", "parent_tool_use_id": nil, keySessionID: sessionID,
		"message": map[string]any{
			keyType: "message", "role": "assistant", "model": s.Model,
			"content": []any{map[string]any{keyType: "text", "text": text}},
		},
	}
}

// Success returns the result event of a session that succeeded with result
// as its final text, carrying the fixed usage.
func (s *Session) Success(result string) Event {
	return s.result("success", false, result)
}

// Failure returns the result event of a session that failed with message,
// carrying the fixed usage.
func (s *Session) Failure(message string) Event {
	return s.result("error_during_execution", true, message)
}

// result returns a result event with the fixed usage.
func (s *Session) result(subtype string, isError bool, text string) Event {
	return Event{
		keyType: "result", "subtype": subtype, "is_error": isError, "result": text,
		keySessionID: sessionID, "num_turns": Turns, "total_cost_usd": Cost,
		"modelUsage": map[string]any{s.Model: map[string]any{
			"inputTokens": InputTokens, "outputTokens": OutputTokens,
			"cacheReadInputTokens": CacheReadTokens, "cacheCreationInputTokens": CacheCreationTokens,
			"costUSD": Cost,
		}},
	}
}

// Succeed returns a script for a session that says text, succeeds with
// text as its result and exits 0.
func Succeed(text string) ScriptFunc {
	return func(_ context.Context, s *Session) int {
		_ = s.Emit(s.Init(), s.Said(text), s.Success(text))
		return 0
	}
}

// Fail returns a script for a session that fails with message as its
// result and exits 1, as claude does after an error.
func Fail(message string) ScriptFunc {
	return func(_ context.Context, s *Session) int {
		_ = s.Emit(s.Init(), s.Failure(message))
		return 1
	}
}

// NoResult returns a script for a session that says text and exits with
// code before printing any result event, as one cut short does.
func NoResult(text string, code int) ScriptFunc {
	return func(_ context.Context, s *Session) int {
		_ = s.Emit(s.Init(), s.Said(text))
		return code
	}
}
