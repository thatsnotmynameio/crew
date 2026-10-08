package core_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// recordingDriver is a driver whose model records crew's statistics, as
// crew v0.1.1 in /repo.
func recordingDriver(t *testing.T) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(draft(), 2, core.RecordingStatistics("v0.1.1", "/repo")), now: t0}
}

// Covers R5, F3, KTD4: the start records the process once, with the id
// minted from the seed the engine stamped on it, and publishes nothing.
func TestStartedRecordsTheProcessOnce(t *testing.T) {
	d := recordingDriver(t)

	cmds, events := d.send(core.Started{})
	process := crew.Process{ID: crew.ProcessID(seed(1).String()), Version: "v0.1.1", Folder: "/repo", Start: d.now}
	wantCommands(t, cmds, core.RecordStatistic{Statistic: process})
	wantEvents(t, events)

	cmds, events = d.send(core.Started{})
	wantCommands(t, cmds)
	wantEvents(t, events)
}

// Covers AE12: without recording, the start records nothing.
func TestStartedWithoutRecordingDoesNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)

	cmds, events := d.send(core.Started{})
	wantCommands(t, cmds)
	wantEvents(t, events)
}

// Covers AE8, KTD6: a record the store could not write is a published
// warning, and the runs, slots and queues go on as they were.
func TestAStatisticNotWrittenIsReportedAndChangesNothingElse(t *testing.T) {
	d := recordingDriver(t)
	d.send(core.Started{})
	d.running(issue("1", 1, ready))
	before := d.m.View()

	process := crew.Process{ID: "p", Version: "v0.1.1", Folder: "/repo", Start: t0}
	cmds, events := d.send(core.StatisticFailed{Statistic: process, Reason: "disk full"})
	wantCommands(t, cmds)
	wantEvents(t, events, core.StatisticNotRecorded{At: d.now, Statistic: process, Reason: "disk full"})
	if after := d.m.View(); !reflect.DeepEqual(after, before) {
		t.Fatalf("view:\n got %#v\nwant %#v", after, before)
	}
}

// Covers KTD4: the start does not change the first tick's listing.
func TestATickAfterStartedListsAsAFirstTick(t *testing.T) {
	d := recordingDriver(t)
	d.send(core.Started{})

	cmds, events := d.send(core.Tick{})
	wantCommands(t, cmds, core.ListIssues{States: draftListing})
	wantEvents(t, events)
}
