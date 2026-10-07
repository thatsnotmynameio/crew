package crew

// Usage is what a harness reported a session used. Each value is optional:
// a harness that cannot tell leaves its Has field false, so the value reads
// as not reported, never as zero.
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

// Spend is the cost and tokens of one session or the sum of several, with
// how many of the sessions reported each. It is comparable, so a Status that
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
