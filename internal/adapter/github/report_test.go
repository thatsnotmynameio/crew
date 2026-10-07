package github

import (
	"context"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Covers R17, KTD23: the report names the action that ended the sequence,
// its verdict, the route and the log, and quotes nothing else.
func TestTheReportNamesTheActionItsVerdictTheRouteAndTheLog(t *testing.T) {
	tests := []struct {
		name   string
		report crew.FailureReport
		want   string
	}{
		{
			name: "blocked",
			report: crew.FailureReport{
				IssueID: issueID("12"), IssueRef: "#12", Rule: "development", Route: "blocked",
				Failures: []crew.ActionFailure{{
					Action: "lfg", Verdict: "blocked", Workspace: "issue-12-development",
					Log: ".crew/logs/issue-12-development.log",
				}},
			},
			want: "crew: `development` ended through `blocked` on #12.\n" +
				"\n**`lfg`** ended with `blocked`. Its log is `.crew/logs/issue-12-development.log`.\n",
		},
		{
			// An action whose workspace was never created has no log.
			name: "failed before it had a log",
			report: crew.FailureReport{
				IssueID: issueID("12"), IssueRef: "#12", Rule: "development", Route: crew.FailedRoute,
				Failures: []crew.ActionFailure{{Action: "lfg", Verdict: crew.Failed}},
			},
			want: "crew: `development` ended through `failed` on #12.\n" +
				"\n**`lfg`** ended with `failed` before it had a log. crew's output says why.\n",
		},
		{
			name:   "a rule without actions",
			report: crew.FailureReport{IssueID: issueID("12"), IssueRef: "#12", Rule: "triage", Route: "stale"},
			want:   "crew: `triage` ended through `stale` on #12.\n",
		},
	}
	postComment := []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/12/comments"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, gh := build(t, reply{prefix: postComment, stdout: "901\n"})
			// The report has no field for a session's words, so the comment
			// cannot carry them.
			if err := tr.ReportFailure(context.Background(), tt.report); err != nil {
				t.Fatalf("ReportFailure: %v", err)
			}
			comments := gh.callsTo(postComment...)
			if n := len(gh.calls); n != 1 || len(comments) != 1 {
				t.Fatalf("made %d gh calls, want 1 comment: %q", n, gh.calls)
			}
			if body := statusBody(t, comments[0]); body != tt.want {
				t.Errorf("comment =\n%s\nwant\n%s", body, tt.want)
			}
		})
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
				IssueID: issueID("42"), IssueRef: "#42", Failures: []crew.ActionFailure{{Action: "development"}},
			})
			if err == nil || !strings.Contains(err.Error(), "report failure on issue #42") {
				t.Fatalf("ReportFailure = %v, want an error naming issue #42", err)
			}
			wantClassified(t, err, tc.want)
		})
	}
}
