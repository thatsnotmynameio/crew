package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Comment implements port.Commenter: one new comment on the issue or pull
// request, posted as the writer, holding body without its control
// characters but with its lines (crew.StripControlsKeepingLines). Its errors
// are postComment's.
func (t *Tracker) Comment(ctx context.Context, id crew.IssueID, body string) error {
	if _, _, err := t.postComment(ctx, id.Key, crew.StripControlsKeepingLines(body)); err != nil {
		return fmt.Errorf("comment on issue #%s: %w", id.Key, err)
	}
	return nil
}

// postComment posts body as a new comment on the issue or pull request
// number, which GitHub's issue comments API serves alike, and returns the
// comment's id, which gh prints. Its errors are classified as on the issue
// itself: 404 or 410 is port.ErrMovedMeanwhile, a 403 that is not a rate
// limit is port.ErrRefused, and any other error is transient.
func (t *Tracker) postComment(ctx context.Context, number, body string) (int64, string, error) {
	out, login, err := t.gh.write(ctx, "api", "--method", "POST", "repos/{owner}/{repo}/issues/"+number+"/comments",
		"-f", "body="+body, "--jq", ".id")
	if err != nil {
		return 0, "", classify(err, out, true)
	}
	id, err := strconv.ParseInt(strings.TrimSpace(string(out.Stdout)), 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("gh printed no comment id: %w", err)
	}
	return id, login, nil
}
