package core_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// recordingDriver is a driver of the draft rules whose model records
// crew's statistics, as crew v0.1.1 in /repo on GitHub.
func recordingDriver(t *testing.T) *driver {
	t.Helper()
	return recordingDriverOf(t, draft())
}

// recordingDriverOf is a driver of rules whose model records crew's
// statistics, as crew v0.1.1 in /repo on GitHub.
func recordingDriverOf(t *testing.T, rules []crew.Rule) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(rules, 2, core.RecordingStatistics("v0.1.1", "/repo", tracker)), now: t0}
}

// tracker is the tracker the recording drivers' issues are on.
const tracker crew.TrackerName = "github"

// The labels of brainstorm, a rule AE1 and AE2 name.
const (
	ideaReady   crew.State = "brainstorm:ready"
	ideaRunning crew.State = "brainstorm:in progress"
	ideaDone    crew.State = "brainstorm:done"
)

// brainstorming is one rule, brainstorm, whose session brainstorms the
// issue and whose passed route moves it to brainstorm:done.
func brainstorming() []crew.Rule {
	return []crew.Rule{{
		Name:    "brainstorm",
		Labels:  crew.Labels{Ready: ideaReady, Running: ideaRunning},
		Actions: []crew.Action{sessionAction("brainstorm", "Brainstorm issue {{.Issue.Ref}}")},
		Routes:  routes(ideaDone, needsAttention),
	}}
}

// sighting is crew seeing it at seen, in its one state when it is in one.
func sighting(it crew.Issue, seen time.Time) crew.IssueSighting {
	s := crew.IssueSighting{
		Tracker: tracker, Issue: it.ID(), Ref: it.Ref(), Kind: it.Kind(), Created: crew.Some(it.Created()), Seen: seen,
	}
	if state, ok := it.OnlyState(); ok {
		s.State = crew.Some(state)
	}
	return s
}

// outsideMove is a move of issue key from one label to another that crew
// saw at seen, made outside crew.
func outsideMove(key string, from, to crew.State, seen time.Time) crew.LabelMove {
	return crew.LabelMove{Tracker: tracker, Issue: issueID(key), From: from, To: to, Seen: seen}
}

// runMove is a move of issue 1 from one label to another that its last
// run made at d.now.
func (d *driver) runMove(from, to crew.State) crew.LabelMove {
	m := outsideMove("1", from, to, d.now)
	m.Run = crew.Some(d.run(issueID("1")))
	return m
}

// statisticsOf returns the records of the RecordStatistic commands in
// cmds, in order.
func statisticsOf(cmds []core.Command) []crew.Statistic {
	var out []crew.Statistic
	for _, c := range cmds {
		if r, ok := c.(core.RecordStatistic); ok {
			out = append(out, r.Statistic)
		}
	}
	return out
}

func wantStatistics(t *testing.T, got []crew.Statistic, want ...crew.Statistic) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statistics:\n got %#v\nwant %#v", got, want)
	}
}

// Covers R5, R6, F3, KTD3, KTD4: the start records the process once, with
// the id minted from the seed the engine stamped on it, then the
// repository the engine found, on the configured tracker, and publishes
// nothing.
func TestStartedRecordsTheProcessThenTheRepositoryOnce(t *testing.T) {
	d := recordingDriver(t)
	repository := crew.Repository{ID: "R_1", Name: "owner/name"}

	cmds, events := d.send(core.Started{Repository: repository})
	process := crew.Process{ID: crew.ProcessID(seed(1).String()), Version: "v0.1.1", Folder: "/repo", Start: d.now}
	wantCommands(t, cmds, core.RecordStatistic{Statistic: process},
		core.RecordStatistic{Statistic: crew.RepositoryRecord{Tracker: tracker, Repository: repository}})
	wantEvents(t, events)

	cmds, events = d.send(core.Started{Repository: repository})
	wantCommands(t, cmds)
	wantEvents(t, events)
}

// Covers AE1, R7: the first listing that finds an issue in a rule's label
// records it once, with the tracker's creation date and the listing's
// time; listing it again records nothing.
func TestAFirstListingRecordsAnIssueOnce(t *testing.T) {
	d := recordingDriverOf(t, brainstorming())
	it := blockedIssue(issue("315", 1, ideaReady))

	cmds, _ := d.poll(it)
	wantStatistics(t, statisticsOf(cmds), sighting(it, d.now))

	cmds, _ = d.poll(it)
	wantStatistics(t, statisticsOf(cmds))
}

