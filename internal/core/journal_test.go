package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestANewRunContinuesTheLastRunOfItsIssueAndRule(t *testing.T) {
	past := failedRun(t, "broke")
	last, _ := past[0].(crew.RunTaken)
	d := resumeDriver(t, past...)

	d.poll(issue("9", 1, readyForDev), issue("10", 1, readyForDev))

	for _, e := range d.events {
		taken, ok := e.(crew.RunTaken)
		if !ok {
			continue
		}
		want := crew.Optional[crew.RuleRunID]{}
		if taken.IssueID == issueID("9") {
			want = crew.Some(last.Run)
		}
		if taken.Continues != want {
			t.Errorf("#%s's run continues %v, want %v", taken.IssueID.Key, taken.Continues, want)
		}
	}
}

func TestReplayingThePastTakesNoSlotAndPublishesNothing(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		cmds, _ := d.send(core.SessionEnded{
			IssueID: issueID("9"), Action: "lfg", Outcome: failed("broke"), Usage: crew.Usage{Cost: crew.Some(3.0)},
		})
		d.settle(cmds)
	})
	d := resumeDriver(t, past...)

	if v := d.m.View(); len(v.Issues) != 0 || len(v.Handled) != 0 || v.Spent != (crew.Spend{}) || v.Queues[0].Busy != 0 {
		t.Fatalf("view = %#v, want nothing held, handled or spent", v)
	}
	cmds, events := d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{readyForDev, crewRunning, readyForFix, ready, inProgress}})
	wantEvents(t, events)
}
