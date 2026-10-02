package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
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

// The status comment holds one entry per stage run, oldest first, each
// starting with a hidden marker line that names its run, kind and stage.
// Entries are separated by a horizontal rule, and a marker counts only at
// the start of the comment, after a continuation preamble, or right after a
// separator, so a marker-shaped line inside an entry is entry text.
const (
	entryMarker     = "<!-- crew:entry "
	entrySeparator  = "\n\n---\n\n"
	continuesMarker = "<!-- crew:continues -->"
	// maxCommentBytes is GitHub's limit on a comment's body, in characters,
	// which bytes never undercount.
	maxCommentBytes = 65536
)

// errCommentGone means the cached status comment no longer exists, as when
// someone deleted it.
var errCommentGone = errors.New("the status comment is gone")

// cachedStatus is an issue's status comment as crew last wrote or read it.
type cachedStatus struct {
	id   int64
	body string
}

// entry is one entry of a status comment: its text, verbatim, and what its
// marker line says. A comment written before entries is one entry without a
// marker.
type entry struct {
	text             string
	marked           bool
	run, kind, stage string
}

// ReportStatus implements port.StatusReporter. It keeps one entry per stage
// run in the issue's status comment: it replaces the latest entry when that
// entry is of status's run, or is the queued entry of status's stage, and
// appends one otherwise. A latest entry still running from another run, as
// when crew stopped before that run ended, first becomes one line saying so.
// When the edit would make the comment longer than GitHub allows, it leaves
// the comment as it is and creates a new one that continues it, holding only
// the new entry.
//
// It remembers each issue's comment, by issue key, with the body it last
// wrote; without one, it lists the issue's comments and takes the newest one
// by the authenticated gh user whose body ends with the marker line, and
// creates the comment when there is none. An edit of a comment that is gone
// forgets it, then looks for the comment again or creates it, once. The
// issue gone (HTTP 404 or 410 on listing or creating) is
// port.ErrMovedMeanwhile, a refusal (HTTP 403, such as a locked issue, but
// not a rate limit) is port.ErrRefused, and any other error is transient.
func (t *Tracker) ReportStatus(ctx context.Context, status crew.Status) error {
	text := t.renderStatus(status)
	err := t.writeStatus(ctx, status, text)
	if errors.Is(err, errCommentGone) {
		err = t.writeStatus(ctx, status, text)
	}
	if err != nil {
		return fmt.Errorf("report status on issue #%s: %w", status.IssueKey, err)
	}
	return nil
}

// writeStatus writes text, status's entry, to the issue's status comment,
// finding or creating the comment first when it is not cached. It caches the
// comment and its body only once the write succeeded. An edit answered with
// HTTP 404 forgets the comment and returns an error wrapping errCommentGone.
func (t *Tracker) writeStatus(ctx context.Context, status crew.Status, text string) error {
	issueKey := status.IssueKey
	c, ok := t.statusComment(issueKey)
	if !ok {
		var err error
		if c, ok, err = t.findStatus(ctx, issueKey); err != nil {
			return fmt.Errorf("list the issue's comments: %w", err)
		}
		if !ok {
			return t.createStatus(ctx, issueKey, joinStatus("", []string{text}))
		}
	}
	body, continues := nextStatus(c.body, status, text)
	if continues {
		return t.createStatus(ctx, issueKey, body)
	}
	out, err := t.gh.call(ctx, "api", "--method", "PATCH", fmt.Sprintf("repos/{owner}/{repo}/issues/comments/%d", c.id),
		"-f", "body="+body)
	if err != nil {
		if httpStatus(string(out.Stderr)) == 404 {
			t.forgetStatus(issueKey)
			return fmt.Errorf("edit comment %d: %w: %w", c.id, errCommentGone, err)
		}
		return fmt.Errorf("edit comment %d: %w", c.id, classify(err, out, false))
	}
	t.rememberStatus(issueKey, cachedStatus{id: c.id, body: body})
	return nil
}

// createStatus creates a status comment on the issue with body and caches
// it, with its id, which gh prints.
func (t *Tracker) createStatus(ctx context.Context, issueKey, body string) error {
	id, err := t.postComment(ctx, issueKey, body)
	if err != nil {
		return fmt.Errorf("create the status comment: %w", err)
	}
	t.rememberStatus(issueKey, cachedStatus{id: id, body: body})
	return nil
}

