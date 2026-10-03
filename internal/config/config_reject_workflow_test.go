package config_test

import "testing"

func TestLoadRejectsInvalidStages(t *testing.T) {
	testRejects(t, invalidStages)
}

func TestLoadRejectsInvalidActions(t *testing.T) {
	testRejects(t, invalidActions)
}

// invalidStages are errors in the workflow's stages and in how they connect.
var invalidStages = []rejectCase{
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
}

// invalidActions are errors in a stage's actions.
var invalidActions = []rejectCase{
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
		name: "empty check",
		body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
        check: ""
`,
		wants: []string{"workflow[0].actions[0].check", "line 10", "must not be empty"},
	},
	{
		name: "blank check",
		body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
        check: "   "
`,
		wants: []string{"workflow[0].actions[0].check", "line 10", "must not be empty"},
	},
	{
		name: "check of the wrong type",
		body: `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
        check: [gh, pr, list]
`,
		wants: []string{"workflow[0].actions[0].check", "line 10"},
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
