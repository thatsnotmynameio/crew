package app_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// The labels of crew's answered rule.
const (
	answeredLabel  crew.State = "crew:answered"
	answeredFailed crew.State = "crew:answered:failed"
)

// plainAnswer is the answer to deps's question blocks, in plain text,
// without the question's parameters (R8).
const plainAnswer = "yes, #284 removes the tests R20 rewrites"

// answerable returns questioned's tracker, on which octocat, the
// answerer, is a code owner.
func answerable() fake.RoutingTracker {
	tr := questioned()
	tr.SetCodeOwners("octocat")
	return tr
}

// askAndDelegate starts r, ends deps's first session on h with the verdict
// unsure, which asks the question blocks, then waits a poll for crew's
// question rule to delegate it: #1 waits in crew:question:waiting answer.
func askAndDelegate(t *testing.T, r *crewRun, tr fake.RoutingTracker, h *fake.Harness) {
	t.Helper()
	r.start()
	s := next(t, h)
	if err := os.WriteFile(s.Run().VerdictFile, []byte("unsure\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.End(success)
	synctest.Wait()
	wantIn(t, tr, questionLabel)
	time.Sleep(poll)
	synctest.Wait()
	wantIn(t, tr, questionWaiting)
}

// answer posts body on #1 as author, then moves #1 to crew:answered, as
// the answerer does (R7).
func answer(tr fake.RoutingTracker, author, body string) {
	tr.AddComment("1", crew.Comment{Author: author, Body: body})
	tr.SetStates("1", answeredLabel)
}

// polls lets n polls go by on the fake clock.
func polls(n int) {
	for range n {
		time.Sleep(poll)
		synctest.Wait()
	}
}

// Covers F1, AE2, R7, R8, R9, R11 and KTD10: on the in-memory tracker, a
// session's verdict asks the question blocks; crew's question rule
// delegates it to octocat and asks for the move to crew:answered; octocat,
// a code owner, answers in plain text and moves #1 there; crew's answered
// rule returns #1 to deps's ready label; and deps's next session gets a
// prompt that holds the answer. #1's thread holds the question, the
// delegation and the answer.
func TestAnAnsweredQuestionReturnsToTheRuleThatAskedWithItsAnswer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := answerable()
		h := fake.NewHarness()
		r := options(t, asking, tr, h)
		askAndDelegate(t, r, tr, h)

		answer(tr, "octocat", plainAnswer)
		polls(1)
		wantIn(t, tr, depsReady)

		time.Sleep(poll)
		s := next(t, h)
		if prompt := s.Run().Prompt; !strings.Contains(prompt, plainAnswer) {
			t.Errorf("deps's next prompt =\n%s\nwant it to hold the answer %q", prompt, plainAnswer)
		}
		s.End(success)
		synctest.Wait()
		stop(t, r)

		wantIn(t, tr, depsDone)
		if got := tr.Delegations(); len(got) != 1 || got[0].MoveTo != answeredLabel {
			t.Errorf("delegations = %+v, want one that asks for the move to %s", got, answeredLabel)
		}
		thread, err := tr.Comments(context.Background(), issueID("1"))
		if err != nil || len(thread) != 3 ||
			!strings.Contains(thread[0].Body, crew.QuestionMarker("blocks", "deps", depsReady)) ||
			!strings.Contains(thread[1].Body, crew.DelegatedMarker("blocks")) ||
			thread[2] != (crew.Comment{Author: "octocat", Body: plainAnswer}) {
			t.Errorf("#1's thread = %+v, %v, want the question, the delegation and the answer", thread, err)
		}
	})
}

// Covers AE5 and R7: a comment alone, without the move to crew:answered,
// returns nothing: #1 stays waiting, and deps runs no session.
func TestACommentWithoutTheMoveLeavesTheItemWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := answerable()
		h := fake.NewHarness()
		r := options(t, asking, tr, h)
		askAndDelegate(t, r, tr, h)

		tr.AddComment("1", crew.Comment{Author: "octocat", Body: "let me think about it"})
		polls(3)
		stop(t, r)

		wantIn(t, tr, questionWaiting)
		if n := len(h.Sessions()); n != 1 {
			t.Errorf("deps ran %d sessions, want only the one that asked", n)
		}
	})
}

// Covers AE4, R10 and KTD7: an answer by someone who is no code owner,
// then the move to crew:answered: crew's answered rule moves #1 to
// crew:answered:failed with a report whose reason is that no answer
// counts, and deps does not resume.
func TestAnAnswerThatDoesNotCountFailsTheItemWithAReport(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := answerable()
		h := fake.NewHarness()
		r := options(t, asking, tr, h)
		askAndDelegate(t, r, tr, h)

		answer(tr, "mallory", plainAnswer)
		polls(2)
		stop(t, r)

		wantIn(t, tr, answeredFailed)
		want := crew.ActionFailure{Action: "answer", Verdict: crew.Unanswered, Reason: crew.ReasonUnanswered}
		reports := tr.Reports()
		if len(reports) != 1 || reports[0].Rule != "answered" || len(reports[0].Failures) != 1 ||
			reports[0].Failures[0] != want {
			t.Errorf("reports = %+v, want the answered rule's, with %+v", reports, want)
		}
		if n := len(h.Sessions()); n != 1 {
			t.Errorf("deps ran %d sessions, want only the one that asked", n)
		}
	})
}

// Covers F1, R9 and KTD9: a question action between two shell actions,
// answered and returned: deps's next run starts at the second shell
// action, and the first does not run again.
func TestAnAnsweredQuestionActionResumesAtTheActionAfterIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := answerable()
		sh := fake.NewShell()
		r := options(t, askingBetweenShells, tr, fake.NewHarness())
		r.opts.Shell = sh
		r.start()
		synctest.Wait()
		wantIn(t, tr, questionLabel)
		polls(1)
		wantIn(t, tr, questionWaiting)

		answer(tr, "octocat", plainAnswer)
		polls(1)
		wantIn(t, tr, depsReady)
		polls(1)
		stop(t, r)

		wantIn(t, tr, depsDone)
		runs := sh.Runs()
		ran := make([]crew.ActionName, 0, len(runs))
		for _, s := range runs {
			ran = append(ran, s.Name)
		}
		if want := []crew.ActionName{"first", "second"}; !slices.Equal(ran, want) {
			t.Errorf("shell actions run = %v, want %v", ran, want)
		}
	})
}
