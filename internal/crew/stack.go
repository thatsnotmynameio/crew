package crew

import "slices"

// StackReading is what a tracker read of a blocked issue to decide whether
// a rule that stacks takes it on its blocker's open pull request: the
// issue's only blocker and the parent's sub-issues. The zero StackReading
// is an issue no tracker read for stacking. It is not journaled.
type StackReading struct {
	// Blocker is the open issue that blocks this one, when exactly one
	// does; none when none or several do.
	Blocker Optional[StackBlocker]
	// Siblings are the sub-issues of the issue's parent as the tracker
	// lists them now, the issue among them; none without a parent.
	Siblings []IssueID
}

// StackBlocker is an issue's only blocker, in its StackReading.
type StackBlocker struct {
	// ID, Ref and URL identify the blocker, as in Issue.
	ID  IssueID
	Ref string
	URL string
	// States are the crew states its labels name.
	States []State
	// PullRequests are the open pull requests linked to it.
	PullRequests []StackPullRequest
}

// StackPullRequest is an open pull request linked to a blocker, in its
// StackBlocker.
type StackPullRequest struct {
	// Ref and URL identify the pull request, such as "#45".
	Ref string
	URL string
	// Head is the branch it comes from, and Base the branch it merges
	// into.
	Head string
	Base string
	// Draft says whether it is a draft.
	Draft bool
	// Connected says whether a person connected it to the blocker, as
	// GitHub's Development sidebar does.
	Connected bool
	// Stacked are the head branches of the open pull requests based on its
	// branch.
	Stacked []string
}

// clone returns a copy of s that shares no list with it.
func (s StackReading) clone() StackReading {
	s.Siblings = slices.Clone(s.Siblings)
	if b, ok := s.Blocker.Get(); ok {
		b.States = slices.Clone(b.States)
		b.PullRequests = slices.Clone(b.PullRequests)
		for i, p := range b.PullRequests {
			b.PullRequests[i].Stacked = slices.Clone(p.Stacked)
		}
		s.Blocker = Some(b)
	}
	return s
}

// Stack is a run's take on its blocker's open pull request: the run's
// workspace is made from the pull request's branch, and its work goes on
// top of it.
type Stack struct {
	// Blocker, BlockerRef and BlockerURL identify the issue that blocks
	// the run's issue.
	Blocker    IssueID
	BlockerRef string
	BlockerURL string
	// PullRequestRef and PullRequestURL identify the blocker's pull
	// request.
	PullRequestRef string
	PullRequestURL string
	// Branch is the pull request's head branch, the run's base.
	Branch string
}
