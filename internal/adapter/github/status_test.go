package github

import (
	"context"
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
	listComments = []string{"api", "--method", "GET", "--paginate",
		"repos/{owner}/{repo}/issues/74/comments?per_page=100"}
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
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusQueued, Slots: 2,
		Updated: updated}
}

func TestFirstStatusCreatesTheCommentAndTheNextEditsIt(t *testing.T) {
	tr, gh := build(t, login,
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
	tr, gh := build(t, login,
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
	tr, gh := build(t, login,
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
			list := orReply(tc.list, reply{prefix: listComments, stdout: "[]"})
			create := orReply(tc.create, reply{prefix: createComment, stdout: "101\n"})
			tr, _ := build(t, login, list, create)
			err := tr.ReportStatus(context.Background(), queued74())
			if err == nil || !strings.Contains(err.Error(), "issue #74") {
				t.Fatalf("ReportStatus = %v, want an error naming issue #74", err)
			}
			wantClassified(t, err, tc.want)
		})
	}
}

// orReply returns r, or fallback when r scripts nothing.
func orReply(r, fallback reply) reply {
	if r.prefix == nil {
		return fallback
	}
	return r
}
