package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
)

// writeGlobal writes body as crew/config.yaml in a new config directory and
// returns its path; for noFile it returns the path with no file there.
func writeGlobal(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crew", "config.yaml")
	if body == noFile {
		return path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// loadAll loads global as the global file and shared and local as the
// repository's files, leaving out each whose body is noFile.
func loadAll(t *testing.T, global, shared, local string) *config.Config {
	t.Helper()
	cfg, err := config.Load(writeFiles(t, shared, local), writeGlobal(t, global))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// loadAllErr loads the three files, expects an error, and returns its lines
// and the global file's path.
func loadAllErr(t *testing.T, global, shared, local string) ([]string, string) {
	t.Helper()
	path := writeGlobal(t, global)
	cfg, err := config.Load(writeFiles(t, shared, local), path)
	if err == nil {
		t.Fatalf("Load succeeded with %+v, want an error", cfg)
	}
	return strings.Split(err.Error(), "\n"), path
}

// Covers AE1 and AE2: config.yaml's keys replace the global file's, and
// config.local.yaml's replace both.
func TestTheRepositorysFilesReplaceTheGlobalFilesKeys(t *testing.T) {
	global := "poll_interval_seconds: 60\n" + sixColumns
	cfg := loadAll(t, global, "poll_interval_seconds: 300\n"+oneRule, noFile)
	if cfg.PollInterval != 300*time.Second {
		t.Errorf("PollInterval = %v, want config.yaml's 5m0s", cfg.PollInterval)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Name != "implement" {
		t.Errorf("Rules = %+v, want config.yaml's implement", cfg.Rules)
	}
	if len(cfg.Board) != 6 || !cfg.BoardWritten {
		t.Errorf("Board = %+v (written %v), want the global file's six columns", cfg.Board, cfg.BoardWritten)
	}
	cfg = loadAll(t, global, "poll_interval_seconds: 300\n"+oneRule, "poll_interval_seconds: 30\n")
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want config.local.yaml's 30s", cfg.PollInterval)
	}
}

// Covers AE3: the repository's agents replace every agent of the global
// file.
func TestTheRepositorysAgentsReplaceTheGlobalAgents(t *testing.T) {
	global := `agents:
  developer:
    harness: {name: claude}
  reviewer:
    harness: {name: claude}
`
	shared := "agents:\n  developer:\n    harness: {name: codex}\n" +
		strings.Replace(ruleOnly, "prompt:", "agent: developer\n        prompt:", 1)
	cfg := loadAll(t, global, shared, noFile)
	if len(cfg.Agents) != 1 || cfg.Agents[0].Name != "developer" || cfg.Agents[0].Harness != "codex" {
		t.Errorf("Agents = %+v, want only the repository's developer on codex", cfg.Agents)
	}
}

// Covers AE4: the global file alone is a config.
func TestTheGlobalFileAloneIsAConfig(t *testing.T) {
	cfg, err := config.Load(t.TempDir(), writeGlobal(t, oneRule))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Name != "implement" {
		t.Errorf("Rules = %+v, want the global file's implement", cfg.Rules)
	}
}

// Covers AE5: with none of the three files, the error names them all and
// points to the example.
func TestNoFileNamesAllThree(t *testing.T) {
	root, global := t.TempDir(), writeGlobal(t, noFile)
	_, err := config.Load(root, global)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %v is not fs.ErrNotExist", err)
	}
	for _, want := range []string{root, sharedName, localName, global, ".crew/config.example.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

// Without a global path, the missing-config error says crew reads no
// global file and why, rather than naming a file it would not read.
func TestNoFileAndNoGlobalPathSaysWhy(t *testing.T) {
	_, err := config.Load(t.TempDir(), "")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %v is not fs.ErrNotExist", err)
	}
	for _, want := range []string{"crew reads no global", "~/.config/crew/config.yaml",
		"neither XDG_CONFIG_HOME nor the home directory is an absolute path"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

// Covers AE6, AE7 and R8: XDG_CONFIG_HOME wins over the home directory, the
// path is the same on every OS, and a relative or unknown directory gives
// no global file.
func TestGlobalFile(t *testing.T) {
	tests := []struct {
		name, xdg, home, want string
	}{
		{"XDG_CONFIG_HOME wins", "/tmp/x", "/home/u", "/tmp/x/crew/config.yaml"},
		{"home without XDG_CONFIG_HOME", "", "/home/u", "/home/u/.config/crew/config.yaml"},
		{"neither", "", "", ""},
		{"relative XDG_CONFIG_HOME", "relative/dir", "/home/u", ""},
		{"relative home", "", "relative/home", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := config.GlobalFile(tt.xdg, tt.home); got != tt.want {
				t.Errorf("GlobalFile(%q, %q) = %q, want %q", tt.xdg, tt.home, got, tt.want)
			}
		})
	}
}

// Covers AE7: a config under ~/Library/Application Support is never read,
// since GlobalFile never points there.
func TestApplicationSupportIsNeverRead(t *testing.T) {
	home := t.TempDir()
	library := filepath.Join(home, "Library", "Application Support", "crew")
	if err := os.MkdirAll(library, 0o750); err != nil {
		t.Fatal(err)
	}
	body := []byte("poll_interval_seconds: 7\n")
	if err := os.WriteFile(filepath.Join(library, "config.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(writeFiles(t, oneRule, noFile), config.GlobalFile("", home))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PollInterval != 300*time.Second {
		t.Errorf("PollInterval = %v, want the default 5m0s, not Application Support's", cfg.PollInterval)
	}
}

// Covers AE8: an error about a key of the global file names its path.
func TestAnErrorNamesTheGlobalFile(t *testing.T) {
	lines, global := loadAllErr(t, "# mine\npoll_interval_seconds: 60\nboard_columns: 3\n", oneRule, noFile)
	if want := global + ": board_columns (line 3): unknown key"; !reflect.DeepEqual(lines, []string{want}) {
		t.Errorf("error = %q, want %q", lines, want)
	}
}

// Covers AE9: an old key of the global file is refused, naming it.
func TestOldKeysOfTheGlobalFileAreRefused(t *testing.T) {
	lines, global := loadAllErr(t, "workflow: []\n", oneRule, noFile)
	want := global + ": workflow (line 1): now rules, which maps each rule's name to the rule"
	if !reflect.DeepEqual(lines, []string{want}) {
		t.Errorf("error = %q, want %q", lines, want)
	}
}

// Covers AE10: a global file of comments only, or an empty one, changes
// nothing.
func TestAnEmptyGlobalFileChangesNothing(t *testing.T) {
	alone := loadAll(t, noFile, oneRule, noFile)
	for name, global := range map[string]string{"comments": "# nothing here yet\n", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			got := loadAll(t, global, oneRule, noFile)
			if got.PollInterval != alone.PollInterval || !reflect.DeepEqual(got.Rules, alone.Rules) ||
				!reflect.DeepEqual(got.Board, alone.Board) || len(got.Agents) != len(alone.Agents) {
				t.Errorf("Config = %+v, want %+v", got, alone)
			}
		})
	}
}

// A global path that exists but cannot be read is an error, not a missing
// file.
func TestAGlobalDirectoryIsAnError(t *testing.T) {
	dir := t.TempDir()
	_, err := config.Load(writeFiles(t, oneRule, noFile), dir)
	if err == nil || errors.Is(err, fs.ErrNotExist) || !strings.HasPrefix(err.Error(), "read crew config:") {
		t.Errorf("error = %v, want a read error that is not fs.ErrNotExist", err)
	}
}

func TestTheGlobalFileMustBeYAMLMapping(t *testing.T) {
	for name, global := range map[string]string{"syntax": "rules: [\n", "list": "- rules\n"} {
		t.Run(name, func(t *testing.T) {
			lines, path := loadAllErr(t, global, oneRule, noFile)
			if !strings.Contains(lines[0], path+":") {
				t.Errorf("error = %q, want it to name %s", lines, path)
			}
		})
	}
}

// An error about a key no file sets names the three files.
func TestAMissingKeyNamesAllThreeFiles(t *testing.T) {
	lines, global := loadAllErr(t, "poll_interval_seconds: 60\n", oneAgent, "board:\n")
	want := global + ", " + sharedName + " and " + localName + ": rules: missing; write at least one rule"
	if !reflect.DeepEqual(lines, []string{want}) {
		t.Errorf("error = %q, want %q", lines, want)
	}
}

// The tracker's section decoder names the global file when the section
// came from it.
func TestASectionOfTheGlobalFileNamesIt(t *testing.T) {
	path := writeGlobal(t, "tracker:\n  name: github\n  hots: example.com\n")
	cfg, err := config.Load(writeFiles(t, oneRule, noFile), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var settings struct {
		Host string `yaml:"host"`
	}
	assertErr(t, cfg.TrackerSection(&settings), path+": tracker.hots (line 3): unknown key")
}
