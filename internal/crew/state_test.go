package crew

import (
	"slices"
	"testing"
)

func TestRuleStates(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
		want  []State
	}{
		{
			name: "every label of every rule, in file order, each once",
			rules: []Rule{
				{Labels: Labels{Ready: "ready", Running: "in progress", Success: "in review", Failure: "needs attention"}},
				{Labels: Labels{Ready: "in review", Running: "reviewing", Success: "done", Failure: "needs attention"}},
			},
			want: []State{"ready", "in progress", "in review", "needs attention", "reviewing", "done"},
		},
		{
			name: "a failure label that is another rule's label is listed once",
			rules: []Rule{
				{Labels: Labels{Ready: "ready", Running: "in progress", Success: "ready to review", Failure: "needs attention"}},
				{Labels: Labels{Ready: "ready to review", Running: "in review", Success: "done", Failure: "ready"}},
			},
			want: []State{"ready", "in progress", "ready to review", "needs attention", "in review", "done"},
		},
		{
			name: "no rules",
			want: []State{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RuleStates(tt.rules); !slices.Equal(got, tt.want) {
				t.Errorf("RuleStates() = %q, want %q", got, tt.want)
			}
		})
	}
}
