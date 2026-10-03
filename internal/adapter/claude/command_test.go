package claude

import (
	"io"
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/port"
)

// developer is the identity of a session acting as the mate developer.
var developer = port.Identity{
	Mate:  "developer",
	Login: "crew-developer[bot]",
	Env:   []string{"GH_CONFIG_DIR=/run/crew/developer"},
	Unset: []string{"GH_TOKEN", "GITHUB_TOKEN"},
}

// AE6: whatever GH_TOKEN the boss's shell exports, the session's gh reads
// the mate's directory, because its command removes the token.
func TestAE6CommandActsAsTheRunsIdentityAndNamesTheBossAndTheMates(t *testing.T) {
	run := port.Run{
		Dir: "/work", Prompt: "Implement #4", Identity: developer,
		Boss: []string{"octocat"}, Mates: []string{"crew-developer[bot]", "crew-ops[bot]"},
	}

	got := command(run, "claude-opus-5-5")

	for _, want := range []string{
		"GH_CONFIG_DIR=/run/crew/developer",
		"CREW_BOSS=octocat",
		"CREW_MATES=crew-developer[bot] crew-ops[bot]",
	} {
		if !slices.Contains(got.Env, want) {
			t.Errorf("env = %q, want it to hold %s", got.Env, want)
		}
	}
	if want := []string{"GH_TOKEN", "GITHUB_TOKEN"}; !slices.Equal(got.Unset, want) {
		t.Errorf("unset = %q, want %q", got.Unset, want)
	}
}

func TestCommandOfTheBossIsTodaysPlusTheBossAndTheMates(t *testing.T) {
	run := port.Run{Dir: "/work", Prompt: "Implement #4", Boss: []string{"octocat", "hubot"}}

	got := command(run, "claude-opus-5-5")

	want := []string{
		"BASH_DEFAULT_TIMEOUT_MS=600000", "BASH_MAX_TIMEOUT_MS=1800000",
		"CREW_BOSS=octocat hubot", "CREW_MATES=",
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
