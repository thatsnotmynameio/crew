package main

import (
	"os/signal"
	"path/filepath"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/app"
)

func TestRunExitsBeforeStartingOnFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "version", args: []string{"--version"}, want: app.ExitClean},
		{name: "help", args: []string{"-h"}, want: app.ExitClean},
		{name: "unknown flag", args: []string{"--fast"}, want: app.ExitConfig},
		{name: "unexpected argument", args: []string{"now"}, want: app.ExitConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args); got != tt.want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunOutsideAGitRepositoryIsAnEnvironmentError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// run catches the stop signals before it looks for git.
	t.Cleanup(func() { signal.Reset() })
	if got := run(nil); got != app.ExitConfig {
		t.Errorf("run outside a git repository = %d, want %d", got, app.ExitConfig)
	}
}
