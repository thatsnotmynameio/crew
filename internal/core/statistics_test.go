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
// the time it landed, after the span its take opened; the next listing
// that finds the issue in the running label records nothing.
func TestALandedTakeRecordsItsMove(t *testing.T) {
	d := recordingDriver(t)
	i1 := issue("1", 1, ready)
	take, _ := d.poll(i1)
	wantStatistics(t, statisticsOf(take), sighting(i1, d.now), d.opened("1"))

	cmds, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(ready, inProgress))

	cmds, _ = d.poll(issue("1", 1, inProgress))
	wantStatistics(t, statisticsOf(cmds))
}

// Covers R8, F1, KTD4: a route's move that lands records the move from the
// rule's running label to the route's label, made by the run, then the end
// of the run's span.
func TestALandedRouteMoveRecordsItsMove(t *testing.T) {
	d := recordingDriver(t)
	ending := implemented(d, succeeded)

	cmds, _ := d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(inProgress, readyToReview),
		d.closed("1", crew.OutcomeRouted, crew.PassedRoute, ""))
}

// Covers R8, KTD4: a return step that lands records the move from the
// answered rule's running label to the label the check found, made by the
// run, then the end of the run's span.
func TestALandedReturnRecordsItsMove(t *testing.T) {
	rules := append(append(delegating(), answering()), depsAsking(t)...)
	m := core.New(rules, 2, core.Journaling(nil), core.WithBots(developerBots()),
		core.WithAnswerers(crewAnswerers()), core.Delegating("octocat"), core.RecordingStatistics("v0.1.1", "/repo", tracker))
	d := &driver{t: t, m: m, now: t0}
	d.checking()
	moved, _ := d.send(returnRead(unsureQuestion("crew-clerk[bot]"), crew.Comment{Author: "alice", Body: "yes"}))

	cmds, _ := d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(answeredRunning, depsReady),
		d.closed("1", crew.OutcomeRouted, crew.PassedRoute, ""))
}

