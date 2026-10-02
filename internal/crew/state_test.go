package crew

import (
	"slices"
	"testing"
)

func TestWorkflowStates(t *testing.T) {
	tests := []struct {
		name     string
		workflow []Stage
		want     []State
	}{
		{
			name: "every key of every stage, in file order, each once",
			workflow: []Stage{
				{Label: "ready", MovesTo: "in progress", OnSuccess: "in review", OnFailure: "needs attention"},
				{Label: "in review", MovesTo: "reviewing", OnSuccess: "done", OnFailure: "needs attention"},
			},
			want: []State{"ready", "in progress", "in review", "needs attention", "reviewing", "done"},
		},
		{
			name: "a failure label that is another stage's label is listed once",
			workflow: []Stage{
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
			if got := WorkflowStates(tt.workflow); !slices.Equal(got, tt.want) {
				t.Errorf("WorkflowStates() = %q, want %q", got, tt.want)
			}
		})
	}
}
