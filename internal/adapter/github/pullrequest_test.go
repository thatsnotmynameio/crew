package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The crew labels of the pull request tests, as crew's own config names them.
const (
	crewInProgress    crew.State = "crew:in progress"
	crewWaitingReview crew.State = "crew:waiting review"
	crewFailed        crew.State = "crew:failed"
	crewReadyForFix   crew.State = "crew:ready for fix"
	crewWaitingBrain  crew.State = "crew:waiting brainstorm"
)

// The scripted prefixes of the pull request report's gh calls on issue #42.
var (
	prQuery       = []string{"api", "graphql"}
	prEdit        = []string{"pr", "edit"}
	issue42List   = []string{"api", "--method", "GET", "--paginate", "repos/{owner}/{repo}/issues/42/comments?per_page=100"}
	issue42URL    = "https://github.com/o/r/issues/42"
	nobodyWatches = "Nobody watches this pull request any more: new review comments and CI failures need a person."
)

// commentOn is the prefix of a new comment posted on number.
func commentOn(number int) []string {
	return []string{"api", "--method", "POST", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments", number)}
}

// prTracker builds a tracker on crew's own labels, with gh scripted.
func prTracker(t *testing.T, script ...reply) (*Tracker, *fakeGh) {
	t.Helper()
	gh := newFakeGh(t, script...)
	states := []crew.State{"crew:ready for development", crewInProgress, crewWaitingReview, crewFailed, crewReadyForFix}
	tr, err := factory(gh.run)(func(any) error { return nil }, states, []crew.State{crewWaitingBrain})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return tr.(*Tracker), gh
}

// prsJSON is the query's reply for issue #42 of o/r with the given closing
// pull request nodes.
func prsJSON(nodes ...string) string {
	return `{"data":{"repository":{"issue":{"url":"` + issue42URL + `","repository":{"nameWithOwner":"o/r"},` +
		`"closedByPullRequestsReferences":{"nodes":[` + strings.Join(nodes, ",") + `]}}}}}`
}

// prNode is one closing pull request in the query's reply.
func prNode(number int, state, repo string, labels ...crew.State) string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = fmt.Sprintf(`{"name":%q}`, l)
	}
	return fmt.Sprintf(`{"number":%d,"state":%q,"repository":{"nameWithOwner":%q},"labels":{"nodes":[%s]}}`,
		number, state, repo, strings.Join(names, ","))
}

// ended is a report of #42 moving to state at the end of development, whose
// actions are actions.
func ended(id string, state crew.State, actions ...crew.ActionStatus) crew.PullRequestReport {
	return crew.PullRequestReport{ID: id, IssueKey: "42", IssueRef: "#42", State: state,
		End: &crew.StageEnd{Stage: "development", Actions: actions}}
}

// comments returns the body of each comment posted on number, failed posts
// included.
func comments(t *testing.T, gh *fakeGh, number int) []string {
	t.Helper()
	var bodies []string
	for _, c := range gh.callsTo(commentOn(number)...) {
		bodies = append(bodies, statusBody(t, c))
	}
	return bodies
}

// Covers AE1.
func TestAReportMirrorsTheLabelAndPostsTheStopComment(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewInProgress, "bug"))},
		reply{prefix: prEdit},
		reply{prefix: commentOn(50), stdout: "900\n"},
	)
	tr.rememberStatus("42", cachedStatus{id: 101, body: "status"})
	report := ended("p1", crewWaitingReview, crew.ActionStatus{Name: "lfg", State: crew.ActionSucceeded})
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}

	queries := gh.callsTo(prQuery...)
	if len(queries) != 1 {
		t.Fatalf("queries = %q, want one", queries)
	}
	for key, want := range map[string]string{"owner": "{owner}", "name": "{repo}", "number": "42"} {
		if got := fieldValues(queries[0], key); !slices.Equal(got, []string{want}) {
			t.Errorf("query field %s = %q, want %q", key, got, want)
		}
	}
	want := []string{"pr", "edit", "50", "--remove-label=crew:in progress", "--add-label=crew:waiting review"}
	if edits := gh.callsTo(prEdit...); len(edits) != 1 || !slices.Equal(edits[0], want) {
		t.Errorf("edits = %q, want one: %q", edits, want)
	}
	body := "crew: `development` succeeded on #42, which moved to `crew:waiting review`, as did this pull request.\n" +
		"\n" + nobodyWatches + "\n" +
		"\n#42's [status comment](https://github.com/o/r/issues/42#issuecomment-101) has the details.\n"
	if got := comments(t, gh, 50); !slices.Equal(got, []string{body}) {
		t.Errorf("comments on 50 = %q, want one:\n%s", got, body)
	}
}

