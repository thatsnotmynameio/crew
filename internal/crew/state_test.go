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
			name: "every key of every stage, in file order, each once",
			rules: []Rule{
				{Label: "ready", MovesTo: "in progress", OnSuccess: "in review", OnFailure: "needs attention"},
				{Label: "in review", MovesTo: "reviewing", OnSuccess: "done", OnFailure: "needs attention"},
			},
			want: []State{"ready", "in progress", "in review", "needs attention", "reviewing", "done"},
		},
		{
			name: "a failure label that is another stage's label is listed once",
			rules: []Rule{
				{Label: "ready", MovesTo: "in progress", OnSuccess: "ready to review", OnFailure: "needs attention"},
				{Label: "ready to review", MovesTo: "in review", OnSuccess: "done", OnFailure: "ready"},
			},
			want: []State{"ready", "in progress", "ready to review", "needs attention", "in review", "done"},
		},
		{
			name: "an empty workflow",
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
