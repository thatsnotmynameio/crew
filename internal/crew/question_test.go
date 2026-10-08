package crew

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// The question tests follow the open questions of the test rule on issue
// #9, whose session lfg may end with waiting, through runs that continue
// one another (KTD-W7).

// asker is the login lfg's session acts as.
const asker = "crew-developer[bot]"

// waits maps lfg's waiting verdict to the route waiting, which moves the
// issue to crew:waiting.
func waits(d RunDefinition) RunDefinition {
	return waitingLeadsTo(ToRoute{Route: "waiting"})(d)
}

// waitingLeadsTo maps lfg's waiting verdict to target, and adds the route
// waiting, which moves the issue to crew:waiting.
func waitingLeadsTo(target Target) func(RunDefinition) RunDefinition {
	return func(d RunDefinition) RunDefinition {
		d.Rule.Actions = slices.Clone(d.Rule.Actions)
		d.Rule.Actions[1].On = On{"blocked": ToRoute{Route: "blocked"}, Waiting: target}
		d.Rule.Routes = append(slices.Clone(d.Rule.Routes),
			Route{Name: "waiting", Steps: []Step{MoveStep{To: "crew:waiting"}}})
		return d
	}
}

// question is the question lfg's session of run may have asked, acting as
// asker.
func question(run RuleRunID) Question { return Question{Run: run, Action: "lfg", Login: asker} }

// life is what happens to one run: the facts it decides, each at a minute
// of its own heads.
type life func(at func(int) FactHead) []Fact

// lived returns the history of the runs run-1, run-2 and on of the test
// rule as def says, one per life, each taken as the history before it
// says: its start, the run it continues and the questions it inherits, with
// def's actions.
func lived(t *testing.T, def RunDefinition, lives ...life) *History {
	t.Helper()
	var h History
	for i, l := range lives {
		id := RuleRunID(fmt.Sprintf("run-%d", i+1))
		take, _ := taken().(RunTaken)
		take.EventHead, take.Actions = hh(id, 0), nil
		for _, a := range def.Rule.Actions {
			take.Actions = append(take.Actions, a.Name)
		}
		take.Start, take.Questions = h.Start(testID, def.Rule), h.Questions(testID, def.Rule.Name)
		if last, ok := h.LastRun(testID, def.Rule.Name); ok {
			take.Continues = Some(last.ID())
		}
		h.Fold(take)
		run := given(t, []RunEvent{take})
		for _, f := range l(func(n int) FactHead { return FactHead{Run: id, At: at(n)} }) {
			events, err := Decide(run, def, f)
			if err != nil {
				t.Fatalf("run %s: Decide(%#v) = %v", id, f, err)
			}
			for _, e := range events {
				run, _ = Apply(run, e)
				h.Fold(e)
			}
		}
	}
	return &h
}

// then returns the life that lives parts one after another.
func then(parts ...life) life {
	return func(at func(int) FactHead) []Fact {
		out := make([]Fact, 0, len(parts))
		for _, p := range parts {
			out = append(out, p(at)...)
		}
		return out
	}
}

// asks is a new run's take landed, its new worktree ready, install passed
// and lfg's session started, acting as asker.
func asks(at func(int) FactHead) []Fact {
	return []Fact{
		TakeSettled{FactHead: at(1), Landed: true},
		WorkspaceReady{FactHead: at(2), Workspace: runWS(), Log: runLog},
		ShellEnded{FactHead: at(3), Action: "install", Outcome: exited(0, "install passed")},
		SessionStarted{FactHead: at(4), Action: "lfg", Login: asker},
	}
}

// reopensWorktree is a resume's take landed and its reopened worktree ready.
func reopensWorktree(at func(int) FactHead) []Fact {
	return []Fact{
		TakeSettled{FactHead: at(1), Landed: true},
		WorkspaceReady{FactHead: at(2), Workspace: runWS(), Log: runLog, Resumed: true},
	}
}

// asksAgain is a resume at lfg, whose session started again, acting as
// asker.
func asksAgain(at func(int) FactHead) []Fact {
	return append(reopensWorktree(at), SessionStarted{FactHead: at(4), Action: "lfg", Login: asker})
}

// endsWith is lfg's session that ended well at minute 5, reporting v; a
// run that passes goes on to judge.
func endsWith(v Verdict) life {
	return func(at func(int) FactHead) []Fact {
		return []Fact{SessionEnded{
			FactHead: at(5), Action: "lfg", Outcome: succeeded("done"), Report: VerdictReported{Verdict: v},
		}}
	}
}

// endsFailed is lfg's session that failed at minute 5, saying reason.
func endsFailed(reason string) life {
	return func(at func(int) FactHead) []Fact {
		return []Fact{SessionEnded{FactHead: at(5), Action: "lfg", Outcome: failedOutcome(reason)}}
	}
}

