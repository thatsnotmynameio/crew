package crew

import "testing"

func TestSpendSumsSessions(t *testing.T) {
	tokens := Tokens{Input: 100, Output: 200, CacheRead: 17_000_000, CacheWrite: 300_000}
	withAll := Usage{Cost: 1.20, HasCost: true, Tokens: tokens, HasTokens: true}
	noCost := Usage{Tokens: tokens, HasTokens: true}

	tests := []struct {
		name string
		sum  Spend
		want Spend
	}{
		{"one session", withAll.Spend(), Spend{Sessions: 1, Cost: 1.20, WithCost: 1, Tokens: tokens, WithTokens: 1}},
		{"one cost missing", withAll.Spend().Add(noCost.Spend()),
			Spend{Sessions: 2, Cost: 1.20, WithCost: 1, Tokens: Tokens{
				Input: 200, Output: 400, CacheRead: 34_000_000, CacheWrite: 600_000,
			}, WithTokens: 2}},
		{"nothing reported", Usage{}.Spend(), Spend{Sessions: 1}},
		{"a reported zero", Usage{HasCost: true, HasTokens: true}.Spend(), Spend{Sessions: 1, WithCost: 1, WithTokens: 1}},
		{"an action without a session adds nothing", withAll.Spend().Add(Spend{}), withAll.Spend()},
	}
	for _, tt := range tests {
		if tt.sum != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, tt.sum, tt.want)
		}
	}
}

func TestTokensTotalCountsAllFourKinds(t *testing.T) {
	if got := (Tokens{Input: 1, Output: 20, CacheRead: 300, CacheWrite: 4_000}).Total(); got != 4_321 {
		t.Errorf("Total() = %d, want 4321", got)
	}
}
