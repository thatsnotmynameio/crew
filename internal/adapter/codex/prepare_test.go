package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// fakeLogin is a scripted `codex login status`: it prints stderr and fails
// with err, and records the commands it got and their deadlines.
type fakeLogin struct {
	stderr    string
	err       error
	commands  []proc.Command
	deadlines []time.Time
}

func (l *fakeLogin) run(ctx context.Context, c proc.Command) (proc.Output, error) {
	l.commands = append(l.commands, c)
	deadline, _ := ctx.Deadline()
	l.deadlines = append(l.deadlines, deadline)
	return proc.Output{Stderr: []byte(l.stderr)}, l.err
}

// onPath puts an executable codex on PATH, alone.
func onPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, binary), []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // a test's stand-in codex must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// prepare runs Prepare on a codex harness whose commands go to login, and
// returns the steps it reported and its error.
func prepare(t *testing.T, login *fakeLogin) ([]string, error) {
	t.Helper()
	h := build(t, noSection, &fakeSpawn{}, &fakeGit{})
	h.run = login.run
	var steps []string
	ctx := port.WithSteps(t.Context(), func(step string) { steps = append(steps, step) })
	err := port.Prepare(ctx, []crew.State{"ready"}, h)
	return steps, err
}

var bothSteps = []string{"looking for codex on PATH", "checking that codex is logged in"}

func TestPrepareFailsNamingCodexWhenItIsNotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	login := &fakeLogin{}

	steps, err := prepare(t, login)

	if err == nil || !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "PATH") {
		t.Errorf("Prepare = %v, want an error naming codex and PATH", err)
	}
	if want := bothSteps[:1]; !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}
	if len(login.commands) != 0 {
		t.Errorf("ran %v, want nothing", login.commands)
	}
}

func TestPrepareChecksTheLoginOfACodexOnPath(t *testing.T) {
	onPath(t)
	t.Setenv(apiKey, "")
	login := &fakeLogin{stderr: "Logged in using ChatGPT\n"}

	steps, err := prepare(t, login)

	if err != nil {
		t.Fatalf("Prepare = %v, want nil", err)
	}
	if !slices.Equal(steps, bothSteps) {
		t.Errorf("steps = %q, want %q", steps, bothSteps)
	}
	if len(login.commands) != 1 {
		t.Fatalf("ran %d commands, want one", len(login.commands))
	}
	if c := login.commands[0]; c.Name != binary || !slices.Equal(c.Args, []string{"login", "status"}) {
		t.Errorf("ran %s %q, want codex login status", c.Name, c.Args)
	}
	if d := login.deadlines[0]; d.IsZero() || time.Until(d) > loginTimeout {
		t.Errorf("login status ran with deadline %v, want one within %v", d, loginTimeout)
	}
}

// codex exec authenticates with CODEX_API_KEY, which codex login status
// ignores, so a key in crew's environment counts as a login.
func TestPrepareTakesAnAPIKeyAsALogin(t *testing.T) {
	onPath(t)
	t.Setenv(apiKey, "sk-test")
	login := &fakeLogin{stderr: "Not logged in\n", err: exitError{code: 1, msg: "exit status 1"}}

	steps, err := prepare(t, login)

	if err != nil {
		t.Errorf("Prepare = %v, want nil", err)
	}
	if !slices.Equal(steps, bothSteps) {
		t.Errorf("steps = %q, want %q", steps, bothSteps)
	}
	if len(login.commands) != 0 {
		t.Errorf("ran %v, want nothing", login.commands)
	}
}

func TestPrepareFailsSayingCodexIsNotLoggedIn(t *testing.T) {
	onPath(t)
	t.Setenv(apiKey, "")
	login := &fakeLogin{stderr: "Not logged in\n", err: exitError{code: 1, msg: "codex: exit status 1: Not logged in"}}

	_, err := prepare(t, login)

	if err == nil || !strings.Contains(err.Error(), "not logged in") || !strings.Contains(err.Error(), "codex login") {
		t.Errorf("Prepare = %v, want an error saying codex is not logged in and naming codex login", err)
	}
}

// Codex also exits 1 when it cannot read its config or its stored login, and
// a check that cannot finish says so instead of claiming a logout.
func TestPrepareFailsSayingTheLoginCouldNotBeChecked(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		err    error
	}{
		{"a config error", "Error loading configuration: bad.toml\n",
			exitError{code: 1, msg: "codex: exit status 1: Error loading configuration: bad.toml"}},
		{"another exit code", "", exitError{code: 2, msg: "codex: exit status 2"}},
		{"a timeout", "", errors.New("codex: " + context.DeadlineExceeded.Error())},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			onPath(t)
			t.Setenv(apiKey, "")

			_, err := prepare(t, &fakeLogin{stderr: tt.stderr, err: tt.err})

			if err == nil || !strings.Contains(err.Error(), "could not check") || !errors.Is(err, tt.err) {
				t.Errorf("Prepare = %v, want an error saying the login could not be checked, wrapping %v", err, tt.err)
			}
			if err != nil && strings.Contains(err.Error(), "codex login`") {
				t.Errorf("Prepare = %v, want no advice to run codex login", err)
			}
		})
	}
}
