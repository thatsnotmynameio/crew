package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// execBits are the permission bits of which any one makes a file
// executable for PATH lookup.
const execBits = 0o111

// toolDirs returns the directories of git and sh, as the test process's
// PATH finds them, without repeats, and the path of git.
func toolDirs() ([]string, string, error) {
	var dirs []string
	var git string
	for _, name := range []string{"git", "sh"} {
		path, err := exec.LookPath(name)
		if err != nil {
			return nil, "", fmt.Errorf("find %s, which crew runs, on the test's PATH: %w", name, err)
		}
		if name == "git" {
			git = path
		}
		if dir := filepath.Dir(path); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs, git, nil
}

// home is the scenario's own home: the directories and file that stand in
// for the user's, so nothing of the developer's configuration reaches crew.
type home struct {
	// dir is HOME and config is XDG_CONFIG_HOME, both empty.
	dir, config string
	// gitConfig is GIT_CONFIG_GLOBAL, with a test identity.
	gitConfig string
}

// environment is crew's whole environment, an allowlist: PATH holds the
// doubles' bin directory, then tools, the directories of git and sh. HOME
// and XDG_CONFIG_HOME are empty directories, so crew finds no bots and no
// gh login. TZ=UTC and LANG=C.UTF-8 make times and text the same on every
// machine. Git reads no system config and a global config with a test
// identity. GORACE=atexit_sleep_ms=0 removes the second the race detector
// waits at every successful exit of a -race test binary, which the doubles
// are: crew passes its environment on to them. SocketEnv tells the doubles
// where the server is. No GitHub token is ever set. TERM is set only in a
// pseudo-terminal, by StartScreen.
func environment(s *Server, h home, tools []string) []string {
	path := strings.Join(append([]string{s.Bin()}, tools...), string(os.PathListSeparator))
	return []string{
		"PATH=" + path,
		"HOME=" + h.dir,
		"XDG_CONFIG_HOME=" + h.config,
		"TZ=UTC",
		"LANG=C.UTF-8",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + h.gitConfig,
		"GORACE=atexit_sleep_ms=0",
		SocketEnv + "=" + s.Socket(),
	}
}

// checkPath returns an error naming gh or claude when the PATH of env does
// not resolve it to the double in bin.
func checkPath(env []string, bin string) error {
	var path string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	for _, name := range []string{ghName, claudeName} {
		want := filepath.Join(bin, name)
		got, ok := lookIn(path, name)
		if !ok {
			return fmt.Errorf("%s is not on crew's PATH %s: the doubles' bin directory lacks it", name, path)
		}
		if got != want {
			return fmt.Errorf("%s resolves to %s on crew's PATH, not to its double %s", name, got, want)
		}
	}
	return nil
}

// lookIn returns the first executable file named name in the directories
// of path, as a shell finds a command.
func lookIn(path, name string) (string, bool) {
	for _, dir := range filepath.SplitList(path) {
		file := filepath.Join(dir, name)
		if info, err := os.Stat(file); err == nil && !info.IsDir() && info.Mode()&execBits != 0 {
			return file, true
		}
	}
	return "", false
}
