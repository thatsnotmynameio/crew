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

// opening is action's workspace ws(action) ready at minute n, in run.
func opening(run RuleRunID, n int, action ActionName) ActionOpened {
	return ActionOpened{EventHead: hh(run, n), Action: action, Workspace: ws(action), Log: logOf(action)}
}

// ending is action's end at minute n, in run, in ws(action), after a
// session that started when session says so.
func ending(run RuleRunID, n int, action ActionName, end ActionEnd, session bool) ActionEnded {
	e := ActionEnded{
		EventHead: hh(run, n), Action: action, End: end,
		Workspace: Some(OpenedWorkspace{Workspace: ws(action), Log: logOf(action), Opened: at(n - 1)}),
	}
	if session {
		e.SessionStarted = Some(at(n - 1))
	}
	return e
}

func failedEnd(reason string) ActionEnd {
	return EndFailed{Reason: NewSessionText(reason), Cause: CauseSession}
}

func folded(events ...RunEvent) *History {
	var h History
	for _, e := range events {
		h.Fold(e)
	}
	return &h
}

// point returns the resume point of action in ws(action) with reason.
func point(action ActionName, reason string) ResumePoint {
	return ResumePoint{Workspace: ws(action), Log: logOf(action), Reason: NewSessionText(reason)}
}

func wantPoints(t *testing.T, h *History, issue IssueID, rule RuleName, want map[ActionName]ResumePoint) {
	t.Helper()
	if got := h.ResumePoints(issue, rule); !maps.Equal(got, want) {
		t.Errorf("ResumePoints(%v, %s) = %+v, want %+v", issue, rule, got, want)
	}
}

func TestAE3AnActionThatStartedAndNeverEndedResumesAsCrashed(t *testing.T) {
	h := folded(opening("run-1", 3, "implement"))
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{
		"implement": point("implement", "crew stopped before the run ended: it crashed or was killed"),
	})
}

func TestAFailedEndResumesAndASucceededOneDoesNot(t *testing.T) {
	h := folded(
		taken(bothActions()...),
		opening(testRun, 3, "development"), ending(testRun, 6, "development", failedEnd("tests fail"), true),
		opening(testRun, 3, "review"), ending(testRun, 6, "review", EndSucceeded{Reason: NewSessionText("done")}, true),
	)
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
}

func TestAnEndWithoutAWorkspaceKeepsTheFailedRunsResumePoint(t *testing.T) {
	gone := ActionEnded{EventHead: hh("run-2", 4), Action: "development", End: stopEnd}
	h := folded(
		opening("run-1", 3, "development"), ending("run-1", 6, "development", failedEnd("tests fail"), true),
		gone,
	)
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
}

// reasonCase is a past that ends in development's last action run, and the
// resume point it leaves.
type reasonCase struct {
	name   string
	events []RunEvent
	want   ResumePoint
}

// notFound is the end of a session that failed to start.
var notFound = EndFailed{Reason: NewSessionText("start claude: not found"), Cause: CauseStart}

// afterFailure is development's failed run in ws("development") at
// minutes 1 and 2, then events.
func afterFailure(events ...RunEvent) []RunEvent {
	return append([]RunEvent{
		opening("run-1", 1, "development"), ending("run-1", 2, "development", failedEnd("the session's reason"), true),
	}, events...)
}

// inheritedReasons are the ends without a session that keep an earlier
// failure's reason.
func inheritedReasons() []reasonCase {
	stoppedOpening := opening("run-2", 3, "development")
	stoppedOpening.Log = ""
	stoppedEnd := ending("run-2", 3, "development", stopEnd, false)
	stoppedEnd.Workspace = Some(OpenedWorkspace{Workspace: ws("development"), Opened: at(3)})
	return []reasonCase{
		{
			name:   "failed to start",
			events: afterFailure(opening("run-2", 3, "development"), ending("run-2", 4, "development", notFound, false)),
			want:   point("development", "the session's reason"),
		},
		{
			name:   "stopped while reopening",
			events: afterFailure(stoppedOpening, stoppedEnd),
			want:   ResumePoint{Workspace: ws("development"), Reason: NewSessionText("the session's reason")},
		},
		{
			name: "after a crash",
			events: []RunEvent{
				opening("run-1", 1, "development"),
				opening("run-2", 3, "development"), ending("run-2", 4, "development", notFound, false),
			},
			want: point("development", "crew stopped before the run ended: it crashed or was killed"),
		},
		{
			name:   "with no start since",
			events: afterFailure(ending("run-2", 4, "development", notFound, false)),
			want:   point("development", "the session's reason"),
		},
	}
}

