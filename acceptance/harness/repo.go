package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The repository every scenario runs crew in: RepositoryOwner/RepositoryName
// on the fake GitHub, and a local clone in a directory named RepositoryName.
const (
	RepositoryOwner = "acme"
	RepositoryName  = "widgets"
)

// The files the scenario's home and repository start with.
const (
	filePerm = 0o600
	dirPerm  = 0o700
	// gitIdentity is the global git config: the test identity commits are
	// made as, and no signing.
	gitIdentity = "[user]\n\tname = Acceptance Tester\n\temail = tester@example.com\n" +
		"[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n"
	// readme is the seed commit's README.
	readme = "# " + RepositoryName + "\n"
	// configPath is where crew reads its config, relative to the
	// repository's root.
	configPath = ".crew/config.yaml"
)

// newHome makes the scenario's empty HOME, its XDG_CONFIG_HOME, holding
// crew/config.yaml with globalConfig unless it is "", and its global git
// config, under base.
func newHome(tb testing.TB, base, globalConfig string) home {
	tb.Helper()
	h := home{dir: filepath.Join(base, "home"), config: filepath.Join(base, "config"),
		gitConfig: filepath.Join(base, "gitconfig")}
	for _, dir := range []string{h.dir, h.config} {
		if err := os.Mkdir(dir, dirPerm); err != nil {
			tb.Fatalf("make the scenario's home: %v", err)
		}
	}
	if err := os.WriteFile(h.gitConfig, []byte(gitIdentity), filePerm); err != nil {
		tb.Fatalf("write the scenario's git config: %v", err)
	}
	if globalConfig != "" {
		if err := writeFile(filepath.Join(h.config, "crew", "config.yaml"), globalConfig); err != nil {
			tb.Fatalf("write crew's global config: %v", err)
		}
	}
	return h
}

// newRepo makes the scenario's repository under base and returns its path:
// a bare origin whose default branch main holds one seed commit, with a
// README and, unless config is "", .crew/config.yaml holding config; and a
// clone of it named RepositoryName, so origin/HEAD is set as in a user's
// clone. git is the git binary and env its environment.
func newRepo(tb testing.TB, git string, env []string, base, config string) string {
	tb.Helper()
	origin := filepath.Join(base, RepositoryName+".git")
	seed := filepath.Join(base, "seed")
	repo := filepath.Join(base, RepositoryName)
	run := func(dir string, args ...string) {
		tb.Helper()
		cmd := exec.CommandContext(tb.Context(), git, args...) //nolint:gosec // G204: git, as the test's PATH finds it
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("git %q in %s: %v\n%s", args, dir, err, out)
		}
	}
	run(base, "init", "--quiet", "--bare", "--initial-branch=main", origin)
	run(base, "init", "--quiet", "--initial-branch=main", seed)
	files := map[string]string{"README.md": readme}
	if config != "" {
		files[configPath] = config
	}
	for name, content := range files {
		if err := writeFile(filepath.Join(seed, name), content); err != nil {
			tb.Fatalf("seed the repository: %v", err)
		}
	}
	run(seed, "add", "--all")
	run(seed, "commit", "--quiet", "-m", "Seed the repository")
	run(seed, "push", "--quiet", origin, "HEAD:main")
	run(base, "clone", "--quiet", origin, repo)
	return repo
}

// writeFile writes content to path, making its directory.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), filePerm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
