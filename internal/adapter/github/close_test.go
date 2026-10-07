package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The scripted prefixes of Close's gh calls on issue #42.
var (
	view42  = []string{"issue", "view", "42", "--json", "state,labels"}
	close42 = []string{"api", "--method", "PATCH", "repos/{owner}/{repo}/issues/42", "-f", "state=closed"}
	edit42  = []string{"issue", "edit", "42"}
	// prs42 finds the pull requests that close issue #42.
	prs42 = []string{"api", "graphql", "-f", "query=" + pullRequestsQuery,
		"-F", "owner={owner}", "-F", "name={repo}", "-F", "number=42"}
)

// viewJSON is gh issue view's reply for an item in state with labels.
func viewJSON(state string, labels ...crew.State) string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = fmt.Sprintf(`{"name":%q}`, l)
	}
	return fmt.Sprintf(`{"state":%q,"labels":[%s]}`, state, strings.Join(names, ","))
}

// closeTracker builds a tracker on crew's own labels writing as ops, with gh
// scripted.
func closeTracker(t *testing.T, script ...reply) (*Tracker, *fakeGh) {
	t.Helper()
	tr, gh := prTracker(t, script...)
	var renewed atomic.Int32
	tr.ActAs(opsWriter(&renewed, nil), []string{opsLogin})
	return tr, gh
}

// wantCalls checks that gh got exactly want, in order, each write as the
// bot and each read as you.
func wantCalls(t *testing.T, gh *fakeGh, want ...[]string) {
	t.Helper()
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if !slices.EqualFunc(gh.calls, want, slices.Equal) {
		t.Fatalf("gh calls =\n%q\nwant\n%q", gh.calls, want)
	}
	for _, c := range gh.cmds {
		if as := runsAs(c); (as == asBot) != isWrite(c.Args) {
			t.Errorf("gh %q runs as %s", c.Args, as)
		}
	}
}

func TestCloseClosesTheIssueThenStripsCrewsLabelsFromItsPullRequestsAndIt(t *testing.T) {
	tr, gh := closeTracker(t,
		reply{prefix: view42, stdout: viewJSON("OPEN", crewInProgress, "bug")},
		reply{prefix: close42},
		reply{prefix: prQuery, stdout: prsJSON(
			prNode(50, "OPEN", "o/r", crewInProgress, "bug"),
			prNode(51, "OPEN", "o/r", "bug"),
			prNode(52, "OPEN", "else/where", crewInProgress))},
		reply{prefix: prEdit},
		reply{prefix: edit42},
	)
	var closer port.Closer = tr
	if err := closer.Close(context.Background(), issueID("42"), crewInProgress); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wantCalls(t, gh, view42, close42, prs42,
		[]string{"pr", "edit", "50", "--remove-label=crew:in progress"},
		[]string{"issue", "edit", "42", "--remove-label=crew:in progress"})
}

func TestCloseOfAnIssueClosedWithoutCrewLabelsStillStripsItsPullRequests(t *testing.T) {
	tr, gh := closeTracker(t,
		reply{prefix: view42, stdout: viewJSON("CLOSED", "bug")},
		reply{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewInProgress))},
		reply{prefix: prEdit},
	)
	if err := tr.Close(context.Background(), issueID("42"), crewInProgress); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wantCalls(t, gh, view42, prs42,
		[]string{"pr", "edit", "50", "--remove-label=crew:in progress"})
}

func TestCloseOfAnIssueClosedInFromStripsEveryCrewLabelWithoutClosingIt(t *testing.T) {
	tr, gh := closeTracker(t,
		reply{prefix: view42, stdout: viewJSON("CLOSED", crewInProgress, "bug", crewFailed)},
		reply{prefix: prQuery, stdout: prsJSON()},
		reply{prefix: edit42},
	)
	if err := tr.Close(context.Background(), issueID("42"), crewInProgress); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wantCalls(t, gh, view42, prs42,
		[]string{"issue", "edit", "42", "--remove-label=crew:in progress", "--remove-label=crew:failed"})
}

func TestCloseThatCannotGoOnWritesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		view reply
		want error
	}{
		"open but no longer in from": {
			view: reply{prefix: view42, stdout: viewJSON("OPEN", crewFailed)}, want: port.ErrMovedMeanwhile},
		"open in no crew state": {
			view: reply{prefix: view42, stdout: viewJSON("OPEN", "bug")}, want: port.ErrMovedMeanwhile},
		"closed in another crew state": {
			view: reply{prefix: view42, stdout: viewJSON("CLOSED", crewFailed)}, want: port.ErrMovedMeanwhile},
		"gone": {view: reply{prefix: view42,
			stderr: "GraphQL: Could not resolve to an issue or pull request with the number of 42. (repository.issue)"},
			want: port.ErrMovedMeanwhile},
		"a merged pull request": {
			view: reply{prefix: view42, stdout: viewJSON("MERGED", crewInProgress)}, want: port.ErrRefused},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := closeTracker(t, tc.view)
			err := tr.Close(context.Background(), issueID("42"), crewInProgress)
			if err == nil || !strings.Contains(err.Error(), "close issue #42") {
				t.Fatalf("Close = %v, want an error naming issue #42", err)
			}
			wantClassified(t, err, tc.want)
			wantCalls(t, gh, view42)
		})
	}
}

func TestCloseErrorsAreClassified(t *testing.T) {
	open := reply{prefix: view42, stdout: viewJSON("OPEN", crewInProgress)}
	for name, tc := range map[string]struct {
		script []reply
		want   error // nil: transient
	}{
		"the read fails":  {script: []reply{{prefix: view42, stderr: "HTTP 502: Bad Gateway"}}},
		"the close fails": {script: []reply{open, {prefix: close42, stderr: "gh: HTTP 502: Bad Gateway\n"}}},
		"the issue is gone": {script: []reply{open, {prefix: close42, stderr: "gh: Not Found (HTTP 404)\n"}},
			want: port.ErrMovedMeanwhile},
		"the close refused": {script: []reply{open, {prefix: close42, stderr: "gh: Forbidden (HTTP 403)\n"}},
			want: port.ErrRefused},
		"the pull requests' query fails": {script: []reply{open, {prefix: close42},
			{prefix: prQuery, stderr: "HTTP 502: Bad Gateway"}}},
		// The issue keeps its labels, so a retry still finds the pull request:
		// an edit of #42 is unscripted and would fail the test.
		"a pull request's edit fails": {script: []reply{open, {prefix: close42},
			{prefix: prQuery, stdout: prsJSON(prNode(50, "OPEN", "o/r", crewInProgress))},
			{prefix: prEdit, stderr: "HTTP 502: Bad Gateway"}}},
		"the label is gone": {script: []reply{open, {prefix: close42}, {prefix: prQuery, stdout: prsJSON()},
			{prefix: edit42, stderr: "'crew:in progress' not found\n"}}, want: port.ErrRefused},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := prTracker(t, tc.script...)
			err := tr.Close(context.Background(), issueID("42"), crewInProgress)
			if err == nil || !strings.Contains(err.Error(), "close issue #42") {
				t.Fatalf("Close = %v, want an error naming issue #42", err)
			}
			wantClassified(t, err, tc.want)
		})
	}
}
