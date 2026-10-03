package github

import (
	"context"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestReportFailurePointsToEachLogWithoutTheSessionsWords(t *testing.T) {
	postComment := []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/12/comments"}
	tr, gh := build(t, reply{prefix: postComment, stdout: "901\n"})
	reasons := []string{
		"ran `go test ./...` and got: FAIL token=s3cret",
		"tests did not build",
		"workspace: fetch failed",
	}
	err := tr.ReportFailure(context.Background(), crew.FailureReport{
		IssueKey: "12", IssueRef: "#12",
		Failures: []crew.ActionFailure{
			{Action: "development", Reason: reasons[0], Workspace: "issue-12-development",
				Log: ".crew/logs/issue-12-development.log"},
			{Action: "acceptance", Reason: reasons[1], Workspace: "issue-12-acceptance",
				Log: ".crew/logs/issue-12-acceptance.log"},
			// An action whose workspace was never created has no log.
			{Action: "lint", Reason: reasons[2]},
		},
	})
	if err != nil {
		t.Fatalf("ReportFailure: %v", err)
	}
	if n := len(gh.calls); n != 1 {
		t.Fatalf("made %d gh calls, want 1: %q", n, gh.calls)
	}
	comments := gh.callsTo(postComment...)
	if len(comments) != 1 {
		t.Fatalf("posted %d comments, want 1", len(comments))
	}
	body := statusBody(t, comments[0])

	want := "crew: 3 actions failed on #12.\n" +
		"\n**`development`** failed. Its log is `.crew/logs/issue-12-development.log`.\n" +
		"\n**`acceptance`** failed. Its log is `.crew/logs/issue-12-acceptance.log`.\n" +
		"\n**`lint`** failed before it had a log. crew's output says why.\n"
	if body != want {
		t.Errorf("comment =\n%s\nwant\n%s", body, want)
	}
	for _, reason := range reasons {
		if strings.Contains(body, reason) {
			t.Errorf("comment carries the session's words %q:\n%s", reason, body)
		}
	}
}

func TestReportFailureErrorsAreClassifiedFromTheHTTPStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		stderr string
		want   error // nil: transient
	}{
		"issue gone": {stderr: "gh: Not Found (HTTP 404)\n", want: port.ErrMovedMeanwhile},
		"issue locked": {
			stderr: "gh: Unable to create comment because issue is locked. (HTTP 403)\n",
			want:   port.ErrRefused,
		},
		"rate limited":   {stderr: "gh: You have exceeded a secondary rate limit. (HTTP 403)\n"},
		"no HTTP status": {stderr: "error connecting to api.github.com\n"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := build(t,
				reply{prefix: []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/42/comments"}, stderr: tc.stderr})
			err := tr.ReportFailure(context.Background(), crew.FailureReport{
				IssueKey: "42", IssueRef: "#42", Failures: []crew.ActionFailure{{Action: "development"}},
			})
			if err == nil || !strings.Contains(err.Error(), "report failure on issue #42") {
				t.Fatalf("ReportFailure = %v, want an error naming issue #42", err)
			}
			wantClassified(t, err, tc.want)
		})
	}
}
