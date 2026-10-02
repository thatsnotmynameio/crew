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
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// writeRoot writes body as .crew/config.yaml in a new repository root and
// returns that root.
func writeRoot(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func load(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.Load(writeRoot(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// loadErr loads body, expects an error, and checks the message holds each of
// wants.
func loadErr(t *testing.T, body string, wants ...string) {
	t.Helper()
	cfg, err := config.Load(writeRoot(t, body))
	if err == nil {
		t.Fatalf("Load succeeded with %+v, want an error", cfg)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// oneStage is a minimal valid workflow, for tests about other keys.
const oneStage = `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`

func TestLoadDraftConfig(t *testing.T) {
	cfg, err := config.Load("testdata/draft")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PollInterval != 300*time.Second {
		t.Errorf("PollInterval = %v, want 5m0s", cfg.PollInterval)
	}
	if cfg.MaxParallelIssues != 2 {
		t.Errorf("MaxParallelIssues = %d, want 2", cfg.MaxParallelIssues)
	}
	if cfg.Harness != "claude" {
		t.Errorf("Harness = %q, want claude", cfg.Harness)
	}
	if cfg.Tracker != "github" {
		t.Errorf("Tracker = %q, want github", cfg.Tracker)
	}
	wantWorkflow := []crew.Stage{
		{
			Name: "implement", Label: "ready", MovesTo: "in_progress", OnSuccess: "ready_to_review",
			Actions: []crew.Action{
				{Name: "acceptance", Prompt: "Implement test acceptance for issue {{.Issue.Ref}}"},
				{Name: "development", Prompt: "Implement development for issue {{.Issue.Ref}}"},
			},
		},
		{
			Name: "review", Label: "ready_to_review", MovesTo: "in_review", OnSuccess: "ready_to_merge",
			Actions: []crew.Action{
				{Name: "custom_review", Prompt: "Review implementation for issue {{.Issue.Ref}}"},
			},
		},
	}
	if !reflect.DeepEqual(cfg.Workflow, wantWorkflow) {
		t.Errorf("Workflow = %+v\nwant %+v", cfg.Workflow, wantWorkflow)
	}

	var harness struct {
		Model string `yaml:"model"`
	}
	if err := cfg.HarnessSection(&harness); err != nil {
		t.Fatalf("HarnessSection: %v", err)
	}
	if harness.Model != "claude-opus-5-5" {
		t.Errorf("harness model = %q, want claude-opus-5-5", harness.Model)
	}

	var tracker struct {
		Labels map[string]string `yaml:"labels"`
	}
	if err := cfg.TrackerSection(&tracker); err != nil {
		t.Fatalf("TrackerSection: %v", err)
	}
	wantLabels := map[string]string{
		"ready": "ready", "in_progress": "in progress", "ready_to_review": "ready to review",
		"in_review": "in review", "needs_attention": "needs attention", "paused": "paused",
		"ready_to_merge": "ready to merge", "done": "done",
	}
	if !reflect.DeepEqual(tracker.Labels, wantLabels) {
		t.Errorf("tracker labels = %v, want %v", tracker.Labels, wantLabels)
	}
}

func TestLoadReadsEngineSettingsAsWritten(t *testing.T) {
	cfg := load(t, `config:
  poll_interval_seconds: 60
  max_parallel_issues: 5
  harness: codex
tracker:
  name: jira
`+oneStage)
	if cfg.PollInterval != time.Minute || cfg.MaxParallelIssues != 5 || cfg.Harness != "codex" || cfg.Tracker != "jira" {
		t.Errorf("got poll %v, parallel %d, harness %q, tracker %q; want 1m0s, 5, codex, jira",
			cfg.PollInterval, cfg.MaxParallelIssues, cfg.Harness, cfg.Tracker)
	}
}

func TestLoadAppliesEngineDefaults(t *testing.T) {
	cfg := load(t, oneStage)
	if cfg.PollInterval != 300*time.Second {
		t.Errorf("PollInterval = %v, want 5m0s", cfg.PollInterval)
	}
	if cfg.MaxParallelIssues != 2 {
		t.Errorf("MaxParallelIssues = %d, want 2", cfg.MaxParallelIssues)
	}
	if cfg.Harness != "claude" {
		t.Errorf("Harness = %q, want claude", cfg.Harness)
	}
	if cfg.Tracker != "github" {
		t.Errorf("Tracker = %q, want github", cfg.Tracker)
	}

	// The model's default belongs to the harness adapter, so an omitted model
	// reaches the harness section unset and the target keeps its own value.
	harness := struct {
		Model string `yaml:"model"`
	}{Model: "adapter default"}
	if err := cfg.HarnessSection(&harness); err != nil {
		t.Fatalf("HarnessSection: %v", err)
	}
	if harness.Model != "adapter default" {
		t.Errorf("harness model = %q, want the target's own default", harness.Model)
	}
	tracker := struct {
		Labels map[string]string `yaml:"labels"`
	}{Labels: map[string]string{"ready": "todo"}}
	if err := cfg.TrackerSection(&tracker); err != nil {
		t.Fatalf("TrackerSection: %v", err)
	}
	if !reflect.DeepEqual(tracker.Labels, map[string]string{"ready": "todo"}) {
		t.Errorf("tracker labels = %v, want the target's own default", tracker.Labels)
	}
}

func TestLoadRendersPromptForIssue(t *testing.T) {
	cfg := load(t, oneStage)
	if len(cfg.Workflow) != 1 || len(cfg.Workflow[0].Actions) != 1 {
		t.Fatalf("Workflow = %+v, want one stage with one action", cfg.Workflow)
	}
	got, err := cfg.Workflow[0].Actions[0].Render(crew.Issue{Key: "42", Ref: "#42", Title: "Fix it", URL: "https://example.com/42"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "Implement #42" {
		t.Errorf("Render = %q, want %q", got, "Implement #42")
	}
}

func TestLoadMissingFileSaysWhereItLooked(t *testing.T) {
	root := t.TempDir()
	_, err := config.Load(root)
	if err == nil {
		t.Fatal("Load succeeded without a config file")
	}
	if want := filepath.Join(root, ".crew", "config.yaml"); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name %s", err, want)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v is not fs.ErrNotExist", err)
	}
	if want := "docs/guide/crew.mdx"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not point to the guide (%s) for creating one", err, want)
	}
}

// crew runs on its own repository, so its .crew/config.yaml must stay valid.
func TestTheRepositorysOwnConfigLoads(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("Load(repository root) = %v", err)
	}
	if got := len(cfg.Workflow); got != 2 {
		t.Errorf("workflow has %d stages, want 2 (implement, review)", got)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		wants []string
	}{
		{
			name: "unknown key",
			body: `config:
  pol_interval_seconds: 60
` + oneStage,
			wants: []string{"config.pol_interval_seconds", "line 2", "unknown key"},
		},
		{
			name: "unknown top-level key",
			body: `trackers:
  name: github
` + oneStage,
			wants: []string{"trackers", "line 1", "unknown key"},
		},
		{
			name: "wrong type",
			body: `config:
  poll_interval_seconds: soon
` + oneStage,
			wants: []string{"config.poll_interval_seconds", "line 2", "soon"},
		},
		{
			name: "duplicate key",
			body: `config:
  max_parallel_issues: 2
  max_parallel_issues: 3
` + oneStage,
			wants: []string{"config.max_parallel_issues", "line 3", "duplicate", "line 2"},
		},
		{
			name: "poll interval not positive",
			body: `config:
  poll_interval_seconds: 0
` + oneStage,
			wants: []string{"config.poll_interval_seconds", "line 2", "positive"},
		},
		{
			name: "parallel issues not positive",
			body: `config:
  max_parallel_issues: -1
` + oneStage,
			wants: []string{"config.max_parallel_issues", "line 2", "positive"},
		},
		{
			name: "model under harness",
			body: `harness:
  model: claude-opus-5-5
` + oneStage,
			wants: []string{"harness.model", "line 2", "config.model"},
		},
		{
			name:  "no workflow",
			body:  "config:\n  max_parallel_issues: 2\n",
			wants: []string{"workflow", "at least one stage"},
		},
		{
			name: "label outside the eight states",
			body: `workflow:
  - name: implement
    label: redy
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{
				"workflow[0].label", "line 3", `"redy"`,
				"ready", "in_progress", "ready_to_review", "in_review",
				"needs_attention", "paused", "ready_to_merge", "done",
			},
		},
		{
			name: "moves_to outside the eight states",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: doing
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].moves_to", "line 4", `"doing"`},
		},
		{
			// Covers AE7.
			name: "on_success equals the stage's own label",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].on_success", "line 5", "label"},
		},
		{
			name: "two stages share a label",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: triage
    label: ready
    moves_to: in_review
    on_success: paused
    actions:
      - name: sort
        prompt: "Triage {{.Issue.Ref}}"
`,
			wants: []string{"workflow[1].label", "line 10", `"implement"`, `"triage"`},
		},
		{
			name: "moves_to is another stage's label",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: ready_to_review
    on_success: done
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: review
    label: ready_to_review
    moves_to: in_review
    on_success: ready_to_merge
    actions:
      - name: custom_review
        prompt: "Review {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].moves_to", "line 4", `"review"`},
		},
		{
			name: "moves_to is its own stage's label",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: ready
    on_success: done
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].moves_to", "line 4", `"implement"`},
		},
		{
			name: "prompt references an unknown issue field",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: acceptance
        prompt: "Implement {{.Issue.Ref}}"
      - name: development
        prompt: "Implement {{.Issue.Numbr}}"
`,
			wants: []string{"workflow[0].actions[1].prompt", "line 10", `"development"`, "Numbr"},
		},
		{
			name: "prompt does not parse",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref"
`,
			wants: []string{"workflow[0].actions[0].prompt", "line 8", `"development"`},
		},
		{
			name: "stage without on_success",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].on_success", "line 2", "required"},
		},
		{
			name: "stage without actions",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
`,
			wants: []string{"workflow[0].actions", "line 2", "at least one action"},
		},
		{
			name: "action without prompt",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
`,
			wants: []string{"workflow[0].actions[0].prompt", "line 7", "required"},
		},
		{
			name: "two stages share a name",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: implement
    label: ready_to_review
    moves_to: in_review
    on_success: ready_to_merge
    actions:
      - name: development
        prompt: "Review {{.Issue.Ref}}"
`,
			wants: []string{"workflow[1].name", "line 9", "workflow[0]"},
		},
		{
			name: "two actions of a stage share a name",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
      - name: development
        prompt: "Implement again {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].actions[1].name", "line 9", "actions[0]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loadErr(t, tt.body, tt.wants...)
		})
	}
}

// The section decoders are what adapter factories receive: they reject
// unknown keys at any depth, naming the key path and its line.
func TestSectionDecodersAreStrict(t *testing.T) {
	cfg := load(t, `config:
  model: claude-opus-5-5
harness:
  effort: high
tracker:
  name: github
  lables:
    ready: todo
  columns:
    - name: Todo
      state: ready
    - name: Doing
      stat: in_progress
`+oneStage)

	var harness struct {
		Model  string `yaml:"model"`
		Effort string `yaml:"effort"`
	}
	if err := cfg.HarnessSection(&harness); err != nil {
		t.Fatalf("HarnessSection: %v", err)
	}
	if harness.Model != "claude-opus-5-5" || harness.Effort != "high" {
		t.Errorf("harness = %+v, want model claude-opus-5-5 and effort high", harness)
	}

	var noEffort struct {
		Model string `yaml:"model"`
	}
	assertErr(t, cfg.HarnessSection(&noEffort), "harness.effort", "line 4", "unknown key")

	var labelsTypo struct {
		Labels  map[string]string `yaml:"labels"`
		Columns []struct {
			Name  string `yaml:"name"`
			State string `yaml:"state"`
		} `yaml:"columns"`
	}
	assertErr(t, cfg.TrackerSection(&labelsTypo), "tracker.lables", "line 7", "unknown key")

	var nestedTypo struct {
		Lables  map[string]string `yaml:"lables"`
		Columns []struct {
			Name  string `yaml:"name"`
			State string `yaml:"state"`
		} `yaml:"columns"`
	}
	assertErr(t, cfg.TrackerSection(&nestedTypo), "tracker.columns[1].stat", "line 13", "unknown key")
}

func TestSectionDecoderNamesNestedTypeErrors(t *testing.T) {
	cfg := load(t, `tracker:
  limits:
    per_poll: many
`+oneStage)
	var tracker struct {
		Limits struct {
			PerPoll int `yaml:"per_poll"`
		} `yaml:"limits"`
	}
	assertErr(t, cfg.TrackerSection(&tracker), "tracker.limits.per_poll", "line 3", "many")
}

func assertErr(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", wants)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
