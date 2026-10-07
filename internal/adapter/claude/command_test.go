package claude

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/port"
)

// developer is the identity of a session acting as the bot developer.
var developer = port.Identity{
	Bot:   "developer",
	Login: "crew-developer[bot]",
	Env:   []string{"GH_CONFIG_DIR=/run/crew/developer"},
	Unset: []string{"GH_TOKEN", "GITHUB_TOKEN"},
}

// AE6: whatever GH_TOKEN your shell exports, the session's gh reads
// the bot's directory, because its command removes the token.
func TestAE6CommandActsAsTheRunsIdentityAndNamesTheCodeOwnersAndTheBots(t *testing.T) {
	run := port.Run{
		Dir: "/work", Prompt: "Implement #4", Identity: developer,
		CodeOwners: []string{"octocat"}, Bots: []string{"crew-developer[bot]", "crew-ops[bot]"},
	}

	got := command(run, "claude-opus-5-5")

	for _, want := range []string{
		"GH_CONFIG_DIR=/run/crew/developer",
		"CREW_CODE_OWNERS=octocat",
		"CREW_BOTS=crew-developer[bot] crew-ops[bot]",
	} {
		if !slices.Contains(got.Env, want) {
			t.Errorf("env = %q, want it to hold %s", got.Env, want)
		}
	}
	for _, e := range got.Env {
		if strings.HasPrefix(e, "CREW_BOSS=") || strings.HasPrefix(e, "CREW_MATES=") {
			t.Errorf("env holds %q, which crew no longer sets", e)
		}
	}
	if want := []string{"GH_TOKEN", "GITHUB_TOKEN"}; !slices.Equal(got.Unset, want) {
		t.Errorf("unset = %q, want %q", got.Unset, want)
	}
}

func TestCommandAsYouIsTodaysPlusTheCodeOwnersAndTheBots(t *testing.T) {
	run := port.Run{Dir: "/work", Prompt: "Implement #4", CodeOwners: []string{"octocat", "hubot"}}

	got := command(run, "claude-opus-5-5")

	want := []string{
		"BASH_DEFAULT_TIMEOUT_MS=600000", "BASH_MAX_TIMEOUT_MS=1800000",
		"CREW_CODE_OWNERS=octocat hubot", "CREW_BOTS=",
	}
	if !slices.Equal(got.Env, want) {
		t.Errorf("env = %q, want %q", got.Env, want)
	}
	if len(got.Unset) != 0 {
		t.Errorf("unset = %q, want nothing", got.Unset)
	}
}

func TestStartRunsClaudeAsTheRunsIdentity(t *testing.T) {
	spawn := &fakeSpawn{process: newProcess(fixture(t, "success.jsonl"), nil)}
	h := build(t, noSection, spawn)

	s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: io.Discard, Identity: developer})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Wait()

	if len(spawn.commands) != 1 {
		t.Fatalf("spawned %d commands, want 1", len(spawn.commands))
	}
	if env := spawn.commands[0].Env; !slices.Contains(env, "GH_CONFIG_DIR=/run/crew/developer") {
		t.Errorf("env = %q, want the identity's GH_CONFIG_DIR", env)
	}
}

// A session given a verdict file finds it in CREW_VERDICT_FILE and may write
// its directory, which --add-dir names before another flag: Claude Code's
// --add-dir takes several paths, so right before -- it would take the
// prompt as one. The file's path stays out of the arguments.
func TestCommandGivesTheSessionItsVerdictFile(t *testing.T) {
	run := port.Run{
		Dir: "/work", Prompt: "Implement #4", Identity: developer,
		VerdictFile: "/tmp/crew-verdict-1/verdict", VerdictDir: "/tmp/crew-verdict-1",
	}

	got := command(run, "claude-opus-5-5")

	if !slices.Contains(got.Env, "CREW_VERDICT_FILE=/tmp/crew-verdict-1/verdict") {
		t.Errorf("env = %q, want CREW_VERDICT_FILE=/tmp/crew-verdict-1/verdict", got.Env)
	}
	want := []string{
		"-p", "--model", "claude-opus-5-5", "--add-dir", "/tmp/crew-verdict-1",
		"--permission-mode", "auto", "--output-format", "stream-json", "--verbose", "--", "Implement #4",
	}
	if !slices.Equal(got.Args, want) {
		t.Errorf("args = %q, want %q", got.Args, want)
	}
}
