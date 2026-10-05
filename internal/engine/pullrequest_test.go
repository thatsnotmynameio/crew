package engine_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// prStates returns the state and whether it ended a rule of each pull
// request report recorded for issue 1, in order.
func prStates(tr fake.PullRequestTracker) []string {
	reports := tr.PullRequestReports("1")
	out := make([]string, 0, len(reports))
	for _, r := range reports {
		s := string(r.State)
		if r.End != nil {
			s += " (end of " + r.End.Rule + ")"
		}
		out = append(out, s)
	}
	return out
}

func TestF1ATakenIssueThatSucceedsReportsItsTakeThenItsVerdictOnThePullRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewPullRequestTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		s := r.sessions(1)["issue-1-development"]
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()

		want := []string{string(inProgress), string(readyToReview) + " (end of implement)"}
		if got := prStates(tr); !reflect.DeepEqual(got, want) {
			t.Errorf("pull request reports = %q, want %q", got, want)
		}
		end := tr.PullRequestReports("1")[1].End
		if len(end.Actions) != 1 || end.Actions[0].State != crew.ActionSucceeded {
			t.Errorf("stage end = %+v, want development succeeded", end)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestATrackerWithoutPullRequestReportsGetsNone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewReportingTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		s := r.sessions(1)["issue-1-development"]
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
	})
}

func TestAE5AFailedPullRequestReportIsRetriedAtTheNextPollWhileTheIssueKeepsItsMove(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewPullRequestTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		s := r.sessions(1)["issue-1-development"]
		synctest.Wait()

		tr.FailPullRequests("1", errors.New("gh: HTTP 502"))
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		if got := states(t, tr, "1"); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
		if got := prStates(tr); len(got) != 1 {
			t.Fatalf("pull request reports before the retry = %q, want only the take's", got)
		}

		time.Sleep(poll)
		synctest.Wait()
		want := []string{string(inProgress), string(readyToReview) + " (end of implement)"}
		if got := prStates(tr); !reflect.DeepEqual(got, want) {
			t.Errorf("pull request reports after the retry = %q, want %q", got, want)
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestARefusedPullRequestReportIsNotRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewPullRequestTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		s := r.sessions(1)["issue-1-development"]
		synctest.Wait()

		tr.FailPullRequests("1", fmt.Errorf("pull request is locked: %w", port.ErrRefused))
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		time.Sleep(poll)
		synctest.Wait()

		if got := prStates(tr); len(got) != 1 {
			t.Errorf("pull request reports = %q, want only the take's", got)
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestStoppingGivesAnOwedPullRequestReportOneFinalTry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewPullRequestTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))
		s := r.sessions(1)["issue-1-development"]
		synctest.Wait()

		tr.FailPullRequests("1", errors.New("gh: HTTP 502"))
		s.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		want := []string{string(inProgress), string(readyToReview) + " (end of implement)"}
		if got := prStates(tr); !reflect.DeepEqual(got, want) {
			t.Errorf("pull request reports = %q, want %q", got, want)
		}
	})
}
