package crew

import (
	"reflect"
	"slices"
	"testing"
)

// The decision tables below follow a rule run through its sequence: each
// row names the fact it decides and the branch it follows.

type decision struct {
	name  string
	given []RunEvent
	finds bool
	def   func(RunDefinition) RunDefinition
	fact  Fact
	want  []RunEvent
}

// decide runs each decision on the sequence definition and checks the
// events, then applies them to the run, which must accept each.
func decide(t *testing.T, tests []decision) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := sequence()
			def.FindsPullRequests = tt.finds
			if tt.def != nil {
				def = tt.def(def)
			}
			run := given(t, tt.given)
			got, err := Decide(run, def, tt.fact)
			if err != nil {
				t.Fatalf("Decide = %v, want events", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decide =\n%#v\nwant\n%#v", got, tt.want)
			}
			given(t, append(slices.Clone(tt.given), got...))
		})
	}
}

func withoutActions(d RunDefinition) RunDefinition {
	d.Rule.Actions = nil
	d.Rule.Routes = d.Rule.Routes[:1]
	return d
}

// stoppedAt is the end of action, stopped before it started, at minute n,
// and the run through failed.
func stoppedAt(n int, action ActionName) []RunEvent {
	return []RunEvent{
		ActionEnded{EventHead: eh(n), Action: action, End: stopEnd, Verdict: Failed, Target: toFailed},
		RouteChosen{EventHead: eh(n), Route: FailedRoute, Action: action},
	}
}

// takeDecisions decide the take.
var takeDecisions = []decision{
	{
		name: "take: a rule without actions chooses passed at once", def: withoutActions,
		given: []RunEvent{takenWithoutActions()}, fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{takeMoved(), RouteChosen{EventHead: eh(1), Route: PassedRoute}},
	},
	{
		name: "take: a rule without actions chooses passed after a stop too", def: withoutActions, finds: true,
		given: seq([]RunEvent{takenWithoutActions()}, stopped(0)), fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{takeMoved(), RouteChosen{EventHead: eh(1), Route: PassedRoute}},
	},
	{
		name:  "take: a landed take asks for the run's one workspace",
		given: []RunEvent{taken()}, fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{takeMoved(), WorkspaceAsked{EventHead: eh(1)}},
	},
	{
		name:  "take: a run that resumes asks to reopen the workspace it resumes",
		given: []RunEvent{resumedAt("lfg")}, fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{takeMoved(), WorkspaceAsked{EventHead: eh(1), Reopen: Some(runWS())}},
	},
	{
		name:  "take: after a stop the first action ends without starting, and the run chooses failed",
		given: seq([]RunEvent{taken()}, stopped(0)), fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: seq([]RunEvent{takeMoved()}, stoppedAt(1, "install")),
	},
	{
		name:  "take: a take given up releases the run without a route",
		given: []RunEvent{taken()}, fact: TakeSettled{FactHead: fh(1)},
		want: []RunEvent{RunReleased{EventHead: eh(1)}},
	},
}

func ready(resumed bool) Fact {
	return WorkspaceReady{FactHead: fh(2), Workspace: runWS(), Log: runLog, Resumed: resumed}
}