// Covers AE2.
func TestAStoppedStageSaysItFailedBecauseCrewStoppedIt(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewInProgress))},
		reply{prefix: prEdit},
		reply{prefix: commentOn(50), stdout: "900\n"},
	)
	tr.rememberStatus("42", cachedStatus{id: 101, body: "status"})
	report := ended("p1", crewFailed, crew.ActionStatus{Name: "lfg", State: crew.ActionFailed,
		Cause: crew.CauseStopped, Log: ".crew/logs/issue-42-lfg.log"})
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}
	want := []string{"pr", "edit", "50", "--remove-label=crew:in progress", "--add-label=crew:failed"}
	if edits := gh.callsTo(prEdit...); len(edits) != 1 || !slices.Equal(edits[0], want) {
		t.Errorf("edits = %q, want one: %q", edits, want)
	}
	body := "crew: `development` failed on #42, which moved to `crew:failed`, as did this pull request.\n" +
		"\n**`lfg`** failed: crew stopped it. Its log is `.crew/logs/issue-42-lfg.log`.\n" +
		"\n" + nobodyWatches + "\n" +
		"\n#42's [status comment](https://github.com/o/r/issues/42#issuecomment-101) has the details.\n"
	if got := comments(t, gh, 50); !slices.Equal(got, []string{body}) {
		t.Errorf("comments on 50 = %q, want one:\n%s", got, body)
	}
}

// Covers AE3: merged and closed pull requests are left alone, and so is an
// open one in another repository, which may share a number with one here.
func TestOnlyTheOpenPullRequestsOfTheIssuesRepositoryAreWritten(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(
			prNode(48, "MERGED", "o/r", crewWaitingReview),
			prNode(47, "CLOSED", "o/r", crewFailed),
			prNode(51, "OPEN", "other/r", crewInProgress),
			prNode(50, "OPEN", "o/r", crewInProgress))},
		reply{prefix: []string{"pr", "edit", "50"}},
		reply{prefix: commentOn(50), stdout: "900\n"},
	)
	tr.rememberStatus("42", cachedStatus{id: 101, body: "status"})
	report := ended("p1", crewWaitingReview, crew.ActionStatus{Name: "lfg", State: crew.ActionSucceeded})
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}
	if edits := gh.callsTo(prEdit...); len(edits) != 1 || edits[0][2] != "50" {
		t.Errorf("edits = %q, want one of 50", edits)
	}
	for _, n := range []int{47, 48, 51} {
		if got := comments(t, gh, n); len(got) != 0 {
			t.Errorf("commented on %d: %q", n, got)
		}
	}
}

// Covers AE3: an issue without a closing pull request gets only the query.
func TestAnIssueWithoutAPullRequestGetsOnlyTheQuery(t *testing.T) {
	tr, gh := prTracker(t, reply{prefix: prQuery, stdout: prsJSON()})
	report := ended("p1", crewWaitingReview, crew.ActionStatus{Name: "triage", State: crew.ActionSucceeded})
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}
	if len(gh.calls) != 1 {
		t.Errorf("calls = %q, want only the query", gh.calls)
	}
}

// Covers AE4: a crew label put on the pull request by hand, and an extra,
// are replaced by the issue's; a label that is not crew's stays.
func TestTheMirrorReplacesEveryOtherCrewLabel(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(
			prNode(50, "OPEN", "o/r", crewWaitingReview, crewReadyForFix, crewWaitingBrain, "bug"))},
		reply{prefix: prEdit},
	)
	report := crew.PullRequestReport{ID: "p1", IssueKey: "42", IssueRef: "#42", State: crewInProgress}
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}
	want := []string{"pr", "edit", "50", "--remove-label=crew:waiting review", "--remove-label=crew:ready for fix",
		"--remove-label=crew:waiting brainstorm", "--add-label=crew:in progress"}
	if edits := gh.callsTo(prEdit...); len(edits) != 1 || !slices.Equal(edits[0], want) {
		t.Errorf("edits = %q, want one: %q", edits, want)
	}
}