// ownReasons are the ends that keep their own reason.
func ownReasons() []reasonCase {
	elsewhere := opening("run-2", 3, "development")
	elsewhere.Workspace = Workspace{Name: "issue-9-development-2", Branch: "crew/issue-9-development-2"}
	return []reasonCase{
		{
			name: "a session that started",
			events: afterFailure(
				opening("run-2", 3, "development"), ending("run-2", 6, "development", failedEnd("new"), true),
			),
			want: point("development", "new"),
		},
		{
			name: "after a success",
			events: []RunEvent{
				opening("run-1", 1, "development"),
				ending("run-1", 2, "development", EndSucceeded{Reason: NewSessionText("done")}, true),
				opening("run-2", 3, "development"), ending("run-2", 4, "development", notFound, false),
			},
			want: point("development", "start claude: not found"),
		},
		{
			name: "in another workspace",
			events: afterFailure(elsewhere, ActionEnded{
				EventHead: hh("run-2", 4), Action: "development", End: notFound,
				Workspace: Some(OpenedWorkspace{Workspace: elsewhere.Workspace, Log: logOf("development"), Opened: at(3)}),
			}),
			want: ResumePoint{
				Workspace: elsewhere.Workspace, Log: logOf("development"), Reason: NewSessionText("start claude: not found"),
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
	inOtherIssue := opening("run-3", 3, "development")
	inOtherIssue.IssueID, inOtherIssue.IssueRef = other, "#10"
	inFix := opening("run-4", 3, "development")
	inFix.Rule = "fix"
	// fix's action starts in implement's workspace: History retires
	// nothing, which the core's claims do.
	h := folded(
		opening("run-1", 3, "development"), ending("run-1", 6, "development", failedEnd("tests fail"), true),
		inOtherIssue, inFix,
	)
	crashed := point("development", "crew stopped before the run ended: it crashed or was killed")
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
	wantPoints(t, h, testID, "fix", map[ActionName]ResumePoint{"development": crashed})
	wantPoints(t, h, other, "implement", map[ActionName]ResumePoint{"development": crashed})
	wantPoints(t, h, other, "fix", map[ActionName]ResumePoint{})
}

func TestTheLastRunIsTheOneWhoseEventsCameLast(t *testing.T) {
	second := taken(bothActions()...)
	h := folded(
		RunTaken{EventHead: hh("run-1", 0), Issue: testIssue(), From: labelReady, To: labelRunning},
		RunReleased{EventHead: hh("run-1", 1)},
		second, takeMoved(),
	)
	run, ok := h.LastRun(testID, "implement")
	if !ok || run.ID() != testRun || run.Phase() != (RunningPhase{}) || len(run.Actions()) != 2 {
		t.Errorf("LastRun = %#v, %v, want run-2 running both actions", run.Snapshot(), ok)
	}
	if _, ok := h.LastRun(testID, "fix"); ok {
		t.Errorf("fix has a last run, want none")
	}
}

func TestARunWithGapsFolds(t *testing.T) {
	h := folded(ending("run-1", 6, "development", failedEnd("tests fail"), true))
	run, ok := h.LastRun(testID, "implement")
	a, _ := run.Action("development")
	if !ok || run.ID() != "run-1" || !a.Ended() {
		t.Errorf("LastRun = %#v, %v, want run-1 rebuilt with development ended", run.Snapshot(), ok)
	}
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{"development": point("development", "tests fail")})
}

func TestAForgottenActionHasNoResumePointAndItsNextStartCarriesNoReason(t *testing.T) {
	h := folded(afterFailure()...)
	h.Forget(testID, "implement", "development")
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{})

	h.Fold(opening("run-2", 3, "development"))
	h.Fold(ending("run-2", 4, "development", notFound, false))
	wantPoints(t, h, testID, "implement", map[ActionName]ResumePoint{
		"development": point("development", "start claude: not found"),
	})
}
