package mates

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// The oldest git that runs hooks set in its config, which the co-author
// hook is.
const (
	hookMajor = 2
	hookMinor = 54
)

// The keys of the boss's git config whose helper crew pins (KTD8).
const (
	helperKey       = "credential.helper"
	githubHelperKey = "credential.https://github.com.helper"
)

// ghHelperSuffix ends a credential helper that runs gh.
const ghHelperSuffix = " auth git-credential"

// gitSetup is what crew learned of the boss's git in the repository.
type gitSetup struct {
	// version is git's own line, such as "git version 2.55.0".
	version string
	// hooks is whether git runs hooks set in its config.
	hooks bool
	// helper is the command of the credential helper for github.com that
	// runs gh, without its leading "!", or "" when there is none.
	helper string
}

// exitCoder is an error of a command that exited, such as *exec.ExitError.
type exitCoder interface {
	error
	ExitCode() int
}

// configEntry is one key and value of git's config.
type configEntry struct {
	key, value string
}

// probeGit asks git, as the boss in root, for its version and its
// credential helpers.
func probeGit(ctx context.Context, run proc.Runner, root string) (gitSetup, error) {
	out, err := run(ctx, proc.Command{Name: "git", Args: []string{"version"}, Dir: root})
	if err != nil {
		return gitSetup{}, envErrorf("crew could not run git version: %w", err)
	}
	setup := gitSetup{version: strings.TrimSpace(string(out.Stdout)), hooks: hooksRun(string(out.Stdout))}
	out, err = run(ctx, proc.Command{Name: "git", Args: []string{"config", "--get-regexp", `^credential\.`}, Dir: root})
	if exit, ok := errors.AsType[exitCoder](err); ok && exit.ExitCode() == 1 {
		// git config exits 1 when no key matches.
		return setup, nil
	}
	if err != nil {
		return gitSetup{}, envErrorf("crew could not read git's credential helpers: %w", err)
	}
	setup.helper = ghHelper(string(out.Stdout))
	return setup, nil
}

// hooksRun reports whether the git whose `git version` printed out runs
// hooks set in its config: version 2.54 or newer.
func hooksRun(out string) bool {
	var major, minor int
	if _, err := fmt.Sscanf(out, "git version %d.%d", &major, &minor); err != nil {
		return false
	}
	return major > hookMajor || (major == hookMajor && minor >= hookMinor)
}

// ghHelper returns the command of the credential helper for github.com
// that runs gh, found in out, the output of `git config --get-regexp` over
// the credential keys, or "" when none does. Like git, it lets an empty
// value reset the helpers before it.
func ghHelper(out string) string {
	helper := ""
	for line := range strings.Lines(out) {
		key, value, _ := strings.Cut(strings.TrimRight(line, "\n"), " ")
		if !strings.EqualFold(key, helperKey) && !strings.EqualFold(key, githubHelperKey) {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "!"))
		switch {
		case command == "":
			helper = ""
		case helper == "" && runsGh(command):
			helper = command
		}
	}
	return helper
}

// runsGh reports whether the helper command runs `gh auth git-credential`.
func runsGh(command string) bool {
	program, ok := strings.CutSuffix(command, ghHelperSuffix)
	return ok && filepath.Base(strings.Trim(program, `'"`)) == "gh"
}

// coAuthorTrailer returns GitHub's co-author trailer of the bot of the app
// slug, whose user id is id.
func coAuthorTrailer(slug string, id int64) string {
	login := botLogin(slug)
	return fmt.Sprintf("Co-authored-by: %s <%d+%s@users.noreply.github.com>", login, id, login)
}

// hookEntries returns the config entries of a commit-msg hook that adds
// trailer to every commit message, unless the message already holds it.
// git appends the message's path to the command.
func hookEntries(trailer string) []configEntry {
	return []configEntry{
		{"hook.crew-co-author.event", "commit-msg"},
		{"hook.crew-co-author.command",
			"git interpret-trailers --in-place --if-exists addIfDifferent --trailer " + shellQuote(trailer)},
	}
}

// credentialEntries returns the config entries that make git ask helper,
// a command running gh, for github.com's credentials with gh's config
// directory pinned to bossDir, the boss's own: they reset the github.com
// helpers and set that one.
func credentialEntries(helper, bossDir string) []configEntry {
	return []configEntry{
		{githubHelperKey, ""},
		{githubHelperKey, "!GH_CONFIG_DIR=" + shellQuote(bossDir) + " " + helper},
	}
}

// configEnv returns the environment entries that add entries to git's
// config after the inherited ones, inherited being the GIT_CONFIG_COUNT
// crew got. It returns none for no entries.
func configEnv(inherited string, entries []configEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	first, err := strconv.Atoi(inherited)
	if err != nil || first < 0 {
		first = 0
	}
	env := make([]string, 0, 1+2*len(entries))
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(first+len(entries)))
	for i, e := range entries {
		n := strconv.Itoa(first + i)
		env = append(env, "GIT_CONFIG_KEY_"+n+"="+e.key, "GIT_CONFIG_VALUE_"+n+"="+e.value)
	}
	return env
}

// bossGhDir returns the boss's own gh config directory, made absolute:
// $GH_CONFIG_DIR, else $XDG_CONFIG_HOME/gh, else $HOME/.config/gh.
func bossGhDir(getenv func(string) string) (string, error) {
	dir := getenv("GH_CONFIG_DIR")
	switch {
	case dir != "":
	case getenv("XDG_CONFIG_HOME") != "":
		dir = filepath.Join(getenv("XDG_CONFIG_HOME"), "gh")
	default:
		dir = filepath.Join(getenv("HOME"), ".config", "gh")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", envErrorf("find the boss's gh config directory: %w", err)
	}
	return abs, nil
}

// shellQuote returns s single-quoted for a POSIX shell, so the shell reads
// it back exactly.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