func TestAReportWithoutAnEndPostsNoComment(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewReadyForFix))},
		reply{prefix: prEdit},
	)
	report := crew.PullRequestReport{ID: "p1", IssueKey: "42", IssueRef: "#42", State: crewInProgress}
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}
	if got := comments(t, gh, 50); len(got) != 0 {
		t.Errorf("commented on 50: %q", got)
	}
}

func TestAPullRequestAlreadyInTheStateIsNotEditedButIsCommented(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", "Crew:Waiting Review", "bug"))},
		reply{prefix: commentOn(50), stdout: "900\n"},
	)
	tr.rememberStatus("42", cachedStatus{id: 101, body: "status"})
	report := ended("p1", crewWaitingReview, crew.ActionStatus{Name: "lfg", State: crew.ActionSucceeded})
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}
	if edits := gh.callsTo(prEdit...); len(edits) != 0 {
		t.Errorf("edits = %q, want none", edits)
	}
	if got := comments(t, gh, 50); len(got) != 1 {
		t.Errorf("comments on 50 = %q, want one", got)
	}
}

// Without a cached status comment, the link comes from the issue's comments,
// or is the issue itself when it has no status comment.
func TestTheStopCommentLinksTheStatusCommentItFinds(t *testing.T) {
	for name, tc := range map[string]struct{ listed, want string }{
		"found": {"[" + commentJSON(12, "me", "status\n\n"+statusMarker+"\n") + "]",
			"#42's [status comment](https://github.com/o/r/issues/42#issuecomment-12) has the details."},
		"none": {"[" + commentJSON(13, "me", "no marker") + "]",
			"See [#42](https://github.com/o/r/issues/42)."},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := prTracker(t, login,
				reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewWaitingReview))},
				reply{prefix: issue42List, stdout: tc.listed},
				reply{prefix: commentOn(50), stdout: "900\n"},
			)
			report := ended("p1", crewWaitingReview, crew.ActionStatus{Name: "lfg", State: crew.ActionSucceeded})
			if err := tr.ReportPullRequests(context.Background(), report); err != nil {
				t.Fatalf("ReportPullRequests: %v", err)
			}
			got := comments(t, gh, 50)
			if len(got) != 1 || !slices.Contains(strings.Split(got[0], "\n"), tc.want) {
				t.Errorf("comments on 50 = %q, want one with the line %q", got, tc.want)
			}
		})
	}
}

// Covers AE5: a failed edit is transient, and the retry edits again without
// commenting twice.
func TestARetryAfterAFailedEditDoesNotCommentTwice(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewInProgress))},
		reply{prefix: prEdit, stderr: "HTTP 502: Bad Gateway"},
		reply{prefix: commentOn(50), stdout: "900\n"},
	)
	tr.rememberStatus("42", cachedStatus{id: 101, body: "status"})
	report := ended("p1", crewWaitingReview, crew.ActionStatus{Name: "lfg", State: crew.ActionSucceeded})
	err := tr.ReportPullRequests(context.Background(), report)
	if err == nil || errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
		t.Fatalf("ReportPullRequests = %v, want a transient error", err)
	}

	gh.script[1] = reply{prefix: prEdit}
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if edits := gh.callsTo(prEdit...); len(edits) != 2 {
		t.Errorf("edits = %q, want two", edits)
	}
	if got := comments(t, gh, 50); len(got) != 1 {
		t.Errorf("comments on 50 = %q, want one", got)
	}
}

// Every pull request is written even when one fails, and the retry comments
// only where the comment did not land.
func TestARetryCommentsOnlyWhereTheCommentFailed(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(
			prNode(51, "OPEN", "o/r", crewInProgress), prNode(50, "OPEN", "o/r", crewInProgress))},
		reply{prefix: prEdit},
		reply{prefix: commentOn(50), stdout: "900\n"},
		reply{prefix: commentOn(51), stderr: "HTTP 502: Bad Gateway"},
	)
	tr.rememberStatus("42", cachedStatus{id: 101, body: "status"})
	report := ended("p1", crewWaitingReview, crew.ActionStatus{Name: "lfg", State: crew.ActionSucceeded})
	err := tr.ReportPullRequests(context.Background(), report)
	if err == nil || errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
		t.Fatalf("ReportPullRequests = %v, want a transient error", err)
	}
	var numbers []string
	for _, e := range gh.callsTo(prEdit...) {
		numbers = append(numbers, e[2])
	}
	if !slices.Equal(numbers, []string{"50", "51"}) {
		t.Errorf("edited %q, want 50 then 51", numbers)
	}
	if got := comments(t, gh, 50); len(got) != 1 {
		t.Errorf("comments on 50 = %q, want one", got)
	}

	gh.script[3] = reply{prefix: commentOn(51), stdout: "901\n"}
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := comments(t, gh, 50); len(got) != 1 {
		t.Errorf("comments on 50 after the retry = %q, want one", got)
	}
	if got := comments(t, gh, 51); len(got) != 2 {
		t.Errorf("comment attempts on 51 = %q, want the failed one and one more", got)
	}
}

