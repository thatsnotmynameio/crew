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
			Name: "implement", Label: "ready", MovesTo: "in progress", OnSuccess: "ready to review", OnFailure: "needs attention",
			Actions: []crew.Action{
				{Name: "acceptance", Prompt: "Implement test acceptance for issue {{.Issue.Ref}}"},
				{Name: "development", Prompt: "Implement development for issue {{.Issue.Ref}}"},
			},
		},
		{
			Name: "review", Label: "ready to review", MovesTo: "in review", OnSuccess: "ready to merge", OnFailure: "needs attention",
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

func TestLoadAcceptsWorkflowLabels(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []crew.Stage
	}{
		{
			name: "any label text, kept as written",
			body: `workflow:
  - name: implement
    label: Ready For Work
    moves_to: crew is on it
    on_success: In Review
    on_failure: Needs Attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			want: []crew.Stage{
				{Name: "implement", Label: "Ready For Work", MovesTo: "crew is on it", OnSuccess: "In Review", OnFailure: "Needs Attention"},
			},
		},
		{
			// Covers AE5: a failed review goes back to implement.
			name: "on_failure is another stage's label",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: review
    label: ready to review
    moves_to: in review
    on_success: ready to merge
    on_failure: ready
    actions:
      - name: custom_review
        prompt: "Review {{.Issue.Ref}}"
`,
			want: []crew.Stage{
				{Name: "implement", Label: "ready", MovesTo: "in progress", OnSuccess: "ready to review", OnFailure: "needs attention"},
				{Name: "review", Label: "ready to review", MovesTo: "in review", OnSuccess: "ready to merge", OnFailure: "ready"},
			},
		},
		{
			// A label is spelled everywhere as it is first written.
			name: "a label written in two cases takes its first spelling",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: In Progress
    on_success: ready to review
    on_failure: Needs Attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: review
    label: Ready To Review
    moves_to: in review
    on_success: in progress
    on_failure: needs attention
    actions:
      - name: custom_review
        prompt: "Review {{.Issue.Ref}}"
`,
			want: []crew.Stage{
				{Name: "implement", Label: "ready", MovesTo: "In Progress", OnSuccess: "ready to review", OnFailure: "Needs Attention"},
				{Name: "review", Label: "ready to review", MovesTo: "in review", OnSuccess: "In Progress", OnFailure: "Needs Attention"},
			},
		},
		{
			// description and issue_template are for the skill, not the stage.
			name: "description and issue_template leave the stage as without them",
			body: `workflow:
  - name: fix
    description: Bugs to fix
    issue_template: bug.md
    label: ready for fix
    moves_to: fixing
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Fix {{.Issue.Ref}}"
`,
			want: []crew.Stage{
				{Name: "fix", Label: "ready for fix", MovesTo: "fixing", OnSuccess: "ready to review", OnFailure: "needs attention"},
			},
		},
		{
			// The issue stays in moves_to when the stage fails.
			name: "on_failure equals the stage's own moves_to",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: in review
    on_failure: in progress
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			want: []crew.Stage{
				{Name: "implement", Label: "ready", MovesTo: "in progress", OnSuccess: "in review", OnFailure: "in progress"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.body)
			for i := range cfg.Workflow {
				cfg.Workflow[i].Actions = nil
			}
			if !reflect.DeepEqual(cfg.Workflow, tt.want) {
				t.Errorf("Workflow = %+v\nwant %+v", cfg.Workflow, tt.want)
			}
		})
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
	if _, err := config.Load(filepath.Join("..", "..")); err != nil {
		t.Fatalf("Load(repository root) = %v", err)
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
			name: "run time limit zero",
			body: `config:
  run_time_limit_seconds: 0
` + oneStage,
			wants: []string{"config.run_time_limit_seconds", "line 2", "positive"},
		},
		{
			name: "run time limit negative",
			body: `config:
  run_time_limit_seconds: -5
` + oneStage,
			wants: []string{"config.run_time_limit_seconds", "line 2", "positive"},
		},
		{
			name: "run time limit as a duration",
			body: `config:
  run_time_limit_seconds: "8h"
` + oneStage,
			wants: []string{"config.run_time_limit_seconds", "line 2", "8h"},
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
			// Covers AE2.
			name: "stage without on_failure",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: in review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].on_failure", "line 2", "required"},
		},
		{
			name: "empty on_failure",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: in review
    on_failure: ""
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].on_failure", "required"},
		},
		{
			// Covers AE5.
			name: "on_failure equals the stage's own label",
			body: `workflow:
  - name: review
    label: ready to review
    moves_to: in review
    on_success: ready to merge
    on_failure: ready to review
    actions:
      - name: custom_review
        prompt: "Review {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].on_failure", "line 6", `"review"`, "label"},
		},
		{
			// Labels are compared ignoring case, as GitHub does.
			name: "on_failure is the stage's own label in another case",
			body: `workflow:
  - name: review
    label: ready to review
    moves_to: in review
    on_success: ready to merge
    on_failure: Ready to Review
    actions:
      - name: custom_review
        prompt: "Review {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].on_failure", "line 6", `"review"`, "label"},
		},
		{
			// Covers AE4.
			name: "two stages take the same label in different cases",
			body: `workflow:
  - name: implement
    label: Ready
    moves_to: in progress
    on_success: in review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: triage
    label: ready
    moves_to: sorting
    on_success: sorted
    on_failure: needs attention
    actions:
      - name: sort
        prompt: "Triage {{.Issue.Ref}}"
`,
			wants: []string{"workflow[1].label", "line 11", `"implement"`, `"triage"`, "same label"},
		},
		{
			name: "moves_to is another stage's label in another case",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: In Review
    on_success: done
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: review
    label: in review
    moves_to: reviewing
    on_success: ready to merge
    on_failure: needs attention
    actions:
      - name: custom_review
        prompt: "Review {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].moves_to", "line 4", `"review"`},
		},
		{
			// Covers AE7.
			name: "on_success equals the stage's own label",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready
    on_failure: needs attention
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
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: triage
    label: ready
    moves_to: in review
    on_success: paused
    on_failure: needs attention
    actions:
      - name: sort
        prompt: "Triage {{.Issue.Ref}}"
`,
			wants: []string{"workflow[1].label", "line 11", `"implement"`, `"triage"`},
		},
		{
			name: "moves_to is another stage's label",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: ready to review
    on_success: done
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: review
    label: ready to review
    moves_to: in review
    on_success: ready to merge
    on_failure: needs attention
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
    on_failure: needs attention
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
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: acceptance
        prompt: "Implement {{.Issue.Ref}}"
      - name: development
        prompt: "Implement {{.Issue.Numbr}}"
`,
			wants: []string{"workflow[0].actions[1].prompt", "line 11", `"development"`, "Numbr"},
		},
		{
			name: "prompt does not parse",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref"
`,
			wants: []string{"workflow[0].actions[0].prompt", "line 9", `"development"`},
		},
		{
			name: "stage without on_success",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_failure: needs attention
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
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
`,
			wants: []string{"workflow[0].actions", "line 2", "at least one action"},
		},
		{
			name: "action without prompt",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
`,
			wants: []string{"workflow[0].actions[0].prompt", "line 8", "required"},
		},
		{
			name: "two stages share a name",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: implement
    label: ready to review
    moves_to: in review
    on_success: ready to merge
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Review {{.Issue.Ref}}"
`,
			wants: []string{"workflow[1].name", "line 10", "workflow[0]"},
		},
		{
			// Covers AE7 of the create-issue plan.
			name: "an extra is a stage's on_failure",
			body: `extra_labels:
  - label: crew:failed
workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: crew:failed
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"extra_labels[0].label", "line 2", `"crew:failed"`},
		},
		{
			name: "an extra is a stage's label in another case",
			body: `extra_labels:
  - label: CREW:READY FOR FIX
workflow:
  - name: fix
    label: crew:ready for fix
    moves_to: crew:fixing
    on_success: crew:ready to review
    on_failure: crew:failed
    actions:
      - name: development
        prompt: "Fix {{.Issue.Ref}}"
`,
			wants: []string{"extra_labels[0].label", "line 2", `"CREW:READY FOR FIX"`},
		},
		{
			name: "two extras share a label in different cases",
			body: `extra_labels:
  - label: crew:parked
  - label: Crew:Parked
` + oneStage,
			wants: []string{"extra_labels[1].label", "line 3", "extra_labels[0]"},
		},
		{
			name: "an extra without a label",
			body: `extra_labels:
  - description: Parked work
` + oneStage,
			wants: []string{"extra_labels[0].label", "line 2", "required"},
		},
		{
			name: "an extra with an empty label",
			body: `extra_labels:
  - label: ""
` + oneStage,
			wants: []string{"extra_labels[0].label", "line 2", "required"},
		},
		{
			name: "an unknown key in an extra",
			body: `extra_labels:
  - label: crew:parked
    labels: crew:waiting
` + oneStage,
			wants: []string{"extra_labels[0].labels", "line 3", "unknown key"},
		},
		{
			name: "an issue_template in a directory",
			body: `extra_labels:
  - label: crew:parked
    issue_template: templates/bug.md
` + oneStage,
			wants: []string{"extra_labels[0].issue_template", "line 3", ".md"},
		},
		{
			name: "an issue_template outside the template directory",
			body: `workflow:
  - name: implement
    issue_template: ../bug.md
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].issue_template", "line 3", ".md"},
		},
		{
			name: "an issue_template that is not Markdown",
			body: `workflow:
  - name: implement
    issue_template: bug.txt
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].issue_template", "line 3", ".md"},
		},
		{
			name: "an empty description",
			body: `workflow:
  - name: implement
    description: ""
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].description", "line 3", "empty"},
		},
		{
			name: "two actions of a stage share a name",
			body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
      - name: development
        prompt: "Implement again {{.Issue.Ref}}"
`,
			wants: []string{"workflow[0].actions[1].name", "line 10", "actions[0]"},
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
