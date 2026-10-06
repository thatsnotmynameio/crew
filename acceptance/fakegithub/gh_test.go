package fakegithub

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestUnknownCallsAreViolationsThatNameTheCall(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Title: "Fix it", Labels: []string{"ready"}})
	before, _ := g.Issue(3)
	cases := map[string]struct {
		args []string
		want string
	}{
		"unknown subcommand": {[]string{"repo", "delete", "x"}, "gh repo delete x (unknown command)"},
		"unknown flag":       {[]string{"issue", "view", "3", "--web"}, "gh issue view 3 --web (unknown flag --web)"},
		"unknown endpoint": {[]string{"api", "repos/{owner}/{repo}/pulls"},
			"gh api 'repos/{owner}/{repo}/pulls' (unknown endpoint GET repos/acme/widgets/pulls)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := gh(g, tc.args...)
			if r.Violation != tc.want {
				t.Errorf("violation = %q, want %q", r.Violation, tc.want)
			}
			if r.Code != 1 {
				t.Errorf("code = %d, want 1", r.Code)
			}
			if want := "acceptance: unknown gh call: " + tc.want + "\n"; string(r.Stderr) != want {
				t.Errorf("stderr = %q, want %q", r.Stderr, want)
			}
		})
	}
	if after, _ := g.Issue(3); !slices.Equal(after.Labels, before.Labels) || after.State != before.State {
		t.Errorf("issue 3 = %+v after the violations, want %+v", after, before)
	}
}

func TestAViolationCutsAnArgumentLongerThan200Characters(t *testing.T) {
	g := New("acme", "widgets")
	body := "body=" + strings.Repeat("x", 65536)
	r := gh(g, "api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments", "-f", body, "--web")
	want := "gh api --method POST 'repos/{owner}/{repo}/issues/3/comments' -f " +
		"'body=" + strings.Repeat("x", 195) + "…(65541 chars)' --web (unknown flag --web)"
	if r.Violation != want {
		t.Errorf("violation = %q, want %q", r.Violation, want)
	}
}

func TestFailMakesMatchingCallsFailUntilItsTimesAreUsedUp(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Labels: []string{"ready"}})
	g.AddLabel("running")
	g.Fail("issue edit 3", 502, "Bad Gateway", 1)
	g.Fail("api --method POST repos/{owner}/{repo}/issues/3/comments", 403,
		"Unable to create comment because issue is locked.", 2)

	if stderr := refused(t, g, "issue", "edit", "3", "--add-label=running"); !strings.Contains(stderr, "HTTP 502") {
		t.Errorf("stderr = %q, want HTTP 502", stderr)
	}
	wantLabels(t, g, 3, "ready")
	ok(t, g, "issue", "edit", "3", "--add-label=running")
	wantLabels(t, g, 3, "ready", "running")

	post := []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments", "-f", "body=hi", "--jq", ".id"}
	for range 2 {
		if stderr := refused(t, g, post...); stderr != "gh: Unable to create comment because issue is locked. (HTTP 403)\n" {
			t.Errorf("stderr = %q", stderr)
		}
	}
	if got := g.Comments(3); len(got) != 0 {
		t.Errorf("comments = %+v while posting failed, want none", got)
	}
	ok(t, g, post...)
	if got := g.Comments(3); len(got) != 1 {
		t.Errorf("comments = %+v, want the one posted once the failures were used up", got)
	}
}

func TestConcurrentCallsKeepTheStateConsistent(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3})
	const n = 50
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			r := gh(g, "api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments",
				"-f", fmt.Sprintf("body=comment %d", i), "--jq", ".id")
			ids[i] = string(r.Stdout)
			gh(g, "api", "--method", "GET", "--paginate", "repos/{owner}/{repo}/issues/3/comments?per_page=100")
		})
	}
	wg.Wait()
	slices.Sort(ids)
	if len(slices.Compact(ids)) != n {
		t.Errorf("ids = %q, want %d distinct ones", ids, n)
	}
	if got := len(g.Comments(3)); got != n {
		t.Errorf("%d comments, want %d", got, n)
	}
}

func TestChangedFiresAtTheNextChangeOnly(t *testing.T) {
	g := New("acme", "widgets")
	g.AddIssue(Issue{Number: 3, Labels: []string{"ready"}})
	changed := g.Changed()
	ok(t, g, "issue", "view", "3", "--json", "state,labels")
	select {
	case <-changed:
		t.Fatal("Changed fired on a read")
	default:
	}
	ok(t, g, "api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments", "-f", "body=hi", "--jq", ".id")
	select {
	case <-changed:
	default:
		t.Fatal("Changed did not fire on a comment")
	}
}

func TestAuthStatusAndTheViewer(t *testing.T) {
	g := New("acme", "widgets")
	ok(t, g, "auth", "status")
	if got := ok(t, g, "api", "user", "--jq", ".login"); got != "boss\n" {
		t.Errorf("login = %q, want boss", got)
	}
	g.SetViewer("ana")
	if got := ok(t, g, "api", "user", "--jq", ".login"); got != "ana\n" {
		t.Errorf("login = %q, want ana", got)
	}
}

func TestCallLogQuotesEachCall(t *testing.T) {
	g := New("acme", "widgets")
	gh(g, "auth", "status")
	gh(g, "label", "create", "in review")
	want := []string{"gh auth status", "gh label create 'in review'"}
	if got := g.CallLog(); !slices.Equal(got, want) {
		t.Errorf("call log = %q, want %q", got, want)
	}
}

// bareInteger is how gh prints a number --jq selects.
var bareInteger = regexp.MustCompile(`^[0-9]+\n$`)
