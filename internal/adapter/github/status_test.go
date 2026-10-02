package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The scripted prefixes of the status comment's gh calls on issue #74.
var (
	listComments  = []string{"api", "--method", "GET", "--paginate", "repos/{owner}/{repo}/issues/74/comments?per_page=100"}
	createComment = []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/74/comments"}
	editComment   = []string{"api", "--method", "PATCH"}
)

// updated is the update time of the statuses below.
var updated = time.Date(2026, 10, 2, 14, 3, 27, 0, time.UTC)

// commentJSON is one issue comment as the REST API lists it.
func commentJSON(id int, login, body string) string {
	return fmt.Sprintf(`{"id":%d,"user":{"login":%q},"body":%q}`, id, login, body)
}

// statusBody returns the body field of a recorded comment call.
func statusBody(t *testing.T, args []string) string {
	t.Helper()
	bodies := fieldValues(args, "body")
	if len(bodies) != 1 {
		t.Fatalf("call %q has %d body fields, want 1", args, len(bodies))
	}
	return bodies[0]
}

// endsWithMarker reports whether body's last line is the status marker.
func endsWithMarker(body string) bool {
	return strings.HasSuffix(strings.TrimRight(body, " \t\r\n"), "\n"+statusMarker)
}

func queued74() crew.Status {
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusQueued, Slots: 2, Updated: updated}
}

func TestFirstStatusCreatesTheCommentAndTheNextEditsIt(t *testing.T) {
	tr, gh := build(t, "", login,
		reply{prefix: listComments, stdout: "[" + commentJSON(5, "me", "Thanks!") + "]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment},
	)
	for range 2 {
		if err := tr.ReportStatus(context.Background(), queued74()); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}

	if n := len(gh.callsTo(listComments...)); n != 1 {
		t.Errorf("listed the comments %d times, want 1", n)
	}
	creates := gh.callsTo(createComment...)
	if len(creates) != 1 {
		t.Fatalf("created %d comments, want 1", len(creates))
	}
	if body := statusBody(t, creates[0]); !endsWithMarker(body) {
		t.Errorf("created body does not end with the marker line:\n%s", body)
	}
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/101") {
		t.Fatalf("edits = %q, want one of comment 101", edits)
	}
	if body := statusBody(t, edits[0]); !endsWithMarker(body) {
		t.Errorf("edited body does not end with the marker line:\n%s", body)
	}
}

// Covers AE5: after a restart, the tracker finds the comment it wrote before.
func TestARestartedTrackerEditsTheViewersNewestStatusComment(t *testing.T) {
	marked := "crew: an older status.\n\n" + statusMarker + "\n"
	// Two pages, as gh --paginate prints them one after the other.
	page1 := "[" + strings.Join([]string{
		commentJSON(7, "someone-else", marked),
		commentJSON(9, "me", marked),
	}, ",") + "]"
	page2 := "[" + strings.Join([]string{
		commentJSON(12, "me", marked),
		commentJSON(15, "me", "Quoting "+statusMarker+" is not a status."),
		commentJSON(20, "someone-else", marked),
	}, ",") + "]"
	tr, gh := build(t, "", login,
		reply{prefix: listComments, stdout: page1 + "\n" + page2 + "\n"},
		reply{prefix: editComment},
	)
	if err := tr.ReportStatus(context.Background(), queued74()); err != nil {
		t.Fatalf("ReportStatus: %v", err)
	}
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/12") {
		t.Errorf("edits = %q, want one of comment 12", edits)
	}
	if creates := gh.callsTo(createComment...); len(creates) != 0 {
		t.Errorf("created %d comments, want none", len(creates))
	}
}

func TestAnEditOfADeletedCommentCreatesItAgain(t *testing.T) {
	tr, gh := build(t, "", login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment, stderr: "gh: Not Found (HTTP 404)\n"},
	)
	for range 2 {
		if err := tr.ReportStatus(context.Background(), queued74()); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if n := len(gh.callsTo(editComment...)); n != 1 {
		t.Errorf("edited %d times, want 1", n)
	}
	if n := len(gh.callsTo(listComments...)); n != 2 {
		t.Errorf("listed the comments %d times, want 2: once more after the 404", n)
	}
	if n := len(gh.callsTo(createComment...)); n != 2 {
		t.Errorf("created %d comments, want 2: once more after the 404", n)
	}
}

func TestStatusErrorsAreClassifiedFromTheHTTPStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		list, create reply
		want         error // nil: transient
	}{
		"issue gone on listing": {
			list: reply{prefix: listComments, stderr: "gh: Not Found (HTTP 404)\n"},
			want: port.ErrMovedMeanwhile,
		},
		"issue gone on create": {
			create: reply{prefix: createComment, stderr: "gh: Not Found (HTTP 404)\n"},
			want:   port.ErrMovedMeanwhile,
		},
		"issue deleted on create": {
			create: reply{prefix: createComment, stderr: "gh: This issue was deleted (HTTP 410)\n"},
			want:   port.ErrMovedMeanwhile,
		},
		"issue locked": {
			create: reply{prefix: createComment, stderr: "gh: Unable to create comment because issue is locked. (HTTP 403)\n"},
			want:   port.ErrRefused,
		},
		"rate limited": {
			create: reply{prefix: createComment, stderr: "gh: You have exceeded a secondary rate limit. (HTTP 403)\n"},
		},
		"network failure": {
			create: reply{prefix: createComment, stderr: "error connecting to api.github.com\n"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			list, create := tc.list, tc.create
			if list.prefix == nil {
				list = reply{prefix: listComments, stdout: "[]"}
			}
			if create.prefix == nil {
				create = reply{prefix: createComment, stdout: "101\n"}
			}
			tr, _ := build(t, "", login, list, create)
			err := tr.ReportStatus(context.Background(), queued74())
			if err == nil || !strings.Contains(err.Error(), "issue #74") {
				t.Fatalf("ReportStatus = %v, want an error naming issue #74", err)
			}
			for _, sentinel := range []error{port.ErrMovedMeanwhile, port.ErrRefused} {
				if got, want := errors.Is(err, sentinel), errors.Is(tc.want, sentinel); got != want {
					t.Errorf("errors.Is(%v, %v) = %t, want %t", err, sentinel, got, want)
				}
			}
		})
	}
}