// stops is a stop that reached the run at minute n.
func stops(n int) life {
	return func(at func(int) FactHead) []Fact { return []Fact{StopReached{FactHead: at(n)}} }
}

// judgeExits is judge's script that exited with status at minute 6.
func judgeExits(status int) life {
	return func(at func(int) FactHead) []Fact {
		return []Fact{ShellEnded{FactHead: at(6), Action: "judge", Outcome: exited(status, "judged")}}
	}
}

// settles is the run's route's steps that settled as outcomes, in order.
func settles(outcomes ...StepOutcome) life {
	return func(at func(int) FactHead) []Fact {
		out := make([]Fact, 0, len(outcomes))
		for i, o := range outcomes {
			out = append(out, StepSettled{FactHead: at(7 + i), Step: i, Outcome: o})
		}
		return out
	}
}

// stopsWhileReopening is a resume's take landed, and a stop that reached
// it while it reopened its worktree.
func stopsWhileReopening(at func(int) FactHead) []Fact {
	return []Fact{
		TakeSettled{FactHead: at(1), Landed: true}, StopReached{FactHead: at(1)},
		WorkspaceReady{FactHead: at(2), Workspace: runWS(), Log: runLog, Resumed: true},
	}
}

// stopsAtTake is a stop that reached the run before its take landed.
func stopsAtTake(at func(int) FactHead) []Fact {
	return []Fact{StopReached{FactHead: at(0)}, TakeSettled{FactHead: at(1), Landed: true}}
}

// waited is a run whose lfg asked and ended with waiting, its move to
// crew:waiting landed.
var waited = then(asks, endsWith(Waiting), settles(StepLanded{}))

// questionCase is the runs of the test rule as def changes it, waits when
// def is nil, the questions their last run held at lfg, and the questions
// a run after it inherits.
type questionCase struct {
	name   string
	def    func(RunDefinition) RunDefinition
	lives  []life
	held   []Question
	passed []Question
}

// check fails the test unless the runs of tc leave their last run holding
// tc.held at lfg, and pass tc.passed on.
func (tc questionCase) check(t *testing.T) {
	t.Helper()
	def := waits(sequence())
	if tc.def != nil {
		def = tc.def(sequence())
	}
	h := lived(t, def, tc.lives...)
	last, _ := h.LastRun(testID, "implement")
	if got := last.Questions("lfg"); !slices.Equal(got, tc.held) {
		t.Errorf("questions at lfg = %#v, want %#v", got, tc.held)
	}
	if got := h.Questions(testID, "implement"); !slices.Equal(got, tc.passed) {
		t.Errorf("questions passed on = %#v, want %#v", got, tc.passed)
	}
}

// askingQuestions are the runs whose one session at lfg asks, or may.
func askingQuestions() []questionCase {
	q1 := []Question{question("run-1")}
	return []questionCase{
		{
			name: "a session whose on: maps waiting starts: one question, its run and login", lives: []life{asks},
			held: q1, passed: q1,
		},
		{
			name: "a session without a waiting entry starts: no question",
			def:  func(d RunDefinition) RunDefinition { return d }, lives: []life{asks},
		},
		{name: "a session ends with waiting: its own question stays", lives: []life{waited}, held: q1, passed: q1},
		{
			name:  "AE4: answered within its wait and ended passed, then judge failed: no question left",
			lives: []life{then(asks, endsWith(Passed), judgeExits(1))},
		},
		{
			name:  "AE20: stopped during its wait: the question stays",
			lives: []life{then(asks, stops(5), endsFailed("stopped"), settles(StepLanded{}, StepLanded{}))},
			held:  q1, passed: q1,
		},
		{
			name:  "its harness failed: the question stays",
			lives: []life{then(asks, endsFailed("harness crashed"))},
			held:  q1, passed: q1,
		},
	}
}

// resumedQuestions are the runs that resume at lfg after one that ended
// with waiting.
func resumedQuestions() []questionCase {
	q1, q2 := question("run-1"), question("run-2")
	return []questionCase{
		{name: "a resumed session ends passed: every question at lfg dropped", lives: []life{
			waited, then(asksAgain, endsWith(Passed), judgeExits(1)),
		}},
		{
			// It may have waited for answers to the earlier question without
			// asking a new one, so the earlier question stays too.
			name:  "a resumed session that ends with waiting again: both stay and pass on",
			lives: []life{waited, then(asksAgain, endsWith(Waiting))},
			held:  []Question{q1, q2}, passed: []Question{q1, q2},
		},
		{
			name:  "a resumed session stopped before it asks again: both stay and pass on",
			lives: []life{waited, then(asksAgain, stops(5), endsFailed("stopped"))},
			held:  []Question{q1, q2}, passed: []Question{q1, q2},
		},
		{
			name:  "a resumed session whose harness failed after it was given the answers: both stay",
			lives: []life{waited, then(asksAgain, endsFailed("harness crashed"))},
			held:  []Question{q1, q2}, passed: []Question{q1, q2},
		},
		{
			name: "a resumed session that could not start: the question stays",
			lives: []life{waited, then(reopensWorktree, func(at func(int) FactHead) []Fact {
				return []Fact{SessionFailedToStart{FactHead: at(4), Action: "lfg", Reason: NewSessionText("no harness")}}
			})},
			held: []Question{q1}, passed: []Question{q1},
		},
		{
			name:  "a resume stopped before its session started: it passes the question on",
			lives: []life{waited, stopsWhileReopening},
			held:  []Question{q1}, passed: []Question{q1},
		},
		{
			name:  "a resume stopped at its take passes its start and the question on",
			lives: []life{waited, stopsAtTake, stopsAtTake},
			held:  []Question{q1}, passed: []Question{q1},
		},
	}
}

