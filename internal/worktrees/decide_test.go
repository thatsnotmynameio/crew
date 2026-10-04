package worktrees

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// created is when the worktrees of these tests were made.
var created = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// worktree returns a clean, listed worktree named name on crew/<name>.
func worktree(name string) port.Found {
	return port.Found{
		Space:   port.Space{Name: name, Dir: "/repo/.crew/worktrees/" + name, Branch: "crew/" + name},
		Listed:  true,
		Tip:     "tip-" + name,
		Created: created,
	}
}

// pullRequest is a lookup that finds pull request ref in state, with head
// as its head commit.
func pullRequest(ref string, state crew.PullRequestState) fake.LookupScript {
	return fake.LookupScript{Found: crew.PullRequest{
		Lookup: crew.PullRequestFound, Ref: ref, URL: "https://github.com/o/r/pull/" + ref[1:],
		State: state, Head: "head-" + ref[1:],
	}}
}

// decideCase is one worktree's facts and what the rule makes of them.
type decideCase struct {
	name       string
	found      port.Found
	journal    journal
	lookup     *fake.LookupScript
	beyond     int
	beyondErr  error
	want       action
	wantReason string
	wantFailed bool
	wantLookup bool
}

func (c decideCase) run(t *testing.T) {
	t.Helper()
	prs := &fake.PullRequests{}
	if c.lookup != nil {
		prs.ScriptLookup(c.found.Space.Branch, *c.lookup)
	}
	ws := fake.NewWorkspace(t.TempDir())
	ws.ScriptBeyond(c.found.Space.Branch, c.beyond, c.beyondErr)

	got := decide(context.Background(), c.found, c.journal, prs, ws)

	if got.action != c.want || got.reason != c.wantReason || got.failed != c.wantFailed {
		t.Fatalf("decision: got %v %q failed %v; want %v %q failed %v",
			got.action, got.reason, got.failed, c.want, c.wantReason, c.wantFailed)
	}
	if got.found.Space != c.found.Space {
		t.Fatalf("decision's worktree: got %#v, want %#v", got.found.Space, c.found.Space)
	}
	lookups := prs.Lookups()
	if !c.wantLookup {
		if len(lookups) != 0 {
			t.Fatalf("lookups: got %#v, want none", lookups)
		}
		return
	}
	want := fake.Lookup{Branch: c.found.Space.Branch, Since: c.found.Created}
	if len(lookups) != 1 || lookups[0] != want {
		t.Fatalf("lookups: got %#v, want one %#v", lookups, want)
	}
}

// changedWorktree returns the worktree issue-42-development changed by edit.
func changedWorktree(edit func(*port.Found)) port.Found {
	f := worktree("issue-42-development")
	edit(&f)
	return f
}

func TestTheRuleKeepsAWorktreeGitCannotWorkWithWithoutALookup(t *testing.T) {
	merged := new(pullRequest("#45", crew.PullRequestMerged))
	tests := []decideCase{
		{
			name: "its folder is gone", lookup: merged, want: keep,
			found:      changedWorktree(func(f *port.Found) { f.Gone, f.Dirty = true, true }),
			wantReason: "its folder is missing or git lists it as prunable: run git worktree prune",
		},
		{
			name: "git does not list it", lookup: merged, want: keep,
			found:      changedWorktree(func(f *port.Found) { f.Listed, f.Space.Branch, f.Tip = false, "", "" }),
			wantReason: "git does not list it as a worktree: run git worktree repair if the repository moved",
		},
		{
			name: "its HEAD is detached", want: keep,
			found:      changedWorktree(func(f *port.Found) { f.Space.Branch, f.Tip = "", "" }),
			wantReason: "its HEAD is detached: there is no branch to find a pull request from",
		},
	}
	for _, c := range tests {
		t.Run(c.name, c.run)
	}
}

func TestTheRuleKeepsAWorktreeInUseOrHoldingWorkWithoutALookup(t *testing.T) {
	merged := new(pullRequest("#45", crew.PullRequestMerged))
	dirty := changedWorktree(func(f *port.Found) { f.Dirty = true })
	started := time.Date(2026, 10, 3, 14, 2, 0, 0, time.Local)
	tests := []decideCase{
		{
			name: "AE10: a run started in it and has not ended, before it is dirty", found: dirty, lookup: merged,
			journal: journal{runs: map[string]time.Time{"issue-42-development": started}}, want: keep,
			wantReason: "a run started in it at 2026-10-03 14:02 and has not ended: an action may be using it " +
				"(if crew is not running, the run was cut short and you can remove it by hand)",
		},
		{
			name: "the run journal cannot be read", found: worktree("issue-42-development"), lookup: merged,
			journal: journal{err: errors.New("permission denied")}, want: keep, wantFailed: true,
			wantReason: "crew's run journal cannot be read: permission denied",
		},
		{
			name: "AE4: it holds uncommitted files", found: dirty, lookup: merged, want: keep,
			journal:    journal{runs: map[string]time.Time{"issue-7-development": started}},
			wantReason: "it holds uncommitted changes or untracked files",
		},
		{
			name: "when it was made is unknown", lookup: merged, want: keep,
			found:      changedWorktree(func(f *port.Found) { f.Created = time.Time{} }),
			wantReason: "crew cannot tell when it was made, so it cannot find its pull request",
		},
	}
	for _, c := range tests {
		t.Run(c.name, c.run)
	}
}

