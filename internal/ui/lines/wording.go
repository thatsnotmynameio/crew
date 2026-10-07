package lines

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// SpendParts words s as the live view shows it: the cost, then the tokens,
// a sum that misses some session's value marked as partial, and a value no
// session reported said so. It is one part when neither was reported, and
// nothing when s sums no session.
func SpendParts(s crew.Spend) []string {
	if s.Sessions == 0 {
		return nil
	}
	if s.WithCost == 0 && s.WithTokens == 0 {
		return []string{"cost and tokens not reported"}
	}
	cost := "cost not reported"
	if s.WithCost > 0 {
		cost = fmt.Sprintf("$%.2f", s.Cost) + partial(s.WithCost, s.Sessions)
	}
	count := "tokens not reported"
	if s.WithTokens > 0 {
		count = tokens(s.Tokens.Total()) + " tokens" + partial(s.WithTokens, s.Sessions)
	}
	return []string{cost, count}
}

// KindName names k: "issue", "pull request", or "unknown" for any other
// value.
func KindName(k crew.Kind) string {
	switch k {
	case crew.KindIssue:
		return "issue"
	case crew.KindPullRequest:
		return "pull request"
	}
	return "unknown"
}

// partial marks a sum of with values out of sessions as partial when some
// session did not report its value.
func partial(with, sessions int) string {
	if with < sessions {
		return " (partial)"
	}
	return ""
}

// thousand and million are the steps of tokens' K and M.
const (
	thousand = 1_000
	million  = 1_000_000
)

// tokens words a token count compactly: 950, 48.2K, 17.2M.
func tokens(n int64) string {
	switch {
	case n < thousand:
		return strconv.FormatInt(n, 10)
	case n < million:
		return compact(float64(n)/thousand) + "K"
	default:
		return compact(float64(n)/million) + "M"
	}
}

// compact formats f with one decimal, without a trailing ".0".
func compact(f float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0")
}
