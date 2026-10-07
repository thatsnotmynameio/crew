package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// appType is the type GitHub's REST API gives the account of a GitHub App.
const appType = "Bot"

// Comments implements port.CommentLister: every comment on the issue or pull
// request, read as you through the REST API, oldest first, as GitHub lists
// them. An author of type Bot is an App. Its errors are classified as
// postComment's are.
func (t *Tracker) Comments(ctx context.Context, id crew.IssueID) ([]crew.Comment, error) {
	what := "list the comments of issue #" + id.Key
	out, err := t.gh.call(ctx, "api", "--method", "GET", "--paginate",
		"repos/{owner}/{repo}/issues/"+id.Key+"/comments?per_page=100")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, classify(err, out, true))
	}
	var comments []crew.Comment
	// --paginate prints the pages' arrays one after the other.
	dec := json.NewDecoder(bytes.NewReader(out.Stdout))
	for {
		var page []struct {
			User struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"user"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
		}
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			return comments, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: unreadable output: %w", what, err)
		}
		for _, c := range page {
			comments = append(comments, crew.Comment{
				Author: c.User.Login, App: c.User.Type == appType, Body: c.Body, Created: c.CreatedAt,
			})
		}
	}
}
