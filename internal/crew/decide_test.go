package crew

import (
	"reflect"
	"slices"
	"testing"
)

// The decision tables below mirror today's core handlers, branch by branch:
// each row names the handler in internal/core and the branch it follows.

type decision struct {
	name  string
	given []RunEvent
	finds bool
	def   func(RunDefinition) RunDefinition
	fact  Fact
	want  []RunEvent
}

// decide runs each decision and checks the events, then applies them to
// the run, which must accept each.
func decide(t *testing.T, tests []decision) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := definition()
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
	return d
}

// judgedAtOnce is the verdict of a rule without actions, whose take landed.
var judgedAtOnce = RunJudged{EventHead: eh(1), Verdict: Verdict{To: labelDone}}

// takeDecisions mirror taken, start and callResult.
var takeDecisions = []decision{
	{
		name: "taken: a rule without actions is judged at once", def: withoutActions,
		given: []RunEvent{taken()}, fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{takeMoved(), judgedAtOnce},
	},
	{
		name: "taken: a rule without actions is judged a success after a stop too", def: withoutActions,
		given: seq([]RunEvent{taken()}, stopped(0)), fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{takeMoved(), judgedAtOnce},
	},
	{
		name:  "taken: after a stop every action ends stopped without a workspace, and the run fails",
		given: seq([]RunEvent{taken(bothActions()...)}, stopped(0)), fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{
			takeMoved(),
			ActionEnded{EventHead: eh(1), Action: "development", End: stopEnd},
			ActionEnded{EventHead: eh(1), Action: "review", End: stopEnd},
			RunJudged{EventHead: eh(1), Verdict: Verdict{To: labelFailed, Failures: []ActionFailure{
				{Action: "development"}, {Action: "review"},
			}}},
		},
	},
	{
		name: "start: a new workspace without a resume point, the failed run's with one",
		given: []RunEvent{
			taken(ActionTaken{Name: "development", Resume: Some(resumePoint())}, ActionTaken{Name: "review"}),
		},
		fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{
			takeMoved(),
			ActionWorkspaceAsked{EventHead: eh(1), Action: "development", Reopen: Some(ws("development"))},
			ActionWorkspaceAsked{EventHead: eh(1), Action: "review"},
		},
	},
	{
		name: "start: a prompt that does not render ends its action, and its sibling starts",
		def: func(d RunDefinition) RunDefinition {
			d.Rule.Actions[1].Prompt = badPrompt("review")
			return d
		},
		given: []RunEvent{taken(bothActions()...)}, fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{
			takeMoved(),
			ActionWorkspaceAsked{EventHead: eh(1), Action: "development"},
			ActionEnded{EventHead: eh(1), Action: "review", End: EndFailed{
				Reason: renderError("review"), Cause: CausePrompt,
			}},
		},
	},
	{
		name: "start: when no prompt renders, the last end judges the run",
		def: func(d RunDefinition) RunDefinition {
			d.Rule.Actions[0].Prompt = badPrompt("development")
			d.Rule.Actions[1].Prompt = badPrompt("review")
			return d
		},
		given: []RunEvent{taken(bothActions()...)}, fact: TakeSettled{FactHead: fh(1), Landed: true},
		want: []RunEvent{
			takeMoved(),
			ActionEnded{EventHead: eh(1), Action: "development", End: EndFailed{
				Reason: renderError("development"), Cause: CausePrompt,
			}},
			ActionEnded{EventHead: eh(1), Action: "review", End: EndFailed{
				Reason: renderError("review"), Cause: CausePrompt,
			}},
			RunJudged{EventHead: eh(1), Verdict: Verdict{To: labelFailed, Failures: []ActionFailure{
				{Action: "development"}, {Action: "review"},
			}}},
		},
	},
	{
		name:  "callResult: a take given up releases the run without a verdict",
		given: []RunEvent{taken(bothActions()...)}, fact: TakeSettled{FactHead: fh(1)},
		want: []RunEvent{RunReleased{EventHead: eh(1)}},
	},
}

// stopDecisions mirror stop and stopActions.
var stopDecisions = []decision{
	{
		name:  "stop: a run taking is marked stopping",
		given: []RunEvent{taken(bothActions()...)}, fact: StopReached{FactHead: fh(1)},
		want: stopped(1),
	},
	{
		name:  "stopActions: a running run stops its running check and session, in action order",
		given: seq(preparing(), inChecks(), inSession("review")), fact: StopReached{FactHead: fh(6)},
		want: []RunEvent{
			RunStopped{EventHead: eh(6)},
			ActionCheckStopAsked{EventHead: eh(6), Action: "development"},
			ActionSessionStopAsked{EventHead: eh(6), Action: "review"},
		},
	},
	{
		name:  "stopActions: actions without a session or check to stop wait for their next fact",
		given: seq(preparing(), starting("review")), fact: StopReached{FactHead: fh(4)},
		want: stopped(4),
	},
	{
		name: "stop: a judging run's verdict goes on",
		given: seq(preparing(), reviewEnded(EndSucceeded{}), []RunEvent{
			ActionEnded{EventHead: eh(7), Action: "development", End: EndFailed{Cause: CauseWorkspace}},
			RunJudged{EventHead: eh(7), Verdict: Verdict{To: labelFailed}},
		}),
		fact: StopReached{FactHead: fh(8)},
	},
	{
		name:  "stop: a stop reaches a run once",
		given: seq(preparing(), stopped(3)), fact: StopReached{FactHead: fh(4)},
	},
}

