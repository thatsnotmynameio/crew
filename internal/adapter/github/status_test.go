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

func TestFirstStatusCreatesTheCommentAndTheNextEditsIt(t *testing.T) {
	tr, gh := build(t, login,
		reply{prefix: listComments, stdout: "[" + commentJSON(5, "me", "Thanks!") + "]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment},
	)
	for range 2 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
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
	if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
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

// wantTrailer fails the test unless body ends with crew's marker line then
// the status marker line, and holds each marker once.
func wantTrailer(t *testing.T, body string) {
	t.Helper()
	if !strings.HasSuffix(body, "\n\n"+crew.PostedMarker+"\n"+statusMarker+"\n") ||
		strings.Count(body, crew.PostedMarker) != 1 || strings.Count(body, statusMarker) != 1 {
		t.Errorf("body =\n%s\nwant crew's marker once, on the line before the status marker, once and last", body)
	}
}

// KTD-W2: a new status comment, every edit of it, and the edit after a
// restart, which finds it by its last line, carry crew's marker once, on the
// line before the status marker, and the entries parse as before.
func TestAStatusCommentHoldsCrewsMarkerOnceBeforeTheStatusMarker(t *testing.T) {
	tr, gh := fresh(t)
	report(t, tr, fix(run2, "Reading the review."), fix(run2, "Pushing the fix."))
	written := writes(t, gh)
	tr, gh = restarted(t, written[len(written)-1])
	report(t, tr, fix(run3, "Starting over."))
	if creates := gh.callsTo(createComment...); len(creates) != 0 {
		t.Errorf("created %d comments after the restart, want the edit of comment 12", len(creates))
	}
	written = append(written, writes(t, gh)...)
	if len(written) != 3 {
		t.Fatalf("wrote %d bodies, want 3", len(written))
	}
	for _, body := range written {
		wantTrailer(t, body)
	}
	_, got := parseStatus(written[2])
	if len(got) != 2 || got[1].text != tr.renderStatus(fix(run3, "Starting over.")) ||
		!strings.HasSuffix(got[0].text, "crew stopped following `fix` on #74 before it ended.") {
		t.Errorf("entries of the edit after the restart = %+v", got)
	}
}

// A status comment an earlier crew wrote without crew's marker gains it on
// its next edit, its entry kept as it was.
func TestAStatusCommentWithoutCrewsMarkerGainsItOnItsNextEdit(t *testing.T) {
	older := "crew: an older status."
	tr, gh := restarted(t, older+"\n\n"+statusMarker+"\n")
	report(t, tr, running74(time.Time{}, ""))
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 {
		t.Fatalf("edits = %q, want one", edits)
	}
	body := statusBody(t, edits[0])
	wantTrailer(t, body)
	if !strings.HasPrefix(body, older+separator) {
		t.Errorf("body =\n%s\nwant the older entry kept first", body)
	}
}

func TestAnEditOfADeletedCommentCreatesItAgain(t *testing.T) {
	tr, gh := build(t, login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment, stderr: "gh: Not Found (HTTP 404)\n"},
	)
	for range 2 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
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
			err := tr.ReportStatus(context.Background(), running74(time.Time{}, ""))
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
