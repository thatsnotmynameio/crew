package shell_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/adapter/shell"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// output is a writer safe for the script's copy goroutine and the test.
type output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, _ := o.buf.Write(p) // a bytes.Buffer's Write never returns an error
	return n, nil
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// script returns a script of command in a new directory, for issue #14.
func script(t *testing.T, command string, out *output) port.Script {
	t.Helper()
	return port.Script{
		Dir: t.TempDir(), Command: command,
		IssueRef: "#14", IssueID: crew.IssueID{Key: "14"}, IssueURL: "https://github.com/o/r/issues/14",
		Branch: "crew/issue-14-lfg", Output: out,
	}
}

// withoutCrewEnv clears every CREW_ variable crew's own environment holds,
// such as those of a crew session running these tests, until the test
// ends, so a script sees only the ones the shell sets.
func withoutCrewEnv(t *testing.T) {
	t.Helper()
	for _, e := range os.Environ() {
		if name, _, _ := strings.Cut(e, "="); strings.HasPrefix(name, "CREW_") {
			t.Setenv(name, "") // restores the variable when the test ends
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// exits runs s and fails the test unless it ran and exited status.
func exits(t *testing.T, s port.Script, status int) {
	t.Helper()
	got, err := shell.New(&proc.Group{}).Run(context.Background(), s)
	if err != nil || got.Status != status {
		t.Fatalf("Run = %+v, %v, want status %d and no error", got, err, status)
	}
}

func TestScriptThatExitsZeroReportsStatusZero(t *testing.T) {
	var out output
	exits(t, script(t, "true", &out), 0)
}

func TestScriptThatExitsNonZeroReportsItsStatusWithItsOutputInOrder(t *testing.T) {
	var out output
	exits(t, script(t, "echo one; echo two >&2; exit 3", &out), 3)
	if got := out.String(); got != "one\ntwo\n" {
		t.Errorf("output = %q, want both lines in order", got)
	}
}

func TestScriptReadsTheIssueFromItsEnvironmentInItsDirectory(t *testing.T) {
	var out output
	command := `printf '%s|%s|%s|%s|%s\n' ` +
		`"$CREW_ISSUE_REF" "$CREW_ISSUE_KEY" "$CREW_ISSUE_URL" "$CREW_BRANCH" "$(pwd -P)"`
	c := script(t, command, &out)
	exits(t, c, 0)
	dir, err := filepath.EvalSymlinks(c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "#14|14|https://github.com/o/r/issues/14|crew/issue-14-lfg|" + dir + "\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestAE6ScriptRunsOnlyItsCommandWhateverTheIssueTitle(t *testing.T) {
	// The title is not part of port.Script at all, so it cannot reach the
	// command: the script sees only crew's ten variables, and the command
	// runs as written.
	withoutCrewEnv(t)
	var out output
	c := script(t, `env | grep '^CREW_' | sort`, &out)
	exits(t, c, 0)
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		name, _, _ := strings.Cut(line, "=")
		switch name {
		case "CREW_ACTION", "CREW_BOTS", "CREW_BRANCH", "CREW_CODE_OWNERS", "CREW_COMMENT_MARKER", "CREW_ISSUE_KEY",
			"CREW_ISSUE_REF", "CREW_ISSUE_URL", "CREW_LAST_MESSAGE_FILE", "CREW_PROMPT_FILE":
		default:
			t.Errorf("unexpected variable %q", line)
		}
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "pwned")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pwned exists: %v", err)
	}
}

// R46, KTD-W2: a shell action or a route shell step, which both run through
// the shell, reads crew's marker from CREW_COMMENT_MARKER, to mark the
// comments it posts as crew's.
func TestScriptReadsCrewsCommentMarker(t *testing.T) {
	withoutCrewEnv(t)
	var out output
	exits(t, script(t, `printf '%s' "$CREW_COMMENT_MARKER"`, &out), 0)
	if got := out.String(); got != crew.PostedMarker {
		t.Errorf("CREW_COMMENT_MARKER = %q, want %q", got, crew.PostedMarker)
	}
}

// AE6 of #80: whatever GH_TOKEN your shell exports, the script's gh
// reads its bot's directory, and the script learns the code owners and the bots.
func TestScriptActsAsItsIdentityAndNamesTheCodeOwnersAndTheBots(t *testing.T) {
	withoutCrewEnv(t)
	t.Setenv("GH_TOKEN", "your-token")
	var out output
	c := script(t, `echo "$GH_CONFIG_DIR|$CREW_CODE_OWNERS|$CREW_BOTS|${GH_TOKEN-unset}"`+
		`"|${CREW_BOSS-unset}|${CREW_MATES-unset}"`, &out)
	c.Identity = port.Identity{
		Bot: "developer", Login: "crew-developer[bot]",
		Env:   []string{"GH_CONFIG_DIR=/run/crew/developer"},
		Unset: []string{"GH_TOKEN", "GITHUB_TOKEN"},
	}
	c.CodeOwners = []string{"octocat"}
	c.Bots = []string{"crew-developer[bot]", "crew-ops[bot]"}

	exits(t, c, 0)
	want := "/run/crew/developer|octocat|crew-developer[bot] crew-ops[bot]|unset|unset|unset\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// KTD-S12, KTD-S13: a script reads the latest session's name from
// CREW_ACTION, not its own, and that session's prompt and last message, as
// written, from the files CREW_PROMPT_FILE and CREW_LAST_MESSAGE_FILE name.
func TestScriptReadsTheLatestSessionThePromptAndTheLastMessage(t *testing.T) {
	var out output
	c := script(t, `echo "$CREW_ACTION"; cat "$CREW_PROMPT_FILE"; echo '|'; cat "$CREW_LAST_MESSAGE_FILE"`, &out)
	c.Name = "judge"
	c.Session = "lfg"
	c.Prompt = "/lfg #14\n\nYou are resuming a failed run."
	c.LastMessage = "PR #20 is open.\n\n- CI is green\n- merging is yours"

	exits(t, c, 0)
	want := "lfg\n" + c.Prompt + "|\n" + c.LastMessage
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// R2: an empty last message is an empty file, which a script tells apart
// from a message.
func TestScriptGetsAnEmptyLastMessageAsAnEmptyFile(t *testing.T) {
	var out output
	c := script(t, `test -f "$CREW_LAST_MESSAGE_FILE" && ! test -s "$CREW_LAST_MESSAGE_FILE"`, &out)
	exits(t, c, 0) // the test command exits 0 only for an empty file
}

// A message longer than one environment string may be (128 KiB on Linux)
// still reaches the script whole.
func TestScriptGetsALongLastMessageWhole(t *testing.T) {
	var out output
	c := script(t, `wc -c < "$CREW_LAST_MESSAGE_FILE"`, &out)
	c.LastMessage = strings.Repeat("a", 200*1024)
	exits(t, c, 0)
	if got := strings.TrimSpace(out.String()); got != strconv.Itoa(len(c.LastMessage)) {
		t.Errorf("the script read %s bytes, want %d", got, len(c.LastMessage))
	}
}

// The files hold the session's words, so they go once the script ended,
// however it ended.
func TestScriptRemovesItsFilesWhenItEnds(t *testing.T) {
	for _, tt := range []struct {
		name, command string
		timeout       time.Duration
	}{
		{name: "passed", command: "true"},
		{name: "failed", command: "exit 1"},
		{name: "stopped", command: "sleep 60", timeout: 100 * time.Millisecond},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out output
			c := script(t, `dirname "$CREW_PROMPT_FILE" > files; dirname "$CREW_LAST_MESSAGE_FILE" >> files; `+tt.command, &out)
			ctx := context.Background()
			if tt.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.timeout)
				defer cancel()
			}
			_, _ = shell.New(&proc.Group{}).Run(ctx, c) // each way of ending is tested elsewhere
			dirs, err := os.ReadFile(filepath.Join(c.Dir, "files"))
			if err != nil {
				t.Fatal(err)
			}
			for dir := range strings.FieldsSeq(string(dirs)) {
				if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s is still there after the script: %v", dir, err)
				}
			}
		})
	}
}

