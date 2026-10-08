package fake_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestOnlyARoutingTrackerCommentsClosesAndListsComments(t *testing.T) {
	for name, tr := range map[string]port.Tracker{
		"Tracker":          fake.NewTracker(),
		"ReportingTracker": fake.NewReportingTracker(),
	} {
		if _, ok := tr.(port.Commenter); ok {
			t.Errorf("%s implements port.Commenter", name)
		}
		if _, ok := tr.(port.Closer); ok {
			t.Errorf("%s implements port.Closer", name)
		}
		if _, ok := tr.(port.CommentLister); ok {
			t.Errorf("%s implements port.CommentLister", name)
		}
	}
}

func TestRoutingTrackerRecordsACommentAndACloseThatHidesTheIssue(t *testing.T) {
	tr := fake.NewRoutingTracker(issue("1", inProgress), issue("2", inProgress))
	tr.SetLabels("1", "bug")
	ctx := context.Background()

	if err := tr.Comment(ctx, issueID("1"), "Done.\n"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if err := tr.Close(ctx, issueID("1"), inProgress); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if want := []fake.Comment{{Key: "1", Body: "Done.\n"}}; !reflect.DeepEqual(tr.Posted(), want) {
		t.Errorf("Posted = %+v, want %+v", tr.Posted(), want)
	}
	if want := []fake.Closing{{Key: "1", From: inProgress}}; !reflect.DeepEqual(tr.Closings(), want) {
		t.Errorf("Closings = %+v, want %+v", tr.Closings(), want)
	}
	got, err := tr.List(ctx, []crew.State{inProgress})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"2"}; !reflect.DeepEqual(keys(got), want) {
		t.Errorf("List keys = %v, want %v: the closed issue is gone", keys(got), want)
	}
	if closed, _ := tr.Issue("1"); len(closed.States()) != 0 {
		t.Errorf("closed issue states = %v, want none", closed.States())
	}
	if want := []crew.State{"bug"}; !reflect.DeepEqual(tr.Labels("1"), want) {
		t.Errorf("closed issue labels = %v, want %v", tr.Labels("1"), want)
	}
}

func TestRoutingTrackerCloseChecksTheIssueIsInFromAsTheAdapterDoes(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(tr fake.RoutingTracker)
		want  error // nil: closed
	}{
		"unknown": {setup: func(fake.RoutingTracker) {}, want: port.ErrMovedMeanwhile},
		"open but no longer in from": {setup: func(tr fake.RoutingTracker) { tr.Add(issue("1", needsAttention)) },
			want: port.ErrMovedMeanwhile},
		"closed in another crew state": {setup: closedIn(needsAttention), want: port.ErrMovedMeanwhile},
		"closed in from":               {setup: closedIn(inProgress)},
		"closed in no crew state":      {setup: closedIn()},
	} {
		t.Run(name, func(t *testing.T) {
			tr := fake.NewRoutingTracker()
			tc.setup(tr)
			err := tr.Close(context.Background(), issueID("1"), inProgress)
			if !errors.Is(err, tc.want) || (err == nil) != (tc.want == nil) {
				t.Fatalf("Close = %v, want %v", err, tc.want)
			}
			if got, ok := tr.Issue("1"); ok && tc.want == nil && len(got.States()) != 0 {
				t.Errorf("states = %v, want none", got.States())
			}
		})
	}
}

// closedIn returns a setup that adds issue 1 in states, closed.
func closedIn(states ...crew.State) func(tr fake.RoutingTracker) {
	return func(tr fake.RoutingTracker) {
		tr.Add(issue("1", states...))
		tr.CloseIssue("1")
	}
}

