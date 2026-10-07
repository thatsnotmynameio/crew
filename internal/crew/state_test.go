package crew

import (
	"slices"
	"testing"
)

// routed returns a rule from ready to running whose passed route moves the
// item to passed and whose failed route reports and moves it to failed.
func routed(ready, running, passed, failed State) Rule {
	return Rule{
		Labels: Labels{Ready: ready, Running: running},
		Routes: []Route{
			{Name: PassedRoute, Steps: []Step{MoveStep{To: passed}}},
			{Name: FailedRoute, Steps: []Step{ReportStep{}, MoveStep{To: failed}}},
		},
	}
}

func TestRuleStates(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
		want  []State
	}{
		{
			name: "every label of every rule, in file order, each once",
			rules: []Rule{
				routed("ready", "in progress", "in review", "needs attention"),
				routed("in review", "reviewing", "done", "needs attention"),
			},
			want: []State{"ready", "in progress", "in review", "needs attention", "reviewing", "done"},
		},
		{
			name: "a route's move that is another rule's label is listed once",
			rules: []Rule{
				routed("ready", "in progress", "ready to review", "needs attention"),
				routed("ready to review", "in review", "done", "ready"),
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
			name: "a rule's route moves come before the next rule's labels",
			rules: []Rule{
				{
					Labels: Labels{Ready: "ready", Running: "in progress"},
					Routes: []Route{{Name: "blocked", Steps: []Step{MoveStep{To: "blocked"}}}},
				},
				{Labels: Labels{Ready: "done", Running: "checking"}},
			},
			want: []State{"ready", "in progress", "blocked", "done", "checking"},
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
