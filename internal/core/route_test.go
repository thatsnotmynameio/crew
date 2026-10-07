package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// wantUnchanged fails the test unless an input left no command, no event
// and the view as it was before.
func wantUnchanged(t *testing.T, d *driver, before core.View, cmds []core.Command, events []core.Published) {
	t.Helper()
	wantCommands(t, cmds)
	wantEvents(t, events)
	if v := d.m.View(); !reflect.DeepEqual(v, before) {
		t.Fatalf("view:\n got %#v\nwant %#v", v, before)
	}
}

// Covers KTD7: a late answer for a released run of issue 1 never reaches
// the newer run of issue 1 that runs the same action.
func TestALateSessionEndOfAReleasedRunChangesNothingInTheNewerRun(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.settle(endedNeedingAttention(d))
	wantHeld(t, d.m)
	released := d.run(issueID("1"))

	d.running(issue("1", 1, ready))
	if d.run(issueID("1")) == released {
		t.Fatalf("issue 1 was not taken again")
	}
	before := d.m.View()
	cmds, events := d.send(core.SessionEnded{
		IssueID: issueID("1"), Run: released, Action: "development", Outcome: failed("late"),
	})
	wantUnchanged(t, d, before, cmds, events)
}

// Covers KTD7: a workspace ready for a run the core does not hold starts
// no session, and the run's own workspace still does.
func TestAWorkspaceReadyNamingAnUnknownRunChangesNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)
	cmds, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, cmds, "1"), Result: core.ResultDone})

	before := d.m.View()
	unknown := space("1", "implement")
	unknown.Run = crew.NewRuleRunID(seed(99), 1)
	cmds, events := d.send(unknown)
	wantUnchanged(t, d, before, cmds, events)

	cmds, _ = d.send(space("1", "implement"))
	wantCommands(t, cmds, d.session("1", "acceptance", "Implement test acceptance for issue #1"))

	// A second answer for the same run finds it no longer waiting.
	before = d.m.View()
	cmds, events = d.send(space("1", "implement"))
	wantUnchanged(t, d, before, cmds, events)
}
