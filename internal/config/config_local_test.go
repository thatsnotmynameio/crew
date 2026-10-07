package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
)

// The two files Load reads, as errors name them.
const (
	sharedName = ".crew/config.yaml"
	localName  = ".crew/config.local.yaml"
)

// writeFiles writes shared as .crew/config.yaml and local as
// .crew/config.local.yaml in a new repository root, leaving out a file
// whose body is noFile, and returns that root.
func writeFiles(t *testing.T, shared, local string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{sharedName: shared, localName: local} {
		if body == noFile {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// noFile stands for a file writeFiles leaves out.
const noFile = "\x00"

func loadFiles(t *testing.T, shared, local string) *config.Config {
	t.Helper()
	cfg, err := config.Load(writeFiles(t, shared, local), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// loadFilesErr loads the two files, expects an error, and returns its lines.
func loadFilesErr(t *testing.T, shared, local string) []string {
	t.Helper()
	cfg, err := config.Load(writeFiles(t, shared, local), "")
	if err == nil {
		t.Fatalf("Load succeeded with %+v, want an error", cfg)
	}
	return strings.Split(err.Error(), "\n")
}

// sixColumns is a board of six columns, each one label.
const sixColumns = `board:
  Ideas: idea
  Ready: ready
  Running: in progress
  Review: ready to review
  Failed: needs attention
  Done: done
`

// Covers AE1: the local file's poll interval and board replace those of
// config.yaml, whose rules stay.
func TestAE1TheLocalFileReplacesItsTopLevelKeys(t *testing.T) {
	cfg := loadFiles(t, "poll_interval_seconds: 300\n"+oneRule, "poll_interval_seconds: 60\n"+sixColumns)
	if cfg.PollInterval != 60*time.Second {
		t.Errorf("PollInterval = %v, want 1m0s", cfg.PollInterval)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Name != "implement" {
		t.Errorf("Rules = %+v, want config.yaml's implement", cfg.Rules)
	}
	if len(cfg.Board) != 6 || !cfg.BoardWritten {
		t.Errorf("Board = %+v (written %v), want the local file's six columns", cfg.Board, cfg.BoardWritten)
	}
	if cfg.MaxParallelIssues != 2 {
		t.Errorf("MaxParallelIssues = %d, want the default 2, which neither file sets", cfg.MaxParallelIssues)
	}
}

// Covers AE2: the local agents replace every agent of config.yaml, so a
// session of config.yaml naming one the local file left out is an error at
// that session, in config.yaml.
func TestAE2TheLocalAgentsReplaceThemAll(t *testing.T) {
	shared := `agents:
  developer:
    harness: {name: claude}
  product-manager:
    harness: {name: claude}
rules:
  refinement:
    labels: {ready: a, running: b}
    actions:
      - agent: product-manager
        name: refine
        prompt: Refine
    routes: {passed: c, failed: d}
`
	local := `agents:
  developer:
    harness: {name: codex}
`
	lines := loadFilesErr(t, shared, local)
	want := sharedName + ": rules.refinement.actions[0].agent (line 10): "
	if len(lines) != 1 || !strings.HasPrefix(lines[0], want) {
		t.Errorf("error = %q, want one line starting %q", lines, want)
	}
	cfg := loadFiles(t, strings.Replace(shared, "agent: product-manager", "agent: developer", 1), local)
	if len(cfg.Agents) != 1 || cfg.Agents[0].Name != "developer" || cfg.Agents[0].Harness != "codex" {
		t.Errorf("Agents = %+v, want only the local developer on codex", cfg.Agents)
	}
}

// Covers AE3: a repository with only config.local.yaml runs from it.
func TestAE3TheLocalFileAloneIsAConfig(t *testing.T) {
	cfg := loadFiles(t, noFile, oneRule)
	if len(cfg.Rules) != 1 || cfg.Rules[0].Name != "implement" {
		t.Errorf("Rules = %+v, want the local file's implement", cfg.Rules)
	}
}

// Covers AE5: an error about a key of the local file names that file, the
// key and its line.
func TestAE5AnErrorNamesTheFileOfItsKey(t *testing.T) {
	lines := loadFilesErr(t, oneRule, "# my settings\npoll_interval_seconds: 60\nboard_columns: 3\n")
	if want := localName + ": board_columns (line 3): unknown key"; !reflect.DeepEqual(lines, []string{want}) {
		t.Errorf("error = %q, want %q", lines, want)
	}
}

// Covers AE7: a local file of comments only, or an empty one, changes
// nothing.
func TestAE7AnEmptyLocalFileChangesNothing(t *testing.T) {
	alone := loadFiles(t, oneRule, noFile)
	for name, local := range map[string]string{"comments": "# nothing here yet\n", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			got := loadFiles(t, oneRule, local)
			if got.PollInterval != alone.PollInterval || !reflect.DeepEqual(got.Rules, alone.Rules) ||
				!reflect.DeepEqual(got.Board, alone.Board) || len(got.Agents) != len(alone.Agents) {
				t.Errorf("Config = %+v, want %+v", got, alone)
			}
		})
	}
}

// A local key with no value replaces the key with nothing, which brings
// back its default.
func TestAnEmptyLocalKeyBringsBackTheDefault(t *testing.T) {
	cfg := loadFiles(t, oneRule+sixColumns, "board:\n")
	if cfg.BoardWritten || len(cfg.Board) != 1 {
		t.Errorf("Board = %+v (written %v), want the default board of one column", cfg.Board, cfg.BoardWritten)
	}
}

func TestErrorsOfBothFilesEachNameTheirFile(t *testing.T) {
	lines := loadFilesErr(t, "max_parallel_issues: 0\n"+oneRule, "poll_interval_seconds: -1\n")
	want := []string{
		sharedName + ": max_parallel_issues (line 1): must be a positive number of issues",
		localName + ": poll_interval_seconds (line 1): must be a positive number of seconds",
	}
	for _, w := range want {
		if !slices.Contains(lines, w) {
			t.Errorf("error lines %q lack %q", lines, w)
		}
	}
}

// A top-level key may hold dots, so an error takes the file of the whole
// top-level key, not of its first segment.
func TestADottedTopLevelKeyNamesItsOwnFile(t *testing.T) {
	lines := loadFilesErr(t, oneRule, "agents.claude.harness.model: x\n")
	want := localName + ": agents.claude.harness.model (line 1): unknown key"
	if !reflect.DeepEqual(lines, []string{want}) {
		t.Errorf("error = %q, want %q", lines, want)
	}
}

// An error about a key neither file sets names both files.
func TestAMissingKeyNamesEveryFile(t *testing.T) {
	lines := loadFilesErr(t, oneAgent, "poll_interval_seconds: 60\n")
	want := sharedName + " and " + localName + ": rules: missing; write at least one rule"
	if !reflect.DeepEqual(lines, []string{want}) {
		t.Errorf("error = %q, want %q", lines, want)
	}
}

func TestTheLocalFileMustBeYAMLMapping(t *testing.T) {
	for name, local := range map[string]string{"syntax": "rules: [\n", "list": "- rules\n"} {
		t.Run(name, func(t *testing.T) {
			lines := loadFilesErr(t, oneRule, local)
			if !strings.Contains(lines[0], localName+":") {
				t.Errorf("error = %q, want it to name %s", lines, localName)
			}
		})
	}
}

// The section decoders adapters receive name the file of their section.
func TestSectionDecodersNameTheirFile(t *testing.T) {
	shared := "agents:\n  claude:\n    harness: {name: claude, model: x}\n" + ruleOnly
	cfg := loadFiles(t, shared, "tracker:\n  name: github\n  hots: example.com\n")
	var settings struct {
		Host string `yaml:"host"`
	}
	assertErr(t, cfg.TrackerSection(&settings), localName+": tracker.hots (line 3): unknown key")
	var harness struct{}
	assertErr(t, cfg.Agents[0].HarnessSection(&harness), sharedName+": agents.claude.harness.model (line 3): unknown key")
}