// Covers R7: an issue the tracker gives no creation date is recorded with
// none.
func TestAnIssueWithNoCreationDateIsRecordedWithNone(t *testing.T) {
	d := recordingDriverOf(t, brainstorming())
	data := issue("315", 1, ideaRunning).Data()
	data.Created = time.Time{}
	it := crew.NewIssue(data)

	cmds, _ := d.poll(it)
	want := sighting(it, d.now)
	want.Created = crew.Optional[time.Time]{}
	wantStatistics(t, statisticsOf(cmds), want)
}

// Covers AE2, R8, KTD6: a listing that finds a recorded issue at another
// rule's label records the move, made outside crew, at the listing's time,
// with no tracker time.
func TestAListingRecordsAMoveMadeOutsideCrew(t *testing.T) {
	d := recordingDriverOf(t, brainstorming())
	d.poll(issue("315", 1, ideaRunning))

	cmds, _ := d.poll(issue("315", 1, ideaDone))
	wantStatistics(t, statisticsOf(cmds), outsideMove("315", ideaRunning, ideaDone, d.now))
}

// Covers R8, KTD4: a take that lands records its move, made by its run, at
// the time it landed; the next listing that finds the issue in the running
// label records nothing.
func TestALandedTakeRecordsItsMove(t *testing.T) {
	d := recordingDriver(t)
	i1 := issue("1", 1, ready)
	take, _ := d.poll(i1)
	wantStatistics(t, statisticsOf(take), sighting(i1, d.now))

	cmds, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(ready, inProgress))

	cmds, _ = d.poll(issue("1", 1, inProgress))
	wantStatistics(t, statisticsOf(cmds))
}

// Covers R8, F1, KTD4: a route's move that lands records the move from the
// rule's running label to the route's label, made by the run.
func TestALandedRouteMoveRecordsItsMove(t *testing.T) {
	d := recordingDriver(t)
	ending := implemented(d, succeeded)

	cmds, _ := d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(inProgress, readyToReview))
}

// Covers R8, KTD4: a return step that lands records the move from the
// answered rule's running label to the label the check found, made by the
// run.
func TestALandedReturnRecordsItsMove(t *testing.T) {
	rules := append(append(delegating(), answering()), depsAsking(t)...)
	m := core.New(rules, 2, core.Journaling(nil), core.WithBots(developerBots()),
		core.WithAnswerers(crewAnswerers()), core.Delegating("octocat"), core.RecordingStatistics("v0.1.1", "/repo", tracker))
	d := &driver{t: t, m: m, now: t0}
	d.checking()
	moved, _ := d.send(returnRead(unsureQuestion("crew-clerk[bot]"), crew.Comment{Author: "alice", Body: "yes"}))

	cmds, _ := d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(answeredRunning, depsReady))
}

// Covers KTD4: a close that lands records no move.
func TestALandedCloseRecordsNoMove(t *testing.T) {
	d := recordingDriverOf(t, implementClosing())
	ending := implemented(d, succeeded)

	cmds, _ := d.send(core.CallResult{ID: closeID(t, ending), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds))
}

// Covers KTD5: a listing answered while the take's move is in flight finds
// the issue in the running label, as the tracker moved it before the move
// returned; it records nothing, and the take that lands records its move
// once.
func TestAListingAheadOfTheTakesResultRecordsNoMove(t *testing.T) {
	d := recordingDriver(t)
	take, _ := d.poll(issue("1", 1, ready))

	cmds, _ := d.poll(issue("1", 1, inProgress))
	wantStatistics(t, statisticsOf(cmds))

	cmds, _ = d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(ready, inProgress))

	cmds, _ = d.poll(issue("1", 1, inProgress))
	wantStatistics(t, statisticsOf(cmds))
}