func TestScriptEndedByItsContextIsKilledWithWhatItStarted(t *testing.T) {
	var out output
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c := script(t, `sleep 60 & echo $! > child; wait`, &out)

	start := time.Now()
	_, err := shell.New(&proc.Group{}).Run(ctx, c)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want an error wrapping the context's", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("Run returned after %v, want soon after the context ended", took)
	}
	pid, err := os.ReadFile(filepath.Join(c.Dir, "child"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(pid)))
	if err != nil {
		t.Fatal(err)
	}
	// A killed child stays a zombie until init reaps it, so give that a
	// moment.
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(n, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the script's child %d still runs", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestScriptThatCannotStartReportsAnErrorNotAStatus(t *testing.T) {
	var out output
	c := script(t, "true", &out)
	c.Dir = filepath.Join(c.Dir, "missing")
	got, err := shell.New(&proc.Group{}).Run(context.Background(), c)
	if err == nil || got != (port.ShellResult{}) {
		t.Fatalf("Run = %+v, %v, want a start error and no status", got, err)
	}
}

// A script killed by a signal crew did not send has no exit status of its
// own: it reports -1, which is not 0.
func TestScriptKilledByASignalReportsMinusOne(t *testing.T) {
	var out output
	exits(t, script(t, "kill -KILL $$", &out), -1)
}
