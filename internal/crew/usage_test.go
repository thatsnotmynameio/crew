package crew

import "testing"

func TestSpendSumsSessions(t *testing.T) {
	tokens := Tokens{Input: 100, Output: 200, CacheRead: 17_000_000, CacheWrite: 300_000}
	withAll := Usage{Cost: 1.20, HasCost: true, Tokens: tokens, HasTokens: true}
	noCost := Usage{Tokens: tokens, HasTokens: true}
	none := Usage{}

	tests := []struct {
		name string
		sum  Spend
		want string
	}{
		{"no session", Spend{}, ""},
		{"one session", withAll.Spend(), "$1.20, 17.3M tokens"},
		{"two sessions", withAll.Spend().Add(Usage{Cost: 3.05, HasCost: true}.Spend()), "$4.25, 17.3M tokens (partial)"},
		{"one cost missing", withAll.Spend().Add(noCost.Spend()), "$1.20 (partial), 34.6M tokens"},
		{"no cost at all", noCost.Spend(), "cost not reported, 17.3M tokens"},
		{"nothing reported", none.Spend(), "cost and tokens not reported"},
		{"a reported zero", Usage{HasCost: true, HasTokens: true}.Spend(), "$0.00, 0 tokens"},
		{"an action without a session adds nothing", withAll.Spend().Add(Spend{}), "$1.20, 17.3M tokens"},
	}
	for _, tt := range tests {
		if got := tt.sum.String(); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"}, {950, "950"}, {1_000, "1K"}, {48_210, "48.2K"}, {999_949, "999.9K"},
		{17_213_000, "17.2M"}, {129_000_000, "129M"},
	}
	for _, tt := range tests {
		if got := FormatTokens(tt.n); got != tt.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	for usd, want := range map[float64]string{12.4: "$12.40", 0.004: "$0.00", 46.9905864: "$46.99", 0: "$0.00"} {
		if got := FormatCost(usd); got != want {
			t.Errorf("FormatCost(%v) = %q, want %q", usd, got, want)
		}
	}
}

func TestPullRequestString(t *testing.T) {
	tests := []struct {
		pr   PullRequest
		want string
	}{
		{PullRequest{Lookup: PullRequestFound, Ref: "#45", URL: "https://github.com/o/r/pull/45"}, "pull request #45"},
		{PullRequest{Lookup: PullRequestNone}, "no pull request"},
		{PullRequest{}, "pull request not looked up"},
	}
	for _, tt := range tests {
		if got := tt.pr.String(); got != tt.want {
			t.Errorf("%+v: %q, want %q", tt.pr, got, tt.want)
		}
	}
}