// Covers KTD5: while a take's move is owed, the listings find the issue in
// the running label, where the failed tries may have moved it, and record
// nothing; the retry that lands records the move once.
func TestListingsWhileATakeIsOwedRecordNoMove(t *testing.T) {
	d := recordingDriver(t)
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultFailed, Reason: "timeout"})

	retry, _ := d.send(core.Tick{})
	cmds, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, inProgress)}})
	wantStatistics(t, statisticsOf(cmds))

	cmds, _ = d.send(core.CallResult{ID: moveID(t, retry, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(ready, inProgress))
}

// Covers KTD5: a held issue that its session moved to its route's label is
// listed there, and the listing records nothing; the route's move that
// lands records the one move, made by the run.
func TestAListingOfAHeldIssueRecordsNoMove(t *testing.T) {
	d := recordingDriver(t)
	d.running(issue("1", 1, ready))

	cmds, _ := d.poll(issue("1", 1, readyToReview))
	wantStatistics(t, statisticsOf(cmds))

	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	ending, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	cmds, _ = d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(inProgress, readyToReview))
}

// Covers KTD5: a listing asked before a route's move lands and answered
// after it may predate the move: finding the issue still in the running
// label, it records nothing, and the next listing, which finds it in the
// route's label, records nothing either.
func TestAListingThatPredatesARouteMoveRecordsNoMove(t *testing.T) {
	d := recordingDriverOf(t, reviewClosed())
	ending := implemented(d, succeeded)
	d.send(core.Tick{})
	d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone})
	wantHeld(t, d.m)

	cmds, _ := d.send(core.IssuesListed{Issues: []crew.Issue{issue("1", 1, inProgress)}})
	wantStatistics(t, statisticsOf(cmds))

	cmds, _ = d.poll(issue("1", 1, readyToReview))
	wantStatistics(t, statisticsOf(cmds))
}

// Covers R7, KTD4: an issue first listed in two crew states is recorded
// with no state; listed later in one of them, it records no move, as crew
// knew no label it left.
func TestAnIssueInTwoStatesIsRecordedWithNoState(t *testing.T) {
	d := recordingDriver(t)
	it := blockedIssue(issue("1", 1, ready, readyToReview))

	cmds, _ := d.poll(it)
	wantStatistics(t, statisticsOf(cmds), sighting(it, d.now))

	cmds, _ = d.poll(blockedIssue(issue("1", 1, readyToReview)))
	wantStatistics(t, statisticsOf(cmds))
}

// Covers KTD1: a pull request in a label only issue rules name is not
// recorded.
func TestAnItemOfTheOtherKindIsNotRecorded(t *testing.T) {
	d := recordingDriver(t)

	cmds, _ := d.poll(pullRequest(issue("1", 1, ready)))
	wantStatistics(t, statisticsOf(cmds))
}

// Covers R8, KTD4: an issue missing from a listing keeps its last label,
// and a later listing that finds it at another records the move from that
// label.
func TestAnIssueBackAtAnotherLabelRecordsTheMoveFromItsLastLabel(t *testing.T) {
	d := recordingDriver(t)
	d.poll(blockedIssue(issue("1", 1, ready)))

	cmds, _ := d.poll()
	wantStatistics(t, statisticsOf(cmds))

	cmds, _ = d.poll(blockedIssue(issue("1", 1, readyToReview)))
	wantStatistics(t, statisticsOf(cmds), outsideMove("1", ready, readyToReview, d.now))
}

// Covers AE12: without recording, listings, takes and route moves record
// nothing.
func TestWithoutRecordingNothingIsRecorded(t *testing.T) {
	d := newDriver(t, draft(), 2)
	var cmds []core.Command
	keep := func(c []core.Command, _ []core.Published) []core.Command {
		cmds = append(cmds, c...)
		return c
	}
	keep(d.send(core.Started{Repository: crew.Repository{ID: "R_1", Name: "owner/name"}}))
	take := keep(d.poll(issue("1", 1, ready)))
	d.settle(keep(d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	ending := keep(d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded}))
	keep(d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone}))
	keep(d.poll(blockedIssue(issue("1", 1, needsAttention))))

	wantStatistics(t, statisticsOf(cmds))
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
	if got := events[0].Time(); !got.Equal(d.now) {
		t.Errorf("StatisticNotRecorded.Time() = %v, want %v", got, d.now)
	}
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
