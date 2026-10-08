package crew

import (
	"reflect"
	"slices"
	"testing"
)

// The rule question tests follow the question blocks the test rule asks
// on issue #9, through runs that continue one another: it stays open once
// it landed, the first session of a later run gets it, and a resume
// neither skips an unasked question nor asks an answered one again (KTD8,
// KTD9).

// blocksAsked is the rule's question blocks, as the run that asked it
// holds it.
var blocksAsked = Question{ID: blocksID, Rule: "implement"}

// asksWhenBlocked makes lfg's verdict blocked lead to a route that asks
// blocks, then moves the item to crew:question.
func asksWhenBlocked(d RunDefinition) RunDefinition {
	d.Rule.Routes = slices.Clone(d.Rule.Routes)
	d.Rule.Routes[2] = Route{Name: "blocked", Steps: []Step{QuestionStep{Question: blocks()}, toQuestion}}
	return d
}

// asksWhenPassed makes the passed route ask blocks before its move.
func asksWhenPassed(d RunDefinition) RunDefinition {
	d.Rule.Routes = slices.Clone(d.Rule.Routes)
	d.Rule.Routes[0] = Route{Name: PassedRoute, Steps: []Step{QuestionStep{Question: blocks()}, MoveStep{To: labelDone}}}
	return d
}

// reviewsAfterWaiting replaces judge with the session review, and makes
// lfg's waiting go on to it.
func reviewsAfterWaiting(d RunDefinition) RunDefinition {
	d = waitingLeadsTo(Next{})(asksWhenBlocked(d))
	d.Rule.Actions[2] = Action{
		Name: "review",
		Kind: SessionSpec{
			Agent:  Agent{Name: "claude", Harness: "claude", Bot: developer.Name},
			Prompt: mustPrompt("review", "Review {{.Issue.Ref}}"), Bot: developer,
		},
	}
	return d
}

// blockedWith is a run whose lfg ended with blocked, its route's question
// and move settled as question and move say.
func blockedWith(question, move StepOutcome) life {
	return then(asks, endsWith("blocked"), settles(question, move))
}

// blocked is a run whose lfg ended with blocked, and whose question and
// move landed.
var blocked = blockedWith(StepLanded{}, StepLanded{})

// freshUntilLfg is a fresh run's take landed, its new worktree ready and
// install passed, so lfg's session is asked.
func freshUntilLfg(at func(int) FactHead) []Fact {
	return []Fact{
		TakeSettled{FactHead: at(1), Landed: true},
		WorkspaceReady{FactHead: at(2), Workspace: runWS(), Log: runLog},
		ShellEnded{FactHead: at(3), Action: "install", Outcome: exited(0, "install passed")},
	}
}

// heldBy returns the open questions of the last run of the test rule in
// h, whatever action they are at.
func heldBy(h *History) []Question {
	last, _ := h.LastRun(testID, "implement")
	return last.Snapshot().Questions
}

