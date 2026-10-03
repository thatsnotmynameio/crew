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
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
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
			Name: "implement", Label: "ready", MovesTo: "in progress",
			OnSuccess: "ready to review", OnFailure: "needs attention",
			Queue: crew.Queue{Name: "default", Slots: 1},
			Actions: []crew.Action{
				{Name: "acceptance", Prompt: "Implement test acceptance for issue {{.Issue.Ref}}"},
				{Name: "development", Prompt: "Implement development for issue {{.Issue.Ref}}"},
			},
		},
		{
			Name: "review", Label: "ready to review", MovesTo: "in review",
			OnSuccess: "ready to merge", OnFailure: "needs attention",
			Queue: crew.Queue{Name: "default", Slots: 1},
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
		Host string `yaml:"host"`
	}{Host: "github.com"}
	if err := cfg.TrackerSection(&tracker); err != nil {
		t.Fatalf("TrackerSection: %v", err)
	}
	if tracker.Host != "github.com" {
		t.Errorf("tracker host = %q, want the target's own default", tracker.Host)
	}
}

func TestLoadReadsExtraLabels(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []crew.State
	}{
		{name: "left out", body: oneStage, want: nil},
		{
			name: "a label with a description and a template",
			body: `extra_labels:
  - label: "crew:waiting brainstorm"
    description: Ideas to brainstorm later
    issue_template: idea.md
  - label: crew:parked
` + oneStage,
			want: []crew.State{"crew:waiting brainstorm", "crew:parked"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := load(t, tt.body).Extras; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Extras = %q, want %q", got, tt.want)
			}
		})
	}
}

// crew runs none of the prompts under prompts:, skills do, so a config with
// them loads like one without.
func TestLoadAcceptsPrompts(t *testing.T) {
	body := `prompts:
  brainstorm: |-
    Brainstorm {{.Issue.Ref}} ({{.Issue.URL}}): {{.Issue.Title}}, issue {{.Issue.Key}}.
  triage: "Look at {{.Issue.Ref}}"
` + oneStage
	got, want := load(t, body), load(t, oneStage)
	if !reflect.DeepEqual(got.Workflow, want.Workflow) || !reflect.DeepEqual(got.Extras, want.Extras) {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestLoadReadsUsageInStatus(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "left out", body: oneStage, want: false},
		{name: "on", body: "config:\n  usage_in_status: true\n" + oneStage, want: true},
		{name: "off", body: "config:\n  usage_in_status: false\n" + oneStage, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := load(t, tt.body).UsageInStatus; got != tt.want {
				t.Errorf("UsageInStatus = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadReadsTheRunTimeLimit(t *testing.T) {
	tests := []struct {
		name string
		body string
		want time.Duration
	}{
		{name: "left out", body: oneStage, want: 0},
		{name: "eight hours", body: "config:\n  run_time_limit_seconds: 28800\n" + oneStage, want: 8 * time.Hour},
		{name: "empty", body: "config:\n  run_time_limit_seconds:\n" + oneStage, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := load(t, tt.body).RunTimeLimit; got != tt.want {
				t.Errorf("RunTimeLimit = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadReadsActionCheck(t *testing.T) {
	stage := func(check string) string {
		return `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
` + check
	}
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "left out", body: oneStage, want: ""},
		{name: "a command", body: stage("        check: \"test -n \\\"$CREW_BRANCH\\\"\"\n"), want: `test -n "$CREW_BRANCH"`},
		// A check is a shell command, never a template: braces stay as written.
		{
			name: "not a template",
			body: stage("        check: \"echo '{{.Issue.Title}}'\"\n"),
			want: "echo '{{.Issue.Title}}'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := load(t, tt.body).Workflow[0].Actions[0].Check; got != tt.want {
				t.Errorf("Check = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadRendersPromptForIssue(t *testing.T) {
	cfg := load(t, oneStage)
	if len(cfg.Workflow) != 1 || len(cfg.Workflow[0].Actions) != 1 {
		t.Fatalf("Workflow = %+v, want one stage with one action", cfg.Workflow)
	}
	issue := crew.Issue{Key: "42", Ref: "#42", Title: "Fix it", URL: "https://example.com/42"}
	got, err := cfg.Workflow[0].Actions[0].Render(issue)
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
	// The stages that open a pull request check that it exists; the others
	// have no check (R15).
	for _, s := range cfg.Workflow {
		for _, a := range s.Actions {
			want := s.Name == "development" || s.Name == "fix"
			if got := a.Check != ""; got != want {
				t.Errorf("%s/%s has a check: %v, want %v", s.Name, a.Name, got, want)
			}
		}
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
      stat: in progress
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
