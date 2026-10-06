package fakegithub

import (
	"slices"
	"strings"
	"testing"
)

func TestIssueEditMovesLabelsAndRefusesAMissingOne(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Labels: []string{"ready", "bug"}})
	g.AddLabel("running", "needs, care")

	if out := ok(t, g, "issue", "edit", "3", "--remove-label=ready", "--add-label=running"); out !=
		"https://github.com/acme/widgets/issues/3\n" {
		t.Errorf("issue edit printed %q, want the issue's URL", out)
	}
	wantLabels(t, g, 3, "bug", "running")

	stderr := refused(t, g, "issue", "edit", "3", "--remove-label=running", "--add-label=missing")
	if !strings.Contains(stderr, "'missing' not found") {
		t.Errorf("stderr = %q, want gh's 'missing' not found", stderr)
	}
	wantLabels(t, g, 3, "bug", "running")

	// gh reads each label flag as comma-separated values, CSV-quoted.
	ok(t, g, "issue", "edit", "3", "--remove-label=RUNNING", `--add-label="needs, care"`)
	wantLabels(t, g, 3, "bug", "needs, care")
}

func TestIssueViewPrintsTheStateAndLabelsAsGHDoes(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Labels: []string{"ready"}})
	out := ok(t, g, "issue", "view", "3", "--json", "state,labels")
	type view struct {
		State  string `json:"state"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	got := decode[view](t, out)
	if got.State != "OPEN" || len(got.Labels) != 1 || got.Labels[0].Name != "ready" {
		t.Errorf("issue view = %+v, want OPEN with ready", got)
	}
	// gh exports --json fields sorted by name, one JSON line.
	if !strings.HasPrefix(out, `{"labels":[{`) || !strings.HasSuffix(out, `"state":"OPEN"}`+"\n") {
		t.Errorf("issue view printed %q, want gh's key order", out)
	}
	if got := ok(t, g, "issue", "view", "3", "--json", "labels", "--jq", ".labels[].name"); got != "ready\n" {
		t.Errorf("--jq printed %q, want ready", got)
	}
	g.SetState(3, Closed)
	if got := decode[view](t, ok(t, g, "issue", "view", "3", "--json", "state,labels")); got.State != "CLOSED" {
		t.Errorf("state = %q, want CLOSED", got.State)
	}
	stderr := refused(t, g, "issue", "view", "99", "--json", "state,labels")
	if !strings.Contains(stderr, "Could not resolve to an issue or pull request with the number of 99") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestLabelListAndCreate(t *testing.T) {
	g := New("acme", "widgets")
	if got := ok(t, g, "label", "list", "--limit", "1000", "--json", "name"); got != "[]\n" {
		t.Errorf("empty repository's labels = %q, want []", got)
	}
	g.AddLabel("ready", "bug")
	ok(t, g, "label", "create", "in review")
	if got := ok(t, g, "label", "list", "--limit", "1000", "--json", "name"); got !=
		`[{"name":"ready"},{"name":"bug"},{"name":"in review"}]`+"\n" {
		t.Errorf("labels = %q", got)
	}
	if stderr := refused(t, g, "label", "create", "In Review"); !strings.Contains(stderr, "already exists") {
		t.Errorf("creating an existing label: stderr %q", stderr)
	}
	if got := g.Labels(); !slices.Equal(got, []string{"ready", "bug", "in review"}) {
		t.Errorf("Labels() = %q", got)
	}
}

func TestPRListFindsThePullRequestsFromABranch(t *testing.T) {
	g := New("acme", "widgets")
	g.AddPullRequest(PullRequest{Number: 7, HeadBranch: "fix-widget", Closes: []int{3},
		CreatedAt: at(t, "2026-09-02T10:00:00Z")})
	args := []string{"pr", "list", "--head=fix-widget", "--state=all", "--limit", "100",
		"--json", "number,url,state,createdAt,isCrossRepository"}
	want := `[{"createdAt":"2026-09-02T10:00:00Z","isCrossRepository":false,"number":7,"state":"OPEN",` +
		`"url":"https://github.com/acme/widgets/pull/7"}]` + "\n"
	if got := ok(t, g, args...); got != want {
		t.Errorf("pr list = %q, want %q", got, want)
	}
	g.SetState(7, Merged)
	g.AddPullRequest(PullRequest{Number: 8, HeadBranch: "fix-widget", CrossRepository: true,
		CreatedAt: at(t, "2026-09-03T10:00:00Z")})
	g.AddPullRequest(PullRequest{Number: 9, HeadBranch: "other", CreatedAt: at(t, "2026-09-04T10:00:00Z")})
	type pr struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Cross  bool   `json:"isCrossRepository"`
	}
	got := decode[[]pr](t, ok(t, g, args...))
	if want := []pr{{8, "OPEN", true}, {7, "MERGED", false}}; !slices.Equal(got, want) {
		t.Errorf("pr list = %+v, want %+v, newest first", got, want)
	}
}

func TestPREditLabelsOnlyAPullRequest(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3})
	g.AddPullRequest(PullRequest{Number: 7, HeadBranch: "b", Labels: []string{"ready"}})
	g.AddLabel("running")
	ok(t, g, "pr", "edit", "7", "--remove-label=ready", "--add-label=running")
	wantLabels(t, g, 7, "running")
	if stderr := refused(t, g, "pr", "edit", "3", "--add-label=running"); !strings.Contains(stderr,
		"Could not resolve to a PullRequest with the number of 3") {
		t.Errorf("pr edit of an issue: stderr %q", stderr)
	}
	wantLabels(t, g, 3)
}
