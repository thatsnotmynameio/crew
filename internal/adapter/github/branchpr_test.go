package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// prList is the prefix of the one gh call a lookup makes.
var prList = []string{"pr", "list"}

// prJSON is one pull request as gh pr list prints it.
func prJSON(number int, state, created string, crossRepo bool) string {
	return fmt.Sprintf(`{"number":%d,"url":"https://github.com/o/r/pull/%d","state":%q,`+
		`"createdAt":%q,"isCrossRepository":%t}`,
		number, number, state, created, crossRepo)
}

// worktreeMade is when the action's worktree was made, in the tests below.
var worktreeMade = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// find looks up the pull request from branch, with gh pr list printing the
// pull requests prs, and checks that it took exactly one gh call, which it
// returns.
func find(t *testing.T, branch string, since time.Time, prs ...string) (crew.PullRequest, []string) {
	t.Helper()
	tr, gh := build(t, reply{prefix: prList, stdout: "[" + strings.Join(prs, ",") + "]"})
	pr, err := tr.FindPullRequest(context.Background(), branch, since)
	if err != nil {
		t.Fatalf("FindPullRequest = %v, want no error", err)
	}
	if len(gh.calls) != 1 {
		t.Fatalf("gh calls = %q, want exactly one", gh.calls)
	}
	return pr, gh.calls[0]
}

func TestAnOpenPullRequestFromTheBranchIsFound(t *testing.T) {
	pr, _ := find(t, "crew/issue-31-lfg", worktreeMade, prJSON(45, "OPEN", "2026-10-02T12:30:00Z", false))
	want := crew.PullRequestFound{Ref: "#45", URL: "https://github.com/o/r/pull/45"}
	if pr != want {
		t.Errorf("FindPullRequest = %+v, want %+v", pr, want)
	}
}

func TestNoPullRequestFromTheBranchIsNone(t *testing.T) {
	pr, _ := find(t, "crew/issue-9-lfg", worktreeMade)
	if pr != (crew.PullRequestNone{}) {
		t.Errorf("FindPullRequest = %+v, want none", pr)
	}
}

func TestTheLookupListsEveryPullRequestFromTheBranchAsGiven(t *testing.T) {
	_, call := find(t, "crew/issue-9-lfg-2", worktreeMade)
	for _, arg := range []string{"--head=crew/issue-9-lfg-2", "--state=all"} {
		if !slices.Contains(call, arg) {
			t.Errorf("gh call %q lacks %s", call, arg)
		}
	}
	i := slices.Index(call, "--json")
	if i < 0 || i+1 == len(call) {
		t.Fatalf("gh call %q asks for no JSON fields", call)
	}
	fields := strings.Split(call[i+1], ",")
	for _, f := range []string{"number", "url", "state", "createdAt", "isCrossRepository"} {
		if !slices.Contains(fields, f) {
			t.Errorf("gh call %q does not ask for %s", call, f)
		}
	}
}

func TestAnOpenPullRequestWinsOverANewerMergedOne(t *testing.T) {
	pr, _ := find(t, "crew/issue-31-lfg", worktreeMade,
		prJSON(46, "MERGED", "2026-10-02T14:00:00Z", false),
		prJSON(45, "OPEN", "2026-10-02T13:00:00Z", false))
	if found, _ := pr.(crew.PullRequestFound); found.Ref != "#45" {
		t.Errorf("FindPullRequest = %+v, want the open #45", pr)
	}
}

func TestTheNewestOpenPullRequestWins(t *testing.T) {
	pr, _ := find(t, "crew/issue-31-lfg", worktreeMade,
		prJSON(45, "OPEN", "2026-10-02T13:00:00Z", false),
		prJSON(47, "OPEN", "2026-10-02T15:00:00Z", false),
		prJSON(46, "OPEN", "2026-10-02T14:00:00Z", false))
	if found, _ := pr.(crew.PullRequestFound); found.Ref != "#47" {
		t.Errorf("FindPullRequest = %+v, want the newest open #47", pr)
	}
}

func TestOnlyAPullRequestMergedBeforeTheWorktreeIsNone(t *testing.T) {
	pr, _ := find(t, "crew/issue-31-lfg", worktreeMade, prJSON(12, "MERGED", "2026-09-20T10:00:00Z", false))
	if pr != (crew.PullRequestNone{}) {
		t.Errorf("FindPullRequest = %+v, want none", pr)
	}
}

func TestTheNewestClosedOrMergedPullRequestSinceTheWorktreeIsFound(t *testing.T) {
	pr, _ := find(t, "crew/issue-31-lfg", worktreeMade,
		prJSON(12, "MERGED", "2026-09-20T10:00:00Z", false),
		prJSON(45, "CLOSED", "2026-10-02T12:00:00Z", false),
		prJSON(46, "MERGED", "2026-10-02T13:00:00Z", false))
	want := crew.PullRequestFound{Ref: "#46", URL: "https://github.com/o/r/pull/46"}
	if pr != want {
		t.Errorf("FindPullRequest = %+v, want %+v", pr, want)
	}
}

func TestAResumedWorktreeAcceptsAnyMergedPullRequest(t *testing.T) {
	pr, _ := find(t, "crew/issue-31-lfg", time.Time{}, prJSON(12, "MERGED", "2026-09-20T10:00:00Z", false))
	if found, _ := pr.(crew.PullRequestFound); found.Ref != "#12" {
		t.Errorf("FindPullRequest = %+v, want #12", pr)
	}
}

func TestACrossRepositoryPullRequestIsNotFromTheBranch(t *testing.T) {
	pr, _ := find(t, "crew/issue-31-lfg", worktreeMade, prJSON(45, "OPEN", "2026-10-02T13:00:00Z", true))
	if pr != (crew.PullRequestNone{}) {
		t.Errorf("FindPullRequest = %+v, want none", pr)
	}
}

func TestAFailedLookupIsAnError(t *testing.T) {
	tr, gh := build(t, reply{prefix: prList, stderr: "HTTP 502: Bad Gateway"})
	pr, err := tr.FindPullRequest(context.Background(), "crew/issue-31-lfg", worktreeMade)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502: Bad Gateway") {
		t.Errorf("FindPullRequest = %v, want an error carrying gh's stderr", err)
	}
	if pr != nil {
		t.Errorf("FindPullRequest = %+v, want nil", pr)
	}
	if len(gh.calls) != 1 {
		t.Errorf("gh calls = %q, want exactly one", gh.calls)
	}
}
