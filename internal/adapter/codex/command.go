package codex

import (
	"slices"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// binary is the Codex CLI the adapter runs.
const binary = "codex"

// envPolicy is the key of the user's Codex config that decides the
// environment of the commands codex runs.
const envPolicy = "shell_environment_policy"

// command builds the headless Codex run of run with model, or with the model
// Codex picks when model is empty, in a sandbox that can also write gitDirs.
// It is pure, apart from the session's judging.
//
// --approve-for-me is Codex's automatic approval review in its
// workspace-write sandbox; network access is opened on top, and each
// distinct git dir is a writable root, since a worktree's commits write to
// the main repository's git dirs. The prompt goes last, after --, so one
// that starts with a dash or names a subcommand is still the prompt; proc
// closes stdin, so Codex appends nothing to it.
//
// The session acts as run's identity, with its environment added and the
// variables it unsets removed, and gets the code owners' and the bots'
// logins as CREW_CODE_OWNERS and CREW_BOTS. Each of those variables is also
// set inside Codex's shell environment policy, and the user's include_only
// is cleared, so no filter of the user's drops them from the commands codex
// runs; the unset variables are set empty there, so no shell profile brings
// a token of yours back.
func command(run port.Run, model string, gitDirs []string) proc.Command {
	env := slices.Concat(
		run.Identity.Env,
		[]string{"CREW_CODE_OWNERS=" + strings.Join(run.CodeOwners, " "), "CREW_BOTS=" + strings.Join(run.Bots, " ")},
	)
	args := []string{"exec", "--json", "--approve-for-me", "-c", "sandbox_workspace_write.network_access=true"}
	var added []string
	for _, dir := range gitDirs {
		if !slices.Contains(added, dir) {
			added = append(added, dir)
			args = append(args, "--add-dir", dir)
		}
	}
	if model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, "-c", envPolicy+".include_only=[]")
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		args = append(args, "-c", envPolicy+".set."+name+"="+tomlString(value))
	}
	for _, name := range run.Identity.Unset {
		args = append(args, "-c", envPolicy+".set."+name+"="+tomlString(""))
	}
	return proc.Command{
		Name:  binary,
		Args:  append(args, "--", run.Prompt),
		Dir:   run.Dir,
		Env:   env,
		Unset: run.Identity.Unset,
	}
}
