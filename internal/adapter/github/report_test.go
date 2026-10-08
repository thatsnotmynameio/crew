package github

import (
	"context"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Covers R17, KTD23 and AE18: the report names the action that ended the
// sequence, its verdict, the route and the log, and quotes nothing else, and
// its last line is crew's marker (R46).
func TestTheReportNamesTheActionItsVerdictTheRouteAndTheLog(t *testing.T) {
	tests := []reportCase{
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
	wantReports(t, tests)
}

// Covers KTD7: the report of the answered rule's check words the reason
// its verdict names in crew's own words, and quotes no comment.
func TestTheAnsweredRulesReportSaysWhyInCrewsWords(t *testing.T) {
	wantReports(t, []reportCase{
		{
			name:   "no question",
			report: answeredReport(crew.NoQuestion, crew.ReasonNoQuestion),
			want: "crew: `answered` ended through `failed` on #12.\n" +
				"\n**`answer`** ended with `no-question`: crew found no open question on #12 that the config " +
				"declares, so it has nowhere to return #12.\n",
		},
		{
			name:   "unanswered",
			report: answeredReport(crew.Unanswered, crew.ReasonUnanswered),
			want: "crew: `answered` ended through `failed` on #12.\n" +
				"\n**`answer`** ended with `unanswered`: no answer counts after the question. An answer " +
				"counts when a code owner, or an App on crew's answering list, posts it after the question.\n",
		},
		{
			name:   "unread",
			report: answeredReport(crew.Unread, crew.ReasonUnread),
			want: "crew: `answered` ended through `failed` on #12.\n" +
				"\n**`answer`** ended with `unread`: crew could not read the comments on #12.\n",
		},
	})
}

// reportCase is a report and the comment its rendering must post.
type reportCase struct {
	name   string
	report crew.FailureReport
	want   string
}

// wantReports posts each case's report on #12 and fails t unless it made
// one comment whose body is the case's, then crew's marker.
func wantReports(t *testing.T, tests []reportCase) {
	t.Helper()
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
			if body, want := statusBody(t, comments[0]), tt.want+postedLine; body != want {
				t.Errorf("comment =\n%s\nwant\n%s", body, want)
			}
		})
	}
}

// answeredReport returns the report of the answered rule's check on #12,
// which failed with verdict for reason, without a log, as the check runs
// in no workspace.
func answeredReport(verdict crew.Verdict, reason crew.FailureReason) crew.FailureReport {
	return crew.FailureReport{
		IssueID: issueID("12"), IssueRef: "#12", Rule: "answered", Route: crew.FailedRoute,
		Failures: []crew.ActionFailure{{Action: "answer", Verdict: verdict, Reason: reason}},
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
