package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

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
