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
	listed, err := t.listComments(ctx, id.Key)
	if err != nil {
		return nil, fmt.Errorf("list the comments of issue #%s: %w", id.Key, err)
	}
	var comments []crew.Comment
	for _, c := range listed {
		comments = append(comments, crew.Comment{
			Author: c.User.Login, App: c.User.Type == appType, Body: c.Body, Created: c.CreatedAt,
		})
	}
	return comments, nil
}

// ghComment is an issue comment as GitHub's REST API lists it.
type ghComment struct {
	ID   int64 `json:"id"`
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// listComments reads every comment on the issue or pull request issueKey, as
// you, oldest first. A failed call is classified as postComment's is.
func (t *Tracker) listComments(ctx context.Context, issueKey string) ([]ghComment, error) {
	out, err := t.gh.call(ctx, "api", "--method", "GET", "--paginate",
		"repos/{owner}/{repo}/issues/"+issueKey+"/comments?per_page=100")
	if err != nil {
		return nil, classify(err, out, true)
	}
	var comments []ghComment
	// --paginate prints the pages' arrays one after the other.
	dec := json.NewDecoder(bytes.NewReader(out.Stdout))
	for {
		var page []ghComment
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			return comments, nil
		}
		if err != nil {
			return nil, fmt.Errorf("unreadable output: %w", err)
		}
		comments = append(comments, page...)
	}
}
