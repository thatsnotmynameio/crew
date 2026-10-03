package config_test

import (
	"fmt"
	"reflect"
	"strings"
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

// defaultQueue is the queue of a stage that names none, at the default limit
// of 2 issues: 2 - clerk's 1 slot.
var defaultQueue = crew.Queue{Name: "default", Slots: 1}

// queuedStages is a workflow with one stage per item of queues, each with
// its own labels and with that queue; a stage whose item is "" names no
// queue.
func queuedStages(queues ...string) string {
	var b strings.Builder
	b.WriteString("workflow:\n")
	for i, queue := range queues {
		fmt.Fprintf(&b, `  - name: stage %[1]d
    label: ready %[1]d
    moves_to: in progress %[1]d
    on_success: done %[1]d
    on_failure: failed %[1]d
`, i)
		if queue != "" {
			fmt.Fprintf(&b, "    queue: %s\n", queue)
		}
		b.WriteString(`    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`)
	}
	return b.String()
}

func TestLoadGivesEveryStageItsQueue(t *testing.T) {
	for _, tt := range queuedWorkflows {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.body)
			got := make([]crew.Queue, len(cfg.Workflow))
			for i, s := range cfg.Workflow {
				got[i] = s.Queue
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stage queues = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// queuedWorkflows are workflows whose stages run in queues, with the queue
// each stage loads with, in stage order.
var queuedWorkflows = []struct {
	name string
	body string
	want []crew.Queue
}{
	{
		// Covers AE3.
		name: "only max_parallel_issues: 2 gives clerk 1 slot and default 1",
		body: "config:\n  max_parallel_issues: 2\n" + queuedStages("", "clerk"),
		want: []crew.Queue{{Name: "default", Slots: 1}, {Name: "clerk", Slots: 1}},
	},
	{
		name: "no config section gives default 1 slot of the default limit 2",
		body: queuedStages(""),
		want: []crew.Queue{{Name: "default", Slots: 1}},
	},
	{
		name: "a declared queue, clerk, and a stage that names none",
		body: `config:
  max_parallel_issues: 4
  clerk_slots: 1
  queues: {review: 2}
` + queuedStages("review", "clerk", ""),
		want: []crew.Queue{{Name: "review", Slots: 2}, {Name: "clerk", Slots: 1}, {Name: "default", Slots: 1}},
	},
	{
		name: "declared queues keep their own slots",
		body: `config:
  max_parallel_issues: 10
  clerk_slots: 2
  queues:
    review: 3
    docs: 1
` + queuedStages("docs", "review", "clerk", ""),
		want: []crew.Queue{
			{Name: "docs", Slots: 1}, {Name: "review", Slots: 3},
			{Name: "clerk", Slots: 2}, {Name: "default", Slots: 4},
		},
	},
	{
		name: "a stage that names default runs in default",
		body: "config:\n  max_parallel_issues: 3\n" + queuedStages("default", ""),
		want: []crew.Queue{{Name: "default", Slots: 2}, {Name: "default", Slots: 2}},
	},
	{
		// AE4's config: default may have 0 slots.
		name: "queues that leave default no slot",
		body: `config:
  max_parallel_issues: 3
  clerk_slots: 1
  queues: {review: 2}
` + queuedStages("review", ""),
		want: []crew.Queue{{Name: "review", Slots: 2}, {Name: "default", Slots: 0}},
	},
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
				Queue: defaultQueue,
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
				Queue: defaultQueue,
			},
			{
				Name: "review", Label: "ready to review", MovesTo: "in review",
				OnSuccess: "ready to merge", OnFailure: "ready",
				Queue: defaultQueue,
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
				Queue: defaultQueue,
			},
			{
				Name: "review", Label: "ready to review", MovesTo: "in review",
				OnSuccess: "In Progress", OnFailure: "Needs Attention",
				Queue: defaultQueue,
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
				Queue: defaultQueue,
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
				Queue: defaultQueue,
			},
		},
	},
}
