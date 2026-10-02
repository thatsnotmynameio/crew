package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// statusMarker is the last line of every status comment, hidden by GitHub's
// Markdown, by which ReportStatus finds the comment again after a restart.
const statusMarker = "<!-- crew:status -->"

// errCommentGone means the cached status comment no longer exists, as when
// someone deleted it.
var errCommentGone = errors.New("the status comment is gone")

// ReportStatus implements port.StatusReporter. It edits the issue's status
// comment, which it remembers by issue key; without one, it lists the
// issue's comments and takes the newest one by the authenticated gh user
// whose body ends with the marker line, and creates the comment when there
// is none. An edit of a comment that is gone forgets it, then looks for the
// comment again or creates it, once. The issue gone (HTTP 404 or 410 on
// listing or creating) is port.ErrMovedMeanwhile, a refusal (HTTP 403, such
// as a locked issue, but not a rate limit) is port.ErrRefused, and any other
// error is transient.
func (t *Tracker) ReportStatus(ctx context.Context, status crew.Status) error {
	body := t.renderStatus(status)
	err := t.writeStatus(ctx, status.IssueKey, body)
	if errors.Is(err, errCommentGone) {
		err = t.writeStatus(ctx, status.IssueKey, body)
	}
	if err != nil {
		return fmt.Errorf("report status on issue #%s: %w", status.IssueKey, err)
	}
	return nil
}

// writeStatus writes body to the issue's status comment, finding or creating
// it first when its id is not cached. An edit answered with HTTP 404 forgets
// the id and returns an error wrapping errCommentGone.
func (t *Tracker) writeStatus(ctx context.Context, issueKey, body string) error {
	id, ok := t.statusComment(issueKey)
	if !ok {
		var err error
		if id, ok, err = t.findStatus(ctx, issueKey); err != nil {
			return fmt.Errorf("list the issue's comments: %w", err)
		}
		if !ok {
			return t.createStatus(ctx, issueKey, body)
		}
		t.rememberStatus(issueKey, id)
	}
	out, err := t.gh.call(ctx, "api", "--method", "PATCH", fmt.Sprintf("repos/{owner}/{repo}/issues/comments/%d", id),
		"-f", "body="+body)
	if err != nil {
		if httpStatus(string(out.Stderr)) == 404 {
			t.forgetStatus(issueKey)
			return fmt.Errorf("edit comment %d: %w: %w", id, errCommentGone, err)
		}
		return fmt.Errorf("edit comment %d: %w", id, classify(err, out, false))
	}
	return nil
}

// createStatus creates the issue's status comment with body and caches its
// id, which gh prints.
func (t *Tracker) createStatus(ctx context.Context, issueKey, body string) error {
	out, err := t.gh.call(ctx, "api", "--method", "POST", "repos/{owner}/{repo}/issues/"+issueKey+"/comments",
		"-f", "body="+body, "--jq", ".id")
	if err != nil {
		return fmt.Errorf("create the status comment: %w", classify(err, out, true))
	}
	id, err := strconv.ParseInt(strings.TrimSpace(string(out.Stdout)), 10, 64)
	if err != nil {
		return fmt.Errorf("create the status comment: gh printed no comment id: %w", err)
	}
	t.rememberStatus(issueKey, id)
	return nil
}

