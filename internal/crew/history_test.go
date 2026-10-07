package crew

import (
	"maps"
	"testing"
)

// The past of the history tests: runs of the rule implement on issue #9,
// whose actions run in the workspaces ws and log to logOf.

// hh returns the head of an event of run at minute n, of the rule
// implement on issue #9.
func hh(run RuleRunID, n int) EventHead {
	h := eh(n)
	h.Run = run
	return h
}

// ws returns the workspace a run of action works in, in the history
// tests.
func ws(action ActionName) Workspace {
	return Workspace{Name: WorkspaceName("issue-9-" + action), Branch: "crew/issue-9-" + string(action)}
}

func logOf(action ActionName) string { return ".crew/logs/issue-9-" + string(action) + ".log" }

// opening is a run's workspace ws(action) ready, and action's session asked
// in it, with head h.
func opening(h EventHead, action ActionName) []RunEvent {
	return []RunEvent{
		WorkspaceOpened{EventHead: h, Workspace: ws(action), Log: logOf(action)},
		ActionSessionAsked{EventHead: h, Action: action},
	}
}

// ending is action's end at minute n, in run, after a session that started
// when session says so.
func ending(run RuleRunID, n int, action ActionName, end ActionEnd, session bool) ActionEnded {
	e := ActionEnded{EventHead: hh(run, n), Action: action, End: end, Verdict: Passed, Target: Next{}}
	if _, failed := end.(EndFailed); failed {
		e.Verdict, e.Target = Failed, toFailed
	}
	if session {
		e.SessionStarted = Some(at(n - 1))
	}
	return e
}

func failedEnd(reason string) ActionEnd {
	return EndFailed{Reason: NewSessionText(reason), Cause: CauseSession}
}

func folded(events ...[]RunEvent) *History {
	var h History
	for _, e := range seq(events...) {
		h.Fold(e)
	}
	return &h
}

// point returns the resume point of action in ws(action) with reason.
func point(action ActionName, reason string) ResumePoint {
	return ResumePoint{Workspace: ws(action), Log: logOf(action), Reason: NewSessionText(reason), Action: action}
}

func wantPoints(t *testing.T, h *History, issue IssueID, rule RuleName, want map[ActionName]ResumePoint) {
	t.Helper()
	if got := h.ResumePoints(issue, rule); !maps.Equal(got, want) {
		t.Errorf("ResumePoints(%v, %s) = %+v, want %+v", issue, rule, got, want)
	}
}

// one returns e alone as events.
func one(e RunEvent) []RunEvent { return []RunEvent{e} }

func TestAE3AnActionThatStartedAndNeverEndedResumesAsCrashed(t *testing.T) {
	h := folded(opening(hh("run-1", 3), "implement"))
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{
		"implement": point("implement", "crew stopped before the run ended: it crashed or was killed"),
	})
}

func TestAFailedEndResumesAndASucceededOneDoesNot(t *testing.T) {
	h := folded(
		one(taken()),
		opening(hh(testRun, 3), "development"), one(ending(testRun, 6, "development", failedEnd("tests fail"), true)),
		opening(hh("run-3", 3), "review"),
		one(ending("run-3", 6, "review", EndSucceeded{Reason: NewSessionText("done")}, true)),
	)
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
}

func TestAnEndWithoutAWorkspaceKeepsTheFailedRunsResumePoint(t *testing.T) {
	gone := ActionEnded{EventHead: hh("run-2", 4), Action: "development", End: stopEnd}
	h := folded(
		opening(hh("run-1", 3), "development"), one(ending("run-1", 6, "development", failedEnd("tests fail"), true)),
		one(gone),
	)
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
}

// reasonCase is a past that ends in development's last action run, and the
// resume point it leaves.
type reasonCase struct {
	name   string
	events [][]RunEvent
	want   ResumePoint
}

// notFound is the end of a session that failed to start.
var notFound = EndFailed{Reason: NewSessionText("start claude: not found"), Cause: CauseStart}

// afterFailure is development's failed run in ws("development") at
// minutes 1 and 2, then events.
func afterFailure(events ...[]RunEvent) [][]RunEvent {
	return append([][]RunEvent{
		opening(hh("run-1", 1), "development"),
		one(ending("run-1", 2, "development", failedEnd("the session's reason"), true)),
	}, events...)
}

// inheritedReasons are the ends without a session that keep an earlier
// failure's reason.
func inheritedReasons() []reasonCase {
	stoppedOpening := WorkspaceOpened{EventHead: hh("run-2", 3), Workspace: ws("development")}
	return []reasonCase{
		{
			name: "failed to start",
			events: afterFailure(
				opening(hh("run-2", 3), "development"), one(ending("run-2", 4, "development", notFound, false)),
			),
			want: point("development", "the session's reason"),
		},
		{
			name:   "stopped while reopening",
			events: afterFailure(one(stoppedOpening), one(ending("run-2", 3, "development", stopEnd, false))),
			want: ResumePoint{
				Workspace: ws("development"), Reason: NewSessionText("the session's reason"), Action: "development",
			},
		},
		{
			name: "after a crash",
			events: [][]RunEvent{
				opening(hh("run-1", 1), "development"),
				opening(hh("run-2", 3), "development"), one(ending("run-2", 4, "development", notFound, false)),
			},
			want: point("development", "crew stopped before the run ended: it crashed or was killed"),
		},
		{
			name:   "with no start since",
			events: afterFailure(one(ending("run-2", 4, "development", notFound, false))),
			want:   point("development", "the session's reason"),
		},
	}
}

