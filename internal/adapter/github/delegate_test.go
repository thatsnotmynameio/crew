package github

import (
	"context"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// delegationTo returns the delegation of #12's question to answerer, as
// search found it: deps's question blocks when found.
func delegationTo(answerer string, search crew.QuestionSearch) crew.Delegation {
	d := crew.Delegation{IssueID: issueID("12"), IssueRef: "#12", Answerer: answerer, Search: search}
	if search == crew.QuestionFound {
		d.ID, d.Rule = "blocks", "deps"
	}
	return d
}

// Covers KTD8, KTD9: a delegation mentions the answerer, a user as @login
// and an App as @<slug> in a code span, names the question crew found, or
// says it found none or could not read the comments, and ends with the
// delegation's marker and crew's own. It never quotes the question.
func TestDelegateMentionsTheAnswererAndNamesTheQuestion(t *testing.T) {
	asked := " crew asks you to answer the question `blocks` that `deps` asked on #12.\n\n" +
		crew.DelegatedMarker("blocks") + "\n"
	tests := []struct {
		name       string
		delegation crew.Delegation
		want       string
	}{
		{"a user, the question found", delegationTo("octocat", crew.QuestionFound), "@octocat," + asked},
		{"an App, the question found", delegationTo("claude[bot]", crew.QuestionFound), "`@claude`," + asked},
		{
			"no open question", delegationTo("octocat", crew.QuestionNotFound),
			"@octocat, crew was to ask you to answer a question on #12, " +
				"and found no open question in its comments.\n\n" + crew.DelegatedMarker("") + "\n",
		},
		{
			"the comments unread", delegationTo("claude[bot]", crew.QuestionUnread),
			"`@claude`, crew was to ask you to answer a question on #12, and could not read its comments.\n" +
				"\n" + crew.DelegatedMarker("") + "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, gh := build(t, reply{prefix: commentOn(12), stdout: "901\n"})
			var delegator port.Delegator = tr
			if err := delegator.Delegate(context.Background(), tt.delegation); err != nil {
				t.Fatalf("Delegate: %v", err)
			}
			posts := gh.callsTo(commentOn(12)...)
			if len(gh.calls) != 1 || len(posts) != 1 {
				t.Fatalf("made gh calls %q, want one comment", gh.calls)
			}
			if body, want := statusBody(t, posts[0]), tt.want+postedLine; body != want {
				t.Errorf("comment =\n%s\nwant\n%s", body, want)
			}
		})
	}
}

func TestDelegateErrorsAreClassifiedFromTheHTTPStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		stderr string
		want   error // nil: transient
	}{
		"issue gone": {stderr: "gh: Not Found (HTTP 404)\n", want: port.ErrMovedMeanwhile},
		"issue locked": {stderr: "gh: Unable to create comment because issue is locked. (HTTP 403)\n",
			want: port.ErrRefused},
		"a server error": {stderr: "gh: HTTP 502: Bad Gateway\n"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := build(t, reply{prefix: commentOn(42), stderr: tc.stderr})
			err := tr.Delegate(context.Background(), crew.Delegation{
				IssueID: issueID("42"), IssueRef: "#42", Answerer: "octocat",
			})
			if err == nil || !strings.Contains(err.Error(), "delegate the question on issue #42") {
				t.Fatalf("Delegate = %v, want an error naming issue #42", err)
			}
			wantClassified(t, err, tc.want)
		})
	}
}
