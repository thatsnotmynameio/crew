package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// exitError is a fake process's non-zero exit, read as *exec.ExitError is:
// code -1 is a signal.
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }
func (e exitError) ExitCode() int { return e.code }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// record feeds stdout and stderr to a recorder seven bytes at a time, so
// lines arrive split as a pipe splits them, and returns it ended.
func record(stdout, stderr []byte) *recorder {
	r := newRecorder()
	for chunk := range slices.Chunk(stdout, 7) {
		_, _ = r.stdout().Write(chunk)
	}
	for chunk := range slices.Chunk(stderr, 7) {
		_, _ = r.stderr().Write(chunk)
	}
	r.end()
	return r
}

// judgeCase is codex's output and exit, and the verdict on them: stdout
// is file's content when file is set.
type judgeCase struct {
	name    string
	file    string
	stdout  string
	stderr  string
	exit    error
	stopped bool
	want    crew.Outcome
}

// ended is a turn ending in turn.started, then an error, then turn.completed.
const ended = `{"type":"turn.started"}` + "\n" + `{"type":"error","message":"model refused"}` + "\n" +
	`{"type":"turn.completed","usage":{}}` + "\n"

var judgeCases = []judgeCase{
	{
		name: "a completed turn and exit 0 succeed with the last message",
		file: "success.jsonl",
		want: crew.Outcome{Succeeded: true, Reason: "I fixed the parser. The tests pass."},
	},
	{
		name: "a failed turn fails with its error even when codex exits 0",
		file: "failed.jsonl",
		want: crew.Outcome{Reason: "stream disconnected before completion: " +
			"error sending request for url (https://api.openai.com/v1/responses)"},
	},
	{
		name: "a logged-out run fails with the turn's error, not a retry",
		file: "loggedout.jsonl",
		exit: exitError{code: 1, msg: "exit status 1"},
		want: crew.Outcome{Reason: "unexpected status 401 Unauthorized: Missing bearer or basic " +
			"authentication in header, url: https://api.openai.com/v1/responses"},
	},
	{
		name: "retries and error items before a completed turn do not fail it",
		file: "retried.jsonl",
		want: crew.Outcome{Succeeded: true, Reason: "Done: the review is posted."},
	},
	{
		name:   "a completed turn fails when codex exits non-zero, naming the last error",
		stdout: ended,
		exit:   exitError{code: 1, msg: "exit status 1"},
		want:   crew.Outcome{Reason: "exit code 1 after: model refused"},
	},
	{
		name: "a completed turn fails when codex exits non-zero, naming the last message",
		file: "success.jsonl",
		exit: exitError{code: 1, msg: "exit status 1"},
		want: crew.Outcome{Reason: "exit code 1 after: I fixed the parser. The tests pass."},
	},
	{
		name:   "a completed turn fails when codex exits non-zero, with nothing else to say",
		stdout: `{"type":"turn.completed","usage":{}}` + "\n",
		exit:   exitError{code: 1, msg: "exit status 1"},
		want:   crew.Outcome{Reason: "exit code 1"},
	},
	{
		name: "no turn event fails with the last error",
		file: "interrupted.jsonl",
		exit: exitError{code: 1, msg: "exit status 1"},
		want: crew.Outcome{Reason: "Reconnecting... 2/5 (stream disconnected before completion: connection reset)"},
	},
	{
		name:   "no stdout at all fails with the last stderr line",
		stderr: "Reading additional input from stdin...\nerror: unexpected argument '--approve-for-me' found\n\n",
		exit:   exitError{code: 2, msg: "exit status 2"},
		want:   crew.Outcome{Reason: "error: unexpected argument '--approve-for-me' found"},
	},
	{
		name: "no stdout and no stderr fail with the exit code",
		exit: exitError{code: 2, msg: "exit status 2"},
		want: crew.Outcome{Reason: "exit code 2"},
	},
	{
		name:   "stderr is not the reason once stdout held an event",
		stdout: `{"type":"thread.started","thread_id":"t"}` + "\n",
		stderr: "2026-10-05T21:13:33Z ERROR codex_core: something\n",
		exit:   exitError{code: 1, msg: "exit status 1"},
		want:   crew.Outcome{Reason: "exit code 1"},
	},
	{
		name: "no stdout and exit 0 fail without a result",
		want: crew.Outcome{Reason: "the session ended without a result"},
	},
	{
		name:   "a signal names itself",
		stdout: `{"type":"thread.started","thread_id":"t"}` + "\n",
		exit:   exitError{code: -1, msg: "signal: killed"},
		want:   crew.Outcome{Reason: "signal: killed"},
	},
	{
		name:    "a session crew stopped fails as stopped whatever it printed",
		file:    "success.jsonl",
		exit:    exitError{code: -1, msg: "signal: terminated"},
		stopped: true,
		want:    crew.Outcome{Reason: "stopped by crew before the session ended"},
	},
	{
		name: "text quoting an event inside a message never counts as one",
		file: "quoted.jsonl",
		want: crew.Outcome{Succeeded: true,
			Reason: `A failed turn prints {"type":"turn.failed","error":{"message":"boom"}} on a line of its own.`},
	},
	{
		name:   "a last line without a newline still counts",
		stdout: `{"type":"turn.started"}` + "\n" + `{"type":"turn.failed","error":{"message":"quota exceeded"}}`,
		exit:   exitError{code: 1, msg: "exit status 1"},
		want:   crew.Outcome{Reason: "quota exceeded"},
	},
	{
		name:   "lines that are not events are skipped",
		stdout: "not json\n[1,2]\n{broken\n" + `{"type":"turn.completed","usage":{}}` + "\n",
		want:   crew.Outcome{Succeeded: true},
	},
}

func TestJudge(t *testing.T) {
	for _, tt := range judgeCases {
		t.Run(tt.name, func(t *testing.T) {
			stdout := []byte(tt.stdout)
			if tt.file != "" {
				stdout = fixture(t, tt.file)
			}
			got := record(stdout, []byte(tt.stderr)).judge(tt.exit, tt.stopped)
			if got != tt.want {
				t.Errorf("judge = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReasonIsOneCleanLineOfAtMost200Characters(t *testing.T) {
	text := "line one\r\nline\ttwo \x00\x1b[31mred\x1b[0m " + strings.Repeat("x", 300)
	message, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	stdout := []byte(`{"type":"turn.failed","error":{"message":` + string(message) + `}}` + "\n")

	got := record(stdout, nil).judge(nil, false).Reason

	if n := utf8.RuneCountInString(got); n != maxReason {
		t.Errorf("reason has %d characters, want %d: %q", n, maxReason, got)
	}
	if !strings.HasPrefix(got, "line one line two [31mred[0m xxx") || !strings.HasSuffix(got, "…") {
		t.Errorf("reason = %q, want one line without control characters, cut with an ellipsis", got)
	}
	for _, r := range got {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("reason %q holds control character %U", got, r)
		}
	}
}
