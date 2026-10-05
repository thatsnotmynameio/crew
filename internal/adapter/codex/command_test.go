package codex

import (
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/port"
)

// botRun is a run acting as a bot, with the environment crew's bots give a
// session: their gh config directory and the git config of the co-author
// hook, numbered after one entry crew inherited.
func botRun(prompt string) port.Run {
	return port.Run{
		Dir:    "/repo/.crew/worktrees/issue-4-review",
		Prompt: prompt,
		Identity: port.Identity{
			Bot:   "reviewer",
			Login: "crew-reviewer[bot]",
			Env: []string{
				"GH_CONFIG_DIR=/home/me/.config/crew/bots/reviewer/sessions",
				"GIT_CONFIG_COUNT=3",
				"GIT_CONFIG_KEY_1=hook.crew-co-author.event",
				"GIT_CONFIG_VALUE_1=commit-msg",
				"GIT_CONFIG_KEY_2=hook.crew-co-author.command",
				"GIT_CONFIG_VALUE_2=git interpret-trailers --in-place --if-exists addIfDifferent --trailer " +
					"'Co-authored-by: crew-reviewer[bot] <7+crew-reviewer[bot]@users.noreply.github.com>'",
			},
			Unset: []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST"},
		},
		CodeOwners: []string{"alice", "bob"},
		Bots:       []string{"crew-developer[bot]", "crew-reviewer[bot]"},
	}
}

var gitDirs = []string{"/repo/.git/worktrees/issue-4-review", "/repo/.git"}

func TestCommandRunsCodexHeadlessInTheSandboxWithTheGitDirs(t *testing.T) {
	got := command(port.Run{Dir: "/repo/.crew/worktrees/issue-4-review", Prompt: "Review #4"}, "", gitDirs)

	want := []string{
		"exec", "--json", "--approve-for-me",
		"-c", "sandbox_workspace_write.network_access=true",
		"--add-dir", "/repo/.git/worktrees/issue-4-review",
		"--add-dir", "/repo/.git",
		"-c", "shell_environment_policy.include_only=[]",
		"-c", `shell_environment_policy.set.CREW_CODE_OWNERS=""`,
		"-c", `shell_environment_policy.set.CREW_BOTS=""`,
		"--", "Review #4",
	}
	if got.Name != "codex" || got.Dir != "/repo/.crew/worktrees/issue-4-review" || !slices.Equal(got.Args, want) {
		t.Errorf("command = %#v,\nwant codex in the worktree with args %q", got, want)
	}
	if wantEnv := []string{"CREW_CODE_OWNERS=", "CREW_BOTS="}; !slices.Equal(got.Env, wantEnv) || got.Unset != nil {
		t.Errorf("env = %q, unset = %q, want %q and none", got.Env, got.Unset, wantEnv)
	}
}

func TestCommandPassesTheModelOnlyWhenSet(t *testing.T) {
	run := port.Run{Dir: "/w", Prompt: "p"}

	if args := command(run, "", gitDirs).Args; slices.Contains(args, "-m") {
		t.Errorf("args = %q, want no -m without a model", args)
	}
	args := command(run, "gpt-5.5", gitDirs).Args
	if i := slices.Index(args, "-m"); i < 0 || i+1 >= len(args) || args[i+1] != "gpt-5.5" {
		t.Errorf("args = %q, want -m gpt-5.5", args)
	}
}

func TestCommandAddsAGitDirOnceWhenTheWorktreeIsTheMainCheckout(t *testing.T) {
	args := command(port.Run{Dir: "/repo", Prompt: "p"}, "", []string{"/repo/.git", "/repo/.git"}).Args

	var dirs []string
	for i, a := range args {
		if a == "--add-dir" {
			dirs = append(dirs, args[i+1])
		}
	}
	if want := []string{"/repo/.git"}; !slices.Equal(dirs, want) {
		t.Errorf("--add-dir %q, want %q", dirs, want)
	}
}

// The bot's environment reaches codex's process and, through the shell
// environment policy, every command codex runs, whatever the user's own
// policy filters; the variables the bot must not inherit are set empty there.
func TestCommandPinsTheBotsEnvironmentInsideCodex(t *testing.T) {
	run := botRun("Review #4")
	got := command(run, "", gitDirs)

	wantEnv := slices.Concat(run.Identity.Env, []string{
		"CREW_CODE_OWNERS=alice bob", "CREW_BOTS=crew-developer[bot] crew-reviewer[bot]",
	})
	if !slices.Equal(got.Env, wantEnv) {
		t.Errorf("env = %q, want %q", got.Env, wantEnv)
	}
	if !slices.Equal(got.Unset, run.Identity.Unset) {
		t.Errorf("unset = %q, want %q", got.Unset, run.Identity.Unset)
	}
	for _, want := range []string{
		`shell_environment_policy.set.GH_CONFIG_DIR="/home/me/.config/crew/bots/reviewer/sessions"`,
		`shell_environment_policy.set.GIT_CONFIG_COUNT="3"`,
		`shell_environment_policy.set.GIT_CONFIG_KEY_2="hook.crew-co-author.command"`,
		`shell_environment_policy.set.GIT_CONFIG_VALUE_2="git interpret-trailers --in-place --if-exists addIfDifferent ` +
			`--trailer 'Co-authored-by: crew-reviewer[bot] <7+crew-reviewer[bot]@users.noreply.github.com>'"`,
		`shell_environment_policy.set.CREW_CODE_OWNERS="alice bob"`,
		`shell_environment_policy.set.CREW_BOTS="crew-developer[bot] crew-reviewer[bot]"`,
		`shell_environment_policy.set.GH_TOKEN=""`,
		`shell_environment_policy.set.GITHUB_TOKEN=""`,
		`shell_environment_policy.set.GH_ENTERPRISE_TOKEN=""`,
		`shell_environment_policy.set.GITHUB_ENTERPRISE_TOKEN=""`,
		`shell_environment_policy.set.GH_HOST=""`,
		"shell_environment_policy.include_only=[]",
	} {
		if !hasConfig(got.Args, want) {
			t.Errorf("args = %q, want -c %s", got.Args, want)
		}
	}
}

// hasConfig reports whether args pass -c override.
func hasConfig(args []string, override string) bool {
	for i, a := range args[:len(args)-1] {
		if a == "-c" && args[i+1] == override {
			return true
		}
	}
	return false
}

// A prompt that starts with a dash, or that names a codex exec subcommand,
// is still the prompt: it comes last, after --, unchanged.
func TestCommandPassesThePromptVerbatimAfterDoubleDash(t *testing.T) {
	for _, prompt := range []string{"- list files\n--dry-run does nothing", "review", "/compound-engineering:lfg #42"} {
		args := command(botRun(prompt), "gpt-5.5", gitDirs).Args

		if n := len(args); n < 2 || args[n-2] != "--" || args[n-1] != prompt {
			t.Errorf("args end %q, want -- and the prompt %q", args[max(0, len(args)-2):], prompt)
		}
	}
}

// crew keeps codex's sandbox, the user's config and codex's session files:
// it passes none of the flags that would drop them.
func TestCommandKeepsTheSandboxAndTheUsersConfig(t *testing.T) {
	args := command(botRun("p"), "gpt-5.5", gitDirs).Args

	for _, flag := range []string{
		"--sandbox", "-s", "--dangerously-bypass-approvals-and-sandbox", "--yolo",
		"--ignore-user-config", "--ephemeral", "--skip-git-repo-check",
	} {
		if slices.Contains(args, flag) {
			t.Errorf("args = %q, want no %s", args, flag)
		}
	}
}
