package github

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// usage words what an ended action's session spent and the pull request it
// opened, after a space, as in " Usage: $12.40, 17.2M tokens. Pull request:
// [#45](url).", or returns "" when its status shows none. These are crew's
// own figures and the tracker's link, never the session's words.
func usage(shown crew.Optional[crew.ShownUsage]) string {
	u, ok := shown.Get()
	if !ok {
		return ""
	}
	return " Usage: " + spendText(u.Spend) + ". Pull request: " + pullRequestText(u.PullRequest) + "."
}

// pullRequestText words the pull request an action opened for the usage
// sentence: a link to it, "none", or "not looked up".
func pullRequestText(pr crew.PullRequest) string {
	switch pr := pr.(type) {
	case crew.PullRequestFound:
		return "[" + pr.Ref + "](" + pr.URL + ")"
	case crew.PullRequestNone:
		return "none"
	case crew.PullRequestNotLookedUp:
	}
	return "not looked up"
}

// spendText words a spend for the usage sentence: the cost, then the
// tokens, each marked partial when some session did not report it, as in
// "$12.40 (partial), 17.2M tokens", or "cost and tokens not reported".
func spendText(s crew.Spend) string {
	if s.WithCost == 0 && s.WithTokens == 0 {
		return "cost and tokens not reported"
	}
	return reported("cost", fmt.Sprintf("$%.2f", s.Cost), s.WithCost, s.Sessions) + ", " +
		reported("tokens", tokenCount(s.Tokens.Total())+" tokens", s.WithTokens, s.Sessions)
}

// reported is value when every one of sessions reported it, value marked
// partial when only with of them did, and what "not reported" when none did.
func reported(what, value string, with, sessions int) string {
	switch {
	case with == 0:
		return what + " not reported"
	case with < sessions:
		return value + " (partial)"
	}
	return value
}

// kilo and mega are the sizes of tokenCount's K and M.
const (
	kilo = 1_000
	mega = 1_000_000
)

// tokenCount words a token count compactly, with at most one decimal: 950,
// 48.2K, 17.2M.
func tokenCount(n int64) string {
	var size int64
	var unit string
	switch {
	case n >= mega:
		size, unit = mega, "M"
	case n >= kilo:
		size, unit = kilo, "K"
	default:
		return strconv.FormatInt(n, 10)
	}
	return strings.TrimSuffix(strconv.FormatFloat(float64(n)/float64(size), 'f', 1, 64), ".0") + unit
}
