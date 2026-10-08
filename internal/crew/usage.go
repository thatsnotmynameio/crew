package crew

// Usage is what a harness reported a session used. Each value is optional:
// a harness that cannot tell leaves it out, so the value reads as not
// reported, never as zero.
type Usage struct {
	// Cost is the session's cost in US dollars, as the harness reports it.
	Cost Optional[float64]
	// Tokens is the session's token usage.
	Tokens Optional[Tokens]
	// Turns is how many turns the session took.
	Turns Optional[int]
	// Models names the models the session used, sorted; empty when the
	// harness does not tell.
	Models []string
	// ByModel is the session's tokens for each model it used, sorted by
	// model; empty when the harness does not tell tokens by model. Tokens
	// stays the session's sum.
	ByModel []ModelTokens
}

// ModelTokens is the tokens a session used with one model.
type ModelTokens struct {
	// Model is the model's name, as the harness reports it.
	Model  string
	Tokens Tokens
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
	if cost, ok := u.Cost.Get(); ok {
		s.Cost, s.WithCost = cost, 1
	}
	if tokens, ok := u.Tokens.Get(); ok {
		s.Tokens, s.WithTokens = tokens, 1
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

// PullRequest is the pull request an action opened, as its tracker found
// it from the action's branch: PullRequestNotLookedUp, PullRequestNone or
// PullRequestFound. A nil PullRequest is one crew did not look up.
//
//sumtype:decl
type PullRequest interface {
	pullRequest()
}

// PullRequestNotLookedUp is a pull request crew did not look up: its
// tracker cannot, or the lookup failed.
type PullRequestNotLookedUp struct{}

// PullRequestNone is a lookup that found no pull request from the branch.
type PullRequestNone struct{}

// PullRequestFound is the pull request a lookup found.
type PullRequestFound struct {
	// Ref is how the tracker refers to it, such as #45.
	Ref string
	// URL is its web address.
	URL string
}

func (PullRequestNotLookedUp) pullRequest() {}
func (PullRequestNone) pullRequest()        {}
func (PullRequestFound) pullRequest()       {}
