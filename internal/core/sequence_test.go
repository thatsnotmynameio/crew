package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// judging is the draft rules with implement running development, acting as
// developer, then the shell action judge, whose exit status 3 gives
// needs_person, which moves the issue to needs person.
func judging() []crew.Rule {
	rules := withSpec(draft(), 0, "development", func(s *crew.SessionSpec) { s.Bot = crew.Bot{Name: "developer"} })
	judge := shellAction("judge", "./judge")
	spec, _ := judge.Kind.(crew.ShellSpec)
	spec.Verdicts = map[int]crew.Verdict{3: "needs_person"}
	judge.Kind = spec
	judge.On = crew.On{"needs_person": crew.ToRoute{Route: "needs-person"}}
	rules[0].Actions = []crew.Action{rules[0].Actions[1], judge}
	rules[0].Routes = append(rules[0].Routes,
		crew.Route{Name: "needs-person", Steps: []crew.Step{crew.MoveStep{To: "needs person"}}})
	return rules
}

// judged runs #1 through judging until judge's script runs, and returns
// its RunShell.
func (d *driver) judged() core.RunShell {
	d.t.Helper()
	d.running(issue("1", 1, ready))
	return runShellOf(d.t, d.ended("1", "development", succeeded))
}

// Covers KTD-S12, KTD13: a shell action after a session runs in the run's
// workspace as that session's bot, with that session's name.
func TestAShellActionAfterASessionRunsAsThatSession(t *testing.T) {
	d := newDriver(t, judging(), 2)
	w := space("1", "implement")
	want := core.RunShell{IssueID: issueID("1"), Run: "", Action: "judge", Script: core.Script{
		Dir: w.Dir, Name: "judge", Command: "./judge", Log: w.Log, Rule: "implement", IssueRef: "#1",
		IssueURL: "https://example.com/issues/1", Branch: w.Branch, Session: "development", Bot: "developer",
	}}
	got := d.judged()
	want.Run = d.run(issueID("1"))
	wantCommands(t, []core.Command{got}, want)
}

// Covers R9: a script's exit status gives its verdict, which its on: sends
// to a route, or to the next action, or failed.
func TestAShellActionsExitStatusGivesItsVerdict(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   core.Command
	}{
		{name: "0 passes", status: 0, want: core.Move{IssueID: issueID("1"), From: inProgress, To: readyToReview}},
		{name: "3 needs a person", status: 3, want: core.Move{IssueID: issueID("1"), From: inProgress, To: "needs person"}},
		{name: "1 fails", status: 1, want: failureOf("1", "implement", "judge")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, judging(), 2)
			d.judged()
			cmds, _ := d.send(core.ShellEnded{IssueID: issueID("1"), Action: "judge", Outcome: exited(tt.status)})
			wantCommands(t, cmds, tt.want)
		})
	}
}

// Covers R9: a session's reported verdict leads where its on: says, and a
// verdict its on: does not name fails it.
func TestASessionsReportedVerdictLeadsWhereItsOnSays(t *testing.T) {
	tests := []struct {
		name    string
		verdict crew.Verdict
		route   crew.RouteName
		want    core.Command
	}{
		{name: "blocked", verdict: "blocked", route: "blocked",
			want: core.Move{IssueID: issueID("1"), From: inProgress, To: "blocked"}},
		{name: "not named", verdict: "lost", route: crew.FailedRoute, want: failureOf("1", "implement", "acceptance")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, blocking(), 2)
			d.running(issue("1", 1, ready))
			cmds, events := d.send(core.SessionEnded{
				IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded,
				Report: crew.VerdictReported{Verdict: tt.verdict},
			})
			wantCommands(t, cmds, tt.want)
			for _, e := range events {
				if chosen, ok := e.(crew.RouteChosen); ok && chosen.Route != tt.route {
					t.Fatalf("chose %q, want %q", chosen.Route, tt.route)
				}
			}
		})
	}
}

// Covers KTD-S11: a shell action before any session of a fresh run acts as
// you, with no session's name.
func TestAShellActionBeforeAnySessionRunsAsYou(t *testing.T) {
	rules := draft()
	rules[0].Actions = append([]crew.Action{shellAction("install", "make deps")}, rules[0].Actions...)
	d := newDriver(t, rules, 2)
	cmds, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

	got := runShellOf(t, d.ready("1"))
	if got.Action != "install" || got.Script.Session != "" || got.Script.Bot != "" {
		t.Fatalf("install = %#v, want it acting as you after no session", got)
	}
	wantCommands(t,
		func() []core.Command {
			cmds, _ := d.send(core.ShellEnded{IssueID: issueID("1"), Action: "install", Outcome: exited(0)})
			return cmds
		}(),
		d.session("1", "acceptance", "Implement test acceptance for issue #1"))
}

// Covers KTD18: a shell action's start is one of the events a resume
// depends on.
func TestAShellActionsStartSaysSoWhenItFailsToWrite(t *testing.T) {
	d := &driver{t: t, m: core.New(judging(), 2, core.Journaling(nil)), now: t0}
	d.judged()
	var asked crew.ActionShellAsked
	for _, e := range d.recorded {
		if a, ok := e.(crew.ActionShellAsked); ok {
			asked = a
		}
	}
	_, events := d.send(core.RecordFailed{Event: asked, Reason: "disk full"})
	wantEvents(t, events, core.RunNotRecorded{
		At: d.now, IssueID: issueID("1"), IssueRef: "#1", Rule: "implement", Action: "judge",
		What: "the start of judge", Reason: "disk full",
	})
}
