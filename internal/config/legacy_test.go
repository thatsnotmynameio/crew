package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
)

// errLines returns the lines of err, without the file name Load puts first.
func errLines(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		t.Fatal("Load succeeded, want an error")
	}
	_, msg, ok := strings.Cut(err.Error(), filepath.Join(".crew", "config.yaml")+": ")
	if !ok {
		t.Fatalf("error %q does not name the file", err)
	}
	return strings.Split(msg, "\n")
}

// oldKeyLines loads body and returns the lines of the error it must fail
// with.
func oldKeyLines(t *testing.T, body string) []string {
	t.Helper()
	_, err := config.Load(writeRoot(t, body))
	return errLines(t, err)
}

// Covers AE1: every old key is reported at once, each on its line with what
// replaced it.
func TestAE1OldKeysAreRefusedWithTheirReplacements(t *testing.T) {
	body := `config:
  poll_interval_seconds: 60
workflow:
  - moves_to: in progress
`
	want := []string{
		"config.poll_interval_seconds (line 2): now poll_interval_seconds",
		"workflow (line 3): now rules, which maps each rule's name to the rule",
		"workflow[0].moves_to (line 4): now labels.running of the rule",
	}
	if got := oldKeyLines(t, body); !reflect.DeepEqual(got, want) {
		t.Errorf("errors =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The replacement of each old key, as the error says it.
const (
	ruleNameGone   = "gone; a rule's name is its key under rules"
	notRead        = "gone; crew does not read it"
	onBoardGone    = "gone; board lists the board's columns, and notify says whether the rule sends notifications"
	actionNameGone = "gone; an action's name is its key under its rule's actions"
	actionMate     = "now agents.<name>.bot of the action's agent"
	workflowNow    = "workflow (line 1): now rules, which maps each rule's name to the rule"
)

// oldKeyCases are configs with one old key each, and the errors they must
// fail with, exactly.
var oldKeyCases = []struct {
	name string
	body string
	want []string
}{
	{"poll_interval_seconds", "config:\n  poll_interval_seconds: 60\n",
		[]string{"config.poll_interval_seconds (line 2): now poll_interval_seconds"}},
	{"max_parallel_issues", "config:\n  max_parallel_issues: 4\n",
		[]string{"config.max_parallel_issues (line 2): now max_parallel_issues"}},
	{"run_time_limit_seconds", "config:\n  run_time_limit_seconds: 60\n",
		[]string{"config.run_time_limit_seconds (line 2): now run_time_limit_seconds"}},
	{"usage_in_status", "config:\n  usage_in_status: true\n",
		[]string{"config.usage_in_status (line 2): now usage_in_status"}},
	{"queues", "config:\n  queues:\n    developer: 2\n",
		[]string{"config.queues (line 2): now queues"}},
	{"clerk_slots", "config:\n  clerk_slots: 1\n", []string{"config.clerk_slots (line 2): " +
		"gone; crew has no clerk queue of its own: declare one under queues and name it in its rules' queue"}},
	{"config.harness", "config:\n  harness: claude\n",
		[]string{"config.harness (line 2): now agents.<name>.harness.name"}},
	{"model", "config:\n  model: claude-opus-5-5\n",
		[]string{"config.model (line 2): now agents.<name>.harness.model"}},
	{"mate", "config:\n  mate: clerk\n", []string{"config.mate (line 2): now tracker.bot"}},
	{"config without a key it had", "config: {}\n",
		[]string{"config (line 1): gone; its keys are now at the top level"}},
	{"harness", "harness:\n  effort: high\n",
		[]string{"harness (line 1): now agents.<name>.harness, beside the harness's name"}},
	{"extra_labels", "extra_labels:\n  - label: parked\n    description: parked work\n",
		[]string{"extra_labels (line 1): gone; crew acts only on its rules' labels and leaves every other label alone"}},
	{"prompts", "prompts:\n  brainstorm: Brainstorm {{.Issue.Ref}}\n",
		[]string{"prompts (line 1): gone; crew never ran them, so keep them in the skills that do"}},
	{"workflow", "workflow: []\n", []string{workflowNow}},
	{"workflow not a list", "workflow:\n  implement: {label: ready}\n", []string{workflowNow}},
	{"stage name", "workflow:\n  - name: implement\n",
		[]string{workflowNow, "workflow[0].name (line 2): " + ruleNameGone}},
	{"stage description", "workflow:\n  - description: a feature\n",
		[]string{workflowNow, "workflow[0].description (line 2): " + notRead}},
	{"stage issue_template", "workflow:\n  - issue_template: feature.md\n",
		[]string{workflowNow, "workflow[0].issue_template (line 2): " + notRead}},
	{"label", "workflow:\n  - label: ready\n",
		[]string{workflowNow, "workflow[0].label (line 2): now labels.ready of the rule"}},
	{"moves_to", "workflow:\n  - moves_to: in progress\n",
		[]string{workflowNow, "workflow[0].moves_to (line 2): now labels.running of the rule"}},
	{"on_success", "workflow:\n  - on_success: done\n",
		[]string{workflowNow, "workflow[0].on_success (line 2): now labels.success of the rule"}},
	{"on_failure", "workflow:\n  - on_failure: failed\n",
		[]string{workflowNow, "workflow[0].on_failure (line 2): now labels.failure of the rule"}},
	{"on_board", "workflow:\n  - on_board: false\n",
		[]string{workflowNow, "workflow[0].on_board (line 2): " + onBoardGone}},
	{"action name", "workflow:\n  - actions:\n      - name: lfg\n",
		[]string{workflowNow, "workflow[0].actions[0].name (line 3): " + actionNameGone}},
	{"action mate", "workflow:\n  - actions:\n      - mate: developer\n",
		[]string{workflowNow, "workflow[0].actions[0].mate (line 3): " + actionMate}},
}

func TestLoadRefusesEachOldKey(t *testing.T) {
	for _, tt := range oldKeyCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := oldKeyLines(t, tt.body); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("errors =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// An old key written inside a new rule or action gets its replacement, not
// a bare unknown key.
func TestLoadRefusesOldKeysInsideARule(t *testing.T) {
	body := strings.Replace(ruleWith("label: ready"), "        prompt:", "        mate: developer\n        prompt:", 1)
	want := []string{
		"rules.implement.label (line 6): now labels.ready of the rule",
		"rules.implement.actions.development.mate (line 14): " + actionMate,
	}
	if got := oldKeyLines(t, body); !reflect.DeepEqual(got, want) {
		t.Errorf("errors =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, key := range []string{"name: implement", "moves_to: a", "on_success: a", "on_failure: a",
		"on_board: false", "description: a", "issue_template: a.md"} {
		old, _, _ := strings.Cut(key, ":")
		loadErr(t, ruleWith(key), "rules.implement."+old+" (line 6): ")
	}
	loadErr(t, strings.Replace(oneRule, "        prompt:", "        name: lfg\n        prompt:", 1),
		"rules.implement.actions.development.name (line 13): "+actionNameGone)
}

// Keys of the new schema named like old ones, such as a check named config
// or a board column named label, are left to the decoder.
func TestLoadLeavesNewKeysNamedLikeOldOnes(t *testing.T) {
	body := oneRule + `checks:
  config: "true"
board:
  label: [ready]
`
	cfg := load(t, body)
	if len(cfg.Board) != 1 || cfg.Board[0].Name != "label" {
		t.Errorf("board = %+v, want one column, label", cfg.Board)
	}
}

// oldExample is today's example config before the new keys, kept to prove
// that a copy of it is refused.
const oldExample = "testdata/old"

// Today's example config is refused with every old key it uses, each once
// and on its line.
func TestTheOldExampleConfigIsRefusedWithEveryOldKey(t *testing.T) {
	_, err := config.Load(oldExample)
	got := errLines(t, err)
	want := []string{
		"config.poll_interval_seconds", "config.max_parallel_issues", "config.clerk_slots", "config.queues",
		"config.harness", "config.model", "config.mate", "extra_labels", "prompts", "workflow",
	}
	stageKeys := []string{"name", "description", "issue_template", "label", "moves_to", "on_success", "on_failure"}
	stages := []struct{ offBoard, bot bool }{{true, false}, {false, true}, {true, false}, {false, true}, {false, true}}
	for i, s := range stages {
		stage := fmt.Sprintf("workflow[%d].", i)
		for _, key := range stageKeys {
			want = append(want, stage+key)
		}
		if s.offBoard {
			want = append(want, stage+"on_board")
		}
		want = append(want, stage+"actions[0].name")
		if s.bot {
			want = append(want, stage+"actions[0].mate")
		}
	}
	data, readErr := os.ReadFile(filepath.Join(oldExample, ".crew", "config.yaml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	paths := oldKeyPaths(t, got, strings.Split(string(data), "\n"))
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("old keys =\n%s\nwant\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}

// oldKeyLine is one error about an old key: its path, line and replacement.
var oldKeyLine = regexp.MustCompile(`^(\S+) \(line (\d+)\): (now|gone; ).+$`)

// oldKeyPaths returns the path of each error line, checking that each is an
// old key's error and that its line of file holds that key.
func oldKeyPaths(t *testing.T, lines, file []string) []string {
	t.Helper()
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		m := oldKeyLine.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("error line %q is not about an old key", line)
			continue
		}
		n, _ := strconv.Atoi(m[2])
		key := m[1][strings.LastIndexAny(m[1], ".]")+1:]
		if n < 1 || n > len(file) || !strings.Contains(file[n-1], key+":") {
			t.Errorf("%s: line %d of the file does not hold %s", line, n, key)
		}
		paths = append(paths, m[1])
	}
	return paths
}
