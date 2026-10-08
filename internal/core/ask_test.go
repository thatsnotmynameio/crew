package core_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The labels of the asking rule deps and of crew's question rule.
const (
	depsReady       crew.State = "crew:deps:ready"
	depsRunning     crew.State = "crew:deps:in progress"
	questionLabel   crew.State = "crew:question"
	questionRunning crew.State = "crew:question:in progress"
	questionWaiting crew.State = "crew:question:waiting answer"
)

// depsAsking is the rule deps: its session check, on unsure, ends through the
// route ask, which asks the question unsure, to return to deps's ready
// label, and moves the issue to crew:question.
func depsAsking(t *testing.T) []crew.Rule {
	t.Helper()
	text, err := crew.ParseCommentTemplate("ask", "Does {{.Issue.Ref}} block #281?")
	if err != nil {
		t.Fatal(err)
	}
	check := sessionAction("check", "Check {{.Issue.Ref}}")
	check.On = crew.On{"unsure": crew.ToRoute{Route: "ask"}}
	return []crew.Rule{{
		Name: "deps", Labels: crew.Labels{Ready: depsReady, Running: depsRunning}, Actions: []crew.Action{check},
		Routes: append(routes("crew:deps:done", "crew:deps:failed"), crew.Route{Name: "ask", Steps: []crew.Step{
			crew.QuestionStep{Question: crew.Ask{ID: "unsure", Text: text, Return: depsReady}},
			crew.MoveStep{To: questionLabel},
		}}),
	}}
}

// unsure runs #1 through deps on d until its session check ends with
// unsure, and returns the commands of the first step of the route ask.
func (d *driver) unsure() []core.Command {
	d.t.Helper()
	d.running(issue("1", 1, depsReady))
	cmds, _ := d.send(core.SessionEnded{
		IssueID: issueID("1"), Action: "check", Outcome: succeeded, Report: crew.VerdictReported{Verdict: "unsure"},
	})
	return cmds
}

// Covers AE1, R1, KTD1: the route ask posts the question, its text rendered
// for the run and its marker naming deps and the return label, then moves
// the issue to crew:question.
func TestAQuestionStepPostsTheQuestionThenTheRouteMoves(t *testing.T) {
	d := newDriver(t, depsAsking(t), 2)

	comment := d.unsure()
	wantCommands(t, comment, core.Comment{
		IssueID: issueID("1"), Body: "Does #1 block #281?\n\n" + crew.QuestionMarker("unsure", "deps", depsReady),
	})
	moved, events := d.send(core.CallResult{ID: commentID(t, comment), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: depsRunning, To: questionLabel})
}

// Covers KTD1: a question the tracker refuses is given up as a comment
// step's is, and the route's move still runs.
func TestARefusedQuestionIsGivenUpAndTheRouteStillMoves(t *testing.T) {
	d := newDriver(t, depsAsking(t), 2)
	comment := d.unsure()

	moved, events := d.send(core.CallResult{ID: commentID(t, comment), Result: core.ResultRefused, Reason: "forbidden"})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepGivenUp{Reason: "forbidden"}))
	wantCommands(t, moved, core.Move{IssueID: issueID("1"), From: depsRunning, To: questionLabel})
}

// delegating is crew's question rule, a rule without actions: its passed
// route delegates the issue's open question, then moves the issue to
// crew:question:waiting answer.
func delegating() []crew.Rule {
	return []crew.Rule{{
		Name: "question", Labels: crew.Labels{Ready: questionLabel, Running: questionRunning},
		Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{
			crew.DelegateStep{}, crew.MoveStep{To: questionWaiting},
		}}},
	}}
}

// delegateDriver drives a model of delegating that journals its runs,
// whose answerer is octocat and whose bots are developerBots: crew writes
// as crew-clerk[bot], and you are boss.
func delegateDriver(t *testing.T) *driver {
	t.Helper()
	m := core.New(delegating(), 2, core.Journaling(nil), core.WithBots(developerBots()), core.Delegating("octocat"))
	return &driver{t: t, m: m, now: t0}
}

// readingQuestion takes #1 in crew:question on d, lands its take and
// returns the commands that follow, the run's records left out.
func (d *driver) readingQuestion() []core.Command {
	d.t.Helper()
	return d.takeIssue(issue("1", 1, questionLabel))
}