func TestTheRuleKeepsAWorktreeWhosePullRequestIsNotMerged(t *testing.T) {
	tests := []decideCase{
		{
			name: "AE9: the lookup fails", found: worktree("issue-42-development"),
			lookup: &fake.LookupScript{Err: errors.New("gh: not logged in")}, want: keep, wantFailed: true,
			wantReason: "its pull request could not be looked up: " +
				"find the pull request from crew/issue-42-development: gh: not logged in",
			wantLookup: true,
		},
		{
			name: "AE3: no pull request", found: worktree("issue-34-triage"), want: keep,
			wantReason: "no pull request from crew/issue-34-triage", wantLookup: true,
		},
		{
			name: "AE2: an open pull request", found: worktree("issue-42-development"),
			lookup: new(pullRequest("#47", crew.PullRequestOpen)), want: keep,
			wantReason: "pull request #47 is open", wantLookup: true,
		},
		{
			name: "a pull request closed without merging", found: worktree("issue-42-development"),
			lookup: new(pullRequest("#47", crew.PullRequestClosed)), want: keep,
			wantReason: "pull request #47 was closed without merging", wantLookup: true,
		},
		{
			name: "a pull request in a state crew does not know", found: worktree("issue-42-development"),
			lookup: new(pullRequest("#47", crew.PullRequestStateUnknown)), want: keep,
			wantReason: "pull request #47 is not known to be merged", wantLookup: true,
		},
	}
	for _, c := range tests {
		t.Run(c.name, c.run)
	}
}

func TestTheRuleRemovesAMergedWorktreeAndItsBranchOnlyWithNothingAfterTheMergedHead(t *testing.T) {
	merged := new(pullRequest("#45", crew.PullRequestMerged))
	tests := []decideCase{
		{
			name: "AE1: nothing after the head", found: worktree("issue-42-development"), lookup: merged,
			want:       removeAll,
			wantReason: "pull request #45 merged; crew/issue-42-development has nothing after its head",
			wantLookup: true,
		},
		{
			name: "AE5: one commit after the head", found: worktree("issue-43-development"), lookup: merged,
			beyond: 1, want: removeWorktree,
			wantReason: "pull request #45 merged; crew/issue-43-development has 1 commit after its head",
			wantLookup: true,
		},
		{
			name: "three commits after the head", found: worktree("issue-43-development"), lookup: merged,
			beyond: 3, want: removeWorktree,
			wantReason: "pull request #45 merged; crew/issue-43-development has 3 commits after its head",
			wantLookup: true,
		},
		{
			name: "the head is not in this repository", found: worktree("issue-43-development"), lookup: merged,
			beyondErr: fmt.Errorf("git: %w", port.ErrCommitUnknown), want: removeWorktree,
			wantReason: "pull request #45 merged; its head commit is not in this repository, " +
				"so crew/issue-43-development stays",
			wantLookup: true,
		},
		{
			name: "the commits after the head cannot be counted", found: worktree("issue-43-development"),
			lookup: merged, beyondErr: errors.New("git: broken"), want: removeWorktree, wantFailed: true,
			wantReason: "pull request #45 merged; the commits of crew/issue-43-development after its head " +
				"cannot be counted, so it stays: count the commits of crew/issue-43-development after head-45: " +
				"git: broken",
			wantLookup: true,
		},
	}
	for _, c := range tests {
		t.Run(c.name, c.run)
	}
}

func TestCheckingAgainActsOnlyOnWhatBothDecisionsAllow(t *testing.T) {
	f := worktree("issue-42-development")
	all := decision{found: f, action: removeAll, reason: "all"}
	worktreeOnly := decision{found: f, action: removeWorktree, reason: "worktree only"}
	kept := decision{found: f, action: keep, reason: "kept"}
	tests := []struct {
		name          string
		first, second decision
		want          action
		wantReason    string
	}{
		{"both remove everything", all, all, removeAll, "all"},
		{"the list kept the branch", worktreeOnly, all, removeWorktree, "worktree only"},
		{"the branch gained a commit", all, worktreeOnly, removeWorktree, "changed since the list: worktree only"},
		{"the worktree changed", all, kept, keep, "changed since the list: kept"},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			got := again(c.first, c.second)
			if got.action != c.want || got.reason != c.wantReason {
				t.Fatalf("got %v %q, want %v %q", got.action, got.reason, c.want, c.wantReason)
			}
		})
	}
}