func TestARouteWhoseQuestionLandedKeepsItOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		life life
		want []Question
	}{
		{name: "the question landed: one rule question, by its id", life: blocked, want: []Question{blocksAsked}},
		{name: "the question was given up: none", life: blockedWith(StepGivenUp{Reason: "refused"}, StepLanded{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := lived(t, asksWhenBlocked(sequence()), tc.life)
			if got := heldBy(h); !slices.Equal(got, tc.want) {
				t.Errorf("open questions = %#v, want %#v", got, tc.want)
			}
			if got := h.Questions(testID, "implement"); !slices.Equal(got, tc.want) {
				t.Errorf("questions passed on = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// firstSessionCase is the runs of the test rule as def changes it, the
// start of the last one, and the action whose session it asked for.
type firstSessionCase struct {
	name   string
	def    func(RunDefinition) RunDefinition
	lives  []life
	start  func(Start) bool
	action ActionName
}

// The first session a later run starts gets the rule's question, whatever
// action the run starts at (KTD8).
func TestTheFirstSessionOfTheNextRunGetsTheRulesQuestion(t *testing.T) {
	startsAt := func(action ActionName) func(Start) bool {
		return func(s Start) bool {
			at, ok := s.(StartAt)
			return ok && at.Action == action
		}
	}
	askedThenInstalled := then(freshUntilLfg, settles(StepLanded{}, StepLanded{}))
	for _, tc := range []firstSessionCase{
		{
			name: "resumed at the session whose verdict chose the question's route", def: asksWhenBlocked,
			lives: []life{blocked, reopensWorktree}, start: startsAt("lfg"), action: "lfg",
		},
		{
			name: "resumed at the session after a question action", def: withActions(askAfterInstall...),
			lives: []life{askedThenInstalled, reopensWorktree}, start: startsAt("lfg"), action: "lfg",
		},
		{
			name: "started fresh after the passed route asked, a shell action first", def: asksWhenPassed,
			lives: []life{then(asks, endsWith(Passed), judgeExits(0), settles(StepLanded{}, StepLanded{})), freshUntilLfg},
			start: func(s Start) bool { return s == StartFresh{} }, action: "lfg",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := lived(t, tc.def(sequence()), tc.lives...)
			last, _ := h.LastRun(testID, "implement")
			a, _ := last.Cursor()
			if !tc.start(last.Start()) || a.Name() != tc.action || a.State() != (StartingSession{}) {
				t.Fatalf("the last run started %#v, its session at %s %#v, want %s's asked", last.Start(), a.Name(),
					a.State(), tc.action)
			}
			if got, want := last.Questions(tc.action), []Question{blocksAsked}; !slices.Equal(got, want) {
				t.Errorf("questions at %s = %#v, want %#v", tc.action, got, want)
			}
		})
	}
}

// A second session of the run, once the first started, gets no rule
// question, and its end closes none: only the first session's does.
func TestOnlyTheRunsFirstSessionGetsTheRulesQuestion(t *testing.T) {
	waitedAgain := then(asksAgain, endsWith(Waiting))
	h := lived(t, reviewsAfterWaiting(sequence()), blocked, waitedAgain)
	last, _ := h.LastRun(testID, "implement")
	if a, _ := last.Cursor(); a.Name() != "review" || a.State() != (StartingSession{}) {
		t.Fatalf("cursor at %s %#v, want review's session asked", a.Name(), a.State())
	}
	if got := last.Questions("review"); got != nil {
		t.Errorf("questions at review = %#v, want none", got)
	}
	reviewed := then(waitedAgain, func(at func(int) FactHead) []Fact {
		return []Fact{
			SessionStarted{FactHead: at(6), Action: "review", Login: asker},
			SessionEnded{
				FactHead: at(7), Action: "review", Outcome: succeeded("done"), Report: VerdictReported{Verdict: Passed},
			},
			StepSettled{FactHead: at(8), Step: 0, Outcome: StepLanded{}},
		}
	})
	h = lived(t, reviewsAfterWaiting(sequence()), blocked, reviewed)
	if got := heldBy(h); !slices.Contains(got, blocksAsked) {
		t.Errorf("after review passed the open questions are %#v, want blocks still open", got)
	}
}

// The session that got the rule's question closes it when it ends well
// with a verdict other than waiting, and keeps it when it waits or fails
// (KTD8).
func TestTheSessionThatGotTheRulesQuestionClosesIt(t *testing.T) {
	both := []Question{blocksAsked, question("run-2")}
	for _, tc := range []questionCase{
		{name: "it ends with passed: closed", lives: []life{blocked, then(asksAgain, endsWith(Passed), judgeExits(1))}},
		{
			name: "it ends with waiting: still open", lives: []life{blocked, then(asksAgain, endsWith(Waiting))},
			held: both, passed: both,
		},
		{
			name:  "it fails: still open, and the next run inherits it",
			lives: []life{blocked, then(asksAgain, endsFailed("harness crashed"))}, held: both, passed: both,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := lived(t, waits(asksWhenBlocked(sequence())), tc.lives...)
			if got := heldBy(h); !slices.Equal(got, tc.held) {
				t.Errorf("open questions = %#v, want %#v", got, tc.held)
			}
			if got := h.Questions(testID, "implement"); !slices.Equal(got, tc.passed) {
				t.Errorf("questions passed on = %#v, want %#v", got, tc.passed)
			}
		})
	}
}

// A run whose passed route asked and finished still passes the rule's
// question on, though no session question survives it.
func TestARulesQuestionSurvivesAFinishedPassedRoute(t *testing.T) {
	h := lived(t, asksWhenPassed(sequence()),
		then(asks, endsWith(Passed), judgeExits(0), settles(StepLanded{}, StepLanded{})))
	want := []Question{blocksAsked}
	if got := h.Questions(testID, "implement"); h.Start(testID, sequence().Rule) != (StartFresh{}) ||
		!slices.Equal(got, want) {
		t.Errorf("after the passed route the next run starts %#v inheriting %#v, want fresh inheriting %#v",
			h.Start(testID, sequence().Rule), got, want)
	}
}

// askedAtAsk is the start at the question action ask, in the run's
// worktree, after a run whose question did not land.
var askedAtAsk = StartAt{Workspace: runWS(), Log: runLog, Action: "ask", Route: "ask", Reason: askedEnd.Reason}

// unaskedStarts are the starts after a run whose question action ended with
// asked, but whose question did not land (KTD9).
func unaskedStarts() []startCase {
	installed := []Fact{landed(), ready(false), shellEnded(3, "install", exited(0, "install passed"))}
	givenUp := append(slices.Clone(installed), settled(7, 0, StepGivenUp{Reason: "refused"}), settled(8, 1, StepLanded{}))
	pastOf := func(facts []Fact, names ...ActionName) past {
		return past{take: takeOf(names...), change: withActions(names...), facts: facts}
	}
	return []startCase{
		{
			name: "its question given up, followed by a session: at the question action",
			past: pastOf(givenUp, askAfterInstall...), rule: withActions(askAfterInstall...), want: askedAtAsk,
		},
		{
			name: "crashed before its question settled: at the question action",
			past: pastOf(installed, askAfterInstall...), rule: withActions(askAfterInstall...), want: askedAtAsk,
		},
		{
			name: "its question given up, the rule's last action: at the question action, not the passed route",
			past: pastOf(givenUp, "install", "ask"), rule: withActions("install", "ask"), want: askedAtAsk,
		},
		{
			name: "its question landed and its move was given up: after it, as the question is open",
			past: pastOf(append(slices.Clone(installed), settled(7, 0, StepLanded{}), settled(8, 1, StepGivenUp{})),
				askAfterInstall...),
			rule: withActions(askAfterInstall...), want: askedAtLfg,
		},
	}
}

func TestARunWhoseQuestionDidNotLandAsksItAgain(t *testing.T) {
	for _, tc := range unaskedStarts() {
		t.Run(tc.name, func(t *testing.T) {
			h := folded(tc.past.events(t))
			if got := h.Start(testID, tc.rule(sequence()).Rule); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Start =\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}

// A judge after an answered question that fails restarts at itself, not at
// the session before the question, which would ask it again (KTD9).
func TestAJudgeDoesNotStepBackAcrossAQuestion(t *testing.T) {
	def := withActions("lfg", "ask", "judge")(sequence())
	sessionAsks := then(func(at func(int) FactHead) []Fact {
		return []Fact{
			TakeSettled{FactHead: at(1), Landed: true},
			WorkspaceReady{FactHead: at(2), Workspace: runWS(), Log: runLog},
			SessionStarted{FactHead: at(4), Action: "lfg", Login: asker},
		}
	}, endsWith(Passed), settles(StepLanded{}, StepLanded{}))
	h := lived(t, def, sessionAsks, then(reopensWorktree, judgeExits(1)))
	start, _ := h.Start(testID, def.Rule).(StartAt)
	if start.Action != "judge" {
		t.Errorf("Start = %#v, want at judge", h.Start(testID, def.Rule))
	}
	if got := def.Rule.sessionBefore("ask"); got != "lfg" {
		t.Errorf("the session before ask = %s, want lfg", got)
	}
}