// findStatus lists the issue's comments and returns the newest one by the
// authenticated gh user whose body ends with the marker line, and whether
// there is one.
func (t *Tracker) findStatus(ctx context.Context, issueKey string) (cachedStatus, bool, error) {
	login, err := t.gh.viewer(ctx)
	if err != nil {
		return cachedStatus{}, false, err
	}
	out, err := t.gh.call(ctx, "api", "--method", "GET", "--paginate",
		"repos/{owner}/{repo}/issues/"+issueKey+"/comments?per_page=100")
	if err != nil {
		return cachedStatus{}, false, classify(err, out, true)
	}
	// --paginate prints the pages' arrays one after the other.
	var newest cachedStatus
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
			return cachedStatus{}, false, fmt.Errorf("unreadable output: %w", err)
		}
		for _, c := range page {
			if c.User.Login == login && c.ID > newest.id &&
				strings.HasSuffix(strings.TrimRight(c.Body, " \t\r\n"), statusMarker) {
				newest = cachedStatus{id: c.ID, body: c.Body}
			}
		}
	}
	return newest, newest.id != 0, nil
}

// statusComment returns the issue's cached status comment.
func (t *Tracker) statusComment(issueKey string) (cachedStatus, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.comments[issueKey]
	return c, ok
}

// rememberStatus caches c as the issue's status comment.
func (t *Tracker) rememberStatus(issueKey string, c cachedStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.comments[issueKey] = c
}

// forgetStatus drops the issue's cached status comment.
func (t *Tracker) forgetStatus(issueKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.comments, issueKey)
}

// nextStatus returns the body the status comment holding current gets for
// status, whose entry is text, and whether that body is instead a new
// comment's, continuing the current one because the edit would be too long.
// A comment holding only the new entry is edited whatever its length, as a
// new comment would be no shorter.
func nextStatus(current string, status crew.Status, text string) (string, bool) {
	preamble, entries := parseStatus(current)
	var latest *entry
	if n := len(entries); n > 0 && entries[n-1].marked {
		latest = &entries[n-1]
	}
	switch {
	case latest != nil && (latest.run == status.Run ||
		latest.kind == kindName(crew.StatusQueued) && latest.stage == status.Stage):
		latest.text = text
	default:
		if latest != nil && latest.kind == kindName(crew.StatusRunning) {
			marker, _, _ := strings.Cut(latest.text, "\n")
			latest.text = fmt.Sprintf("%s\ncrew stopped following %s on %s before it ended.",
				marker, codeSpan(latest.stage), status.IssueRef)
		}
		entries = append(entries, entry{text: text})
	}
	texts := make([]string, len(entries))
	for i, e := range entries {
		texts[i] = e.text
	}
	body := joinStatus(preamble, texts)
	if len(body) > maxCommentBytes && len(entries) > 1 {
		preamble = fmt.Sprintf("%s\ncrew: this comment continues crew's earlier status comment on %s, which is full.\n\n",
			continuesMarker, status.IssueRef)
		return joinStatus(preamble, []string{text}), true
	}
	return body, false
}

// joinStatus returns a status comment's body: the preamble, the entries'
// texts between separators, then the marker line.
func joinStatus(preamble string, texts []string) string {
	return preamble + strings.Join(texts, entrySeparator) + "\n\n" + statusMarker + "\n"
}

// parseStatus splits a status comment's body into its continuation
// preamble, if any, and its entries, oldest first. Text before the first
// entry marker, as in a comment written before entries, is one unmarked
// entry; the marked entries crew appended after it still split off.
func parseStatus(body string) (string, []entry) {
	rest := strings.TrimRight(body, " \t\r\n")
	rest = strings.TrimRight(strings.TrimSuffix(rest, statusMarker), " \t\r\n")
	var preamble string
	if strings.HasPrefix(rest, continuesMarker+"\n") {
		if i := strings.Index(rest, "\n\n"); i >= 0 {
			preamble, rest = rest[:i+2], rest[i+2:]
		}
	}
	if rest == "" {
		return preamble, nil
	}
	var entries []entry
	for {
		end := len(rest)
		for from := 0; ; {
			i := strings.Index(rest[from:], entrySeparator)
			if i < 0 {
				break
			}
			if _, ok := parseMarker(rest[from+i+len(entrySeparator):]); ok {
				end = from + i
				break
			}
			from += i + 1
		}
		e, _ := parseMarker(rest)
		e.text = rest[:end]
		entries = append(entries, e)
		if end == len(rest) {
			return preamble, entries
		}
		rest = rest[end+len(entrySeparator):]
	}
}

