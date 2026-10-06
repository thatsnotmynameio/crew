package config_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// queuedRules is one agent and one rule per item of queues, each with its
// own labels and with that queue; a rule whose item is "" names no queue.
func queuedRules(queues ...string) string {
	var b strings.Builder
	b.WriteString(oneAgent + "rules:\n")
	for i, queue := range queues {
		fmt.Fprintf(&b, "  rule %[1]d:\n    labels: {ready: ready %[1]d, running: running %[1]d, "+
			"success: done %[1]d, failure: failed %[1]d}\n", i)
		if queue != "" {
			fmt.Fprintf(&b, "    queue: %s\n", queue)
		}
		b.WriteString("    actions:\n      development: {prompt: \"Implement {{.Issue.Ref}}\"}\n")
	}
	return b.String()
}

func TestLoadGivesEveryRuleItsQueue(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []crew.Queue
	}{
		{
			name: "no queue gives default every slot of the default limit 2",
			body: queuedRules(""),
			want: []crew.Queue{{Name: "default", Slots: 2}},
		},
		{
			// There is no clerk queue to leave room for.
			name: "a limit of 1 and no queue",
			body: "max_parallel_issues: 1\n" + queuedRules("", "default"),
			want: []crew.Queue{{Name: "default", Slots: 1}, {Name: "default", Slots: 1}},
		},
		{
			name: "declared queues keep their own slots, and default gets the rest",
			body: "max_parallel_issues: 10\nqueues:\n  review: 3\n  docs: 1\n" + queuedRules("docs", "review", ""),
			want: []crew.Queue{{Name: "docs", Slots: 1}, {Name: "review", Slots: 3}, {Name: "default", Slots: 6}},
		},
		{
			// default may have 0 slots.
			name: "queues that leave default no slot",
			body: "max_parallel_issues: 3\nqueues: {clerk: 1, review: 2}\n" + queuedRules("review", "clerk", ""),
			want: []crew.Queue{{Name: "review", Slots: 2}, {Name: "clerk", Slots: 1}, {Name: "default", Slots: 0}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.body)
			got := make([]crew.Queue, len(cfg.Rules))
			for i, r := range cfg.Rules {
				got[i] = r.Queue
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rule queues = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidQueues(t *testing.T) {
	testRejects(t, []rejectCase{
		{
			name: "queues that leave default fewer than 0 slots",
			body: "max_parallel_issues: 3\nqueues:\n  review: 2\n  docs: 2\n" + oneRule,
			wants: []string{
				"queues", "line 3", "the default queue would have 3 - 4 = -1 slots",
				"(max_parallel_issues - these queues' slots)",
			},
		},
		{
			name:  "a queue of 0 slots",
			body:  "queues: {review: 0}\n" + oneRule,
			wants: []string{"queues.review", "line 1", "positive"},
		},
		{
			name:  "a queue named default in another case",
			body:  "queues:\n  Default: 1\n" + oneRule,
			wants: []string{"queues.Default", "line 2", "crew's default queue"},
		},
		{
			name:  "two queues share a name",
			body:  "queues:\n  review: 1\n  review: 1\n" + oneRule,
			wants: []string{"queues.review", "line 3", "duplicate key, first set on line 2"},
		},
		{
			name:  "queues is a list",
			body:  "queues:\n  - review\n" + oneRule,
			wants: []string{"queues", "line 2", "must be a mapping"},
		},
		{
			name:  "a queue's slots are not a number",
			body:  "queues: {review: many}\n" + oneRule,
			wants: []string{"queues.review", "line 1", "many"},
		},
	})
}

// One mistake in the queues gives one error, not one more for each sum it
// upsets.
func TestLoadReportsAQueueMistakeOnce(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "max_parallel_issues not positive skips the queue sum",
			body: "max_parallel_issues: 0\nqueues: {review: 1}\n" + oneRule,
			want: "max_parallel_issues",
		},
		{
			name: "a rule naming a queue whose slots are wrong",
			body: "queues: {review: 0}\n" + queuedRules("review"),
			want: "queues.review",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(writeRoot(t, tt.body), "")
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) || strings.Contains(got, "\n") {
				t.Errorf("error %q, want one error, on %s", got, tt.want)
			}
		})
	}
}
