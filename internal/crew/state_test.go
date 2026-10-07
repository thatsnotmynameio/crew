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

func TestRuleStatesListsRouteMoves(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
		want  []State
	}{
		{
			name: "every route's move labels after ready and running, each once",
			rules: []Rule{{
				Labels: Labels{Ready: "ready", Running: "in progress"},
				Routes: []Route{
					{Name: PassedRoute, Steps: []Step{MoveStep{To: "in review"}}},
					{Name: FailedRoute, Steps: []Step{ReportStep{}, MoveStep{To: "failed"}}},
					{Name: "blocked", Steps: []Step{CommentStep{}, MoveStep{To: "failed"}}},
				},
			}},
			want: []State{"ready", "in progress", "in review", "failed"},
		},
		{
			name: "a route that only closes adds no state",
			rules: []Rule{{
				Labels: Labels{Ready: "ready", Running: "in progress"},
				Routes: []Route{
					{Name: PassedRoute, Steps: []Step{ShellStep{Name: "notify"}, CloseStep{}}},
					{Name: FailedRoute, Steps: []Step{MoveStep{To: "failed"}}},
				},
			}},
			want: []State{"ready", "in progress", "failed"},
		},
		{
			name: "route moves come before the success and failure labels",
			rules: []Rule{
				{
					Labels: Labels{Ready: "ready", Running: "in progress", Success: "done", Failure: "failed"},
					Routes: []Route{{Name: "blocked", Steps: []Step{MoveStep{To: "blocked"}}}},
				},
				{Labels: Labels{Ready: "done", Running: "checking"}},
			},
			want: []State{"ready", "in progress", "blocked", "done", "failed", "checking"},
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

func TestWaitingStatesAreTheFinalMovesOfTheRoutesWaitingLeadsTo(t *testing.T) {
	ask := ToRoute{Route: "ask"}
	tests := []struct {
		name string
		rule Rule
		want []State
	}{
		{
			name: "the final move of each route an on: sends waiting to, in route order, each once",
			rule: Rule{
				Actions: []Action{
					{Name: "lfg", On: On{Waiting: ask, "blocked": ToRoute{Route: "blocked"}}},
					{Name: "review", On: On{Waiting: ToRoute{Route: "pause"}}},
					{Name: "again", On: On{Waiting: ask}},
				},
				Routes: []Route{
					{Name: PassedRoute, Steps: []Step{MoveStep{To: "done"}}},
					{Name: "pause", Steps: []Step{CommentStep{}, MoveStep{To: "paused"}}},
					{Name: "blocked", Steps: []Step{MoveStep{To: "blocked"}}},
					{Name: "ask", Steps: []Step{MoveStep{To: "waiting answer"}}},
				},
			},
			want: []State{"paused", "waiting answer"},
		},
		{
			name: "a waiting route that closes, and waiting left to its default, add none",
			rule: Rule{
				Actions: []Action{{Name: "lfg", On: On{Waiting: ToRoute{Route: "close"}}}, {Name: "judge"}},
				Routes: []Route{
					{Name: FailedRoute, Steps: []Step{MoveStep{To: "failed"}}},
					{Name: "close", Steps: []Step{CloseStep{}}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.WaitingStates(); !slices.Equal(got, tt.want) {
				t.Errorf("WaitingStates() = %q, want %q", got, tt.want)
			}
		})
	}
}
