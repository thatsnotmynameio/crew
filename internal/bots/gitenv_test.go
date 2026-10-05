package bots

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// isolateGit keeps the developer's git config and any enclosing repository
// out of the test, and skips it unless git runs config-based hooks.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	// A git hook running the tests sets these to the enclosing repository.
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR"} {
		if _, ok := os.LookupEnv(k); ok {
			t.Setenv(k, "")
			_ = os.Unsetenv(k)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	out, err := exec.CommandContext(t.Context(), "git", "version").Output()
	if err != nil {
		t.Fatalf("git version: %v", err)
	}
	if !hooksRun(string(out)) {
		t.Skipf("%s is older than git 2.54", strings.TrimSpace(string(out)))
	}
}

// gitRun runs git in dir with a fixed identity and env added to the
// environment, and returns its trimmed stdout.
func gitRun(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=crew test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			t.Fatalf("git %q in %s: %v: %s", args, dir, err, exit.Stderr)
		}
		t.Fatalf("git %q in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

const testTrailer = "Co-authored-by: crew-developer[bot] <123+crew-developer[bot]@users.noreply.github.com>"

func TestHookEntriesAddTheCoAuthorToEveryCommit(t *testing.T) {
	isolateGit(t)
	repo, hooks := t.TempDir(), t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\necho \"$1\" >> '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(hooks, "commit-msg"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, nil, "init", "--quiet")
	gitRun(t, repo, nil, "config", "core.hooksPath", hooks)
	env := configEnv("", hookEntries(coAuthorTrailer("crew-developer", 123)))

	// AE9: a commit with its own message, as lfg makes.
	gitRun(t, repo, env, "commit", "--allow-empty", "-m", "feat: x", "-m", "Its own body.")
	msg := gitRun(t, repo, nil, "log", "-1", "--format=%B")
	if !strings.HasSuffix(msg, "\n\n"+testTrailer) || !strings.HasPrefix(msg, "feat: x\n\nIts own body.") {
		t.Errorf("message = %q, want its own text then the co-author trailer", msg)
	}
	gitRun(t, repo, env, "commit", "--amend", "--allow-empty", "--no-edit")
	msg = gitRun(t, repo, nil, "log", "-1", "--format=%B")
	if n := strings.Count(msg, "Co-authored-by:"); n != 1 {
		t.Errorf("after --amend the message holds %d trailers, want 1:\n%s", n, msg)
	}
	ran, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the core.hooksPath hook did not run: %v", err)
	}
	if lines := strings.Count(string(ran), "\n"); lines != 2 {
		t.Errorf("the core.hooksPath hook ran %d times, want 2", lines)
	}
}

func TestPinnedHelperRunsWithYourGhDirectory(t *testing.T) {
	isolateGit(t)
	bin := t.TempDir()
	// A gh that answers git's credential request with its GH_CONFIG_DIR.
	gh := "#!/bin/sh\ncat >/dev/null\nprintf 'username=x\\npassword=%s\\n' \"$GH_CONFIG_DIR\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o700); err != nil {
		t.Fatal(err)
	}
	loginDir := filepath.Join(t.TempDir(), "the boss's gh")
	repo := t.TempDir()
	gitRun(t, repo, nil, "init", "--quiet")
	gitRun(t, repo, nil, "config", "credential.helper", "!"+filepath.Join(bin, "gh")+" auth git-credential")
	setup, err := probeGit(t.Context(), new(proc.Group).Run, repo)
	if err != nil {
		t.Fatalf("probeGit: %v", err)
	}
	env := configEnv("", credentialEntries(setup.helper, loginDir))
	env = append(env, "GH_CONFIG_DIR=/the/mate's/dir")
	cmd := exec.CommandContext(t.Context(), "git", "credential", "fill")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git credential fill: %v", err)
	}
	if !strings.Contains(string(out), "password="+loginDir+"\n") {
		t.Errorf("git credential fill = %q, want the helper run with GH_CONFIG_DIR=%s", out, loginDir)
	}
}

// exitError is an error of a command that exited with code.
type exitError struct{ code int }

func (e exitError) Error() string { return "exit status " + strconv.Itoa(e.code) }
func (e exitError) ExitCode() int { return e.code }

// scriptedGit returns a runner answering `git version` with version and
// `git config --get-regexp` with config, or with err when it is not nil.
func scriptedGit(version, config string, err error) proc.Runner {
	return func(_ context.Context, c proc.Command) (proc.Output, error) {
		if slices.Equal(c.Args, []string{"version"}) {
			return proc.Output{Stdout: []byte(version)}, nil
		}
		return proc.Output{Stdout: []byte(config)}, err
	}
}

