package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// listJSON is the listing's reply with issue and pull request nodes.
func listJSON(issues, pullRequests []string) string {
	return `{"data":{"repository":{"issues0":{"nodes":[` + strings.Join(issues, ",") +
		`]},"pullRequests":{"nodes":[` + strings.Join(pullRequests, ",") + `]}}}}`
}

// pullNode is an open pull request node of the listing, opened by author,
// or by a deleted account when author is empty.
func pullNode(number int, created, author string, labels ...string) string {
	by := "null"
	if author != "" {
		by = fmt.Sprintf(`{"__typename":"User","login":%q}`, author)
	}
	return fmt.Sprintf(`{"number":%d,"title":"Pull request %d","url":"https://github.com/o/r/pull/%d",`+
		`"createdAt":%q,"author":%s,"labels":{"nodes":[%s]}}`,
		number, number, number, created, by, labelNodes(labels...))
}

// wantItems checks that List returned want, in order, field by field.
func wantItems(t *testing.T, got, want []crew.Issue) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("List = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Ref != want[i].Ref || got[i].Title != want[i].Title ||
			got[i].URL != want[i].URL || !got[i].Created.Equal(want[i].Created) ||
			!slices.Equal(got[i].States, want[i].States) || got[i].Priority != want[i].Priority ||
			got[i].Blocked != want[i].Blocked || got[i].Kind != want[i].Kind {
			t.Errorf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Covers AE1 and AE2 of #35: the listing also returns the open pull requests
// the login opened that carry a rule's label, as items of kind pull request
// with no priority that nothing blocks, oldest first among the issues. Covers
// AE5 of #92 on the adapter's side: #90 carries the label the mirror copies
// from an issue, and is listed in that state as a pull request.
func TestListAlsoReturnsTheLoginsOpenPullRequests(t *testing.T) {
	tr, gh := build(t, login, reply{
		prefix: []string{"api", "graphql"},
		stdout: listJSON(
			[]string{issueNode(12, "2026-09-02T10:00:00Z", "ready")},
			[]string{
				pullNode(90, "2026-09-01T10:00:00Z", "me", "ready to review", "bug"),
				pullNode(91, "2026-09-01T11:00:00Z", "someone", "ready"),
				pullNode(92, "2026-09-01T12:00:00Z", "", "ready"),
				pullNode(93, "2026-09-03T10:00:00Z", "me", "ready", "needs attention"),
			},
		),
	})

	got, err := tr.List(context.Background(), []crew.State{ready, readyToReview})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	queries := gh.callsTo("api", "graphql")
	if len(queries) != 1 {
		t.Fatalf("sent %d GraphQL queries, want 1", len(queries))
	}
	query := strings.Join(fieldValues(queries[0], "query"), "")
	for _, want := range []string{
		"pullRequests(first: 100, states: OPEN, labels: $labels", "author { __typename login }",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "totalCount") {
		t.Errorf("query asks for totalCount:\n%s", query)
	}

	want := []crew.Issue{
		{ID: issueID("90"), Ref: "#90", Title: "Pull request 90", URL: "https://github.com/o/r/pull/90",
			Created: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), States: []crew.State{readyToReview},
			Kind: crew.KindPullRequest},
		{ID: issueID("12"), Ref: "#12", Title: "Issue 12", URL: "https://github.com/o/r/issues/12",
			Created: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), States: []crew.State{ready}},
		{ID: issueID("93"), Ref: "#93", Title: "Pull request 93", URL: "https://github.com/o/r/pull/93",
			Created: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC), States: []crew.State{ready, needsAttention},
			Kind: crew.KindPullRequest},
	}
	wantItems(t, got, want)
}
