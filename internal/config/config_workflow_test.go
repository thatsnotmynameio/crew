package config_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestLoadAcceptsWorkflowLabels(t *testing.T) {
	for _, tt := range acceptedWorkflows {
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

// acceptedWorkflows are workflows Load accepts, with the stages each loads
// to, leaving out their actions.
var acceptedWorkflows = []struct {
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
			{
				Name: "implement", Label: "Ready For Work", MovesTo: "crew is on it",
				OnSuccess: "In Review", OnFailure: "Needs Attention",
			},
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
			{
				Name: "implement", Label: "ready", MovesTo: "in progress",
				OnSuccess: "ready to review", OnFailure: "needs attention",
			},
			{
				Name: "review", Label: "ready to review", MovesTo: "in review",
				OnSuccess: "ready to merge", OnFailure: "ready",
			},
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
			{
				Name: "implement", Label: "ready", MovesTo: "In Progress",
				OnSuccess: "ready to review", OnFailure: "Needs Attention",
			},
			{
				Name: "review", Label: "ready to review", MovesTo: "in review",
				OnSuccess: "In Progress", OnFailure: "Needs Attention",
			},
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
			{
				Name: "fix", Label: "ready for fix", MovesTo: "fixing",
				OnSuccess: "ready to review", OnFailure: "needs attention",
			},
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
			{
				Name: "implement", Label: "ready", MovesTo: "in progress",
				OnSuccess: "in review", OnFailure: "in progress",
			},
		},
	},
}