func TestProbeGitFindsTheGhHelper(t *testing.T) {
	const version = "git version 2.55.0\n"
	tests := []struct {
		name, config string
		err          error
		want         string
	}{
		{name: "github.com helper with !", config: "credential.https://github.com.helper \n" +
			"credential.https://github.com.helper !/opt/homebrew/bin/gh auth git-credential\n",
			want: "/opt/homebrew/bin/gh auth git-credential"},
		{name: "helper without !", config: "credential.helper /usr/bin/gh auth git-credential\n",
			want: "/usr/bin/gh auth git-credential"},
		{name: "reset after it", config: "credential.helper !gh auth git-credential\ncredential.helper \n"},
		{name: "osxkeychain", config: "credential.helper osxkeychain\n"},
		{name: "another program", config: "credential.helper !/usr/bin/notgh auth git-credential\n"},
		{name: "another url", config: "credential.https://gitlab.com.helper !gh auth git-credential\n"},
		{name: "none", err: exitError{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup, err := probeGit(context.Background(), scriptedGit(version, tt.config, tt.err), "/repo")
			if err != nil {
				t.Fatalf("probeGit: %v", err)
			}
			if setup.helper != tt.want || !setup.hooks {
				t.Errorf("probeGit = %+v, want helper %q and hooks", setup, tt.want)
			}
		})
	}
}

func TestProbeGitFailures(t *testing.T) {
	failing := func(_ context.Context, _ proc.Command) (proc.Output, error) {
		return proc.Output{}, errors.New("git: not found")
	}
	if _, err := probeGit(context.Background(), failing, "/repo"); err == nil {
		t.Error("probeGit without git = nil error, want one")
	}
	broken := scriptedGit("git version 2.55.0", "", exitError{128})
	if _, err := probeGit(context.Background(), broken, "/repo"); err == nil {
		t.Error("probeGit with a broken config = nil error, want one")
	}
}

func TestHooksRunFromGit254(t *testing.T) {
	for out, want := range map[string]bool{
		"git version 2.54.0\n":               true,
		"git version 2.55.0.windows.1":       true,
		"git version 3.0.0":                  true,
		"git version 2.53.1":                 false,
		"git version 2.39.3 (Apple Git-146)": false,
		"git version 1.99.0":                 false,
		"git version":                        false,
		"git version x.y":                    false,
		"git version 2":                      false,
	} {
		if got := hooksRun(out); got != want {
			t.Errorf("hooksRun(%q) = %v, want %v", out, got, want)
		}
	}
}

func TestConfigEnvStartsAfterTheInheritedEntries(t *testing.T) {
	entries := []configEntry{{"a.b", "1"}, {"c.d", "2"}}
	got := configEnv("2", entries)
	want := []string{"GIT_CONFIG_COUNT=4", "GIT_CONFIG_KEY_2=a.b", "GIT_CONFIG_VALUE_2=1",
		"GIT_CONFIG_KEY_3=c.d", "GIT_CONFIG_VALUE_3=2"}
	if !slices.Equal(got, want) {
		t.Errorf("configEnv(2) = %q, want %q", got, want)
	}
	if got := configEnv("", entries); got[0] != "GIT_CONFIG_COUNT=2" || got[1] != "GIT_CONFIG_KEY_0=a.b" {
		t.Errorf("configEnv() = %q, want entries from 0", got)
	}
	if got := configEnv("2", nil); got != nil {
		t.Errorf("configEnv without entries = %q, want none", got)
	}
}

func TestCredentialEntriesQuoteYourGhDirectory(t *testing.T) {
	got := credentialEntries("/usr/bin/gh auth git-credential", "/home/o'neil/gh dir")
	want := []configEntry{
		{"credential.https://github.com.helper", ""},
		{"credential.https://github.com.helper", `!GH_CONFIG_DIR='/home/o'\''neil/gh dir' /usr/bin/gh auth git-credential`},
	}
	if !slices.Equal(got, want) {
		t.Errorf("credentialEntries = %q, want %q", got, want)
	}
}

func TestLoginGhDir(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	for _, tt := range []struct {
		vars map[string]string
		want string
	}{
		{map[string]string{"GH_CONFIG_DIR": "/gh", "XDG_CONFIG_HOME": "/xdg", "HOME": "/home/b"}, "/gh"},
		{map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/b"}, "/xdg/gh"},
		{map[string]string{"HOME": "/home/b"}, "/home/b/.config/gh"},
	} {
		if got, err := loginGhDir(env(tt.vars)); err != nil || got != tt.want {
			t.Errorf("bossGhDir(%v) = %q, %v; want %q", tt.vars, got, err, tt.want)
		}
	}
	got, err := loginGhDir(env(map[string]string{"GH_CONFIG_DIR": "rel"}))
	if err != nil || !filepath.IsAbs(got) {
		t.Errorf("bossGhDir of a relative directory = %q, %v; want it absolute", got, err)
	}
}
