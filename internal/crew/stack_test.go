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

// openedOnMain is the run's new workspace, ready at minute 2, made where
// origin's default branch is main.
func openedOnMain() RunEvent {
	return WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog, DefaultBranch: "main"}
}

func TestAPromptReadsTheRunsBaseAndStack(t *testing.T) {
	stacked, _ := stackedTake().(RunTaken)
	tests := []struct {
		name   string
		events []RunEvent
		want   PromptData
	}{
		{
			name: "unstacked, the default branch", events: []RunEvent{taken(), openedOnMain()},
			want: PromptData{Issue: NewIssue(testIssue()), Base: "main"},
		},
		{
			name: "stacked, the pull request's branch", events: []RunEvent{stackedTake(), openedOnMain()},
			want: PromptData{
				Issue: NewIssue(stacked.Issue), Base: stackOn45.Branch, Stack: Some(stackOn45),
			},
		},
		{
			name: "before its workspace, no base", events: []RunEvent{taken()},
			want: PromptData{Issue: NewIssue(testIssue())},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := given(t, tt.events).PromptData(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PromptData() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// stackedOnlyPrompt gives lfg a prompt that renders for the samples and
// for any unstacked run, and fails for a stacked run of the shorter
// "Issue 9".
func stackedOnlyPrompt(d RunDefinition) RunDefinition {
	spec, _ := d.Rule.Actions[1].Kind.(SessionSpec)
	spec.Prompt = mustPrompt("lfg", "{{if .Stack.Stacked}}{{index .Issue.Title 11}}{{end}}")
	d.Rule.Actions[1].Kind = spec
	return d
}

// stackedInstalling is installing, for the run stacked on #45.
func stackedInstalling() []RunEvent {
	events := installing()
	events[0] = stackedTake()
	return events
}

// stackedRenderError is the reason stackedOnlyPrompt gives for the stacked
// run.
func stackedRenderError() SessionText {
	spec, _ := stackedOnlyPrompt(sequence()).Rule.Actions[1].Kind.(SessionSpec)
	data := PromptData{Issue: NewIssue(testIssue()), Stack: Some(stackOn45)}
	_, err := spec.Prompt.Render(data)
	if err == nil {
		panic("the stacked-only prompt rendered")
	}
	return NewSessionText(err.Error())
}

// The prompt the run checks before it asks for the session is the one the
// session starts with: rendered with the run's stack, which can fail it.
func TestTheSessionsPromptIsCheckedWithTheRunsStack(t *testing.T) {
	decide(t, []decision{
		{
			name: "stacked: the prompt that fails for the stack fails the session",
			def:  stackedOnlyPrompt, given: stackedInstalling(), fact: shellEnded(3, "install", exited(0, "install passed")),
			want: []RunEvent{
				ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
				passedEnd(3, "install", "install passed"),
				ActionEnded{
					EventHead: eh(3), Action: "lfg", Verdict: Failed, Target: toFailed,
					End: EndFailed{Reason: stackedRenderError(), Cause: CausePrompt},
				},
				chose(3, FailedRoute, "lfg"), asked(3, 0),
			},
		},
		{
			name: "unstacked: the same prompt asks for the session",
			def:  stackedOnlyPrompt, given: installing(), fact: shellEnded(3, "install", exited(0, "install passed")),
			want: []RunEvent{
				ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
				passedEnd(3, "install", "install passed"),
				ActionSessionAsked{EventHead: eh(3), Action: "lfg"},
			},
		},
	})
}