// postedQuestion is the comment that asks deps's question blocks on #1,
// posted as author, with crew's own marker as the tracker writes it.
func postedQuestion(author string) crew.Comment {
	return crew.Comment{
		Author: author,
		Body: "Does #1 block #281? question-secret\n\n" + crew.QuestionMarker("blocks", "deps", depsReady) + "\n" +
			crew.PostedMarker,
	}
}

// delegationOf is the delegation of #1's question to octocat, as search
// found it: the question id of deps when found.
func delegationOf(search crew.QuestionSearch, id crew.QuestionID) core.Delegate {
	d := crew.Delegation{IssueID: issueID("1"), IssueRef: "#1", Answerer: "octocat", Search: search, ID: id}
	if id != "" {
		d.Rule = "deps"
	}
	return core.Delegate{Delegation: d}
}

// delegateID returns the ID of the delegation in cmds.
func delegateID(t *testing.T, cmds []core.Command) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if delegate, ok := c.(core.Delegate); ok {
			return delegate.ID
		}
	}
	t.Fatalf("no delegation in %#v", cmds)
	return 0
}

// delegated is the call of #1's delegation as CallOwed, CallDropped and
// View.Owed show it.
var delegated = core.Call{Kind: core.CallDelegate, IssueID: issueID("1"), IssueRef: "#1"}

// waitingMove is #1's move from the question rule's running label to
// crew:question:waiting answer.
var waitingMove = core.Move{IssueID: issueID("1"), From: questionRunning, To: questionWaiting}

// Covers AE1, R4, R5, R13, KTD8: the question rule reads the comments, then
// delegates the open question to the answerer, naming the question and
// the rule that asked it, and then moves the issue to wait for an answer.
// What a comment says reaches no event and no record.
func TestTheQuestionRuleReadsTheCommentsAndDelegatesTheOpenQuestion(t *testing.T) {
	d := delegateDriver(t)

	read := d.readingQuestion()
	wantCommands(t, read, core.ReadQuestion{IssueID: issueID("1"), Run: d.run(issueID("1")), Step: 0})
	delegate, _ := d.send(core.QuestionRead{IssueID: issueID("1"), Step: 0, Comments: []crew.Comment{
		{Author: "alice", Body: "answer-secret"}, postedQuestion("crew-clerk[bot]"),
	}})
	wantCommands(t, delegate, delegationOf(crew.QuestionFound, "blocks"))
	moved, events := d.send(core.CallResult{ID: delegateID(t, delegate), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))
	wantCommands(t, unrecorded(moved), waitingMove)
	d.settle(moved)
	wantHeld(t, d.m)

	if seen := fmt.Sprintf("%#v %#v", d.events, d.recorded); strings.Contains(seen, "secret") {
		t.Errorf("a comment's text reached an event or a record: %s", seen)
	}
}

// Covers KTD8: every delegation mentions the answerer and says what the
// read found: the question crew posted, as its writer or as you, none when
// only someone else forged one, or nothing as the read failed. The route
// still ends with its move.
func TestADelegationSaysWhatTheReadFound(t *testing.T) {
	tests := []struct {
		name string
		read core.QuestionRead
		want core.Delegate
	}{
		{
			name: "posted as crew's writer",
			read: core.QuestionRead{Comments: []crew.Comment{postedQuestion("crew-clerk[bot]")}},
			want: delegationOf(crew.QuestionFound, "blocks"),
		},
		{
			name: "posted as you",
			read: core.QuestionRead{Comments: []crew.Comment{postedQuestion("boss")}},
			want: delegationOf(crew.QuestionFound, "blocks"),
		},
		{
			name: "forged by someone else",
			read: core.QuestionRead{Comments: []crew.Comment{postedQuestion("mallory")}},
			want: delegationOf(crew.QuestionNotFound, ""),
		},
		{name: "no comment", read: core.QuestionRead{}, want: delegationOf(crew.QuestionNotFound, "")},
		{
			name: "read failed",
			read: core.QuestionRead{Failed: true, Reason: "gh: HTTP 502"},
			want: delegationOf(crew.QuestionUnread, ""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := delegateDriver(t)
			d.readingQuestion()
			tt.read.IssueID = issueID("1")

			delegate, _ := d.send(tt.read)
			wantCommands(t, delegate, tt.want)
			moved, _ := d.send(core.CallResult{ID: delegateID(t, delegate), Result: core.ResultDone})
			wantCommands(t, unrecorded(moved), waitingMove)
		})
	}
}

