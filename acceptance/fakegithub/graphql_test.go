package fakegithub

import (
	"fmt"
	"strings"
	"testing"
)

// issueFields is the fragment the listing query below reads issues with.
const issueFields = `fragment issueFields on Issue {
  number
  title
  url
  createdAt
  labels(first: 100) { nodes { name } }
  issueDependenciesSummary { blockedBy }
  issueFieldValues(first: 25) {
    nodes {
      ... on IssueFieldSingleSelectValue {
        optionId
        field { ... on IssueFieldSingleSelect { name options { id } } }
      }
    }
  }
}`

// listingQuery lists the open issues each of the authors opened with
// any of $labels, one aliased field per author, and the open pull requests
// with any of them.
func listingQuery(authors int) string {
	var vars, fields strings.Builder
	for i := range authors {
		fmt.Fprintf(&vars, ", $author%d: String!", i)
		fmt.Fprintf(&fields, `
    issues%d: issues(first: 100, states: OPEN, filterBy: {createdBy: $author%d, labels: $labels},
           orderBy: {field: CREATED_AT, direction: ASC}) { nodes { ...issueFields } }`, i, i)
	}
	fields.WriteString(`
    pullRequests: pullRequests(first: 100, states: OPEN, labels: $labels,
                 orderBy: {field: CREATED_AT, direction: ASC}) {
      nodes {
        number
        title
        url
        createdAt
        author { __typename login }
        labels(first: 100) { nodes { name } }
      }
    }`)
	return `query($owner: String!, $name: String!, $labels: [String!]` + vars.String() + `) {
  repository(owner: $owner, name: $name) {` + fields.String() + `
  }
}
` + issueFields
}