// ownReasons are the ends that keep their own reason.
func ownReasons() []reasonCase {
	elsewhere := Workspace{Name: "issue-9-development-2", Branch: "crew/issue-9-development-2"}
	return []reasonCase{
		{
			name: "a session that started",
			events: afterFailure(
				opening(hh("run-2", 3), "development"),
				one(ending("run-2", 6, "development", failedEnd("new"), true)),
			),
			want: point("development", "new"),
		},
		{
			name: "after a success",
			events: [][]RunEvent{
				opening(hh("run-1", 1), "development"),
				one(ending("run-1", 2, "development", EndSucceeded{Reason: NewSessionText("done")}, true)),
				opening(hh("run-2", 3), "development"), one(ending("run-2", 4, "development", notFound, false)),
			},
			want: point("development", "start claude: not found"),
		},
		{
			name: "in another workspace",
			events: afterFailure([]RunEvent{
				WorkspaceOpened{EventHead: hh("run-2", 3), Workspace: elsewhere, Log: logOf("development")},
				ActionSessionAsked{EventHead: hh("run-2", 3), Action: "development"},
				ending("run-2", 4, "development", notFound, false),
			}),
			want: ResumePoint{
				Workspace: elsewhere, Log: logOf("development"), Reason: NewSessionText("start claude: not found"),
				Action: "development",
			},
		},
	}
}

func TestAnEndWithoutASessionKeepsTheEarlierFailuresReason(t *testing.T) {
	for _, tc := range inheritedReasons() {
		t.Run(tc.name, func(t *testing.T) {
			wantPoints(t, folded(tc.events...), testID, "implement", map[ActionName]ResumePoint{"development": tc.want})
		})
	}
}

func TestAnEndKeepsItsOwnReasonOtherwise(t *testing.T) {
	for _, tc := range ownReasons() {
		t.Run(tc.name, func(t *testing.T) {
			wantPoints(t, folded(tc.events...), testID, "implement", map[ActionName]ResumePoint{"development": tc.want})
		})
	}
}

func TestEachIssueAndRuleKeepsItsOwnResumePoints(t *testing.T) {
	other := IssueID{Repository: "R_1", Key: "10"}
	inOtherIssue := hh("run-3", 3)
	inOtherIssue.IssueID, inOtherIssue.IssueRef = other, "#10"
	inFix := hh("run-4", 3)
	inFix.Rule = "fix"
	// fix's run starts in implement's workspace: History retires nothing
	// by itself; the core has it Retire.
	h := folded(
		opening(hh("run-1", 3), "development"), one(ending("run-1", 6, "development", failedEnd("tests fail"), true)),
		opening(inOtherIssue, "development"), opening(inFix, "development"),
	)
	crashed := point("development", "crew stopped before the run ended: it crashed or was killed")
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
	wantPoints(t, h, testID, "fix", map[ActionName]ResumePoint{"development": crashed})
	wantPoints(t, h, other, "implement", map[ActionName]ResumePoint{"development": crashed})
	wantPoints(t, h, other, "fix", map[ActionName]ResumePoint{})
}

func TestTheLastRunIsTheOneWhoseEventsCameLast(t *testing.T) {
	h := folded([]RunEvent{
		RunTaken{EventHead: hh("run-1", 0), Issue: testIssue(), From: labelReady, To: labelRunning},
		RunReleased{EventHead: hh("run-1", 1)},
		taken(), takeMoved(),
	})
	run, ok := h.LastRun(testID, "implement")
	if !ok || run.ID() != testRun || run.Phase() != (RunningPhase{}) || len(run.Actions()) != 3 {
		t.Errorf("LastRun = %#v, %v, want run-2 running its three actions", run.Snapshot(), ok)
	}
	if _, ok := h.LastRun(testID, "fix"); ok {
		t.Errorf("fix has a last run, want none")
	}
}

func TestARunWithGapsFolds(t *testing.T) {
	h := folded(
		one(WorkspaceOpened{EventHead: hh("run-1", 5), Workspace: ws("development"), Log: logOf("development")}),
		one(ending("run-1", 6, "development", failedEnd("tests fail"), true)),
	)
	run, ok := h.LastRun(testID, "implement")
	a, _ := run.Action("development")
	if !ok || run.ID() != "run-1" || !a.Ended() {
		t.Errorf("LastRun = %#v, %v, want run-1 rebuilt with development ended", run.Snapshot(), ok)
	}
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
}

func TestARetiredActionHasNoResumePointAndItsNextStartCarriesNoReason(t *testing.T) {
	h := folded(afterFailure()...)
	h.Retire("issue-9-development", testID, "fix", "development")
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{})

	again := seq(opening(hh("run-2", 3), "development"), one(ending("run-2", 4, "development", notFound, false)))
	for _, e := range again {
		h.Fold(e)
	}
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{
		"development": point("development", "start claude: not found"),
	})
}

func TestRetireKeepsAnActionWhoseLastRunMovedToAnotherWorkspace(t *testing.T) {
	moved := Workspace{Name: "issue-9-development-2", Branch: "crew/issue-9-development-2"}
	h := folded(
		opening(hh("run-1", 3), "development"), one(ending("run-1", 4, "development", failedEnd("tests fail"), true)),
		[]RunEvent{
			WorkspaceOpened{EventHead: hh("run-2", 5), Workspace: moved, Log: logOf("development")},
			ActionSessionAsked{EventHead: hh("run-2", 5), Action: "development"},
			ending("run-2", 6, "development", failedEnd("still broken"), true),
		},
	)
	h.Retire("issue-9-development", testID, "fix", "development")
	got := h.ResumePoints(testID, "implement")["development"]
	if got.Workspace.Name != "issue-9-development-2" || got.Reason.String() != "still broken" {
		t.Errorf("resume point = %#v, want the failed run in issue-9-development-2", got)
	}
}
