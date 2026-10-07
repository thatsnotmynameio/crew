package lines_test

import (
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

func TestSpendPartsWordCostAndTokens(t *testing.T) {
	tokens := crew.Tokens{Input: 100, Output: 200, CacheRead: 17_000_000, CacheWrite: 300_000}
	withAll := crew.Usage{Cost: crew.Some(1.20), Tokens: crew.Some(tokens)}
	noCost := crew.Usage{Tokens: crew.Some(tokens)}

	tests := []struct {
		name string
		sum  crew.Spend
		want []string
	}{
		{"no session", crew.Spend{}, nil},
		{"one session", withAll.Spend(), []string{"$1.20", "17.3M tokens"}},
		{"one tokens missing", withAll.Spend().Add(crew.Usage{Cost: crew.Some(3.05)}.Spend()),
			[]string{"$4.25", "17.3M tokens (partial)"}},
		{"one cost missing", withAll.Spend().Add(noCost.Spend()), []string{"$1.20 (partial)", "34.6M tokens"}},
		{"no cost at all", noCost.Spend(), []string{"cost not reported", "17.3M tokens"}},
		{"no tokens at all", crew.Usage{Cost: crew.Some(1.20)}.Spend(), []string{"$1.20", "tokens not reported"}},
		{"nothing reported", crew.Usage{}.Spend(), []string{"cost and tokens not reported"}},
		{"a reported zero", crew.Usage{Cost: crew.Some(0.0), Tokens: crew.Some(crew.Tokens{})}.Spend(),
			[]string{"$0.00", "0 tokens"}},
	}
	for _, tt := range tests {
		if got := lines.SpendParts(tt.sum); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSpendPartsWordTokenCountsCompactly(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"}, {950, "950"}, {1_000, "1K"}, {48_210, "48.2K"}, {999_949, "999.9K"},
		{17_213_000, "17.2M"}, {129_000_000, "129M"},
	}
	for _, tt := range tests {
		spend := crew.Usage{Tokens: crew.Some(crew.Tokens{Input: tt.n})}.Spend()
		if got := lines.SpendParts(spend)[1]; got != tt.want+" tokens" {
			t.Errorf("%d tokens: %q, want %q", tt.n, got, tt.want+" tokens")
		}
	}
}

func TestSpendPartsWordCostsInDollarsAndCents(t *testing.T) {
	for usd, want := range map[float64]string{12.4: "$12.40", 0.004: "$0.00", 46.9905864: "$46.99", 0: "$0.00"} {
		spend := crew.Usage{Cost: crew.Some(usd)}.Spend()
		if got := lines.SpendParts(spend)[0]; got != want {
			t.Errorf("cost %v: %q, want %q", usd, got, want)
		}
	}
}

func TestKindNameNamesIssuesAndPullRequests(t *testing.T) {
	tests := []struct {
		kind crew.Kind
		want string
	}{
		{crew.KindIssue, "issue"},
		{crew.KindPullRequest, "pull request"},
		{crew.Kind(7), "unknown"},
	}
	for _, tt := range tests {
		if got := lines.KindName(tt.kind); got != tt.want {
			t.Errorf("KindName(%d) = %q, want %q", int(tt.kind), got, tt.want)
		}
	}
}
