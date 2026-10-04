package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/app"
)

func TestWorktreesNeedsExactlyClean(t *testing.T) {
	outsideGit(t)
	tests := []struct {
		name string
		args []string
	}{
		{name: "no command", args: []string{"worktrees"}},
		{name: "unknown command", args: []string{"worktrees", "prune"}},
		{name: "extra argument", args: []string{"worktrees", "clean", "extra"}},
		{name: "flag instead of a command", args: []string{"worktrees", "--plain"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runCaptured(t, tt.args...)
			if got.code != app.ExitConfig {
				t.Errorf("run(%q) = %d, want %d", tt.args, got.code, app.ExitConfig)
			}
			if !strings.Contains(got.stderr, "crew: usage: crew worktrees clean") {
				t.Errorf("run(%q) stderr = %q, want the usage", tt.args, got.stderr)
			}
			if got.stdout != "" {
				t.Errorf("run(%q) stdout = %q, want nothing", tt.args, got.stdout)
			}
		})
	}
}

func TestWorktreesCleanOutsideAGitRepositoryIsAnEnvironmentError(t *testing.T) {
	outsideGit(t)
	got := runCaptured(t, "worktrees", "clean")
	if got.code != app.ExitConfig {
		t.Errorf("worktrees clean outside a git repository = %d, want %d", got.code, app.ExitConfig)
	}
	if !strings.Contains(got.stderr, "crew worktrees clean must run inside a git repository") {
		t.Errorf("stderr = %q, want the git repository message", got.stderr)
	}
}

func TestWorktreesCleanWithoutAConfigIsAConfigError(t *testing.T) {
	insideGit(t)
	got := runCaptured(t, "worktrees", "clean")
	if got.code != app.ExitConfig {
		t.Errorf("worktrees clean without a config = %d, want %d", got.code, app.ExitConfig)
	}
	if !strings.Contains(got.stderr, "crew: ") || !strings.Contains(got.stderr, "config.yaml") {
		t.Errorf("stderr = %q, want the config error", got.stderr)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want nothing listed", got.stdout)
	}
}

// githubConfig is a config of the production adapters, with one stage.
const githubConfig = `
config:
  harness: claude
tracker:
  name: github
workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: done
    on_failure: failed
    actions:
      - name: development
        prompt: "Implement issue {{.Issue.Ref}}"
`

func TestWorktreesCleanWithoutWorktreesSaysSo(t *testing.T) {
	root := insideGit(t)
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(githubConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	got := runCaptured(t, "worktrees", "clean")
	if got.code != app.ExitClean {
		t.Errorf("worktrees clean without worktrees = %d, want %d; stderr:\n%s", got.code, app.ExitClean, got.stderr)
	}
	want := "No crew worktrees in " + filepath.Join(root, ".crew", "worktrees") + "."
	if !strings.Contains(got.stdout, want) {
		t.Errorf("stdout = %q, want %q", got.stdout, want)
	}
}

func TestWorktreesAfterAFlagIsUnexpected(t *testing.T) {
	got := runCaptured(t, "--plain", "worktrees", "clean")
	if got.code != app.ExitConfig {
		t.Errorf("run(--plain worktrees clean) = %d, want %d", got.code, app.ExitConfig)
	}
	if !strings.Contains(got.stderr, `crew: unexpected argument "worktrees"`) {
		t.Errorf("stderr = %q, want the unexpected argument", got.stderr)
	}
}

func TestUsageShowsTheWorktreesCommand(t *testing.T) {
	got := runCaptured(t, "-h")
	if got.code != app.ExitClean {
		t.Errorf("run(-h) = %d, want %d", got.code, app.ExitClean)
	}
	if !strings.Contains(got.stderr, "crew worktrees clean") {
		t.Errorf("run(-h) stderr = %q, want %q", got.stderr, "crew worktrees clean")
	}
}
