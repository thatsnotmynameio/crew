package github

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// boardJSON is the board's reply with each author's issue nodes, by author
// index, and no pull requests field.
func boardJSON(byAuthor ...[]string) string {
	fields := make([]string, len(byAuthor))
	for i, nodes := range byAuthor {
		fields[i] = `"issues` + strconv.Itoa(i) + `":{"nodes":[` + strings.Join(nodes, ",") + `]}`
	}
	return `{"data":{"repository":{` + strings.Join(fields, ",") + `}}}`
}

// Covers AE7: the board's query lists only the issues the code owners and the
// bots opened, one issues field per author and no pull requests, filtered by
// the board's labels. Before Prepare the code owner is gh's login.
func TestListBoardSendsOneQueryPerAuthorWithoutPullRequests(t *testing.T) {
	tr, gh := build(t, login, reply{prefix: []string{"api", "graphql"}, stdout: boardJSON(nil, nil)})
	tr.ActAs(port.Identity{}, []string{opsLogin})

	if _, err := tr.ListBoard(context.Background(), []crew.State{"bug", "Idea"}); err != nil {
		t.Fatalf("ListBoard: %v", err)
	}

	queries := gh.callsTo("api", "graphql")
	if len(queries) != 1 {
		t.Fatalf("sent %d GraphQL queries, want 1", len(queries))
	}
	q := queries[0]
	for key, want := range map[string][]string{
		"author0": {"me"}, "author1": {opsLogin}, "author2": nil, "labels[]": {"bug", "Idea"},
	} {
		if got := fieldValues(q, key); !slices.Equal(got, want) {
			t.Errorf("%s variable = %q, want %q", key, got, want)
		}
	}
	query := strings.Join(fieldValues(q, "query"), "")
	for _, want := range []string{"issues0: issues(", "createdBy: $author0", "issues1: issues(", "createdBy: $author1",
		"labels: $labels", "states: OPEN"} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "pullRequests") {
		t.Errorf("query reads pull requests:\n%s", query)
	}
}

// The board's issues come back once each, oldest first, with the asked
// labels they carry ignoring case, in the asked spelling; an issue carrying
// none of them is dropped.
func TestListBoardKeepsTheAskedLabelsEachIssueCarries(t *testing.T) {
	tr, _ := build(t, login, reply{
		prefix: []string{"api", "graphql"},
		stdout: boardJSON(
			[]string{
				issueNode(12, "2026-09-03T10:00:00Z", "Bug", "in progress"),
				issueNode(14, "2026-09-02T10:00:00Z", "idea", "BUG"),
				issueNode(15, "2026-09-04T10:00:00Z", "bugfix"),
			},
			[]string{
				issueNode(14, "2026-09-02T10:00:00Z", "idea", "BUG"),
				issueNode(9, "2026-09-01T10:00:00Z", "Idea"),
			},
		),
	})
	tr.ActAs(port.Identity{}, []string{opsLogin})

	got, err := tr.ListBoard(context.Background(), []crew.State{"bug", "Idea"})
	if err != nil {
		t.Fatalf("ListBoard: %v", err)
	}

	want := []crew.BoardIssue{
		crew.NewBoardIssue(crew.NewIssue(crew.IssueData{
			ID: issueID("9"), Ref: "#9", Title: "Issue 9", URL: "https://github.com/o/r/issues/9",
			Created: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)}), []crew.State{"Idea"}),
		crew.NewBoardIssue(crew.NewIssue(crew.IssueData{
			ID: issueID("14"), Ref: "#14", Title: "Issue 14", URL: "https://github.com/o/r/issues/14",
			Created: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)}), []crew.State{"bug", "Idea"}),
		crew.NewBoardIssue(crew.NewIssue(crew.IssueData{
			ID: issueID("12"), Ref: "#12", Title: "Issue 12", URL: "https://github.com/o/r/issues/12",
			Created: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC), States: []crew.State{inProgress}}), []crew.State{"bug"}),
	}
	if len(got) != len(want) {
		t.Fatalf("ListBoard = %+v, want %+v", got, want)
	}
	issues := make([]crew.Issue, len(got))
	for i, b := range got {
		issues[i] = b.Issue()
		if !slices.Equal(b.Labels(), want[i].Labels()) {
			t.Errorf("issue %d labels = %q, want %q", i, b.Labels(), want[i].Labels())
		}
	}
	wantIssues := make([]crew.Issue, len(want))
	for i, b := range want {
		wantIssues[i] = b.Issue()
	}
	wantItems(t, issues, wantIssues)
}

func TestListBoardNamesTheBoardReadWhenGhFails(t *testing.T) {
	for name, script := range map[string][]reply{
		"the login":   {{prefix: []string{"api", "user"}, stderr: "HTTP 401"}},
		"the listing": {login, {prefix: []string{"api", "graphql"}, stderr: "HTTP 502"}},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := build(t, script...)
			_, err := tr.ListBoard(context.Background(), []crew.State{"bug"})
			if err == nil || !strings.Contains(err.Error(), "list the board's issues") {
				t.Errorf("ListBoard error = %v, want one naming the board's issues", err)
			}
		})
	}
}
