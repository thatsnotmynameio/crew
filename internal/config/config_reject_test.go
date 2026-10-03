package config_test

import "testing"

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

func TestLoadRejectsInvalidSettings(t *testing.T) {
	testRejects(t, invalidSettings)
}

func TestLoadRejectsInvalidExtraLabels(t *testing.T) {
	testRejects(t, invalidExtraLabels)
}

func TestLoadRejectsInvalidPrompts(t *testing.T) {
	testRejects(t, invalidPrompts)
}

// invalidSettings are errors in the engine's settings and in the keys of the
// file's top level.
var invalidSettings = []rejectCase{
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
		name: "usage in status not a boolean",
		body: `config:
  usage_in_status: maybe
` + oneStage,
		wants: []string{"config.usage_in_status", "line 2", "maybe"},
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
}

// invalidExtraLabels are errors in extra_labels.
var invalidExtraLabels = []rejectCase{
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
}

// invalidPrompts are errors in the top-level prompts.
var invalidPrompts = []rejectCase{
	{
		name: "prompts is a list",
		body: `prompts:
  - brainstorm
` + oneStage,
		wants: []string{"prompts", "line 2", "must be a mapping"},
	},
	{
		name: "a prompt is a mapping",
		body: `prompts:
  brainstorm:
    prompt: "Brainstorm {{.Issue.Ref}}"
` + oneStage,
		wants: []string{"prompts.brainstorm", "line 3"},
	},
	{
		name: "an empty prompt",
		body: `prompts:
  brainstorm:
` + oneStage,
		wants: []string{"prompts.brainstorm", "line 2", "empty"},
	},
	{
		name: "a prompt with an unknown field",
		body: `prompts:
  brainstorm: "Brainstorm {{.Issue.Number}}"
` + oneStage,
		wants: []string{"prompts.brainstorm", "line 2", "Number"},
	},
	{
		name: "a prompt that does not parse",
		body: `prompts:
  brainstorm: "Brainstorm {{.Issue.Ref"
` + oneStage,
		wants: []string{"prompts.brainstorm", "line 2"},
	},
	{
		name: "two prompts share a name",
		body: `prompts:
  brainstorm: "Brainstorm {{.Issue.Ref}}"
  brainstorm: "Again {{.Issue.Ref}}"
` + oneStage,
		wants: []string{"prompts.brainstorm", "line 3", "duplicate key"},
	},
}
