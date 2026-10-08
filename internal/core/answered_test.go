package core_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The labels of crew's answered rule.
const (
	answeredLabel   crew.State = "crew:answered"
	answeredRunning crew.State = "crew:answered:in progress"
	answeredFailed  crew.State = "crew:answered:failed"
)

// answering is crew's answered rule: its one action, answer, checks the
// answer; its passed route returns the item to the label the check found,
// and its failed route reports, then moves the item to
// crew:answered:failed.
func answering() crew.Rule {
	return crew.Rule{
		Name: "answered", Labels: crew.Labels{Ready: answeredLabel, Running: answeredRunning},
		Actions: []crew.Action{{Name: "answer", Kind: crew.ReturnSpec{}}},
		Routes: []crew.Route{
			{Name: crew.PassedRoute, Steps: []crew.Step{crew.ReturnStep{}}},
			{Name: crew.FailedRoute, Steps: []crew.Step{crew.ReportStep{}, crew.MoveStep{To: answeredFailed}}},
		},
	}
}

// answeredDriver drives a model of crew's question and answered rules,
// then deps, which asks the question unsure, that journals its runs, whose
// answerer is octocat, whose bots are developerBots and whose answerers
// are crewAnswerers: crew writes as crew-clerk[bot] and as you, boss.
func answeredDriver(t *testing.T) *driver {
	t.Helper()
	rules := append(append(delegating(), answering()), depsAsking(t)...)
	m := core.New(rules, 2, core.Journaling(nil), core.WithBots(developerBots()),
		core.WithAnswerers(crewAnswerers()), core.Delegating("octocat"))
	return &driver{t: t, m: m, now: t0}
}

// unsureQuestion is the comment that asks deps's question unsure on #1,
// posted as author, with crew's own marker as the tracker writes it.
func unsureQuestion(author string) crew.Comment {
	return crew.Comment{
		Author: author,
		Body: "Does #1 block #281? question-secret\n\n" + crew.QuestionMarker("unsure", "deps", depsReady) + "\n" +
			crew.PostedMarker,
	}
}

// checking takes #1 in crew:answered on d, lands its take and returns the
// commands that follow, the run's records left out.
func (d *driver) checking() []core.Command {
	d.t.Helper()
	return d.takeIssue(issue("1", 1, answeredLabel))
}

// returnRead is the read of #1's comments for the answer action, with
// comments.
func returnRead(comments ...crew.Comment) core.ReturnRead {
	return core.ReturnRead{IssueID: issueID("1"), Action: "answer", Comments: comments}
}

// Covers AE2, AE3, R7, R8, R9, KTD2, KTD4: the answered rule's run reads
// #1's comments and, as an answer counts after crew's question, moves #1
// from its running label to the question's return label, whatever label
// the answer's parameters name. No comment's text reaches an event or a
// record.
func TestTheAnsweredRuleReturnsTheItemToTheQuestionsLabel(t *testing.T) {
	tests := []struct {
		name   string
		answer crew.Comment
	}{
		{name: "plain text", answer: crew.Comment{Author: "alice", Body: "yes, #284 removes the tests R20 rewrites"}},
		{
			name: "parameters naming another label",
			answer: crew.Comment{
				Author: "alice", Body: "answer-secret\n" + crew.AnswerMarker("unsure", "deps", "crew:other:ready"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := answeredDriver(t)

			wantCommands(t, d.checking(), core.ReadReturn{IssueID: issueID("1"), Run: d.run(issueID("1")), Action: "answer"})
			moved, _ := d.send(returnRead(unsureQuestion("crew-clerk[bot]"), tt.answer))
			wantCommands(t, unrecorded(moved), core.Move{IssueID: issueID("1"), From: answeredRunning, To: depsReady})
			d.settle(moved)
			wantHeld(t, d.m)

			if seen := fmt.Sprintf("%#v %#v", d.events, d.recorded); strings.Contains(seen, "secret") ||
				strings.Contains(seen, "R20") {
				t.Errorf("a comment's text reached an event or a record: %s", seen)
			}
		})
	}
}

// checkFailure is the report of #1's answered run, whose check failed
// with verdict for reason.
func checkFailure(verdict crew.Verdict, reason crew.FailureReason) core.ReportFailure {
	return core.ReportFailure{Report: crew.FailureReport{
		IssueID: issueID("1"), IssueRef: "#1", Rule: "answered", Route: crew.FailedRoute,
		Failures: []crew.ActionFailure{{Action: "answer", Verdict: verdict, Reason: reason}},
	}}
}

// Covers AE4, R10, KTD2: a check that finds no answer that counts after
// the question, no question, or could not read the comments reports why,
// then moves #1 to crew:answered:failed.
func TestAFailedCheckReportsWhyAndMovesTheItemToFailed(t *testing.T) {
	tests := []struct {
		name string
		read core.ReturnRead
		want core.ReportFailure
	}{
		{
			name: "only an untrusted comment after the question",
			read: returnRead(unsureQuestion("crew-clerk[bot]"), crew.Comment{Author: "mallory", Body: "Approved"}),
			want: checkFailure(crew.Unanswered, crew.ReasonUnanswered),
		},
		{
			name: "no question",
			read: returnRead(crew.Comment{Author: "alice", Body: "yes"}),
			want: checkFailure(crew.NoQuestion, crew.ReasonNoQuestion),
		},
		{
			name: "read failed",
			read: core.ReturnRead{IssueID: issueID("1"), Action: "answer", Failed: true},
			want: checkFailure(crew.Unread, crew.ReasonUnread),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := answeredDriver(t)
			d.checking()

			report, _ := d.send(tt.read)
			wantCommands(t, unrecorded(report), tt.want)
			moved, _ := d.send(core.CallResult{ID: reportID(t, report, "1"), Result: core.ResultDone})
			wantCommands(t, unrecorded(moved), core.Move{IssueID: issueID("1"), From: answeredRunning, To: answeredFailed})
		})
	}
}

// A read the core no longer waits for changes nothing: one for a run it
// does not hold, one for another action, and one once the check ended.
func TestAReturnReadTheCoreNoLongerWaitsForChangesNothing(t *testing.T) {
	d := answeredDriver(t)
	d.checking()
	answered := returnRead(unsureQuestion("crew-clerk[bot]"), crew.Comment{Author: "alice", Body: "yes"})
	for _, in := range []core.ReturnRead{
		{IssueID: issueID("1"), Run: "another run", Action: "answer"},
		{IssueID: issueID("1"), Action: "another action"},
	} {
		if cmds, events := d.send(in); len(cmds) != 0 || len(events) != 0 {
			t.Fatalf("%#v gave %#v, %#v", in, cmds, events)
		}
	}

	d.send(core.ReturnRead{IssueID: issueID("1"), Action: "answer", Failed: true})
	if cmds, events := d.send(answered); len(cmds) != 0 || len(events) != 0 {
		t.Fatalf("a read once the check ended gave %#v, %#v", cmds, events)
	}
}