// findStatus lists the issue's comments and returns the id of the newest
// one by the authenticated gh user whose body ends with the marker line, and
// whether there is one.
func (t *Tracker) findStatus(ctx context.Context, issueKey string) (int64, bool, error) {
	login, err := t.gh.viewer(ctx)
	if err != nil {
		return 0, false, err
	}
	out, err := t.gh.call(ctx, "api", "--method", "GET", "--paginate",
		"repos/{owner}/{repo}/issues/"+issueKey+"/comments?per_page=100")
	if err != nil {
		return 0, false, classify(err, out, true)
	}
	// --paginate prints the pages' arrays one after the other.
	var newest int64
	dec := json.NewDecoder(bytes.NewReader(out.Stdout))
	for {
		var page []struct {
			ID   int64 `json:"id"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
			Body string `json:"body"`
		}
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, false, fmt.Errorf("unreadable output: %w", err)
		}
		for _, c := range page {
			if c.User.Login == login && c.ID > newest &&
				strings.HasSuffix(strings.TrimRight(c.Body, " \t\r\n"), statusMarker) {
				newest = c.ID
			}
		}
	}
	return newest, newest != 0, nil
}

// statusComment returns the cached id of the issue's status comment.
func (t *Tracker) statusComment(issueKey string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id, ok := t.comments[issueKey]
	return id, ok
}

// rememberStatus caches id as the issue's status comment.
func (t *Tracker) rememberStatus(issueKey string, id int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.comments[issueKey] = id
}

// forgetStatus drops the issue's cached status comment.
func (t *Tracker) forgetStatus(issueKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.comments, issueKey)
}

// httpCode finds the HTTP status gh api prints on failure, as in
// "gh: Not Found (HTTP 404)".
var httpCode = regexp.MustCompile(`\bHTTP (\d{3})\b`)

// httpStatus returns the HTTP status in gh's stderr, or 0 without one.
func httpStatus(stderr string) int {
	m := httpCode.FindStringSubmatch(stderr)
	if m == nil {
		return 0
	}
	code, _ := strconv.Atoi(m[1])
	return code
}

// classify wraps err, from a gh api call, with the port error its HTTP
// status means: 404 or 410, when onIssue says the call was on the issue
// itself, mean the issue is gone, and 403 is a refusal unless GitHub is rate
// limiting, which passes. Any other error is returned as is, transient.
func classify(err error, out proc.Output, onIssue bool) error {
	stderr := string(out.Stderr)
	switch code := httpStatus(stderr); {
	case onIssue && (code == 404 || code == 410):
		return fmt.Errorf("%w: %w", port.ErrMovedMeanwhile, err)
	case code == 403 && !strings.Contains(strings.ToLower(stderr), "rate limit"):
		return fmt.Errorf("%w: %w", port.ErrRefused, err)
	}
	return err
}

// renderStatus renders a status as the status comment's Markdown: what the
// stage does, each action with its state, the update time in UTC, then the
// marker line. A session's last words go in a fenced code block, so nothing
// in them may render, link or mention anyone.
func (t *Tracker) renderStatus(s crew.Status) string {
	var b strings.Builder
	stage := codeSpan(s.Stage)
	switch s.Kind {
	case crew.StatusQueued:
		fmt.Fprintf(&b, "crew: %s is queued for %s, waiting for a free slot: crew runs at most %s at once.\n",
			s.IssueRef, stage, plural(s.Slots, "issue"))
	case crew.StatusRunning:
		fmt.Fprintf(&b, "crew: %s is running on %s.\n", stage, s.IssueRef)
	case crew.StatusEnded:
		fmt.Fprintf(&b, "crew: %s ended on %s.\n", stage, s.IssueRef)
	}
	for _, a := range s.Actions {
		name := "**" + codeSpan(a.Name) + "**"
		switch {
		case a.State == crew.ActionSucceeded:
			fmt.Fprintf(&b, "\n%s succeeded.\n", name)
		case a.State == crew.ActionFailed:
			fmt.Fprintf(&b, "\n%s failed.\n", name)
		case a.Started.IsZero():
			fmt.Fprintf(&b, "\n%s is running.\n", name)
		default:
			fmt.Fprintf(&b, "\n%s has been running for %s.", name, elapsed(s.Updated.Sub(a.Started)))
			if a.Said == "" {
				b.WriteString("\n")
				break
			}
			fence := strings.Repeat("`", max(3, longestBacktickRun(a.Said)+1))
			fmt.Fprintf(&b, " It last said:\n\n%stext\n%s\n%s\n", fence, a.Said, fence)
		}
	}
	if s.Kind == crew.StatusEnded {
		to := codeSpan(string(s.To))
		switch s.Move {
		case crew.MovePending:
			fmt.Fprintf(&b, "\n%s is moving to %s.\n", s.IssueRef, to)
		case crew.MoveDone:
			fmt.Fprintf(&b, "\n%s moved to %s.\n", s.IssueRef, to)
		case crew.MoveDropped:
			fmt.Fprintf(&b, "\ncrew could not move it to %s.\n", to)
		}
	}
	fmt.Fprintf(&b, "\nUpdated %s UTC.\n\n%s\n", s.Updated.UTC().Format("2006-01-02 15:04"), statusMarker)
	return b.String()
}

// elapsed renders d in whole minutes, as "less than a minute", "42 minutes"
// or "1 hour 5 minutes".
func elapsed(d time.Duration) string {
	minutes := int(d / time.Minute)
	if minutes < 1 {
		return "less than a minute"
	}
	var parts []string
	if h := minutes / 60; h > 0 {
		parts = append(parts, plural(h, "hour"))
	}
	if m := minutes % 60; m > 0 {
		parts = append(parts, plural(m, "minute"))
	}
	return strings.Join(parts, " ")
}

// plural renders n of unit, as "1 hour" or "2 hours".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
