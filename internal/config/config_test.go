package config_test

import (
	"errors"
	"io/fs"
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
	return writeFiles(t, body, noFile)
}

func load(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.Load(writeRoot(t, body), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// loadErr loads body, expects an error, and checks the message holds each of
// wants.
func loadErr(t *testing.T, body string, wants ...string) {
	t.Helper()
	cfg, err := config.Load(writeRoot(t, body), "")
	if err == nil {
		t.Fatalf("Load succeeded with %+v, want an error", cfg)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// rejectCase is a config that Load must reject, with what its error must
// say.
type rejectCase struct {
	name  string
	body  string
	wants []string
}

// testRejects checks that Load rejects each case's body with an error
// holding each of its wants.
func testRejects(t *testing.T, cases []rejectCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			loadErr(t, tt.body, tt.wants...)
		})
	}
}

// oneAgent declares one agent, claude, for tests about other keys.
const oneAgent = `agents:
  claude:
    harness: {name: claude}
`

// oneRule is a minimal valid config, one agent and one rule of one action,
// for tests about other keys. Its rules key is on line 4.
const oneRule = oneAgent + ruleOnly

// ruleOnly is oneRule without its agent.
const ruleOnly = `rules:
  implement:
    labels:
      ready: ready
      running: in progress
      success: ready to review
      failure: needs attention
    actions:
      development:
        prompt: "Implement {{.Issue.Ref}}"
`

// draftRules are the rules testdata/draft's config loads into.
func draftRules(t *testing.T) []crew.Rule {
	t.Helper()
	queue := crew.Queue{Name: "default", Slots: 2}
	return []crew.Rule{
		{
			Name: "implement", Queue: queue, Notify: true,
			Labels: crew.Labels{
				Ready: "ready", Running: "in progress", Success: "ready to review", Failure: "needs attention",
			},
			Actions: []crew.Action{
				{
					Name: "acceptance", Agent: "claude",
					Prompt: parsedPrompt(t, "acceptance", "Implement test acceptance for issue {{.Issue.Ref}}"),
				},
				{
					Name: "development", Agent: "claude",
					Prompt: parsedPrompt(t, "development", "Implement development for issue {{.Issue.Ref}}"),
				},
			},
		},
		{
			Name: "review", Queue: queue, Notify: true,
			Labels: crew.Labels{
				Ready: "ready to review", Running: "in review", Success: "ready to merge", Failure: "needs attention",
			},
			Actions: []crew.Action{
				{
					Name: "custom_review", Agent: "claude",
					Prompt: parsedPrompt(t, "custom_review", "Review implementation for issue {{.Issue.Ref}}"),
				},
			},
		},
	}
}

func TestLoadDraftConfig(t *testing.T) {
	cfg, err := config.Load("testdata/draft", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PollInterval != 300*time.Second || cfg.MaxParallelIssues != 2 || cfg.Tracker != "github" {
		t.Errorf("got poll %v, parallel %d, tracker %q; want 5m0s, 2, github",
			cfg.PollInterval, cfg.MaxParallelIssues, cfg.Tracker)
	}
	wantRules := draftRules(t)
	if !reflect.DeepEqual(cfg.Rules, wantRules) {
		t.Errorf("Rules = %+v\nwant %+v", cfg.Rules, wantRules)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0].Name != "claude" || cfg.Agents[0].Harness != "claude" ||
		!cfg.Agents[0].Used {
		t.Fatalf("Agents = %+v, want claude on claude, in use", cfg.Agents)
	}
	var harness struct {
		Model string `yaml:"model"`
	}
	if err := cfg.Agents[0].HarnessSection(&harness); err != nil {
		t.Fatalf("HarnessSection: %v", err)
	}
	if harness.Model != "claude-opus-5-5" {
		t.Errorf("harness model = %q, want claude-opus-5-5", harness.Model)
	}
}

// Covers F1: a tracker, one agent and one rule of one action that names no
// agent is a whole config.
func TestLoadAMinimalConfig(t *testing.T) {
	cfg := load(t, `tracker:
  name: github
agents:
  developer:
    harness:
      name: claude
rules:
  development:
    labels: {ready: todo, running: doing, success: review, failure: failed}
    actions:
      lfg:
        prompt: "/lfg {{.Issue.Ref}}"
`)
	rule := cfg.Rules[0]
	if got := rule.Actions[0].Agent; got != "developer" {
		t.Errorf("the action's agent = %q, want developer", got)
	}
	if want := (crew.Queue{Name: "default", Slots: 2}); rule.Queue != want {
		t.Errorf("Queue = %+v, want %+v", rule.Queue, want)
	}
	wantBoard := []crew.BoardColumn{{Name: "development", Labels: []crew.State{"todo", "doing"}}}
	if !reflect.DeepEqual(cfg.Board, wantBoard) || cfg.BoardWritten {
		t.Errorf("Board = %+v (written %v), want %+v, not written", cfg.Board, cfg.BoardWritten, wantBoard)
	}
	if cfg.Bot != "" || cfg.Bots != nil {
		t.Errorf("Bot = %q, Bots = %q, want none", cfg.Bot, cfg.Bots)
	}
}

func TestLoadReadsEngineSettingsAsWritten(t *testing.T) {
	cfg := load(t, `poll_interval_seconds: 60
max_parallel_issues: 5
run_time_limit_seconds: 28800
usage_in_status: true
tracker:
  name: jira
`+oneRule)
	if cfg.PollInterval != time.Minute || cfg.MaxParallelIssues != 5 || cfg.Tracker != "jira" ||
		cfg.RunTimeLimit != 8*time.Hour || !cfg.UsageInStatus {
		t.Errorf("got poll %v, parallel %d, tracker %q, run time %v, usage %v; want 1m0s, 5, jira, 8h0m0s, true",
			cfg.PollInterval, cfg.MaxParallelIssues, cfg.Tracker, cfg.RunTimeLimit, cfg.UsageInStatus)
	}
}

func TestLoadAppliesEngineDefaults(t *testing.T) {
	cfg := load(t, "run_time_limit_seconds:\n"+oneRule)
	if cfg.PollInterval != 300*time.Second || cfg.MaxParallelIssues != 2 || cfg.Tracker != "github" ||
		cfg.RunTimeLimit != 0 || cfg.UsageInStatus {
		t.Errorf("got poll %v, parallel %d, tracker %q, run time %v, usage %v; want 5m0s, 2, github, 0s, false",
			cfg.PollInterval, cfg.MaxParallelIssues, cfg.Tracker, cfg.RunTimeLimit, cfg.UsageInStatus)
	}

	// The model's default belongs to the harness adapter, so an omitted model
	// reaches the harness section unset and the target keeps its own value.
	harness := struct {
		Model string `yaml:"model"`
	}{Model: "adapter default"}
	if err := cfg.Agents[0].HarnessSection(&harness); err != nil {
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

// parsedPrompt parses text as the prompt of the action named action, and
// fails t when it does not parse.
func parsedPrompt(t *testing.T, action crew.ActionName, text string) crew.Prompt {
	t.Helper()
	p, err := crew.ParsePrompt(action, text)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadRendersPromptForIssue(t *testing.T) {
	cfg := load(t, oneRule)
	issue := crew.Issue{ID: issueID("42"), Ref: "#42", Title: "Fix it", URL: "https://example.com/42"}
	got, err := cfg.Rules[0].Actions[0].Prompt.Render(issue)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "Implement #42" {
		t.Errorf("Render = %q, want %q", got, "Implement #42")
	}
}

// Covers AE4 at the config level: with neither file, the error names both
// and points to the example.
func TestLoadMissingFileSaysWhereItLooked(t *testing.T) {
	root := t.TempDir()
	_, err := config.Load(root, "")
	if err == nil {
		t.Fatal("Load succeeded without a config file")
	}
	for _, want := range []string{root, sharedName, localName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v is not fs.ErrNotExist", err)
	}
	if want := ".crew/config.example.yaml"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not point to the example (%s) for creating one", err, want)
	}
}

// The section decoders are what adapter factories receive: they reject
// unknown keys at any depth, naming the key path and its line.
func TestSectionDecodersAreStrict(t *testing.T) {
	cfg := load(t, `agents:
  claude:
    harness:
      name: claude
      model: claude-opus-5-5
      effort: high
tracker:
  name: github
  bot: clerk
  lables:
    ready: todo
  columns:
    - name: Todo
      state: ready
    - name: Doing
      stat: in progress
`+ruleOnly)

	var harness struct {
		Model  string `yaml:"model"`
		Effort string `yaml:"effort"`
	}
	if err := cfg.Agents[0].HarnessSection(&harness); err != nil {
		t.Fatalf("HarnessSection: %v", err)
	}
	if harness.Model != "claude-opus-5-5" || harness.Effort != "high" {
		t.Errorf("harness = %+v, want model claude-opus-5-5 and effort high", harness)
	}
	var noEffort struct {
		Model string `yaml:"model"`
	}
	assertErr(t, cfg.Agents[0].HarnessSection(&noEffort), "agents.claude.harness.effort", "line 6", "unknown key")

	var labelsTypo struct {
		Labels  map[string]string `yaml:"labels"`
		Columns []struct {
			Name  string `yaml:"name"`
			State string `yaml:"state"`
		} `yaml:"columns"`
	}
	assertErr(t, cfg.TrackerSection(&labelsTypo), "tracker.lables", "line 10", "unknown key")

	var nestedTypo struct {
		Lables  map[string]string `yaml:"lables"`
		Columns []struct {
			Name  string `yaml:"name"`
			State string `yaml:"state"`
		} `yaml:"columns"`
	}
	assertErr(t, cfg.TrackerSection(&nestedTypo), "tracker.columns[1].stat", "line 16", "unknown key")
}

func TestSectionDecoderNamesNestedTypeErrors(t *testing.T) {
	cfg := load(t, `tracker:
  limits:
    per_poll: many
`+oneRule)
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

func TestLoadRejectsInvalidSettings(t *testing.T) {
	testRejects(t, invalidSettings)
}

// invalidSettings are errors in the top-level keys and the tracker.
var invalidSettings = []rejectCase{
	{
		name:  "unknown top-level key",
		body:  "harnes: claude\n" + oneRule,
		wants: []string{"harnes", "line 1", "unknown key"},
	},
	{
		name:  "wrong type",
		body:  "poll_interval_seconds: soon\n" + oneRule,
		wants: []string{"poll_interval_seconds", "line 1", "soon"},
	},
	{
		name:  "duplicate key",
		body:  "max_parallel_issues: 2\nmax_parallel_issues: 3\n" + oneRule,
		wants: []string{"max_parallel_issues", "line 2", "duplicate key, first set on line 1"},
	},
	{
		name:  "poll interval not positive",
		body:  "poll_interval_seconds: 0\n" + oneRule,
		wants: []string{"poll_interval_seconds", "line 1", "positive"},
	},
	{
		name:  "parallel issues not positive",
		body:  "max_parallel_issues: -1\n" + oneRule,
		wants: []string{"max_parallel_issues", "line 1", "positive"},
	},
	{
		name:  "run time limit zero",
		body:  "run_time_limit_seconds: 0\n" + oneRule,
		wants: []string{"run_time_limit_seconds", "line 1", "positive"},
	},
	{
		name:  "run time limit as a duration",
		body:  "run_time_limit_seconds: 8h\n" + oneRule,
		wants: []string{"run_time_limit_seconds", "line 1", "8h"},
	},
	{
		name:  "usage in status not a boolean",
		body:  "usage_in_status: maybe\n" + oneRule,
		wants: []string{"usage_in_status", "line 1", "maybe"},
	},
	{
		name:  "tracker.bot of the wrong type",
		body:  "tracker:\n  bot: {name: ops}\n" + oneRule,
		wants: []string{"tracker.bot", "line 2"},
	},
	{
		name:  "tracker is a list",
		body:  "tracker: [github]\n" + oneRule,
		wants: []string{"tracker", "line 1", "must be a mapping"},
	},
	{
		name:  "the config is a list",
		body:  "- rules\n",
		wants: []string{"line 1", "must be a mapping", "rules"},
	},
}

// issueID returns the id of the issue keyed key, in no repository.
func issueID(key string) crew.IssueID { return crew.IssueID{Key: key} }