func ready(action ActionName, resumed bool) Fact {
	return WorkspaceReady{FactHead: fh(3), Action: action, Workspace: ws(action), Log: logOf(action), Resumed: resumed}
}

// workspaceDecisions mirror workspaceReady, workspaceGone and actionInput's
// WorkspaceFailed.
var workspaceDecisions = []decision{
	{
		name:  "workspaceReady: a new workspace records the start and asks for the session",
		given: preparing(), fact: ready("development", false),
		want: []RunEvent{
			ActionOpened{EventHead: eh(3), Action: "development", Workspace: ws("development"), Log: logOf("development")},
			ActionSessionAsked{EventHead: eh(3), Action: "development"},
		},
	},
	{
		name:  "workspaceReady: the failed run's reopened workspace resumes",
		given: reopening(), fact: ready("development", true),
		want: []RunEvent{
			ActionOpened{
				EventHead: eh(3), Action: "development", Workspace: ws("development"), Log: logOf("development"),
				Resumed: true,
			},
			ActionSessionAsked{EventHead: eh(3), Action: "development"},
		},
	},
	{
		name:  "workspaceReady: after a stop the action starts without a log and ends stopped",
		given: seq(reopening(), stopped(2)), fact: ready("development", true),
		want: []RunEvent{
			ActionOpened{EventHead: eh(3), Action: "development", Workspace: ws("development")},
			ActionEnded{
				EventHead: eh(3), Action: "development", End: stopEnd,
				Workspace: Some(OpenedWorkspace{Workspace: ws("development"), Opened: at(3)}),
			},
		},
	},
	{
		name:  "workspaceGone: a reopened workspace that is gone asks for a new one",
		given: reopening(), fact: WorkspaceGone{FactHead: fh(3), Action: "development"},
		want: []RunEvent{
			WorkspaceMissing{EventHead: eh(3), Action: "development", Workspace: ws("development")},
			ActionWorkspaceAsked{EventHead: eh(3), Action: "development"},
		},
	},
	{
		name:  "workspaceGone: after a stop it is still missing, and the action ends stopped",
		given: seq(reopening(), stopped(2)), fact: WorkspaceGone{FactHead: fh(3), Action: "development"},
		want: []RunEvent{
			WorkspaceMissing{EventHead: eh(3), Action: "development", Workspace: ws("development")},
			ActionEnded{EventHead: eh(3), Action: "development", End: stopEnd},
		},
	},
	{
		name:  "actionInput WorkspaceFailed: a workspace that could not be made fails its action",
		given: preparing(), fact: WorkspaceFailed{FactHead: fh(3), Action: "review", Reason: NewSessionText("disk full")},
		want: []RunEvent{ActionEnded{
			EventHead: eh(3), Action: "review", End: EndFailed{Reason: NewSessionText("disk full"), Cause: CauseWorkspace},
		}},
	},
	{
		name:  "actionInput WorkspaceFailed: after a stop too, a reopened one",
		given: seq(reopening(), stopped(2)),
		fact:  WorkspaceFailed{FactHead: fh(3), Action: "development", Reason: NewSessionText("not listed")},
		want: []RunEvent{ActionEnded{
			EventHead: eh(3), Action: "development",
			End: EndFailed{Reason: NewSessionText("not listed"), Cause: CauseWorkspace},
		}},
	},
}

func ended(outcome Outcome) Fact {
	return SessionEnded{FactHead: fh(5), Action: "review", Outcome: outcome, Usage: usage}
}

func reviewSession(outcome Outcome) RunEvent {
	return ActionSessionEnded{EventHead: eh(5), Action: "review", Outcome: outcome, Usage: usage}
}

func reviewEnd(end ActionEnd) RunEvent {
	return ActionEnded{
		EventHead: eh(5), Action: "review", End: end, Workspace: opened("review"),
		SessionStarted: Some(at(4)), Usage: usage,
	}
}

