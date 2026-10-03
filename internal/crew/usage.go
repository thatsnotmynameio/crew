package crew

import (
	"fmt"
	"strings"
)

// Usage is what a harness reported a session used. Each value is optional:
// a harness that cannot tell leaves its Has field false, and crew shows the
// value as not reported, never as zero.
type Usage struct {
	// Cost is the session's cost in US dollars, as the harness reports it;
	// set when HasCost.
	Cost    float64
	HasCost bool
	// Tokens is the session's token usage; set when HasTokens.
	Tokens    Tokens
	HasTokens bool
	// Turns is how many turns the session took; set when HasTurns.
	Turns    int
	HasTurns bool
	// Models names the models the session used, sorted; empty when the
	// harness does not tell.
	Models []string
}

// Tokens is a token usage by kind.
type Tokens struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Total is every token of t, of all four kinds.
func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// add returns t plus u.
func (t Tokens) add(u Tokens) Tokens {
	return Tokens{
		Input: t.Input + u.Input, Output: t.Output + u.Output,
		CacheRead: t.CacheRead + u.CacheRead, CacheWrite: t.CacheWrite + u.CacheWrite,
	}
}

// Spend is the cost and tokens of one session or the sum of several, for
// the live view and the status comment. It is comparable, so a Status that
// holds one still compares with ==.
type Spend struct {
	// Sessions is how many sessions it sums; zero for an action that never
	// had one, which adds nothing to a sum.
	Sessions int
	// Cost sums the costs of the WithCost sessions that reported one.
	Cost     float64
	WithCost int
	// Tokens sums the tokens of the WithTokens sessions that reported them.
	Tokens     Tokens
	WithTokens int
}

// Spend returns u as the spend of one session.
func (u Usage) Spend() Spend {
	s := Spend{Sessions: 1}
	if u.HasCost {
		s.Cost, s.WithCost = u.Cost, 1
	}
	if u.HasTokens {
		s.Tokens, s.WithTokens = u.Tokens, 1
	}
	return s
}

// Add returns s plus t.
func (s Spend) Add(t Spend) Spend {
	return Spend{
		Sessions: s.Sessions + t.Sessions,
		Cost:     s.Cost + t.Cost, WithCost: s.WithCost + t.WithCost,
		Tokens: s.Tokens.add(t.Tokens), WithTokens: s.WithTokens + t.WithTokens,
	}
}

// String words s as the live view and the status comment show it: the cost
// next to the tokens, a sum that misses some session's value marked as
// partial, and a value no session reported said so. It is "" when s sums
// no session.
func (s Spend) String() string {
	if s.Sessions == 0 {
		return ""
	}
	if s.WithCost == 0 && s.WithTokens == 0 {
		return "cost and tokens not reported"
	}
	cost := "cost not reported"
	if s.WithCost > 0 {
		cost = formatCost(s.Cost) + partial(s.WithCost, s.Sessions)
	}
	tokens := "tokens not reported"
	if s.WithTokens > 0 {
		tokens = formatTokens(s.Tokens.Total()) + " tokens" + partial(s.WithTokens, s.Sessions)
	}
	return cost + ", " + tokens
}

// partial marks a sum of with values out of sessions as partial when some
// session did not report its value.
func partial(with, sessions int) string {
	if with < sessions {
		return " (partial)"
	}
	return ""
}

// formatCost words a cost in US dollars with two decimals, as $12.40.
func formatCost(usd float64) string {
	return fmt.Sprintf("$%.2f", usd)
}

// formatTokens words a token count compactly: 950, 48.2K, 17.2M.
func formatTokens(n int64) string {
	switch {
	case n < 1_000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return compact(float64(n)/1_000) + "K"
	default:
		return compact(float64(n)/1_000_000) + "M"
	}
}

// compact formats f with one decimal, without a trailing ".0".
func compact(f float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0")
}

// PullRequestLookup is what came of looking up the pull request an action
// opened.
type PullRequestLookup int

// The outcomes of a pull request lookup. The zero value means crew did not
// look it up: its tracker cannot, or the lookup failed.
const (
	PullRequestNotLookedUp PullRequestLookup = iota
	// PullRequestNone: the tracker found no pull request from the branch.
	PullRequestNone
	// PullRequestFound: Ref and URL name the pull request.
	PullRequestFound
)

// PullRequest is the pull request an action opened, as its tracker found
// it from the action's branch.
type PullRequest struct {
	Lookup PullRequestLookup
	// Ref is how the tracker refers to it, such as #45; set when Lookup is
	// PullRequestFound.
	Ref string
	// URL is its web address; set when Lookup is PullRequestFound.
	URL string
}

// String words p: "pull request #45", "no pull request" or "pull request
// not looked up".
func (p PullRequest) String() string {
	switch p.Lookup {
	case PullRequestFound:
		return "pull request " + p.Ref
	case PullRequestNone:
		return "no pull request"
	default:
		return "pull request not looked up"
	}
}
