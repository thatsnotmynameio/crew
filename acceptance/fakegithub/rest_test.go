package fakegithub

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// rawAccept asks GitHub's contents API for a file's raw text.
const rawAccept = "Accept: application/vnd.github.raw+json"

// restComment is a comment as GitHub's REST API returns it, in the part gh's
// callers read.
type restComment struct {
	ID   int64 `json:"id"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body string `json:"body"`
}

func TestPostingACommentPrintsItsIDAndPatchingItChangesItsBody(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3})
	out := ok(t, g, "api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments", "-f", "body=hi", "--jq", ".id")
	if !bareInteger.MatchString(out) {
		t.Fatalf("POST printed %q, want a bare integer", out)
	}
	id := strings.TrimSpace(out)
	ok(t, g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/comments/"+id, "-f", "body=bye")
	got := g.Comments(3)
	if len(got) != 1 || strconv.FormatInt(got[0].ID, 10) != id || got[0].Body != "bye" || got[0].Author != "boss" {
		t.Errorf("comments = %+v, want comment %s by boss saying bye", got, id)
	}
	stderr := refused(t, g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/comments/999999", "-f", "body=x")
	if stderr != "gh: Not Found (HTTP 404)\n" {
		t.Errorf("PATCH of an unknown id: stderr %q, want gh's HTTP 404", stderr)
	}
	stderr = refused(t, g, "api", "--method", "POST", "repos/{owner}/{repo}/issues/99/comments", "-f", "body=x")
	if stderr != "gh: Not Found (HTTP 404)\n" {
		t.Errorf("POST on a missing issue: stderr %q, want gh's HTTP 404", stderr)
	}
}

func TestAPaginatedCommentListIsOneArrayOfTheIssuesComments(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3})
	g.AddIssue(Issue{Number: 4})
	g.AddComment(3, "ana", "first")
	g.AddComment(4, "ana", "elsewhere")
	g.AddComment(3, "deploy-bot[bot]", "status\n<!-- status -->")
	ok(t, g, "api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments", "-f", "body=third", "--jq", ".id")

	out := ok(t, g, "api", "--method", "GET", "--paginate", "repos/{owner}/{repo}/issues/3/comments?per_page=100")
	got := decode[[]restComment](t, out)
	want := []struct{ login, body string }{{"ana", "first"}, {"deploy-bot[bot]", "status\n<!-- status -->"},
		{"boss", "third"}}
	if len(got) != len(want) {
		t.Fatalf("comments = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		if got[i].User.Login != w.login || got[i].Body != w.body || got[i].ID == 0 {
			t.Errorf("comment %d = %+v, want %s saying %q", i, got[i], w.login, w.body)
		}
	}
	if !strings.Contains(out, "<!-- status -->") {
		t.Errorf("output %q escapes HTML, which GitHub does not", out)
	}
	stderr := refused(t, g, "api", "--method", "GET", "--paginate", "repos/{owner}/{repo}/issues/99/comments?per_page=100")
	if !strings.Contains(stderr, "HTTP 404") {
		t.Errorf("comments of a missing issue: stderr %q, want HTTP 404", stderr)
	}
}

func TestContentsServesAFilesRawTextOrHTTP404(t *testing.T) {
	g := New("acme", "widgets")
	stderr := refused(t, g, "api", "-H", rawAccept, "repos/{owner}/{repo}/contents/CODEOWNERS")
	if stderr != "gh: Not Found (HTTP 404)\n" {
		t.Errorf("missing file: stderr %q, want gh's HTTP 404", stderr)
	}
	g.SetFile("CODEOWNERS", "* @boss @acme/devs\n")
	if got := ok(t, g, "api", "-H", rawAccept, "repos/{owner}/{repo}/contents/CODEOWNERS"); got != "* @boss @acme/devs\n" {
		t.Errorf("file = %q", got)
	}
	refused(t, g, "api", "-H", rawAccept, "repos/{owner}/{repo}/contents/.github/CODEOWNERS")
}

func TestTeamMembersPrintOneLoginPerLineWithJQ(t *testing.T) {
	g := New("acme", "widgets")
	g.AddTeam("acme", "devs", "ana", "bo")
	if got := ok(t, g, "api", "--paginate", "orgs/acme/teams/devs/members", "--jq", ".[].login"); got != "ana\nbo\n" {
		t.Errorf("members = %q, want ana and bo on their own lines, unquoted", got)
	}
	if stderr := refused(t, g, "api", "--paginate", "orgs/acme/teams/ops/members", "--jq", ".[].login"); !strings.Contains(
		stderr, "HTTP 404") {
		t.Errorf("missing team: stderr %q, want HTTP 404", stderr)
	}
}

func TestTheRepositoryNamesItsOwnerAndWhetherItIsAnOrganization(t *testing.T) {
	type repo struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
			Type  string `json:"type"`
		} `json:"owner"`
	}
	g := New("acme", "widgets")
	got := decode[repo](t, ok(t, g, "api", "repos/{owner}/{repo}"))
	if got.Name != "widgets" || got.Owner.Login != "acme" || got.Owner.Type != "User" || got.Owner.ID == 0 {
		t.Errorf("repository = %+v, want widgets of the user acme", got)
	}
	g.AddTeam("acme", "devs", "ana")
	if got := decode[repo](t, ok(t, g, "api", "repos/{owner}/{repo}")); got.Owner.Type != "Organization" {
		t.Errorf("owner type = %q once acme has a team, want Organization", got.Owner.Type)
	}
}

func TestAMutationIsWrittenAsTheAccountOfGHConfigDir(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3})
	dir := t.TempDir()
	hosts := "github.com:\n    git_protocol: https\n    oauth_token: ghs_x\n    user: deploy-bot[bot]\n"
	if err := writeFile(dir, "hosts.yml", hosts); err != nil {
		t.Fatal(err)
	}
	r := g.Run(Invocation{Args: []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments",
		"-f", "body=hi", "--jq", ".id"}, Env: map[string]string{"GH_CONFIG_DIR": dir}})
	if r.Code != 0 {
		t.Fatalf("POST: %q", r.Stderr)
	}
	if got := g.Comments(3); len(got) != 1 || got[0].Author != "deploy-bot[bot]" {
		t.Errorf("comments = %+v, want one by deploy-bot[bot]", got)
	}
}

func TestPatchingAnItemsStateClosesIt(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Labels: []string{"ready", "bug"}})
	g.AddPullRequest(PullRequest{Number: 7, HeadBranch: "fix"})
	type restIssue struct {
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	out := ok(t, g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/3", "-f", "state=closed")
	if got := decode[restIssue](t, out); got.Number != 3 || got.State != "closed" {
		t.Errorf("PATCH printed %+v, want issue 3 closed", got)
	}
	if is, _ := g.Issue(3); is.State != Closed || !slices.Equal(is.Labels, []string{"ready", "bug"}) {
		t.Errorf("issue 3 = %+v, want closed with its labels", is)
	}
	ok(t, g, "api", "-X", "PATCH", "repos/{owner}/{repo}/issues/7", "-f", "state=closed")
	if pr, _ := g.PullRequest(7); pr.State != Closed {
		t.Errorf("pull request 7 = %+v, want closed", pr)
	}
	g.AddPullRequest(PullRequest{Number: 8, HeadBranch: "done", State: Merged})
	ok(t, g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/8", "-f", "state=closed")
	if pr, _ := g.PullRequest(8); pr.State != Merged {
		t.Errorf("merged pull request 8 = %+v, want it still merged", pr)
	}
	ok(t, g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/3", "-f", "state=closed")
	if is, _ := g.Issue(3); is.State != Closed {
		t.Errorf("issue 3 closed again = %+v, want it still closed", is)
	}
	stderr := refused(t, g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/99", "-f", "state=closed")
	if stderr != "gh: Not Found (HTTP 404)\n" {
		t.Errorf("PATCH of a missing issue: stderr %q, want gh's HTTP 404", stderr)
	}
	stderr = refused(t, g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/3", "-f", "state=open")
	if stderr != "gh: Validation Failed (HTTP 422)\n" {
		t.Errorf("PATCH to another state: stderr %q, want gh's HTTP 422", stderr)
	}
	r := gh(g, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/3", "-f", "title=x")
	want := "gh api --method PATCH 'repos/{owner}/{repo}/issues/3' -f title=x (unknown field title)"
	if r.Violation != want {
		t.Errorf("an unknown field: violation %q, want %q", r.Violation, want)
	}
}