// Covers KTD9: a delegation the tracker refuses is given up, and the
// route's move still runs.
func TestARefusedDelegationIsGivenUpAndTheRouteStillMoves(t *testing.T) {
	d := delegateDriver(t)
	d.readingQuestion()
	delegate, _ := d.send(core.QuestionRead{IssueID: issueID("1")})

	moved, events := d.send(core.CallResult{ID: delegateID(t, delegate), Result: core.ResultRefused, Reason: "no App"})
	hasEvent(t, events, core.CallDropped{At: d.now, Call: delegated, Result: core.ResultRefused, Reason: "no App"})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepGivenUp{Reason: "no App"}))
	wantCommands(t, unrecorded(moved), waitingMove)
}

// Covers KTD9: a delegation that fails transiently is owed and retried at
// the next tick, like a report.
func TestAnOwedDelegationIsRetriedAtTheNextTick(t *testing.T) {
	d := delegateDriver(t)
	d.readingQuestion()
	delegate, _ := d.send(core.QuestionRead{IssueID: issueID("1")})

	cmds, events := d.send(core.CallResult{ID: delegateID(t, delegate), Result: core.ResultFailed, Reason: "timeout"})
	wantCommands(t, cmds)
	wantEvents(t, events, core.CallOwed{At: d.now, Call: delegated, Reason: "timeout"})
	wantOwed(t, d.m, delegated)

	retry, _ := d.send(core.Tick{})
	wantCommands(t, retry, core.ListIssues{States: []crew.State{questionLabel, questionRunning}},
		delegationOf(crew.QuestionNotFound, ""))
	moved, _ := d.send(core.CallResult{ID: delegateID(t, retry), Result: core.ResultDone})
	wantCommands(t, unrecorded(moved), waitingMove)
	wantOwed(t, d.m)
}

// A read the core no longer waits for changes nothing: one for a run it
// does not hold, a second one while the delegation is in flight, and one
// once the delegation settled.
func TestAQuestionReadTheCoreNoLongerWaitsForChangesNothing(t *testing.T) {
	d := delegateDriver(t)
	d.readingQuestion()
	stray := core.QuestionRead{IssueID: issueID("1"), Run: "another run", Step: 0}
	if cmds, events := d.send(stray); len(cmds) != 0 || len(events) != 0 {
		t.Fatalf("a read for a run not held gave %#v, %#v", cmds, events)
	}

	delegate, _ := d.send(core.QuestionRead{IssueID: issueID("1")})
	again := core.QuestionRead{IssueID: issueID("1"), Comments: []crew.Comment{postedQuestion("crew-clerk[bot]")}}
	if cmds, events := d.send(again); len(cmds) != 0 || len(events) != 0 {
		t.Fatalf("a read while the delegation is in flight gave %#v, %#v", cmds, events)
	}
	d.send(core.CallResult{ID: delegateID(t, delegate), Result: core.ResultDone})
	if cmds, events := d.send(again); len(cmds) != 0 || len(events) != 0 {
		t.Fatalf("a read once the delegation settled gave %#v, %#v", cmds, events)
	}
}

// A stop while crew reads the comments waits for the read: the delegation
// is posted, and the route still ends with its move, as other tracker
// steps do after a stop.
func TestAStopWhileCrewReadsTheCommentsStillDelegatesAndMoves(t *testing.T) {
	d := delegateDriver(t)
	d.readingQuestion()

	if cmds, _ := d.send(core.StopRequested{}); len(unrecorded(cmds)) != 0 {
		t.Fatalf("the stop gave %#v, want nothing while crew reads", cmds)
	}
	delegate, _ := d.send(core.QuestionRead{IssueID: issueID("1"), Failed: true, Reason: "stopping"})
	wantCommands(t, delegate, delegationOf(crew.QuestionUnread, ""))
	moved, events := d.send(core.CallResult{ID: delegateID(t, delegate), Result: core.ResultDone})
	hasEvent(t, events, d.stepEnded("1", 0, crew.StepLanded{}))
	wantCommands(t, unrecorded(moved), waitingMove)
	d.settle(moved)
	if !d.m.Stopped() {
		t.Errorf("crew did not stop once the route ended")
	}
}