func TestAMissingLabelIsRefused(t *testing.T) {
	tr, _ := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewInProgress))},
		reply{prefix: prEdit, stderr: "could not add label: 'crew:waiting review' not found\n"},
	)
	report := crew.PullRequestReport{ID: "p1", IssueKey: "42", IssueRef: "#42", State: crewWaitingReview}
	if err := tr.ReportPullRequests(context.Background(), report); !errors.Is(err, port.ErrRefused) {
		t.Errorf("ReportPullRequests = %v, want ErrRefused", err)
	}
}

// A transient failure on one pull request outweighs a refusal on another:
// the report is retried, and the refusal comes back on the retry.
func TestATransientFailureOutweighsARefusal(t *testing.T) {
	tr, _ := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(
			prNode(50, "OPEN", "o/r", crewInProgress), prNode(51, "OPEN", "o/r", crewInProgress))},
		reply{prefix: []string{"pr", "edit", "50"}, stderr: "could not add label: 'crew:waiting review' not found\n"},
		reply{prefix: []string{"pr", "edit", "51"}, stderr: "HTTP 502: Bad Gateway"},
	)
	report := crew.PullRequestReport{ID: "p1", IssueKey: "42", IssueRef: "#42", State: crewWaitingReview}
	err := tr.ReportPullRequests(context.Background(), report)
	if err == nil || errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
		t.Errorf("ReportPullRequests = %v, want a transient error", err)
	}
}

func TestTheQuerysErrorsAreClassified(t *testing.T) {
	for name, tc := range map[string]struct {
		reply reply
		want  error
	}{
		"an issue GitHub cannot resolve": {reply{prefix: prQuery,
			stdout: `{"data":{"repository":{"issue":null}},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to an Issue with the number of 42."}]}`,
			stderr: "gh: Could not resolve to an Issue with the number of 42."}, port.ErrMovedMeanwhile},
		"anything else": {reply{prefix: prQuery, stderr: "HTTP 502: Bad Gateway"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := prTracker(t, tc.reply)
			report := crew.PullRequestReport{ID: "p1", IssueKey: "42", IssueRef: "#42", State: crewWaitingReview}
			err := tr.ReportPullRequests(context.Background(), report)
			switch {
			case err == nil:
				t.Fatal("ReportPullRequests = nil, want an error")
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Errorf("ReportPullRequests = %v, want %v", err, tc.want)
			case tc.want == nil && (errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused)):
				t.Errorf("ReportPullRequests = %v, want a transient error", err)
			}
			if len(gh.calls) != 1 {
				t.Errorf("calls = %q, want only the query", gh.calls)
			}
		})
	}
}

// A failed check's reason may hold backticks; it renders in a code span it
// cannot close, as in the status comment.
func TestACheckReasonWithBackticksStaysInItsCodeSpan(t *testing.T) {
	tr, gh := prTracker(t,
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewInProgress))},
		reply{prefix: prEdit},
		reply{prefix: commentOn(50), stdout: "900\n"},
	)
	tr.rememberStatus("42", cachedStatus{id: 101, body: "status"})
	report := ended("p1", crewFailed, crew.ActionStatus{Name: "lfg", State: crew.ActionFailed, Cause: crew.CauseCheck,
		Reason: "`gh` found no @someone **pull request**", Log: ".crew/logs/issue-42-lfg.log"})
	if err := tr.ReportPullRequests(context.Background(), report); err != nil {
		t.Fatalf("ReportPullRequests: %v", err)
	}
	want := "**`lfg`** failed: `` `gh` found no @someone **pull request** ``. Its log is `.crew/logs/issue-42-lfg.log`."
	got := comments(t, gh, 50)
	if len(got) != 1 || !slices.Contains(strings.Split(got[0], "\n"), want) {
		t.Errorf("comments on 50 = %q, want one with the line %q", got, want)
	}
}