// workspaceDecisions decide the run's workspace.
var workspaceDecisions = []decision{
	{
		name:  "ready: a new workspace is recorded and the first action, a shell, runs as the tracker's identity",
		given: asking(), fact: ready(false),
		want: []RunEvent{
			WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog},
			ActionShellAsked{EventHead: eh(2), Action: "install"},
		},
	},
	{
		name:  "ready: the reopened workspace resumes at the resume point's action",
		given: reopening(), fact: ready(true),
		want: []RunEvent{
			WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog, Resumed: true},
			ActionSessionAsked{EventHead: eh(2), Action: "lfg"},
		},
	},
	{
		name:  "ready: after a stop the workspace names no log, and the action at the cursor ends stopped",
		given: seq(reopening(), stopped(1)), fact: ready(true),
		want: seq([]RunEvent{WorkspaceOpened{EventHead: eh(2), Workspace: runWS()}}, stoppedAt(2, "lfg")),
	},
	{
		name:  "gone: a reopened workspace that is gone asks for a new one",
		given: reopening(), fact: WorkspaceGone{FactHead: fh(2)},
		want: []RunEvent{
			WorkspaceMissing{EventHead: eh(2), Workspace: runWS()},
			WorkspaceAsked{EventHead: eh(2)},
		},
	},
	{
		name: "ready: after a gone workspace the run starts again at its first action",
		given: seq(reopening(), []RunEvent{
			WorkspaceMissing{EventHead: eh(2), Workspace: runWS()}, WorkspaceAsked{EventHead: eh(2)},
		}),
		fact: ready(false),
		want: []RunEvent{
			WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog},
			ActionShellAsked{EventHead: eh(2), Action: "install"},
		},
	},
	{
		name:  "gone: after a stop it is still missing, and the first action ends stopped",
		given: seq(reopening(), stopped(1)), fact: WorkspaceGone{FactHead: fh(2)},
		want: seq([]RunEvent{WorkspaceMissing{EventHead: eh(2), Workspace: runWS()}}, stoppedAt(2, "install")),
	},
	{
		name:  "failed: a workspace that could not be made fails the action at the cursor",
		given: asking(), fact: WorkspaceFailed{FactHead: fh(2), Reason: NewSessionText("disk full")},
		want: []RunEvent{
			ActionEnded{
				EventHead: eh(2), Action: "install", Verdict: Failed, Target: toFailed,
				End: EndFailed{Reason: NewSessionText("disk full"), Cause: CauseWorkspace},
			},
			RouteChosen{EventHead: eh(2), Route: FailedRoute, Action: "install"},
		},
	},
	{
		name:  "failed: after a stop too, a reopened one",
		given: seq(reopening(), stopped(1)), fact: WorkspaceFailed{FactHead: fh(2), Reason: NewSessionText("not listed")},
		want: []RunEvent{
			ActionEnded{
				EventHead: eh(2), Action: "lfg", Verdict: Failed, Target: toFailed,
				End: EndFailed{Reason: NewSessionText("not listed"), Cause: CauseWorkspace},
			},
			RouteChosen{EventHead: eh(2), Route: FailedRoute, Action: "lfg"},
		},
	},
}

// stopDecisions decide a stop.
var stopDecisions = []decision{
	{
		name:  "stop: a run taking is marked stopping",
		given: []RunEvent{taken()}, fact: StopReached{FactHead: fh(1)},
		want: stopped(1),
	},
	{
		name:  "stop: the running session is asked to stop",
		given: inSession(), fact: StopReached{FactHead: fh(5)},
		want: []RunEvent{RunStopped{EventHead: eh(5)}, ActionSessionStopAsked{EventHead: eh(5), Action: "lfg"}},
	},
	{
		name:  "stop: the running script is asked to stop",
		given: installing(), fact: StopReached{FactHead: fh(3)},
		want: []RunEvent{RunStopped{EventHead: eh(3)}, ActionShellStopAsked{EventHead: eh(3), Action: "install"}},
	},
	{
		name:  "stop: a session that is starting waits for its next fact",
		given: starting(), fact: StopReached{FactHead: fh(4)},
		want: stopped(4),
	},
	{
		name:  "stop: a run whose sequence is over goes on",
		given: passedAll(), fact: StopReached{FactHead: fh(7)},
	},
	{
		name:  "stop: a stop reaches a run once",
		given: seq(asking(), stopped(1)), fact: StopReached{FactHead: fh(2)},
	},
}

func TestDecideTheTake(t *testing.T)      { decide(t, takeDecisions) }
func TestDecideTheWorkspace(t *testing.T) { decide(t, workspaceDecisions) }
func TestDecideTheStop(t *testing.T)      { decide(t, stopDecisions) }
