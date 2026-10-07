package github

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// codeownersAt is the prefix of the CODEOWNERS lookup at path.
func codeownersAt(path string) []string {
	return []string{"api", "-H", rawAccept, "repos/{owner}/{repo}/contents/" + path}
}

// prepared builds a tracker whose Prepare ran with gh scripted by the
// CODEOWNERS replies in owners, a lookup they leave out being a 404, and the
// further replies in script.
func prepared(t *testing.T, owners []reply, script ...reply) (*Tracker, *fakeGh) {
	t.Helper()
	all := slices.Concat([]reply{{prefix: []string{"auth", "status"}}, login}, owners,
		[]reply{noCodeowners, repositoryReply,
			{prefix: []string{"label", "list"}, stdout: `[{"name":"ready"},{"name":"waiting brainstorm"}]`}},
		script)
	tr, gh := build(t, all...)
	if err := tr.Prepare(context.Background(), []crew.State{ready}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return tr, gh
}

// Covers AE7.
func TestTheCatchAllRuleOfCodeownersNamesTheCodeOwners(t *testing.T) {
	tr, gh := prepared(t,
		[]reply{{prefix: codeownersAt(".github/CODEOWNERS"), stdout: "# owners\n*.go @gophers\n* @mguilarducci @alice\n"}},
		reply{prefix: []string{"api", "graphql"}, stdout: listJSON(
			[]string{issueNode(12, "2026-09-01T10:00:00Z", "ready")},
			[]string{
				pullNode(90, "2026-09-02T10:00:00Z", "alice", "ready"),
				pullNode(91, "2026-09-02T11:00:00Z", "me", "ready"),
			},
		)},
	)
	if codeOwners := tr.CodeOwners(); !slices.Equal(codeOwners, []string{"mguilarducci", "alice"}) {
		t.Errorf("CodeOwners = %q, want mguilarducci and alice", codeOwners)
	}
	got, err := tr.List(context.Background(), []crew.State{ready})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	graphql := gh.callsTo("api", "graphql")
	q := graphql[len(graphql)-1] // the listing, after Prepare's repository read
	if a0, a1 := fieldValues(q, "author0"), fieldValues(q, "author1"); !slices.Equal(a0, []string{"mguilarducci"}) ||
		!slices.Equal(a1, []string{"alice"}) || fieldValues(q, "author2") != nil {
		t.Errorf("query authors = %q, %q; want mguilarducci and alice", a0, a1)
	}
	query := strings.Join(fieldValues(q, "query"), "")
	for _, want := range []string{"$author0: String!", "$author1: String!", "issues1: issues(", "createdBy: $author1"} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %q:\n%s", want, query)
		}
	}
	keys := make([]string, 0, len(got))
	for _, i := range got {
		keys = append(keys, i.ID().Key)
	}
	if !slices.Equal(keys, []string{"12", "90"}) {
		t.Errorf("List = %q, want 12 and alice's 90, not gh's login's 91", keys)
	}
}

func TestTheCodeOwnersComeFromWhereGitHubFindsCodeowners(t *testing.T) {
	for name, tc := range map[string]struct {
		owners []reply
		want   []string
	}{
		"root file": {owners: []reply{{prefix: codeownersAt("CODEOWNERS"), stdout: "* @root-owner\n"}},
			want: []string{"root-owner"}},
		"docs file": {owners: []reply{{prefix: codeownersAt("docs/CODEOWNERS"), stdout: "* @docs-owner\n"}},
			want: []string{"docs-owner"}},
		"the last catch-all wins": {owners: []reply{{prefix: codeownersAt(".github/CODEOWNERS"),
			stdout: "* @first\n/docs/ @writer\n*   @second  @Second # the same login\n"}},
			want: []string{"second"}},
		"no file": {want: []string{"me"}},
		"emails only": {owners: []reply{{prefix: codeownersAt(".github/CODEOWNERS"), stdout: "* owner@example.com\n"}},
			want: []string{"me"}},
		"a catch-all without owners": {owners: []reply{{prefix: codeownersAt(".github/CODEOWNERS"),
			stdout: "* @first\n*\n"}}, want: []string{"me"}},
		"a team": {owners: []reply{
			{prefix: codeownersAt(".github/CODEOWNERS"), stdout: "* @org/devs @Bob\n"},
			{prefix: []string{"api", "--paginate", "orgs/org/teams/devs/members"}, stdout: "alice\nbob\n"},
		}, want: []string{"alice", "bob"}},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := prepared(t, tc.owners)
			if codeOwners := tr.CodeOwners(); !slices.Equal(codeOwners, tc.want) {
				t.Errorf("CodeOwners = %q, want %q", codeOwners, tc.want)
			}
		})
	}
}

