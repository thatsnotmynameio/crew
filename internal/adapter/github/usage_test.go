package github

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestUsageWordsWhatTheSessionsSpent(t *testing.T) {
	tokens := crew.Tokens{Input: 100, Output: 200, CacheRead: 17_000_000, CacheWrite: 300_000}
	withAll := crew.Usage{Cost: 1.20, HasCost: true, Tokens: tokens, HasTokens: true}
	noCost := crew.Usage{Tokens: tokens, HasTokens: true}
	costOnly := crew.Usage{Cost: 3.05, HasCost: true}
	none := crew.PullRequest{Lookup: crew.PullRequestNone}

	tests := []struct {
		name  string
		spend crew.Spend
		want  string
	}{
		{"one session", withAll.Spend(), " Usage: $1.20, 17.3M tokens. Pull request: none."},
		{"both partial", withAll.Spend().Add(noCost.Spend()).Add(costOnly.Spend()),
			" Usage: $4.25 (partial), 34.6M tokens (partial). Pull request: none."},
		{"no cost at all", noCost.Spend(), " Usage: cost not reported, 17.3M tokens. Pull request: none."},
		{"no tokens at all", costOnly.Spend(), " Usage: $3.05, tokens not reported. Pull request: none."},
		{"nothing reported", crew.Usage{}.Spend(), " Usage: cost and tokens not reported. Pull request: none."},
		{"a reported zero", crew.Usage{HasCost: true, HasTokens: true}.Spend(),
			" Usage: $0.00, 0 tokens. Pull request: none."},
	}
	for _, tt := range tests {
		if got := usage(crew.Some(crew.ShownUsage{Spend: tt.spend, PullRequest: none})); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	if got := usage(crew.Optional[crew.ShownUsage]{}); got != "" {
		t.Errorf("not shown: %q, want nothing", got)
	}
}

func TestUsageWordsTokenCountsCompactly(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"}, {950, "950"}, {1_000, "1K"}, {48_210, "48.2K"}, {999_949, "999.9K"},
		{17_213_000, "17.2M"}, {129_000_000, "129M"},
	}
	for _, tt := range tests {
		spend := crew.Usage{Cost: 0.004, HasCost: true, Tokens: crew.Tokens{Output: tt.n}, HasTokens: true}.Spend()
		want := " Usage: $0.00, " + tt.want + " tokens. Pull request: not looked up."
		if got := usage(crew.Some(crew.ShownUsage{Spend: spend})); got != want {
			t.Errorf("%d tokens: %q, want %q", tt.n, got, want)
		}
	}
}