// Covers KTD4: a close that lands records no move, only the end of the
// run's span.
func TestALandedCloseRecordsNoMove(t *testing.T) {
	d := recordingDriverOf(t, implementClosing())
	ending := implemented(d, succeeded)

	cmds, _ := d.send(core.CallResult{ID: closeID(t, ending), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.closed("1", crew.OutcomeRouted, crew.PassedRoute, ""))
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
// lands records the one move, made by the run, then the end of its span.
func TestAListingOfAHeldIssueRecordsNoMove(t *testing.T) {
	d := recordingDriver(t)
	d.running(issue("1", 1, ready))

	cmds, _ := d.poll(issue("1", 1, readyToReview))
	wantStatistics(t, statisticsOf(cmds))

	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	ending, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	cmds, _ = d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultDone})
	wantStatistics(t, statisticsOf(cmds), d.runMove(inProgress, readyToReview),
		d.closed("1", crew.OutcomeRouted, crew.PassedRoute, ""))
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

// startedDriverOf is a driver of rules whose model records crew's
// statistics, as crew v0.1.1 in /repo on GitHub, journals from past when
// there is a past, and has recorded its process. Its inputs are seeded
// after those of the drivers that left past, as resumeDriverOf's are.
func startedDriverOf(t *testing.T, rules []crew.Rule, past ...crew.RunEvent) *driver {
	t.Helper()
	opts := []core.Option{core.RecordingStatistics("v0.1.1", "/repo", tracker)}
	if past != nil {
		opts = append(opts, core.Journaling(past))
	}
	d := &driver{t: t, m: core.New(rules, 2, opts...), now: t0}
	for _, e := range past {
		if _, ok := e.(crew.RunTaken); ok {
			d.inputs += 1000
		}
	}
	d.send(core.Started{Repository: crew.Repository{ID: "R_1", Name: "owner/name"}})
	return d
}

// process returns the crew process d's model recorded, which the spans of
// its runs name; empty before it recorded one.
func (d *driver) process() crew.ProcessID {
	for _, st := range d.statistics {
		if p, ok := st.(crew.Process); ok {
			return p.ID
		}
	}
	return ""
}

// opened is the span of issue key's last run as its take opened it: by
// d's process, in no named queue.
func (d *driver) opened(key string) crew.RuleRunSpan {
	d.t.Helper()
	taken := takenOf(d.t, d, key)
	return crew.RuleRunSpan{
		Tracker: tracker, Issue: taken.IssueID, Run: taken.Run, Process: d.process(), Rule: taken.Rule,
		Continues: taken.Continues, Start: taken.At,
	}
}

// closed is the span of issue key's last run ended at d.now with outcome,
// through route unless it is empty, halted by halt unless it is empty.
func (d *driver) closed(key string, outcome crew.RunOutcome, route crew.RouteName, halt crew.RunHalt) crew.RuleRunSpan {
	d.t.Helper()
	sp := d.opened(key)
	end := crew.RuleRunEnd{At: d.now, Outcome: outcome}
	if route != "" {
		end.Route = crew.Some(route)
	}
	if halt != "" {
		end.Halt = crew.Some(halt)
	}
	sp.End = crew.Some(end)
	return sp
}

// spansOf returns the rule run spans among sts, in order.
func spansOf(sts []crew.Statistic) []crew.Statistic {
	var out []crew.Statistic
	for _, st := range sts {
		if sp, ok := st.(crew.RuleRunSpan); ok {
			out = append(out, sp)
		}
	}
	return out
}

// promoteRunning is the running label of promote.
const promoteRunning crew.State = "promote:in progress"

// promoting is one rule, promote, whose session promotes a brainstormed
// issue from brainstorm:done.
func promoting() []crew.Rule {
	return []crew.Rule{{
		Name:    "promote",
		Labels:  crew.Labels{Ready: ideaDone, Running: promoteRunning},
		Actions: []crew.Action{sessionAction("promote", "Promote issue {{.Issue.Ref}}")},
		Routes:  routes("plan:ready", needsAttention),
	}}
}

// Covers AE3, R8, R9, KTD2: a take of #315 by promote opens the run's span
// under #315, by the process, before the take's move, which names the same
// run once it lands.
func TestATakeOpensTheRunsSpanBeforeItsMove(t *testing.T) {
	d := startedDriverOf(t, promoting())
	it := issue("315", 1, ideaDone)

	take, _ := d.poll(it)
	open := d.opened("315")
	if open.Process == "" || open.Rule != "promote" || open.Issue != issueID("315") || !open.Start.Equal(d.now) {
		t.Fatalf("open = %#v, want promote's span of #315 by the process, at the take", open)
	}
	wantStatistics(t, statisticsOf(take), sighting(it, d.now), open)

	cmds, _ := d.send(core.CallResult{ID: moveID(t, take, "315"), Result: core.ResultDone})
	move := outsideMove("315", ideaDone, promoteRunning, d.now)
	move.Run = crew.Some(open.Run)
	wantStatistics(t, statisticsOf(cmds), move)
}

// Covers R9, KTD3: each way a run ends records its span's end at the
// release, with its outcome, its route and the halt that chose it.
// releases are the ways a run of #1 by implement ends, with the outcome,
// route and halt its span's end records.
var releases = []struct {
	name    string
	play    func(d *driver)
	outcome crew.RunOutcome
	route   crew.RouteName
	halt    crew.RunHalt
}{
	{"its passed route's move lands", func(d *driver) {
		d.settle(implemented(d, succeeded))
	}, crew.OutcomeRouted, crew.PassedRoute, ""},
	{"its final move is dropped", func(d *driver) {
		ending := implemented(d, succeeded)
		d.send(core.CallResult{ID: moveID(d.t, ending, "1"), Result: core.ResultMovedMeanwhile, Reason: "moved"})
	}, crew.OutcomeRouteDropped, crew.PassedRoute, ""},
	{"its final move is given up", func(d *driver) {
		ending := implemented(d, succeeded)
		d.send(core.CallResult{ID: moveID(d.t, ending, "1"), Result: core.ResultRefused, Reason: "nope"})
	}, crew.OutcomeRouteGivenUp, crew.PassedRoute, ""},
	{"its take does not land", func(d *driver) {
		take, _ := d.poll(issue("1", 1, ready))
		d.send(core.CallResult{ID: moveID(d.t, take, "1"), Result: core.ResultRefused, Reason: "nope"})
	}, crew.OutcomeNotTaken, "", ""},
	{"crew's stop ends its session", func(d *driver) {
		d.running(issue("1", 1, ready))
		d.send(core.StopRequested{})
		d.settle(d.ended("1", "acceptance", failed("stopped")))
	}, crew.OutcomeRouted, crew.FailedRoute, crew.HaltStop},
	{"crew's stop reaches it while its take is in flight", func(d *driver) {
		take, _ := d.poll(issue("1", 1, ready))
		d.send(core.StopRequested{})
		d.settle(take)
	}, crew.OutcomeRouted, crew.FailedRoute, crew.HaltStop},
	{"the run time limit keeps its next action from starting", func(d *driver) {
		d.running(issue("1", 1, ready))
		d.send(core.TimeUp{Limit: limit})
		d.settle(d.ended("1", "acceptance", succeeded))
	}, crew.OutcomeRouted, crew.FailedRoute, crew.HaltRunTimeLimit},
	{"its session fails on its own during the wind-down", func(d *driver) {
		d.running(issue("1", 1, ready))
		d.send(core.TimeUp{Limit: limit})
		d.settle(d.ended("1", "acceptance", failed("broke")))
	}, crew.OutcomeRouted, crew.FailedRoute, ""},
	{"crew's stop reaches it going through passed", func(d *driver) {
		ending := implemented(d, succeeded)
		d.send(core.StopRequested{})
		d.settle(ending)
	}, crew.OutcomeRouted, crew.PassedRoute, ""},
	{"crew's stop reaches it going through a failed it chose", func(d *driver) {
		ending := implemented(d, failed("broke"))
		d.send(core.StopRequested{})
		d.settle(ending)
	}, crew.OutcomeRouted, crew.FailedRoute, ""},
	{"its session judged its work failed", func(d *driver) {
		d.settle(implemented(d, failed("broke")))
	}, crew.OutcomeRouted, crew.FailedRoute, ""},
}

func TestTheReleaseEndsTheRunsSpan(t *testing.T) {
	for _, tc := range releases {
		t.Run(tc.name, func(t *testing.T) {
			d := startedDriverOf(t, draft())

			tc.play(d)

			wantHeld(t, d.m)
			wantStatistics(t, spansOf(d.statistics), d.opened("1"), d.closed("1", tc.outcome, tc.route, tc.halt))
		})
	}
}

// Covers AE11, R9: a run whose session still runs has an open span and no
// end.
func TestARunningRunHasAnOpenSpanOnly(t *testing.T) {
	d := startedDriverOf(t, draft())

	d.running(issue("1", 1, ready))

	wantStatistics(t, spansOf(d.statistics), d.opened("1"))
}

// Covers KTD6: a run of a rule in queue fast records queue fast.
func TestARunsSpanNamesItsRulesQueue(t *testing.T) {
	d := startedDriverOf(t, inQueues(brainstorming(), crew.Queue{Name: "fast", Slots: 1}))

	d.running(issue("315", 1, ideaReady))

	want := d.opened("315")
	want.Queue = crew.Some[crew.QueueName]("fast")
	wantStatistics(t, spansOf(d.statistics), want)
}

// firstRun returns the id of the first run past took.
func firstRun(t *testing.T, past []crew.RunEvent) crew.RuleRunID {
	t.Helper()
	for _, e := range past {
		if taken, ok := e.(crew.RunTaken); ok {
			return taken.Run
		}
	}
	t.Fatal("no take in the journal")
	return ""
}

// Covers KTD1, F3: a run that continues a run of the journal names it in
// its span.
func TestARunsSpanNamesTheRunItContinues(t *testing.T) {
	past := failedRun(t, "broke")
	d := startedDriverOf(t, crewRules(), past...)

	d.takeIssue(issue("9", 1, readyForDev))

	if got, ok := d.opened("9").Continues.Get(); !ok || got != firstRun(t, past) {
		t.Fatalf("continues %q, %v; want %q", got, ok, firstRun(t, past))
	}
	wantStatistics(t, spansOf(d.statistics), d.opened("9"))
}

// Covers KTD2: a run that only runs the passed route of the run it
// continues opens and ends its span like any run.
func TestARunOfThePassedRouteAloneOpensAndEndsItsSpan(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		moved := d.ended("9", "lfg", succeeded)
		d.send(core.CallResult{ID: moveID(t, moved, "9"), Result: core.ResultRefused, Reason: "nope"})
	})
	d := startedDriverOf(t, crewRules(), past...)

	d.settle(d.takeIssue(issue("9", 1, readyForDev)))

	if _, ok := takenOf(t, d, "9").Start.(crew.StartPassedRoute); !ok {
		t.Fatalf("start = %#v, want the passed route alone", takenOf(t, d, "9").Start)
	}
	wantHeld(t, d.m)
	wantStatistics(t, spansOf(d.statistics), d.opened("9"), d.closed("9", crew.OutcomeRouted, crew.PassedRoute, ""))
}

// Covers KTD2: the runs a journal replays at the start record no span.
func TestAReplayedJournalRecordsNoSpan(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		d.settle(d.ended("9", "lfg", succeeded))
	})

	d := startedDriverOf(t, crewRules(), past...)
	d.poll()

	wantStatistics(t, spansOf(d.statistics))
}

// Covers KTD3: a stop that reaches a rule without actions while its take
// is in flight does not choose its route, which is passed: its span's end
// names no halt.
func TestAStopThatChoosesNoRouteNamesNoHalt(t *testing.T) {
	d := startedDriverOf(t, promoted())

	take, _ := d.poll(issue("1", 1, triageDone))
	d.send(core.StopRequested{})
	d.settle(take)

	wantHeld(t, d.m)
	wantStatistics(t, spansOf(d.statistics), d.opened("1"), d.closed("1", crew.OutcomeRouted, crew.PassedRoute, ""))
}
