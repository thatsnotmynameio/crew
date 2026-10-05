package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// maxReason is how many characters of a reason an outcome keeps.
const maxReason = 200

// stoppedReason is the Outcome.Reason of a session ended by Stop.
const stoppedReason = "stopped by crew before the session ended"

// recorder keeps what judging a codex session needs from what it prints:
// the JSONL events of `codex exec --json` on stdout, and the last line of
// stderr. Its stdout and stderr writers are each written from one goroutine
// and touch separate fields; the recorder is read once both are done, but
// for what the session said, which may be read at any time.
type recorder struct {
	out, errs lines

	mu sync.Mutex // guards said, which lastSaid reads while stdout is written

	evented   bool        // stdout held at least one event
	turn      *ending     // the turn's terminal event, or nil
	turns     int         // how many turns completed
	used      *tokenUsage // the usage of the last completed turn, or nil
	lastError string      // the message of the last top-level error event
	said      string      // the text of the last agent message
	errLine   string      // the last non-empty stderr line
}

// ending is a turn's terminal event: turn.completed, or turn.failed with
// its error's message.
type ending struct {
	failed  bool
	message string
}

// event is one line of codex's JSONL. Decoding reads top-level keys only,
// so an event quoted inside an agent's message never counts.
type event struct {
	Type    string `json:"type"`
	Message string `json:"message"` // an error event's
	Error   struct {
		Message string `json:"message"`
	} `json:"error"` // a turn.failed event's
	Item struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage *tokenUsage `json:"usage"` // a turn.completed event's
}

// tokenUsage is the usage a turn.completed event carries: the thread's
// tokens so far. Cache reads and writes are parts of the input tokens, and
// reasoning tokens a part of the output tokens.
type tokenUsage struct {
	Input      int64 `json:"input_tokens"`
	CacheRead  int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
}

func newRecorder() *recorder {
	r := &recorder{}
	r.out.line = r.event
	r.errs.line = r.stderrLine
	return r
}

// stdout returns the writer codex's stdout goes to.
func (r *recorder) stdout() *lines { return &r.out }

// stderr returns the writer codex's stderr goes to.
func (r *recorder) stderr() *lines { return &r.errs }

// end reads the last lines when the output did not end with a newline. Call
// it once the writes are over.
func (r *recorder) end() {
	r.out.flush()
	r.errs.flush()
}

// event reads one stdout line. A line that is not a JSON object is skipped,
// and so are the event and item types judging does not need.
func (r *recorder) event(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var ev event
	if json.Unmarshal(line, &ev) != nil || ev.Type == "" {
		return
	}
	r.evented = true
	switch ev.Type {
	case "turn.completed":
		r.turn = &ending{}
		r.turns++
		r.used = ev.Usage
	case "turn.failed":
		r.turn = &ending{failed: true, message: ev.Error.Message}
	case "error":
		r.lastError = ev.Message
	case "item.completed":
		if ev.Item.Type == "agent_message" {
			r.mu.Lock()
			r.said = ev.Item.Text
			r.mu.Unlock()
		}
	}
}

// lastSaid returns the text of the last agent message so far, on one line,
// or "" before the first. It may be called from any goroutine.
func (r *recorder) lastSaid() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(strings.Fields(r.said), " ")
}

// stderrLine keeps line when it is not blank.
func (r *recorder) stderrLine(line []byte) {
	if text := strings.TrimSpace(string(line)); text != "" {
		r.errLine = text
	}
}

// judge is the verdict on a session that printed what r recorded and whose
// process exited with exit (nil for status 0), stopped telling whether crew
// stopped it. codex runs one turn and prints its end: the session succeeded
// only when that turn completed and codex exited 0. An error event alone
// never fails it, since codex prints one for each retry too.
func (r *recorder) judge(exit error, stopped bool) crew.Outcome {
	switch {
	case stopped:
		return crew.Outcome{Reason: stoppedReason}
	case r.turn == nil:
		return crew.Outcome{Reason: oneLine(r.unended(exit))}
	case r.turn.failed:
		return crew.Outcome{Reason: oneLine(r.turn.message)}
	case exit != nil:
		return crew.Outcome{Reason: oneLine(r.failedAfterTurn(exit))}
	}
	return crew.Outcome{Succeeded: true, Reason: oneLine(r.said)}
}

// usage is what a session that printed what r recorded used, stopped
// telling whether crew stopped it. Its last completed turn's usage covers
// the whole thread, and its turns are the turns that completed. Codex
// reports no cost. A session crew stopped, or one whose turn did not
// complete, reports nothing.
func (r *recorder) usage(stopped bool) crew.Usage {
	if stopped || r.turns == 0 {
		return crew.Usage{}
	}
	u := crew.Usage{Turns: r.turns, HasTurns: true}
	if r.used != nil {
		read, write := max(r.used.CacheRead, 0), max(r.used.CacheWrite, 0)
		u.Tokens = crew.Tokens{
			Input:      max(r.used.Input-read-write, 0),
			Output:     max(r.used.Output, 0),
			CacheRead:  read,
			CacheWrite: write,
		}
		u.HasTokens = true
	}
	return u
}

// failedAfterTurn is the reason of a session whose turn completed but whose
// codex exited non-zero: how it exited, then the last error or, without one,
// the last message.
func (r *recorder) failedAfterTurn(exit error) string {
	detail := r.lastError
	if detail == "" {
		detail = r.said
	}
	if detail == "" {
		return exitText(exit)
	}
	return exitText(exit) + " after: " + detail
}

// unended is the reason of a session whose turn printed no end: the last
// error, or, when codex printed no event at all and failed, as when it
// refused its arguments, its last stderr line, or how it exited.
func (r *recorder) unended(exit error) string {
	switch {
	case r.lastError != "":
		return r.lastError
	case exit == nil:
		return "the session ended without a result"
	case !r.evented && r.errLine != "":
		return r.errLine
	}
	return exitText(exit)
}

// exitText says how a process that did not exit 0 ended: "exit code N", or
// the error itself, such as "signal: killed", when no code applies.
func exitText(err error) string {
	coded, ok := errors.AsType[interface {
		error
		ExitCode() int
	}](err)
	if ok && coded.ExitCode() > 0 {
		return fmt.Sprintf("exit code %d", coded.ExitCode())
	}
	return err.Error()
}

// oneLine joins s's words with single spaces, drops the control characters
// left, such as NUL and ESC, and cuts it to maxReason characters, ending a
// cut text with an ellipsis.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(strings.Join(strings.Fields(s), " "), ""))
	if runes := []rune(s); len(runes) > maxReason {
		return string(runes[:maxReason-1]) + "…"
	}
	return s
}

// lines splits what is written to it into lines and hands each, without its
// newline, to line, which must not keep the slice. It never fails, so it
// never stops proc's copy of a pipe.
type lines struct {
	line    func([]byte)
	partial []byte // the start of a line whose newline has not come yet
}

// Write implements io.Writer.
func (l *lines) Write(p []byte) (int, error) {
	n := len(p)
	for {
		before, after, found := bytes.Cut(p, []byte{'\n'})
		l.partial = append(l.partial, before...)
		if !found {
			return n, nil
		}
		l.line(l.partial)
		l.partial = l.partial[:0]
		p = after
	}
}

// flush hands on a last line that has no newline.
func (l *lines) flush() {
	if len(l.partial) > 0 {
		l.line(l.partial)
		l.partial = nil
	}
}