func TestATeamCrewCannotExpandFailsPrepare(t *testing.T) {
	tr, _ := build(t, reply{prefix: []string{"auth", "status"}}, login,
		reply{prefix: codeownersAt(".github/CODEOWNERS"), stdout: "* @org/devs\n"},
		reply{prefix: []string{"api", "--paginate", "orgs/org/teams/devs/members"}, stderr: "gh: Not Found (HTTP 404)"},
	)
	err := tr.Prepare(context.Background(), []crew.State{ready})
	if err == nil || !strings.Contains(err.Error(), "@org/devs") ||
		!strings.Contains(err.Error(), "gh auth refresh -s read:org") {
		t.Errorf("Prepare = %v, want an error naming the team and gh auth refresh -s read:org", err)
	}
}

func TestACodeownersLookupThatFailsFailsPrepare(t *testing.T) {
	tr, gh := build(t, reply{prefix: []string{"auth", "status"}}, login,
		reply{prefix: codeownersAt(".github/CODEOWNERS"), stderr: "gh: Server Error (HTTP 502)"},
	)
	err := tr.Prepare(context.Background(), []crew.State{ready})
	if err == nil || !strings.Contains(err.Error(), ".github/CODEOWNERS") {
		t.Errorf("Prepare = %v, want an error naming .github/CODEOWNERS", err)
	}
	if n := len(gh.callsTo("label")); n != 0 {
		t.Errorf("touched labels %d times, want none", n)
	}
}

func TestPrepareFailsWithoutGhsLogin(t *testing.T) {
	tr, _ := build(t, reply{prefix: []string{"auth", "status"}},
		reply{prefix: []string{"api", "user"}, stderr: "gh: Bad Gateway (HTTP 502)"})
	if err := tr.Prepare(context.Background(), []crew.State{ready}); err == nil ||
		!strings.Contains(err.Error(), "find the code owners") {
		t.Errorf("Prepare = %v, want an error finding the code owners", err)
	}
}

// Covers AE8: the issues a bot opened are listed, each once.
func TestListTakesTheIssuesTheBotsOpened(t *testing.T) {
	tr, gh := build(t, login, reply{prefix: []string{"api", "graphql"},
		stdout: `{"data":{"repository":{` +
			`"issues0":{"nodes":[` + issueNode(12, "2026-09-01T10:00:00Z", "ready") + `]},` +
			`"issues1":{"nodes":[` + issueNode(12, "2026-09-01T10:00:00Z", "ready") + `,` +
			issueNode(13, "2026-09-02T10:00:00Z", "ready") + `]},` +
			`"pullRequests":{"nodes":[]}}}}`})
	tr.ActAs(port.Identity{}, []string{opsLogin, "Me"})
	got, err := tr.List(context.Background(), []crew.State{ready})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	q := gh.callsTo("api", "graphql")[0]
	if a1 := fieldValues(q, "author1"); !slices.Equal(a1, []string{opsLogin}) || fieldValues(q, "author2") != nil {
		t.Errorf("author1 = %q, want crew-ops[bot] alone after me", a1)
	}
	want := []crew.Issue{
		crew.NewIssue(crew.IssueData{
			ID: issueID("12"), Ref: "#12", Title: "Issue 12", URL: "https://github.com/o/r/issues/12",
			Created: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), States: []crew.State{ready}}),
		crew.NewIssue(crew.IssueData{
			ID: issueID("13"), Ref: "#13", Title: "Issue 13", URL: "https://github.com/o/r/issues/13",
			Created: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), States: []crew.State{ready}}),
	}
	wantItems(t, got, want)
}

func TestListTakesThePullRequestsABotOpenedAsABot(t *testing.T) {
	bot := func(number int, typename, login string) string {
		return strings.Replace(pullNode(number, "2026-09-01T10:00:00Z", login, "ready"),
			`"__typename":"User"`, `"__typename":"`+typename+`"`, 1)
	}
	tr, _ := build(t, login, reply{prefix: []string{"api", "graphql"}, stdout: listJSON(nil, []string{
		bot(90, "Bot", "crew-developer"),
		bot(91, "User", "crew-developer"),
		bot(92, "User", "outsider"),
		bot(93, "Bot", "renovate"),
	})})
	tr.ActAs(port.Identity{}, []string{"crew-developer[bot]"})
	got, err := tr.List(context.Background(), []crew.State{ready})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID().Key != "90" {
		t.Errorf("List = %+v, want the bot's 90 alone", got)
	}
}
