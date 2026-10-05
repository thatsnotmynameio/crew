package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/bots"
)

// captured is what one run returned and printed.
type captured struct {
	code           int
	stdout, stderr string
}

// runCaptured runs run with args, with os.Stdout and os.Stderr swapped for
// files, and returns its exit code and what it printed on each.
func runCaptured(t *testing.T, args ...string) captured {
	t.Helper()
	dir := t.TempDir()
	capture := func(name string) *os.File {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	outFile, errFile := capture("stdout"), capture("stderr")
	oldOut, oldErr := os.Stdout, os.Stderr
	t.Cleanup(func() { os.Stdout, os.Stderr = oldOut, oldErr })
	os.Stdout, os.Stderr = outFile, errFile
	code := run(args)
	read := func(f *os.File) string {
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	return captured{code: code, stdout: read(outFile), stderr: read(errFile)}
}

// outsideGit moves the test into a directory no git repository contains,
// with the user's git config kept out.
func outsideGit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Cleanup(func() { signal.Reset() })
}

func TestBotsNeedsExactlyCreateAndAName(t *testing.T) {
	outsideGit(t)
	tests := []struct {
		name string
		args []string
	}{
		{name: "no command", args: []string{"mates"}},
		{name: "unknown command", args: []string{"mates", "list"}},
		{name: "no name", args: []string{"mates", "create"}},
		{name: "two names", args: []string{"mates", "create", "a", "b"}},
		{name: "flag instead of a command", args: []string{"mates", "--plain"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runCaptured(t, tt.args...)
			code, stdout, stderr := got.code, got.stdout, got.stderr
			if code != app.ExitConfig {
				t.Errorf("run(%q) = %d, want %d", tt.args, code, app.ExitConfig)
			}
			if !strings.Contains(stderr, "usage: crew mates create <name>") {
				t.Errorf("run(%q) stderr = %q, want the usage", tt.args, stderr)
			}
			if stdout != "" {
				t.Errorf("run(%q) stdout = %q, want nothing", tt.args, stdout)
			}
		})
	}
}

func TestBotsCreateChecksTheNameBeforeLookingForGit(t *testing.T) {
	outsideGit(t)
	got := runCaptured(t, "mates", "create", "Bad_Name")
	code, stderr := got.code, got.stderr
	if code != app.ExitConfig {
		t.Errorf("mates create Bad_Name = %d, want %d", code, app.ExitConfig)
	}
	if !strings.Contains(stderr, `crew: the mate name "Bad_Name" holds 'B'`) {
		t.Errorf("stderr = %q, want the name rule", stderr)
	}
	if strings.Contains(stderr, "git repository") {
		t.Errorf("stderr = %q, want no word about the git repository", stderr)
	}
}

func TestBotsCreateOutsideAGitRepositoryIsAnEnvironmentError(t *testing.T) {
	outsideGit(t)
	got := runCaptured(t, "mates", "create", "tester")
	code, stderr := got.code, got.stderr
	if code != app.ExitConfig {
		t.Errorf("mates create tester outside a git repository = %d, want %d", code, app.ExitConfig)
	}
	if !strings.Contains(stderr, "crew mates create must run inside a git repository") {
		t.Errorf("stderr = %q, want the git repository message", stderr)
	}
}

// insideGit moves the test into the root of a new git repository, with the
// user's git config kept out and the bots kept under the test's own config
// directory, and returns the root.
func insideGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "repo")
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Chdir(root)
	t.Cleanup(func() { signal.Reset() })
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBotsCreateAsksGhForTheRepositoryAtItsRoot(t *testing.T) {
	root := insideGit(t)
	// gh fails as it does in a repository without a GitHub remote; git is
	// the real one, later on PATH.
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"no git remotes found in $(pwd -P)\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	got := runCaptured(t, "mates", "create", "tester")
	if got.code != app.ExitConfig {
		t.Errorf("mates create tester with gh failing = %d, want %d", got.code, app.ExitConfig)
	}
	for _, want := range []string{"crew: gh could not resolve", "gh auth login", "no git remotes found in " + root} {
		if !strings.Contains(got.stderr, want) {
			t.Errorf("stderr = %q, want %q", got.stderr, want)
		}
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want nothing before GitHub is asked anything", got.stdout)
	}
}

func TestArgumentsOtherThanBotsFirstAreUnexpected(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "argument", args: []string{"now"}, want: `crew: unexpected argument "now"`},
		{
			name: "mates after a flag", args: []string{"--plain", "mates", "create", "x"},
			want: `crew: unexpected argument "mates"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runCaptured(t, tt.args...)
			code, stderr := got.code, got.stderr
			if code != app.ExitConfig {
				t.Errorf("run(%q) = %d, want %d", tt.args, code, app.ExitConfig)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("run(%q) stderr = %q, want %q", tt.args, stderr, tt.want)
			}
		})
	}
}

func TestUsageShowsTheBotsCommand(t *testing.T) {
	got := runCaptured(t, "-h")
	code, stderr := got.code, got.stderr
	if code != app.ExitClean {
		t.Errorf("run(-h) = %d, want %d", code, app.ExitClean)
	}
	for _, want := range []string{"crew [--plain] [--version]", "crew mates create <name>", "-plain", "-version"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("run(-h) stderr = %q, want %q", stderr, want)
		}
	}
}

func TestBotsExitCodeFollowsTheError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		want       int
		wantStderr string
	}{
		{name: "ready", want: app.ExitClean},
		{
			name: "environment", err: &bots.EnvError{Err: errors.New("gh is not on PATH")},
			want: app.ExitConfig, wantStderr: "crew: gh is not on PATH\n",
		},
		{
			name: "wrapped environment", err: fmt.Errorf("create: %w", &bots.EnvError{Err: errors.New("no repository")}),
			want: app.ExitConfig, wantStderr: "crew: create: no repository\n",
		},
		{
			name: "failure", err: errors.New("GitHub did not create the app"),
			want: app.ExitFailure, wantStderr: "crew: GitHub did not create the app\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := botsExit(&stderr, tt.err); got != tt.want {
				t.Errorf("matesExit(%v) = %d, want %d", tt.err, got, tt.want)
			}
			if stderr.String() != tt.wantStderr {
				t.Errorf("matesExit(%v) printed %q, want %q", tt.err, stderr.String(), tt.wantStderr)
			}
		})
	}
}