// running74 is #74 in implement with one action, lfg, running since started
// and having said said.
func running74(started time.Time, said string) crew.Status {
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusRunning,
		Actions: []crew.ActionStatus{{Name: "lfg", State: crew.ActionRunning, Started: started, Said: said}},
		Updated: updated}
}

// Covers AE6: what a session said can neither render nor mention anyone.
func TestASessionsWordsAreFencedAsText(t *testing.T) {
	tr, _ := build(t, "")
	said := "ask @someone why ```make``` failed in ./internal/core/update.go"
	body := tr.renderStatus(running74(updated.Add(-time.Minute), said))
	if !slices.Contains(strings.Split(body, "\n"), "````text") {
		t.Errorf("no ````text fence:\n%s", body)
	}
	if blocks := fenced(t, body); !slices.Equal(blocks, []string{said}) {
		t.Errorf("fenced blocks = %q, want [%q]:\n%s", blocks, said, body)
	}
	if n := strings.Count(body, "@someone"); n != 1 {
		t.Errorf("@someone appears %d times, want once, inside its fence:\n%s", n, body)
	}
}

// Covers AE2.
func TestARunningStatusShowsTheStageElapsedTimeAndLastWords(t *testing.T) {
	tr, _ := build(t, "")
	said := "U1 committed: 168 tests pass. Starting U2."
	body := tr.renderStatus(running74(updated.Add(-42*time.Minute-10*time.Second), said))
	for _, want := range []string{"`implement`", "`lfg`", "running", "42 minutes", "Updated 2026-10-02 14:03 UTC."} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q:\n%s", want, body)
		}
	}
	if blocks := fenced(t, body); !slices.Equal(blocks, []string{said}) {
		t.Errorf("fenced blocks = %q, want [%q]:\n%s", blocks, said, body)
	}
	if !endsWithMarker(body) {
		t.Errorf("body does not end with the marker line:\n%s", body)
	}

	body = tr.renderStatus(running74(updated.Add(-40*time.Second), said))
	if !strings.Contains(body, "less than a minute") {
		t.Errorf("an action started 40s ago does not show less than a minute:\n%s", body)
	}
}

// KTD8: an action whose session has not started yet is running, with no time
// and no words.
func TestARunningActionWithoutASessionShowsNoTimeAndNoWords(t *testing.T) {
	tr, _ := build(t, "")
	body := tr.renderStatus(running74(time.Time{}, ""))
	if !strings.Contains(body, "`lfg`** is running.") {
		t.Errorf("body does not show lfg running:\n%s", body)
	}
	if strings.Contains(body, " for ") || len(fenced(t, body)) != 0 {
		t.Errorf("body shows a time or words for an action without a session:\n%s", body)
	}
}

func TestElapsedTimeIsInWholeMinutes(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Second:                    "less than a minute",
		59 * time.Second:                "less than a minute",
		time.Minute:                     "1 minute",
		42*time.Minute + 59*time.Second: "42 minutes",
		time.Hour:                       "1 hour",
		time.Hour + 5*time.Minute:       "1 hour 5 minutes",
		2*time.Hour + 30*time.Second:    "2 hours",
		3*time.Hour + time.Minute:       "3 hours 1 minute",
		26*time.Hour + 10*time.Minute:   "26 hours 10 minutes",
	} {
		if got := elapsed(d); got != want {
			t.Errorf("elapsed(%v) = %q, want %q", d, got, want)
		}
	}
}

// Covers AE1.
func TestAQueuedStatusNamesTheStageAndTheLimit(t *testing.T) {
	tr, _ := build(t, "")
	body := tr.renderStatus(queued74())
	for _, want := range []string{"`implement`", "free slot", "at most 2 issues at once", "Updated 2026-10-02 14:03 UTC."} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q:\n%s", want, body)
		}
	}
	if !endsWithMarker(body) {
		t.Errorf("body does not end with the marker line:\n%s", body)
	}

	one := queued74()
	one.Slots = 1
	if body := tr.renderStatus(one); !strings.Contains(body, "at most 1 issue at once") {
		t.Errorf("body does not say at most 1 issue at once:\n%s", body)
	}
}

// Covers AE3 and AE4.
func TestAnEndedStatusShowsEachActionAndTheMove(t *testing.T) {
	tr, _ := build(t, "")
	status := crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded,
		Actions: []crew.ActionStatus{
			{Name: "development", State: crew.ActionFailed},
			{Name: "acceptance", State: crew.ActionSucceeded},
		},
		To: needsAttention, Updated: updated}
	for move, want := range map[crew.MoveProgress]string{
		crew.MovePending: "moving to `needs attention`",
		crew.MoveDone:    "moved to `needs attention`",
		crew.MoveDropped: "could not move it to `needs attention`",
	} {
		status.Move = move
		body := tr.renderStatus(status)
		for _, want := range []string{"`implement`", "**`development`** failed", "**`acceptance`** succeeded", want} {
			if !strings.Contains(body, want) {
				t.Errorf("move %d: body does not contain %q:\n%s", move, want, body)
			}
		}
		if !endsWithMarker(body) {
			t.Errorf("move %d: body does not end with the marker line:\n%s", move, body)
		}
	}
}