// closingQuery reads an issue's URL and repository and the pull requests
// GitHub links as closing it.
const closingQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issueOrPullRequest(number: $number) {
      ... on Issue {
        url
        repository { nameWithOwner }
        closedByPullRequestsReferences(first: 100) {
          nodes {
            number
            state
            repository { nameWithOwner }
            labels(first: 100) { nodes { name } }
          }
        }
      }
    }
  }
}`

// list runs the listing query for one author and labels.
func list(t *testing.T, g *GitHub, author string, labels ...string) string {
	t.Helper()
	args := []string{"api", "graphql", "-f", "query=" + listingQuery(1), "-F", "owner={owner}", "-F", "name={repo}"}
	for _, l := range labels {
		args = append(args, "-f", "labels[]="+l)
	}
	return ok(t, g, append(args, "-f", "author0="+author)...)
}

// listing is the listing query's reply, in the part these tests read.
type listing struct {
	Data struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					Number       int `json:"number"`
					Dependencies struct {
						BlockedBy int `json:"blockedBy"`
					} `json:"issueDependenciesSummary"`
					FieldValues struct {
						Nodes []struct {
							OptionID string `json:"optionId"`
							Field    struct {
								Name    string `json:"name"`
								Options []struct {
									ID string `json:"id"`
								} `json:"options"`
							} `json:"field"`
						} `json:"nodes"`
					} `json:"issueFieldValues"`
				} `json:"nodes"`
			} `json:"issues0"`
		} `json:"repository"`
	} `json:"data"`
}

func TestTheListingQueryReturnsTheLabeledIssuesOfTheAuthor(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Title: "Fix the widget", Labels: []string{"ready", "bug"},
		CreatedAt: at(t, "2026-09-01T10:00:00Z")})
	g.AddIssue(Issue{Number: 4, Title: "Not ready", Labels: []string{"bug"}})
	g.AddIssue(Issue{Number: 5, Title: "Someone else's", Author: "eve", Labels: []string{"ready"}})
	g.AddIssue(Issue{Number: 6, Title: "Closed", Labels: []string{"ready"}, State: Closed})
	g.AddPullRequest(PullRequest{Number: 7, Title: "A bot's", HeadBranch: "b", Author: "deploy-bot[bot]",
		Labels: []string{"ready"}, CreatedAt: at(t, "2026-09-02T10:00:00Z")})

	want := `{"data":{"repository":{"issues0":{"nodes":[{"number":3,"title":"Fix the widget",` +
		`"url":"https://github.com/acme/widgets/issues/3","createdAt":"2026-09-01T10:00:00Z",` +
		`"labels":{"nodes":[{"name":"ready"},{"name":"bug"}]},"issueDependenciesSummary":{"blockedBy":0},` +
		`"issueFieldValues":{"nodes":[]}}]},"pullRequests":{"nodes":[{"number":7,"title":"A bot's",` +
		`"url":"https://github.com/acme/widgets/pull/7","createdAt":"2026-09-02T10:00:00Z",` +
		`"author":{"__typename":"Bot","login":"deploy-bot"},"labels":{"nodes":[{"name":"ready"}]}}]}}}}`
	// GitHub's labels filter ignores case.
	if got := list(t, g, "boss", "READY"); got != want {
		t.Errorf("listing =\n%s\nwant\n%s", got, want)
	}
}

func TestABlockedIssueCountsItsOpenBlockers(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Labels: []string{"ready"}, BlockedBy: []int{4}})
	g.AddIssue(Issue{Number: 4})
	blocked := func() int {
		nodes := decode[listing](t, list(t, g, "boss", "ready")).Data.Repository.Issues.Nodes
		if len(nodes) != 1 {
			t.Fatalf("nodes = %+v, want issue 3", nodes)
		}
		return nodes[0].Dependencies.BlockedBy
	}
	if got := blocked(); got != 1 {
		t.Errorf("blockedBy = %d with #4 open, want 1", got)
	}
	g.SetState(4, Closed)
	if got := blocked(); got != 0 {
		t.Errorf("blockedBy = %d with #4 closed, want 0", got)
	}
}

func TestAnIssuesPriorityIsItsOptionAmongThePriorityFieldsOptions(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Labels: []string{"ready"}, Priority: "Medium"})
	nodes := decode[listing](t, list(t, g, "boss", "ready")).Data.Repository.Issues.Nodes
	if len(nodes) != 1 || len(nodes[0].FieldValues.Nodes) != 1 {
		t.Fatalf("nodes = %+v, want issue 3 with one field value", nodes)
	}
	v := nodes[0].FieldValues.Nodes[0]
	const medium = 2 // Urgent, High, Medium, Low
	if v.Field.Name != "Priority" || len(v.Field.Options) != 4 || v.OptionID == "" ||
		v.Field.Options[medium].ID != v.OptionID {
		t.Errorf("field value = %+v, want Priority's third option", v)
	}
}

func TestTheClosingQueryListsOpenAndMergedPullRequests(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3})
	g.AddPullRequest(PullRequest{Number: 7, HeadBranch: "a", Labels: []string{"running"}, Closes: []int{3}})
	g.AddPullRequest(PullRequest{Number: 8, HeadBranch: "b", State: Merged, Closes: []int{3}})
	g.AddPullRequest(PullRequest{Number: 9, HeadBranch: "c", State: Closed, Closes: []int{3}})
	g.AddPullRequest(PullRequest{Number: 10, HeadBranch: "d"})
	out := ok(t, g, "api", "graphql", "-f", "query="+closingQuery, "-F", "owner={owner}", "-F", "name={repo}",
		"-F", "number=3")
	want := `{"data":{"repository":{"issueOrPullRequest":{"url":"https://github.com/acme/widgets/issues/3",` +
		`"repository":{"nameWithOwner":"acme/widgets"},"closedByPullRequestsReferences":{"nodes":[` +
		`{"number":7,"state":"OPEN","repository":{"nameWithOwner":"acme/widgets"},` +
		`"labels":{"nodes":[{"name":"running"}]}},` +
		`{"number":8,"state":"MERGED","repository":{"nameWithOwner":"acme/widgets"},"labels":{"nodes":[]}}]}}}}}`
	if out != want {
		t.Errorf("closing query =\n%s\nwant\n%s", out, want)
	}
	// A pull request's number resolves to a pull request, which the Issue
	// fragment does not match.
	if out := ok(t, g, "api", "graphql", "-f", "query="+closingQuery, "-F", "owner={owner}", "-F", "name={repo}",
		"-F", "number=7"); out != `{"data":{"repository":{"issueOrPullRequest":{}}}}` {
		t.Errorf("closing query of a pull request = %s", out)
	}
	stderr := refused(t, g, "api", "graphql", "-f", "query="+closingQuery, "-F", "owner={owner}", "-F", "name={repo}",
		"-F", "number=42")
	if stderr != "gh: Could not resolve to an issue or pull request with the number of 42.\n" {
		t.Errorf("missing number: stderr %q", stderr)
	}
}

func TestAnUnknownGraphQLFieldIsAViolationNamingIt(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3})
	query := `query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {
  issues(first: 1) { nodes { number assignees(first: 5) { nodes { login } } } } } }`
	r := gh(g, "api", "graphql", "-f", "query="+query, "-F", "owner={owner}", "-F", "name={repo}")
	if r.Code != 1 || !strings.HasSuffix(r.Violation, "(unknown GraphQL field Issue.assignees)") {
		t.Errorf("reply = %+v, want a violation naming Issue.assignees", r)
	}
	if !strings.Contains(string(r.Stderr), "unknown GraphQL field Issue.assignees") {
		t.Errorf("stderr = %q", r.Stderr)
	}
}
