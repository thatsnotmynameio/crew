package worktrees

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// action is what clean does with a worktree.
type action int

// The actions, from the most kept to the most removed.
const (
	// keep keeps the worktree and its branch.
	keep action = iota
	// removeWorktree removes the worktree and keeps its branch.
	removeWorktree
	// removeAll removes the worktree and deletes its branch.
	removeAll
)

// runTime is how a run's start time is shown: in the boss's zone, with the
// day, since a run cut short may be days old.
const runTime = "2006-01-02 15:04"

// journal is what the run journal says: the start time of each workspace's
// unended run, or why it could not be read.
type journal struct {
	runs map[string]time.Time
	err  error
}

// decision is what clean does with one worktree, and why.
type decision struct {
	found  port.Found
	action action
	reason string
	// failed means a lookup, a check or a removal failed, so the worktree
	// or its branch was kept for want of an answer.
	failed bool
}

// decide applies the rule to the worktree f: it keeps it on the first local
// check it fails, and only then looks up its pull request, so a worktree
// kept for a local reason costs no lookup. It removes the worktree only when
// its pull request merged, and deletes its branch only when the branch has
// nothing after the merged head.
func decide(ctx context.Context, f port.Found, j journal, prs port.PullRequestFinder, sweeper port.Sweeper) decision {
	if d, kept := local(f, j); kept {
		return d
	}
	branch := f.Space.Branch
	pr, err := prs.FindPullRequest(ctx, branch, f.Created)
	switch {
	case err != nil:
		return keeping(f, "its pull request could not be looked up: "+err.Error(), true)
	case pr.Lookup != crew.PullRequestFound:
		return keeping(f, "no pull request from "+branch, false)
	case pr.State == crew.PullRequestOpen:
		return keeping(f, pr.String()+" is open", false)
	case pr.State == crew.PullRequestClosed:
		return keeping(f, pr.String()+" was closed without merging", false)
	case pr.State != crew.PullRequestMerged:
		return keeping(f, pr.String()+" is not known to be merged", false)
	}
	return merged(ctx, f, pr, sweeper)
}

// keeping keeps f and its branch, for reason; failed means the reason is a
// lookup, a check or a removal that failed.
func keeping(f port.Found, reason string, failed bool) decision {
	return decision{found: f, action: keep, reason: reason, failed: failed}
}

// local keeps f, with the reason, when a check that needs no lookup fails.
func local(f port.Found, j journal) (decision, bool) {
	kept := func(reason string, failed bool) (decision, bool) {
		return keeping(f, reason, failed), true
	}
	switch {
	case f.Gone:
		return kept("its folder is missing or git lists it as prunable: run git worktree prune", false)
	case !f.Listed:
		return kept("git does not list it as a worktree: run git worktree repair if the repository moved", false)
	case f.Space.Branch == "":
		return kept("its HEAD is detached: there is no branch to find a pull request from", false)
	case j.err != nil:
		return kept("crew's run journal cannot be read: "+j.err.Error(), true)
	}
	if at, ok := j.runs[f.Space.Name]; ok {
		return kept("a run started in it at "+at.Local().Format(runTime)+" and has not ended: "+
			"an action may be using it (if crew is not running, the run was cut short and you can remove it by hand)",
			false)
	}
	switch {
	case f.Dirty:
		return kept("it holds uncommitted changes or untracked files", false)
	case f.Created.IsZero():
		return kept("crew cannot tell when it was made, so it cannot find its pull request", false)
	}
	return decision{}, false
}

// merged removes f, whose pull request pr merged, and deletes its branch
// only when the branch has no commit after pr's head.
func merged(ctx context.Context, f port.Found, pr crew.PullRequest, sweeper port.Sweeper) decision {
	branch := f.Space.Branch
	d := decision{found: f, action: removeWorktree}
	n, err := sweeper.Beyond(ctx, branch, pr.Head)
	switch {
	case errors.Is(err, port.ErrCommitUnknown):
		d.reason = fmt.Sprintf("%s merged; its head commit is not in this repository, so %s stays", pr, branch)
	case err != nil:
		d.reason = fmt.Sprintf("%s merged; the commits of %s after its head cannot be counted, so it stays: %v",
			pr, branch, err)
		d.failed = true
	case n > 0:
		d.reason = fmt.Sprintf("%s merged; %s has %s after its head", pr, branch, count(n, "commit", "commits"))
	default:
		d.action = removeAll
		d.reason = fmt.Sprintf("%s merged; %s has nothing after its head", pr, branch)
	}
	return d
}

// again combines the decision shown in the list, first, with the one made
// just before removing, second: it acts only on what both allow, and says
// the worktree changed when the second allows less.
func again(first, second decision) decision {
	if second.action >= first.action {
		return first
	}
	second.reason = changed(second.reason)
	return second
}

// changed words a reason found only when checking a worktree again.
func changed(reason string) string {
	return "changed since the list: " + reason
}

// count words n things, as "1 commit" or "2 commits".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
