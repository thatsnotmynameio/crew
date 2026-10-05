package fakegithub

import (
	"cmp"
	"slices"
)

// Issue returns the issue number as it is now, and false when the
// repository has no issue with that number (a pull request's number
// included).
func (g *GitHub) Issue(number int) (Issue, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	it := g.items[number]
	if it == nil || it.pull != nil {
		return Issue{}, false
	}
	return Issue{Number: it.number, Title: it.title, Author: it.author, Labels: slices.Clone(it.labels),
		CreatedAt: it.createdAt, State: it.state, Priority: it.priority, BlockedBy: slices.Clone(it.blockedBy)}, true
}

// PullRequest returns the pull request number as it is now, and false when
// the repository has no pull request with that number.
func (g *GitHub) PullRequest(number int) (PullRequest, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	it := g.items[number]
	if it == nil || it.pull == nil {
		return PullRequest{}, false
	}
	return it.pullRequest(), true
}

// PullRequests returns every pull request of the repository, in number
// order.
func (g *GitHub) PullRequests() []PullRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	var prs []PullRequest
	for _, it := range g.items {
		if it.pull != nil {
			prs = append(prs, it.pullRequest())
		}
	}
	slices.SortFunc(prs, func(a, b PullRequest) int { return cmp.Compare(a.Number, b.Number) })
	return prs
}

// pullRequest returns it, a pull request, as a PullRequest.
func (it *item) pullRequest() PullRequest {
	return PullRequest{Number: it.number, Title: it.title, HeadBranch: it.pull.head, State: it.state,
		Author: it.author, Labels: slices.Clone(it.labels), CrossRepository: it.pull.cross,
		Closes: slices.Clone(it.pull.closes), CreatedAt: it.createdAt}
}

// Comments returns the comments on the issue or pull request number, oldest
// first, with the text they hold now.
func (g *GitHub) Comments(number int) []Comment {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []Comment
	for _, c := range g.comments {
		if c.number == number {
			out = append(out, Comment{ID: c.id, Author: c.author, Body: c.body})
		}
	}
	return out
}

// Labels returns the names of the repository's labels, in the order they
// were created.
func (g *GitHub) Labels() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.labels)
}