// parseMarker reads the entry marker on s's first line, and reports whether
// that line is one.
func parseMarker(s string) (entry, bool) {
	line, _, _ := strings.Cut(s, "\n")
	fields, ok := strings.CutPrefix(line, entryMarker)
	if !ok {
		return entry{}, false
	}
	if fields, ok = strings.CutSuffix(fields, " -->"); !ok {
		return entry{}, false
	}
	e := entry{marked: true}
	for _, f := range strings.Fields(fields) {
		key, value, ok := strings.Cut(f, "=")
		if !ok {
			return entry{}, false
		}
		value, err := url.QueryUnescape(value)
		if err != nil {
			return entry{}, false
		}
		switch key {
		case "run":
			e.run = value
		case "kind":
			e.kind = value
		case "stage":
			e.stage = value
		default:
			return entry{}, false
		}
	}
	return e, true
}

// markerLine returns the entry marker of status. Its values are
// query-escaped, so none can hold a space or close the HTML comment.
func markerLine(s crew.Status) string {
	return fmt.Sprintf("%srun=%s kind=%s stage=%s -->",
		entryMarker, url.QueryEscape(s.Run), kindName(s.Kind), url.QueryEscape(s.Stage))
}

// kindName names a status kind in an entry marker.
func kindName(k crew.StatusKind) string {
	switch k {
	case crew.StatusQueued:
		return "queued"
	case crew.StatusRunning:
		return "running"
	default:
		return "ended"
	}
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

// renderStatus renders a status as its entry in the status comment, in
// Markdown: the entry's marker line, what the stage does, each action with
// its state and, when it resumed, its worktree, then the update time in
// UTC. A session's last words go in a fenced code block, so nothing in them
// may render, link or mention anyone. A failed action says why in crew's
// words, from its cause; only a failed check's reason shows, in a code
// span, as no session's or tool's own words may.
func (t *Tracker) renderStatus(s crew.Status) string {
	var b strings.Builder
	b.WriteString(markerLine(s) + "\n")
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
		// A resumed action's line names its worktree: "**`lfg`** resumed in
		// worktree `issue-9-lfg` and failed." A fresh one reads "**`lfg`**
		// failed."
		name, and := "**"+codeSpan(a.Name)+"**", ""
		if a.Workspace != "" {
			name += " resumed in worktree " + codeSpan(a.Workspace)
			and = " and"
		}
		switch {
		case a.State == crew.ActionSucceeded:
			fmt.Fprintf(&b, "\n%s%s succeeded.\n", name, and)
		case a.State == crew.ActionFailed:
			fmt.Fprintf(&b, "\n%s%s failed%s.", name, and, failureCause(a))
			if a.Log == "" {
				b.WriteString(" It failed before it had a log.\n")
				break
			}
			fmt.Fprintf(&b, " Its log is %s.\n", codeSpan(a.Log))
		case a.Started.IsZero():
			fmt.Fprintf(&b, "\n%s%s is running.\n", name, and)
		default:
			fmt.Fprintf(&b, "\n%s%s has been running for %s.", name, and, elapsed(s.Updated.Sub(a.Started)))
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
	fmt.Fprintf(&b, "\nUpdated %s UTC.", s.Updated.UTC().Format("2006-01-02 15:04"))
	return b.String()
}

// failureCause words what made a failed action fail, after a colon, or
// returns "" for an action without a cause.
func failureCause(a crew.ActionStatus) string {
	switch a.Cause {
	case crew.CauseSession:
		return ": its session failed"
	case crew.CauseCheck:
		// The reason already says the check failed, ran out of time or
		// could not start.
		if a.Reason == "" {
			return ": its check failed"
		}
		return ": " + codeSpan(a.Reason)
	case crew.CauseStopped:
		return ": crew stopped it"
	case crew.CauseWorkspace:
		return ": its workspace could not be created"
	case crew.CauseStart:
		return ": its session could not start"
	case crew.CausePrompt:
		return ": its prompt did not render"
	}
	return ""
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