func TestARunKeepsTheQuestionsNoLaterSessionEndedOn(t *testing.T) {
	for _, tc := range slices.Concat(askingQuestions(), resumedQuestions()) {
		t.Run(tc.name, tc.check)
	}
}

// otherWS is the new worktree of a run whose reopened worktree was gone.
func otherWS() Workspace {
	return Workspace{Name: "issue-9-implement-2", Branch: "crew/issue-9-implement-2"}
}

// Covers AE5: the worktree is gone after waiting, so the run starts fresh
// at install and still holds the question; install fails, and the resume
// at install still holds it.
func TestAE5ARunWhoseWorktreeIsGoneStillHoldsTheQuestion(t *testing.T) {
	gone := func(at func(int) FactHead) []Fact {
		return []Fact{
			TakeSettled{FactHead: at(1), Landed: true}, WorkspaceGone{FactHead: at(2)},
			WorkspaceReady{FactHead: at(3), Workspace: otherWS(), Log: ".crew/logs/issue-9-implement-2.log"},
			ShellEnded{FactHead: at(4), Action: "install", Outcome: exited(1, "install failed")},
		}
	}
	h := lived(t, waits(sequence()), waited, gone)
	want := []Question{question("run-1")}
	last, _ := h.LastRun(testID, "implement")
	if got := last.Questions("lfg"); last.Start() != (StartFresh{}) || !slices.Equal(got, want) {
		t.Fatalf("run-2 started %#v holding %#v, want fresh holding %#v", last.Start(), got, want)
	}
	h = lived(t, waits(sequence()), waited, gone, func(at func(int) FactHead) []Fact {
		return []Fact{
			TakeSettled{FactHead: at(5), Landed: true},
			WorkspaceReady{FactHead: at(6), Workspace: otherWS(), Log: ".crew/logs/issue-9-implement-2.log", Resumed: true},
		}
	})
	last, _ = h.LastRun(testID, "implement")
	start, _ := last.Start().(StartAt)
	if got := last.Questions("lfg"); start.Action != "install" || !slices.Equal(got, want) {
		t.Errorf("run-3 started %#v holding %#v, want at install holding %#v", last.Start(), got, want)
	}
}

func TestASessionsStartRecordsItsLoginAndWhetherItMayAsk(t *testing.T) {
	started := func(asks bool) []RunEvent {
		return []RunEvent{ActionSessionStarted{EventHead: eh(4), Action: "lfg", Bot: developer, Login: asker, Asks: asks}}
	}
	fact := SessionStarted{FactHead: fh(4), Action: "lfg", Login: asker}
	decide(t, []decision{
		{name: "on: maps waiting: it may ask", given: starting(), def: waits, fact: fact, want: started(true)},
		{name: "no waiting entry: it does not ask", given: starting(), fact: fact, want: started(false)},
	})
}

func TestARunsQuestionsAreItsOwnAndSurviveItsSnapshot(t *testing.T) {
	take, _ := taken().(RunTaken)
	take.Questions = []Question{question("run-1")}
	run := given(t, []RunEvent{take})
	run, _ = Apply(run, ActionSessionStarted{EventHead: eh(4), Action: "lfg", Login: asker, Asks: true})
	want := []Question{question("run-1"), question(testRun)}

	take.Questions[0].Login = "changed"
	got := run.Questions("lfg")
	got[0].Login = "changed too"
	if got := run.Questions("lfg"); !slices.Equal(got, want) {
		t.Errorf("questions = %#v, want %#v, untouched by their take or a copy", got, want)
	}
	if got := run.Questions("judge"); got != nil {
		t.Errorf("questions at judge = %#v, want none", got)
	}
	restored, err := RestoreRuleRun(run.Snapshot())
	if err != nil || !reflect.DeepEqual(restored, run) {
		t.Errorf("RestoreRuleRun = %#v, %v, want the run back", restored.Snapshot(), err)
	}
}
