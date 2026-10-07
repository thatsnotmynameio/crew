package claude

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/port"
)

// lastMessageOf runs one session of p to its end and returns the last
// message it reports.
func lastMessageOf(t *testing.T, p *fakeProcess) string {
	t.Helper()
	h := build(t, noSection, &fakeSpawn{process: p})
	s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Wait()
	r, ok := s.(port.LastMessageReporter)
	if !ok {
		t.Fatalf("session %T is not a port.LastMessageReporter", s)
	}
	return r.LastMessage()
}

// resultLine is a top-level result event whose result is text.
func resultLine(t *testing.T, text string) []byte {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false, "num_turns": 1, "result": text,
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

// R1: a shell action reads the session's last message as the session
// wrote it, not the one-line reason cut to maxReason characters.
func TestLastMessageIsTheLastResultsTextAsWritten(t *testing.T) {
	text := "PR #128 is open.\n\n- CI is green\n- merging is yours\n" + strings.Repeat("Detail. ", 40)
	stream := append(resultLine(t, "An earlier query's answer."), resultLine(t, text)...)

	if got := lastMessageOf(t, newProcess(stream, nil)); got != text {
		t.Errorf("LastMessage = %q, want %q", got, text)
	}
}

// R2: a session without a result, or whose result is empty, has an empty
// last message.
func TestLastMessageIsEmptyWithoutAResultOrItsText(t *testing.T) {
	if got := lastMessageOf(t, newProcess(fixture(t, "noresult.jsonl"), nil)); got != "" {
		t.Errorf("LastMessage without a result = %q, want empty", got)
	}
	if got := lastMessageOf(t, newProcess(resultLine(t, ""), nil)); got != "" {
		t.Errorf("LastMessage of an empty result = %q, want empty", got)
	}
}