// sessionDecisions mirror sessionStarted, actionInput's SessionFailedToStart
// and sessionEnded.
var sessionDecisions = []decision{
	{
		name:  "sessionStarted: the session runs",
		given: seq(preparing(), starting("review")), fact: SessionStarted{FactHead: fh(4), Action: "review"},
		want: []RunEvent{ActionSessionStarted{
			EventHead: eh(4), Action: "review", Workspace: ws("review"), Log: logOf("review"),
		}},
	},
	{
		name:  "sessionStarted: a session that starts after a stop is asked to stop",
		given: seq(preparing(), starting("review"), stopped(3)), fact: SessionStarted{FactHead: fh(4), Action: "review"},
		want: []RunEvent{
			ActionSessionStarted{EventHead: eh(4), Action: "review", Workspace: ws("review"), Log: logOf("review")},
			ActionSessionStopAsked{EventHead: eh(4), Action: "review"},
		},
	},
	{
		name:  "actionInput SessionFailedToStart: the action fails to start",
		given: seq(preparing(), starting("review")),
		fact:  SessionFailedToStart{FactHead: fh(4), Action: "review", Reason: NewSessionText("claude: not found")},
		want: []RunEvent{ActionEnded{
			EventHead: eh(4), Action: "review", Workspace: opened("review"),
			End: EndFailed{Reason: NewSessionText("claude: not found"), Cause: CauseStart},
		}},
	},
	{
		name:  "sessionEnded: a failed session fails its action",
		given: seq(preparing(), inSession("review")), fact: ended(failedOutcome("gave up")),
		want: []RunEvent{
			reviewSession(failedOutcome("gave up")),
			reviewEnd(EndFailed{Reason: NewSessionText("gave up"), Cause: CauseSession}),
		},
	},
	{
		name:  "sessionEnded: a failed session after a stop counts as stopped",
		given: seq(preparing(), inSession("review"), stopped(4)), fact: ended(failedOutcome("killed")),
		want: []RunEvent{
			reviewSession(failedOutcome("killed")),
			reviewEnd(EndFailed{Reason: NewSessionText("killed"), Cause: CauseStopped}),
		},
	},
	{
		name:  "sessionEnded: a successful session without checks succeeds",
		given: seq(preparing(), inSession("review")), fact: ended(succeeded("done")),
		want: []RunEvent{reviewSession(succeeded("done")), reviewEnd(EndSucceeded{Reason: NewSessionText("done")})},
	},
	{
		name:  "sessionEnded: a successful session without checks succeeds after a stop too",
		given: seq(preparing(), inSession("review"), stopped(4)), fact: ended(succeeded("done")),
		want: []RunEvent{reviewSession(succeeded("done")), reviewEnd(EndSucceeded{Reason: NewSessionText("done")})},
	},
	{
		name:  "sessionEnded: a session that ends before its start was seen keeps no start",
		given: seq(preparing(), starting("review")), fact: ended(succeeded("done")),
		want: []RunEvent{reviewSession(succeeded("done")), ActionEnded{
			EventHead: eh(5), Action: "review", End: EndSucceeded{Reason: NewSessionText("done")},
			Workspace: opened("review"), Usage: usage,
		}},
	},
	{
		name:  "sessionEnded: a successful session with checks runs the first",
		given: seq(preparing(), inSession("development")),
		fact:  SessionEnded{FactHead: fh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
		want: []RunEvent{
			ActionSessionEnded{EventHead: eh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
			ActionCheckAsked{EventHead: eh(5), Action: "development", Check: "build"},
		},
	},
	{
		name:  "sessionEnded: after a stop no check runs, and the action ends stopped",
		given: seq(preparing(), inSession("development"), stopped(4)),
		fact:  SessionEnded{FactHead: fh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
		want: []RunEvent{
			ActionSessionEnded{EventHead: eh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
			developmentEnded(5, stopEnd),
		},
	},
	{
		name:  "sessionEnded: with lookups, the lookup is asked and an action that ended waits for it",
		given: seq(preparing(), inSession("review")), finds: true, fact: ended(failedOutcome("gave up")),
		want: []RunEvent{
			reviewSession(failedOutcome("gave up")),
			ActionLookupAsked{EventHead: eh(5), Action: "review"},
			ActionFinishing{EventHead: eh(5), Action: "review", End: EndFailed{
				Reason: NewSessionText("gave up"), Cause: CauseSession,
			}},
		},
	},
	{
		name:  "sessionEnded: with lookups, the lookup runs alongside the checks",
		given: seq(preparing(), inSession("development")), finds: true,
		fact: SessionEnded{FactHead: fh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
		want: []RunEvent{
			ActionSessionEnded{EventHead: eh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
			ActionLookupAsked{EventHead: eh(5), Action: "development"},
			ActionCheckAsked{EventHead: eh(5), Action: "development", Check: "build"},
		},
	},
}

func TestDecideTheTake(t *testing.T)      { decide(t, takeDecisions) }
func TestDecideTheStop(t *testing.T)      { decide(t, stopDecisions) }
func TestDecideTheWorkspace(t *testing.T) { decide(t, workspaceDecisions) }
func TestDecideTheSession(t *testing.T)   { decide(t, sessionDecisions) }
