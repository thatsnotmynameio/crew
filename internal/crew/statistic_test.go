package crew

import (
	"slices"
	"testing"
)

// Covers KTD1, KTD9: each span's id derives from its rule run's, and its
// parent is the span it hangs from.
func TestSpanIDsAndParents(t *testing.T) {
	chosen := RouteChoice{Action: "review", Verdict: Passed}
	tests := []struct {
		name               string
		id, parent         SpanID
		wantID, wantParent SpanID
	}{
		{"an action hangs from its run", ActionSpan{Run: "r.1", Action: "review"}.ID(),
			ActionSpan{Run: "r.1", Action: "review"}.Parent(), "r.1/action/review", "r.1"},
		{"a route chosen by an action hangs from it", RouteSpan{Run: "r.1", ChosenBy: Some(chosen)}.ID(),
			RouteSpan{Run: "r.1", ChosenBy: Some(chosen)}.Parent(), "r.1/route", "r.1/action/review"},
		{"a route no action chose hangs from its run", RouteSpan{Run: "r.1"}.ID(), RouteSpan{Run: "r.1"}.Parent(),
			"r.1/route", "r.1"},
		{"a step hangs from its route", StepSpan{Run: "r.1", Index: 2}.ID(), StepSpan{Run: "r.1", Index: 2}.Parent(),
			"r.1/route/2", "r.1/route"},
	}
	for _, tt := range tests {
		if tt.id != tt.wantID || tt.parent != tt.wantParent {
			t.Errorf("%s: id %q, parent %q; want %q, %q", tt.name, tt.id, tt.parent, tt.wantID, tt.wantParent)
		}
	}
}

// Covers KTD1: an action named route is not the run's route.
func TestTheSpanOfAnActionNamedRouteIsNotTheRoutes(t *testing.T) {
	action, route := ActionSpan{Run: "r.1", Action: "route"}.ID(), RouteSpan{Run: "r.1"}.ID()
	if action == route {
		t.Errorf("the action named route and the route share the id %q", action)
	}
}

// Covers KTD7, R14: a session's token records are its tokens by model,
// else its sum under no model, else none, which reads as not reported.
func TestSessionTokenRecords(t *testing.T) {
	opus := ModelTokens{Model: "claude-opus", Tokens: Tokens{Input: 10, Output: 20, CacheRead: 30, CacheWrite: 40}}
	sonnet := ModelTokens{Model: "claude-sonnet", Tokens: Tokens{Input: 1}}
	sum := Tokens{Input: 11, Output: 20, CacheRead: 30, CacheWrite: 40}
	tests := []struct {
		name  string
		usage Usage
		want  []ModelTokens
	}{
		{"by model", Usage{Tokens: Some(sum), ByModel: []ModelTokens{opus, sonnet}}, []ModelTokens{opus, sonnet}},
		{"a sum with no model", Usage{Tokens: Some(sum)}, []ModelTokens{{Tokens: sum}}},
		{"a reported zero", Usage{Tokens: Some(Tokens{})}, []ModelTokens{{}}},
		{"nothing reported", Usage{Turns: Some(3)}, nil},
	}
	for _, tt := range tests {
		if got := tt.usage.TokenRecords(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
}
