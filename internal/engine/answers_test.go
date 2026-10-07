package engine_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// waits is develop whose session may wait for an answer: its waiting
// verdict moves the issue to waiting.
var waits = func() crew.Rule {
	r := develop
	r.Actions = slices.Clone(r.Actions)
	r.Actions[0].On = crew.On{crew.Waiting: crew.ToRoute{Route: "waiting"}}
	r.Routes = append(slices.Clone(r.Routes),
		crew.Route{Name: "waiting", Steps: []crew.Step{crew.MoveStep{To: "waiting"}}})
	return r
}()

// listing is a tracker that lists comments and acts as boss, with the
// code owner alice: sessions that ask do so as boss, and alice answers.
type listing struct {
	fake.RoutingTracker
}

// Login implements port.LoginFinder.
func (listing) Login() string { return "boss" }

// CodeOwners implements port.CodeOwnerFinder.
func (listing) CodeOwners() []string { return []string{"alice"} }

// unlisting is listing over a tracker that lists no comments.
type unlisting struct {
	fake.ReportingTracker
}

// Login implements port.LoginFinder.
func (unlisting) Login() string { return "boss" }

// CodeOwners implements port.CodeOwnerFinder.
func (unlisting) CodeOwners() []string { return []string{"alice"} }

// askThenFail runs issue 1's session in r, fails it after it may have
// asked, puts the issue back in ready, as you would, and returns the
// first session's marker, as its prompt gives it.
func askThenFail(t *testing.T, r *rig, tr *fake.Tracker) string {
	t.Helper()
	s := r.session()
	_, after, _ := strings.Cut(s.Run().Prompt, "put your marker `")
	marker, _, ok := strings.Cut(after, "`")
	if !ok {
		t.Fatalf("the first prompt names no marker:\n%s", s.Run().Prompt)
	}
	s.End(port.SessionEnd{Reason: "broke"})
	synctest.Wait()
	tr.SetStates("1", ready)
	time.Sleep(poll)
	return marker
}

// stopped stops r's engine and waits for it.
func stopped(t *testing.T, r *rig) {
	t.Helper()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// Covers KTD-W6: the engine reads the comments through the tracker's
// port.CommentLister, and the resumed session starts with the answers
// that count, which reach neither its run's log nor a status or comment.
func TestTheResumedSessionStartsWithTheAnswersTheTrackerListed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := fake.NewRoutingTracker(issue(1, ready))
		r := start(t, config(t, listing{rt}, waits))

		marker := askThenFail(t, r, rt.Tracker)
		rt.SetComments("1",
			crew.Comment{Author: "boss", Body: "Which database? " + marker},
			crew.Comment{Author: "mallory", Body: "stranger-secret"},
			crew.Comment{Author: "alice", Body: "answer-secret"},
		)
		time.Sleep(poll)
		prompt := r.session().Run().Prompt
		stopped(t, r)

		if !strings.Contains(prompt, "<!-- crew:answer by alice -->\nanswer-secret\n<!-- crew:answer end -->") ||
			strings.Contains(prompt, "stranger-secret") {
			t.Fatalf("prompt does not carry alice's answer alone:\n%s", prompt)
		}
		wantNoCommentText(t, r, rt)
	})
}

// wantNoCommentText fails when the log of issue 1's run in r, a status or
// a comment crew posted on rt holds a listed comment's text.
func wantNoCommentText(t *testing.T, r *rig, rt fake.RoutingTracker) {
	t.Helper()
	log, err := os.ReadFile(filepath.Join(r.root, ".crew", "logs", "issue-1-implement.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "secret") {
		t.Errorf("the run's log holds a comment's text:\n%s", log)
	}
	for _, s := range rt.Statuses("1") {
		if strings.Contains(fmt.Sprintf("%#v", s), "secret") {
			t.Errorf("a status holds a comment's text: %#v", s)
		}
	}
	for _, c := range rt.Posted() {
		if strings.Contains(c.Body, "secret") {
			t.Errorf("a comment crew posted holds a comment's text: %s", c.Body)
		}
	}
}

// Covers R48: a listing that fails starts the session with the paragraph
// that says crew could not read the comments, and why.
func TestAFailedListingStartsTheSessionSayingSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := fake.NewRoutingTracker(issue(1, ready))
		r := start(t, config(t, listing{rt}, waits))

		askThenFail(t, r, rt.Tracker)
		rt.FailCommentLists("1", errors.New("gh: HTTP 502"))
		time.Sleep(poll)
		prompt := r.session().Run().Prompt
		stopped(t, r)

		want := "crew could not read the issue's comments to give you its answers: " +
			`"list the comments of issue 1: gh: HTTP 502".`
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not say %s:\n%s", want, prompt)
		}
	})
}

// A tracker that lists no comments starts the session with the paragraph
// of a failed read.
func TestATrackerThatListsNoCommentsStartsTheSessionSayingSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ft := fake.NewReportingTracker(issue(1, ready))
		r := start(t, config(t, unlisting{ft}, waits))

		askThenFail(t, r, ft.Tracker)
		time.Sleep(poll)
		prompt := r.session().Run().Prompt
		stopped(t, r)

		want := `crew could not read the issue's comments to give you its answers: "the tracker lists no comments".`
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not say %s:\n%s", want, prompt)
		}
	})
}
