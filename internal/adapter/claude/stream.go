package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// maxReason is how many characters of a reason an outcome keeps.
const maxReason = 200

// stream reads claude's stream-json output as it is written: one JSON event
// per line. It keeps the last top-level result event, which is what the
// session is judged by, and the last text the session said. A line that is
// not a JSON object is skipped.
//
// It is written from one goroutine. The result is read with end once the
// writes are over; what the session said is read with said from any
// goroutine, while the writes go on.
type stream struct {
	partial []byte  // the start of a line whose newline has not come yet
	last    *result // the last top-level result event so far

	mu   sync.Mutex
	text string // the last top-level text so far, from lastWords
}

// result is a top-level result event. Decoding a line into it reads only
// top-level keys, so text inside a message that quotes `"is_error": true`,
// or a whole result event, never counts.
type result struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// assistant is an assistant event: a message from the session, or from a
// subagent when ParentToolUseID is set. Decoding it also reads only the
// event's own keys and its message's content blocks.
type assistant struct {
	ParentToolUseID *string `json:"parent_tool_use_id"`
	Message         struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// Write implements io.Writer. It never fails, so the stream never stops the
// copy of claude's output.
func (s *stream) Write(p []byte) (int, error) {
	n := len(p)
	for {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.partial = append(s.partial, p...)
			return n, nil
		}
		if len(s.partial) > 0 {
			s.line(append(s.partial, p[:i]...))
			s.partial = s.partial[:0]
		} else {
			s.line(p[:i])
		}
		p = p[i+1:]
	}
}

// line reads one line of the stream.
func (s *stream) line(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var ev result
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "result":
		s.last = &ev
	case "assistant":
		s.assistant(line)
	}
}

// assistant reads a top-level assistant event and keeps its last text
// block. An event from a subagent, or one holding no text, such as a lone
// tool_use, changes nothing.
func (s *stream) assistant(line []byte) {
	var ev assistant
	if err := json.Unmarshal(line, &ev); err != nil || ev.ParentToolUseID != nil {
		return
	}
	for _, block := range slices.Backward(ev.Message.Content) {
		if block.Type == "text" {
			s.mu.Lock()
			s.text = lastWords(block.Text)
			s.mu.Unlock()
			return
		}
	}
}

// said returns the last text the session said, on one line, or "" when it
// said nothing yet.
func (s *stream) said() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text
}

// end reads the last line, when the stream did not end with a newline, and
// returns the last top-level result event, or nil when there was none.
func (s *stream) end() *result {
	if len(s.partial) > 0 {
		s.line(s.partial)
		s.partial = nil
	}
	return s.last
}

// judge is the verdict on a session whose last top-level result event is
// last, or nil, and whose process exited with exit (nil for status 0). It
// succeeded only when last exists, is not an error, and the process exited
// 0. The reason is the result's text on one line, cut to maxReason
// characters, or how the process exited when there is no result.
func judge(last *result, exit error) crew.Outcome {
	switch {
	case last == nil && exit == nil:
		return crew.Outcome{Reason: "the session ended without a result"}
	case last == nil:
		return crew.Outcome{Reason: exited(exit)}
	case last.IsError:
		text := last.Result
		if strings.TrimSpace(text) == "" {
			text = last.Subtype // such as error_max_turns, which has no text
		}
		return crew.Outcome{Reason: oneLine(text)}
	case exit != nil:
		return crew.Outcome{Reason: oneLine(fmt.Sprintf("%s after: %s", exited(exit), last.Result))}
	}
	return crew.Outcome{Succeeded: true, Reason: oneLine(last.Result)}
}

// exited says how a process that did not exit 0 ended: "exit code N", or the
// error itself, such as "signal: terminated", when no code applies.
func exited(err error) string {
	var coded interface{ ExitCode() int } // *exec.ExitError is one
	if errors.As(err, &coded) && coded.ExitCode() > 0 {
		return fmt.Sprintf("exit code %d", coded.ExitCode())
	}
	return err.Error()
}

// oneLine joins s's words with single spaces and cuts it to maxReason
// characters, ending a cut text with an ellipsis.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxReason {
		return s
	}
	return string(runes[:maxReason-1]) + "…"
}

// lastWords joins s's words with single spaces and keeps its last maxReason
// characters, starting a cut text with an ellipsis: the end of what a session
// says is the newest.
func lastWords(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxReason {
		return s
	}
	return "…" + string(runes[len(runes)-maxReason+1:])
}