func TestRoutingTrackerServesTheScriptedComments(t *testing.T) {
	tr := fake.NewRoutingTracker(issue("1", inProgress))
	listed := []crew.Comment{
		{Author: "ana", Body: "First.", Created: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
		{Author: "crew-ops[bot]", App: true, Body: "crew: done."},
	}
	tr.SetComments("1", listed...)
	got, err := tr.Comments(context.Background(), issueID("1"))
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if !reflect.DeepEqual(got, listed) {
		t.Errorf("Comments = %+v, want %+v", got, listed)
	}
	if none, err := tr.Comments(context.Background(), issueID("2")); err != nil || len(none) != 0 {
		t.Errorf("Comments of an unscripted issue = %v, %v; want none", none, err)
	}
}

func TestRoutingTrackerScriptedFailuresComeInOrderThenCallsSucceed(t *testing.T) {
	tr := fake.NewRoutingTracker(issue("1", inProgress))
	tr.FailComments("1", port.ErrRefused)
	tr.FailClosings("1", port.ErrMovedMeanwhile)
	tr.FailCommentLists("1", errors.New("HTTP 502"))
	ctx := context.Background()

	if err := tr.Comment(ctx, issueID("1"), "hi"); !errors.Is(err, port.ErrRefused) {
		t.Errorf("first Comment = %v, want ErrRefused", err)
	}
	if err := tr.Close(ctx, issueID("1"), inProgress); !errors.Is(err, port.ErrMovedMeanwhile) {
		t.Errorf("first Close = %v, want ErrMovedMeanwhile", err)
	}
	if _, err := tr.Comments(ctx, issueID("1")); err == nil || errors.Is(err, port.ErrRefused) ||
		errors.Is(err, port.ErrMovedMeanwhile) {
		t.Errorf("first Comments = %v, want a transient error", err)
	}
	if len(tr.Posted()) != 0 || len(tr.Closings()) != 0 {
		t.Fatalf("Posted = %v, Closings = %v; want none after failed calls", tr.Posted(), tr.Closings())
	}
	if err := tr.Comment(ctx, issueID("1"), "hi"); err != nil {
		t.Errorf("second Comment: %v", err)
	}
	if err := tr.Close(ctx, issueID("1"), inProgress); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := tr.Comments(ctx, issueID("1")); err != nil {
		t.Errorf("second Comments: %v", err)
	}
}

// R12, AE7: the comments and delegations the routing tracker posts join
// the issue's comments, after the scripted ones, written as the writer set
// and carrying crew's own marker, as the github adapter posts them; it
// counts each listing and tells the login set.
func TestRoutingTrackerListsWhatItPostedAsTheWriter(t *testing.T) {
	tr := fake.NewRoutingTracker(issue("1", inProgress))
	scripted := crew.Comment{Author: "ana", Body: "First."}
	tr.SetComments("1", scripted)
	tr.SetWriter("crew-ops[bot]")
	tr.SetLogin("boss")
	ctx := context.Background()

	if err := tr.Comment(ctx, issueID("1"), "Is it done?"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	d := crew.Delegation{
		IssueID: issueID("1"), IssueRef: "#1", Answerer: "octocat", Search: crew.QuestionFound, ID: "done",
	}
	if err := tr.Delegate(ctx, d); err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	got, err := tr.Comments(ctx, issueID("1"))
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != 3 || got[0] != scripted {
		t.Fatalf("Comments = %+v, want the scripted one, then the comment and the delegation", got)
	}
	if got[1].Author != "crew-ops[bot]" || got[1].Body != "Is it done?\n\n"+crew.PostedMarker+"\n" {
		t.Errorf("posted comment = %+v, want the body and crew's marker, by crew-ops[bot]", got[1])
	}
	if got[2].Author != "crew-ops[bot]" || !crew.HoldsPostedMarker(got[2].Body) ||
		!strings.Contains(got[2].Body, crew.DelegatedMarker("done")) || !strings.Contains(got[2].Body, "octocat") {
		t.Errorf("delegation = %+v, want octocat, its marker and crew's, by crew-ops[bot]", got[2])
	}
	if n := tr.CommentLists("1"); n != 1 {
		t.Errorf("CommentLists = %d, want 1", n)
	}
	if login := tr.Login(); login != "boss" {
		t.Errorf("Login = %q, want boss", login)
	}
}
