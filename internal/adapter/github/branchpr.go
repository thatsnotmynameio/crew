package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// ghPullRequest is a pull request as gh pr list prints it in JSON.
type ghPullRequest struct {
	Number    int       `json:"number"`
	URL       string    `json:"url"`
	State     string    `json:"state"` // OPEN, CLOSED or MERGED
	CreatedAt time.Time `json:"createdAt"`
	CrossRepo bool      `json:"isCrossRepository"`
	Head      string    `json:"headRefOid"` // the head commit's id
}

// state is p's state in crew's terms; one gh names that crew does not know
// is unknown.
func (p ghPullRequest) state() crew.PullRequestState {
	switch p.State {
	case stateOpen:
		return crew.PullRequestOpen
	case "CLOSED":
		return crew.PullRequestClosed
	case "MERGED":
		return crew.PullRequestMerged
	default:
		return crew.PullRequestStateUnknown
	}
}

// newer reports whether p was created after q, the higher number breaking
// a tie.
func (p ghPullRequest) newer(q ghPullRequest) bool {
	if !p.CreatedAt.Equal(q.CreatedAt) {
		return p.CreatedAt.After(q.CreatedAt)
	}
	return p.Number > q.Number
}

// FindPullRequest implements port.PullRequestFinder with one gh pr list call:
// the pull requests whose head is branch, in any state, keeping only those
// from this repository, as a fork's branch may share the name. The newest
// open one wins; otherwise the newest closed or merged one created at or
// after since, as git reuses a branch name whose old pull request must not
// count; otherwise there is none. A zero since accepts any. The one found
// carries its state and head commit.
func (t *Tracker) FindPullRequest(ctx context.Context, branch string, since time.Time) (crew.PullRequest, error) {
	out, err := t.gh.call(ctx, "pr", "list", "--head="+branch, "--state=all", "--limit", "100",
		"--json", "number,url,state,createdAt,isCrossRepository,headRefOid")
	if err != nil {
		return crew.PullRequest{}, fmt.Errorf("find the pull request from %s: %w", branch, classify(err, out, false))
	}
	var prs []ghPullRequest
	if err := json.Unmarshal(out.Stdout, &prs); err != nil {
		return crew.PullRequest{}, fmt.Errorf("find the pull request from %s: unreadable output: %w", branch, err)
	}
	var open, ended *ghPullRequest
	for i := range prs {
		p := &prs[i]
		switch {
		case p.CrossRepo:
		case p.State == stateOpen:
			if open == nil || p.newer(*open) {
				open = p
			}
		case !p.CreatedAt.Before(since):
			if ended == nil || p.newer(*ended) {
				ended = p
			}
		}
	}
	found := open
	if found == nil {
		found = ended
	}
	if found == nil {
		return crew.PullRequest{Lookup: crew.PullRequestNone}, nil
	}
	return crew.PullRequest{
		Lookup: crew.PullRequestFound, Ref: "#" + strconv.Itoa(found.Number), URL: found.URL,
		State: found.state(), Head: found.Head,
	}, nil
}
