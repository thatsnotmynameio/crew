package main

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
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

func TestVersionPrefersTheStampThenTheModuleVersion(t *testing.T) {
	const pseudo = "v0.1.1-0.20261003000000-abcdef123456+dirty"
	module := func(version string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/thatsnotmynameio/crew", Version: version}}
	}
	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		want    string
	}{
		{name: "stamped release", stamped: "v0.2.0", info: module("v0.1.0"), want: "v0.2.0"},
		{name: "go install of a tag", info: module("v0.2.0"), want: "v0.2.0"},
		{name: "go build in a checkout", info: module(pseudo), want: pseudo},
		{name: "devel build", info: module("(devel)"), want: "dev"},
		{name: "no module version", info: module(""), want: "dev"},
		{name: "no build info", want: "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := crewVersion(tt.stamped, tt.info); got != tt.want {
				t.Errorf("crewVersion(%q, %v) = %q, want %q", tt.stamped, tt.info, got, tt.want)
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

// R1: crew's data folder is $XDG_DATA_HOME/crew, or ~/.local/share/crew
// without it, beside the global config file.
func TestFoldersReadXDGDataHomeThenTheHomeFolder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "/x")
	if global, data := folders("/home/u"); global != "/home/u/.config/crew/config.yaml" || data != "/x/crew" {
		t.Errorf("folders = %q, %q; want the home's config file and /x/crew", global, data)
	}
	t.Setenv("XDG_DATA_HOME", "")
	if _, data := folders("/home/u"); data != "/home/u/.local/share/crew" {
		t.Errorf("data folder = %q, want /home/u/.local/share/crew", data)
	}
}

// crew --version records nothing: it creates no data folder.
func TestVersionCreatesNoStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	if got := run([]string{"--version"}); got != app.ExitClean {
		t.Fatalf("run(--version) = %d, want %d", got, app.ExitClean)
	}
	if _, err := os.Stat(filepath.Join(dir, "crew")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat %s/crew = %v, want no data folder", dir, err)
	}
}
