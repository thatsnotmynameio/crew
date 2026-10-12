package crew

import (
	"reflect"
	"testing"
)

// blockerID is the issue that blocks #9 in the stacking tests: #8.
var blockerID = IssueID{Repository: "R_1", Key: "8"}

// stackReading is #9's stack reading: #8 blocks it alone, with its open
// pull request #45, which #46 is based on, and #9's parent has #9 and #10.
func stackReading() StackReading {
	return StackReading{
		Blocker: Some(StackBlocker{
			ID: blockerID, Ref: "#8", URL: "https://example.com/issues/8", States: []State{labelDone},
			PullRequests: []StackPullRequest{{
				Ref: "#45", URL: "https://example.com/pull/45", Head: "crew/issue-8-implement", Base: "main",
				Connected: true, Stacked: []string{"crew/issue-11-implement"},
			}},
		}),
		Siblings: []IssueID{testID, {Repository: "R_1", Key: "10"}},
	}
}

// stackOn45 is a run's stack on #8's pull request #45.
var stackOn45 = Stack{
	Blocker: blockerID, BlockerRef: "#8", BlockerURL: "https://example.com/issues/8",
	PullRequestRef: "#45", PullRequestURL: "https://example.com/pull/45", Branch: "crew/issue-8-implement",
}

func TestAnIssueKeepsItsOwnCopyOfItsStackReading(t *testing.T) {
	given := stackReading()
	i := NewIssue(IssueData{ID: testID, Stacking: given})
	changeStackReading(given)
	read := i.Data()
	changeStackReading(read.Stacking)
	if got := i.Data().Stacking; !reflect.DeepEqual(got, stackReading()) {
		t.Errorf("Stacking = %#v after changing the given and returned readings, want %#v", got, stackReading())
	}
}

// changeStackReading changes every list s holds in place.
func changeStackReading(s StackReading) {
	s.Siblings[0] = IssueID{Key: "changed"}
	b, _ := s.Blocker.Get()
	b.States[0] = "changed"
	b.PullRequests[0].Head = "changed"
	b.PullRequests[0].Stacked[0] = "changed"
}

// stackedTake is the test run's take, stacked on #45 and read with #9's
// stack reading.
func stackedTake() RunEvent {
	e, _ := taken().(RunTaken)
	e.Issue.Stacking = stackReading()
	e.Stack = Some(stackOn45)
	return e
}

func TestATakeWithAStackGivesAStackedRun(t *testing.T) {
	if got := given(t, []RunEvent{stackedTake()}).Stack(); got != Some(stackOn45) {
		t.Errorf("Stack() = %#v, want %#v", got, stackOn45)
	}
	if got := given(t, []RunEvent{taken()}).Stack(); got != (Optional[Stack]{}) {
		t.Errorf("Stack() without one taken = %#v, want none", got)
	}
}

func TestAnOpenedWorkspaceNamesTheDefaultBranch(t *testing.T) {
	run := given(t, []RunEvent{
		taken(), WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog, DefaultBranch: "main"},
	})
	w, _ := run.Workspace().Get()
	if w.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want main", w.DefaultBranch)
	}
}
