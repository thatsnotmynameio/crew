package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// stackOf returns the stack a rule with stacking on takes issue, a blocked
// issue, on, and whether it may take it (KTD4 of #446): the open pull
// request of its only blocker, once crew is done with that blocker, when
// nothing but the issue's siblings stacks there (R2, R5, R6). Without a
// run journal, crew cannot tell how the blocker's run ended, and no issue
// stacks (KTD10).
func (m *Model) stackOf(issue crew.Issue) (crew.Stack, bool) {
	if m.journal == nil {
		return crew.Stack{}, false
	}
	reading := issue.Data().Stacking
	b, ok := reading.Blocker.Get()
	if !ok || !m.blockerDone(b) {
		return crew.Stack{}, false
	}
	pr, ok := m.ownPullRequest(b)
	if !ok || !m.mayStackOn(issue.ID(), reading.Siblings, pr) {
		return crew.Stack{}, false
	}
	return crew.Stack{
		Blocker: b.ID, BlockerRef: b.Ref, BlockerURL: b.URL,
		PullRequestRef: pr.Ref, PullRequestURL: pr.URL, Branch: pr.Head,
	}, true
}

// blockerDone reports whether crew is done with b: it holds no run of b,
// b is in no rule's ready or running label, and crew's latest run of b
// that worked it, if any, ended through passed (R2). A rule without
// actions, such as crew's question rule, or whose only action checks an
// answer, as crew's answered rule does, passes while b's work goes on, so
// its runs do not count (KTD4).
func (m *Model) blockerDone(b crew.StackBlocker) bool {
	if m.held(b.ID) != nil {
		return false
	}
	var working []crew.RuleName
	for _, r := range m.rules {
		if slices.Contains(b.States, r.Labels.Ready) || slices.Contains(b.States, r.Labels.Running) {
			return false
		}
		if works(r) {
			working = append(working, r.Name)
		}
	}
	last, ok := m.journal.history.LatestRun(b.ID, working...)
	if !ok {
		return true
	}
	released, _ := last.Phase().(crew.ReleasedPhase)
	route, ok := released.Route.Get()
	return ok && route.Route == crew.PassedRoute
}

// works reports whether r works the issues it takes: it has an action
// other than the check of an answer.
func works(r crew.Rule) bool {
	return slices.ContainsFunc(r.Actions, func(a crew.Action) bool {
		_, checks := a.Kind.(crew.ReturnSpec)
		return !checks
	})
}

// ownPullRequest returns b's own open pull request, and whether it has
// exactly one, not a draft (R2): one a person connected to b, or one from
// the branch of a crew run of b (KTD3).
func (m *Model) ownPullRequest(b crew.StackBlocker) (crew.StackPullRequest, bool) {
	branches := m.journal.history.Branches(b.ID)
	var own []crew.StackPullRequest
	for _, pr := range b.PullRequests {
		if pr.Connected || slices.Contains(branches, pr.Head) {
			own = append(own, pr)
		}
	}
	if len(own) != 1 || own[0].Draft {
		return crew.StackPullRequest{}, false
	}
	return own[0], true
}

// mayStackOn reports whether the issue identified by id may stack on pr:
// every other issue stacked there, through a held run or an open pull
// request based on its branch, is one of siblings, the sub-issues of the
// issue's parent (R5, R6). A pull request from a branch no crew run made
// is another issue's (KTD5).
func (m *Model) mayStackOn(id crew.IssueID, siblings []crew.IssueID, pr crew.StackPullRequest) bool {
	var others []crew.IssueID
	for _, h := range m.issues {
		if s, ok := h.run.Stack().Get(); ok && s.Branch == pr.Head {
			others = append(others, h.id())
		}
	}
	for _, branch := range pr.Stacked {
		other, ok := m.journal.history.IssueOfBranch(branch)
		if !ok {
			return false
		}
		others = append(others, other)
	}
	for _, other := range others {
		if other != id && !slices.Contains(siblings, other) {
			return false
		}
	}
	return true
}
