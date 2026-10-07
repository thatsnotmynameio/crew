package github

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// pullRequestsQuery reads an issue's URL and repository, and the pull
// requests GitHub links as closing it, with their state, repository and
// labels. GitHub lists merged and closed ones too. The number may be a pull
// request's, which closes no issue: its fragment is empty.
const pullRequestsQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issueOrPullRequest(number: $number) {
      ... on Issue {
        url
        repository { nameWithOwner }
        closedByPullRequestsReferences(first: 100) {
          nodes {
            number
            state
            repository { nameWithOwner }
            labels(first: 100) { nodes { name } }
          }
        }
      }
    }
  }
}`

// pullRequest is an open pull request that closes an issue.
type pullRequest struct {
	number int
	labels []ghLabel
}

// ReportPullRequests implements port.PullRequestReporter. One GraphQL query
// finds the open pull requests in the issue's repository that GitHub links
// as closing it; a pull request crew moved closes none, so it gets only the
// query. Each, in number order, gets the swap Move makes, through gh pr edit,
// unless its only crew label is already report.State()'s; then, when the
// report has an end, the stop comment, unless this report's ID already
// posted it there. It writes every pull request even when one fails, and
// returns a transient error when any write failed transiently, so the report
// is retried, and otherwise the first refusal or moved-meanwhile error. A
// number GitHub cannot resolve is port.ErrMovedMeanwhile, and gh saying a
// label does not exist is a refusal, as in Move.
func (t *Tracker) ReportPullRequests(ctx context.Context, report crew.PullRequestReport) error {
	what := fmt.Sprintf("update the pull requests of issue #%s to %s", report.IssueID().Key, report.State())
	issueURL, prs, err := t.pullRequests(ctx, report.IssueID().Key)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	var errs writeErrors
	link := ""
	for _, pr := range prs {
		errs.add(t.mirror(ctx, pr, report.State()))
		end, ended := report.End().Get()
		if !ended || t.commented(report.ID(), pr.number) {
			continue
		}
		var err error
		if link == "" {
			link, err = t.statusLink(ctx, report.IssueID().Key, report.IssueRef(), issueURL)
		}
		if err == nil {
			err = t.postStop(ctx, report, end, pr.number, link)
		}
		errs.add(err)
	}
	if len(errs.transient) > 0 {
		return fmt.Errorf("%s: %w", what, errors.Join(errs.transient...))
	}
	t.forgetStops(report.ID())
	if errs.final != nil {
		return fmt.Errorf("%s: %w", what, errs.final)
	}
	return nil
}

// writeErrors sorts the errors of a report's writes: the transient ones, and
// the first refusal or moved-meanwhile error.
type writeErrors struct {
	transient []error
	final     error
}

// add sorts err, which may be nil.
func (w *writeErrors) add(err error) {
	switch {
	case err == nil:
	case errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused):
		if w.final == nil {
			w.final = err
		}
	default:
		w.transient = append(w.transient, err)
	}
}

// pullRequests returns the issue's URL and the open pull requests in its
// repository that close it, in number order. An issue GitHub cannot resolve
// is port.ErrMovedMeanwhile.
func (t *Tracker) pullRequests(ctx context.Context, issueKey string) (string, []pullRequest, error) {
	out, err := t.gh.call(ctx, "api", "graphql",
		"-f", "query="+pullRequestsQuery,
		"-F", "owner={owner}", "-F", "name={repo}",
		"-F", "number="+issueKey)
	if err != nil {
		if strings.Contains(string(out.Stderr), "Could not resolve to an issue or pull request") {
			return "", nil, fmt.Errorf("find its pull requests: %w: %w", port.ErrMovedMeanwhile, err)
		}
		return "", nil, fmt.Errorf("find its pull requests: %w", err)
	}
	var reply struct {
		Data struct {
			Repository struct {
				Issue struct {
					URL        string `json:"url"`
					Repository struct {
						NameWithOwner string `json:"nameWithOwner"`
					} `json:"repository"`
					Closing struct {
						Nodes []struct {
							Number     int    `json:"number"`
							State      string `json:"state"`
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
							Labels struct {
								Nodes []ghLabel `json:"nodes"`
							} `json:"labels"`
						} `json:"nodes"`
					} `json:"closedByPullRequestsReferences"`
				} `json:"issueOrPullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Stdout, &reply); err != nil {
		return "", nil, fmt.Errorf("find its pull requests: unreadable output: %w", err)
	}
	issue := reply.Data.Repository.Issue
	var prs []pullRequest
	for _, n := range issue.Closing.Nodes {
		// gh pr edit and the comment work on crew's repository, so a pull
		// request elsewhere would name another one with the same number.
		if n.State == stateOpen && n.Repository.NameWithOwner == issue.Repository.NameWithOwner {
			prs = append(prs, pullRequest{number: n.Number, labels: n.Labels.Nodes})
		}
	}
	slices.SortFunc(prs, func(a, b pullRequest) int { return cmp.Compare(a.number, b.number) })
	return issue.URL, prs, nil
}

// mirror puts pr in to with the swap Move makes, through one gh pr edit as
// the writer, and edits nothing when to's is already its only crew label.
func (t *Tracker) mirror(ctx context.Context, pr pullRequest, to crew.State) error {
	remove, states := t.swap(pr.labels, to)
	if len(remove) == 0 && slices.Equal(states, []crew.State{to}) {
		return nil
	}
	if err := t.editLabels(ctx, "pr", strconv.Itoa(pr.number), remove, to); err != nil {
		return fmt.Errorf("edit pull request #%d: %w", pr.number, err)
	}
	return nil
}

// statusLink returns the stop comment's last line: a link to the issue's
// status comment, cached or found among the issue's comments, or to the
// issue itself when it has none.
func (t *Tracker) statusLink(ctx context.Context, issueKey, issueRef, issueURL string) (string, error) {
	c, ok := t.statusComment(issueKey)
	if !ok {
		var err error
		if c, ok, err = t.findStatus(ctx, issueKey); err != nil {
			return "", fmt.Errorf("list the issue's comments: %w", err)
		}
	}
	if !ok {
		return fmt.Sprintf("See [%s](%s).", issueRef, issueURL), nil
	}
	return fmt.Sprintf("%s's [status comment](%s#issuecomment-%d) has the details.", issueRef, issueURL, c.id), nil
}

// postStop posts the stop comment of report, which has end, on the pull
// request number, ending with link, and remembers it under the report's ID.
func (t *Tracker) postStop(
	ctx context.Context, report crew.PullRequestReport, end crew.RuleEnd, number int, link string,
) error {
	if _, _, err := t.postComment(ctx, strconv.Itoa(number), renderStop(report, end, link)); err != nil {
		return fmt.Errorf("comment on pull request #%d: %w", number, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped[report.ID()] = append(t.stopped[report.ID()], number)
	return nil
}

// commented reports whether the report with id already posted its stop
// comment on the pull request number.
func (t *Tracker) commented(id crew.PullRequestReportID, number int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Contains(t.stopped[id], number)
}

// forgetStops drops what the report with id posted, once no retry of it
// will come.
func (t *Tracker) forgetStops(id crew.PullRequestReportID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.stopped, id)
}
