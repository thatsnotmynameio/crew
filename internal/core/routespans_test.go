package core_test

import (
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// workSpansOf returns the route and step spans among sts, and the rule run
// spans that end, in order: what a run's route records, and the end it
// comes before.
func workSpansOf(sts []crew.Statistic) []crew.Statistic {
	var out []crew.Statistic
	for _, st := range sts {
		switch st := st.(type) {
		case crew.RouteSpan, crew.StepSpan:
			out = append(out, st)
		case crew.RuleRunSpan:
			if _, ended := st.End.Get(); ended {
				out = append(out, st)
			}
		case crew.Process, crew.RepositoryRecord, crew.IssueSighting, crew.LabelMove, crew.ActionSpan:
		}
	}
	return out
}

// routeOpened is the span of the route issue key's last run chose at
// chosen, by d's process, chosen by the verdict of action, or by no action
// of the run when action is empty.
func (d *driver) routeOpened(key string, route crew.RouteName, chosen time.Time, action crew.ActionName,
	verdict crew.Verdict,
) crew.RouteSpan {
	d.t.Helper()
	h := d.runHead(key)
	sp := crew.RouteSpan{
		Issue: h.IssueID, Rule: h.Rule, Run: h.Run, Process: d.process(), Route: route, Start: chosen,
	}
	if action != "" {
		sp.ChosenBy = crew.Some(crew.RouteChoice{Action: action, Verdict: verdict})
	}
	return sp
}

// routeClosed is sp ended at d.now with the rule run's outcome.
func (d *driver) routeClosed(sp crew.RouteSpan, outcome crew.RunOutcome) crew.RouteSpan {
	sp.End = crew.Some(crew.RouteEnd{At: d.now, Outcome: outcome})
	return sp
}

// stepSpan is the span of the step at index of #1's last run's route,
// which runs name, started at start and ended at d.now with outcome, doing
// work.
func (d *driver) stepSpan(index int, name crew.ActionName, start time.Time, outcome crew.WorkOutcome,
	work crew.Work,
) crew.StepSpan {
	d.t.Helper()
	h := d.runHead("1")
	return crew.StepSpan{
		Issue: h.IssueID, Rule: h.Rule, Run: h.Run, Index: index, Name: name, Process: d.process(), Start: start,
		End: d.now, Outcome: outcome, Work: work,
	}
}

// exitedWith is the work of a shell step whose script exited with status.
func exitedWith(status int) crew.ShellWork { return crew.ShellWork{ExitStatus: crew.Some(status)} }

// checkPassedUse is the place of the config the function step check of
// implement's passed route is built for.
const checkPassedUse crew.FunctionUse = "rules.implement.routes.passed[1]"

// fullyRouted is the draft rules with implement's passed route running the
// shell step notify, then the function step check, before its move.
func fullyRouted(t *testing.T) []crew.Rule {
	t.Helper()
	rules := draft()
	rules[0].Routes[0].Steps = []crew.Step{
		crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}},
		crew.FunctionStep{Name: "check", Function: checkSpec(t, checkPassedUse)},
		crew.MoveStep{To: readyToReview},
	}
	return rules
}

// Covers AE5, R11, R13, KTD2, KTD4, KTD9: a session whose verdict leads to
// a route records the route's open under the action, with its verdict,
// then each shell and function step's span under the route as it ends, then
// the route's end, before the rule run's end.
func TestARoutesSpanHoldsItsStepsSpans(t *testing.T) {
	d := startedDriverOf(t, fullyRouted(t))
	notify := implemented(d, succeeded)
	chosen := d.now
	open := d.routeOpened("1", crew.PassedRoute, chosen, "development", crew.Passed)
	wantStatistics(t, workSpansOf(statisticsOf(notify)), open)

	check, _ := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: exited(0)})
	shell := d.stepSpan(0, "notify", chosen, crew.WorkSucceeded, exitedWith(0))
	wantStatistics(t, workSpansOf(statisticsOf(check)), shell)
	asked := d.now

	moved, _ := d.send(core.StepFunctionEnded{IssueID: issueID("1"), Step: 1, Outcome: returned(crew.Passed)})
	function := d.stepSpan(1, "check", asked, crew.WorkSucceeded,
		crew.FunctionWork{Function: "pr-open", Verdict: crew.Some(crew.Passed)})
	wantStatistics(t, workSpansOf(statisticsOf(moved)), function)

	cmds, _ := d.send(core.CallResult{ID: moveID(t, moved, "1"), Result: core.ResultDone})
	wantStatistics(t, workSpansOf(statisticsOf(cmds)), d.routeClosed(open, crew.OutcomeRouted),
		d.closed("1", crew.OutcomeRouted, crew.PassedRoute, ""))
	if open.Parent() != crew.SpanID(open.Run)+"/action/development" || shell.Parent() != open.ID() {
		t.Fatalf("parents: route %q, step %q; want the action's, then the route's", open.Parent(), shell.Parent())
	}
}

// Covers R13, KTD5: a shell step that exits 2 records failed and its exit
// status.
func TestAShellStepThatExitsTwoRecordsFailedAndItsStatus(t *testing.T) {
	d := startedDriverOf(t, shellRouted())
	d.running(issue("1", 1, ready))
	d.ended("1", "acceptance", failed("tests fail"))
	chosen := d.now

	cmds, _ := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: exited(2)})

	wantStatistics(t, workSpansOf(statisticsOf(cmds)), d.stepSpan(0, "notify", chosen, crew.WorkFailed, exitedWith(2)))
}

// Covers KTD3, KTD5: a shell step crew stopped while it ran records
// stopped, with no exit status, and the shell step the stop skipped after
// it records stopped_before_start, starting and ending at once.
func TestAStoppedShellStepAndTheStepItsStopSkippedRecordTheirHalt(t *testing.T) {
	d := startedDriverOf(t, shellRouted())
	d.running(issue("1", 1, ready))
	d.ended("1", "acceptance", failed("tests fail"))
	chosen := d.now
	d.send(core.StopRequested{})

	stopped := crew.ShellOutcome{Reason: crew.NewShellReason("crew stopped notify")}
	cmds, _ := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: stopped})

	wantStatistics(t, workSpansOf(statisticsOf(cmds)),
		d.stepSpan(0, "notify", chosen, crew.WorkStopped, crew.ShellWork{}),
		d.stepSpan(1, "cleanup", d.now, crew.WorkStoppedBeforeStart, crew.ShellWork{}))
}

// twiceNotified is the draft rules with implement's failed route running
// the shell action notify in two steps before its move.
func twiceNotified() []crew.Rule {
	rules := draft()
	notify := crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}}
	rules[0].Routes[1].Steps = []crew.Step{notify, notify, crew.MoveStep{To: needsAttention}}
	return rules
}

// Covers KTD3, KTD4: a route that runs the same shell action in two steps
// records two step spans, by index, each with its own start and status;
// an end the run refuses, as its step already settled, changes no span.
func TestTwoStepsOfOneShellActionRecordTwoSpansByIndex(t *testing.T) {
	d := startedDriverOf(t, twiceNotified())
	d.running(issue("1", 1, ready))
	d.ended("1", "acceptance", failed("tests fail"))
	chosen := d.now

	cmds, _ := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: exited(2)})
	wantStatistics(t, workSpansOf(statisticsOf(cmds)), d.stepSpan(0, "notify", chosen, crew.WorkFailed, exitedWith(2)))
	asked := d.now

	late, _ := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: exited(7)})
	wantStatistics(t, statisticsOf(late))

	cmds, _ = d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 1, Outcome: exited(0)})
	wantStatistics(t, workSpansOf(statisticsOf(cmds)), d.stepSpan(1, "notify", asked, crew.WorkSucceeded, exitedWith(0)))
}

// Covers KTD3, KTD5: a function step whose text parameters do not render
// for the issue records failed, with no verdict, starting at its end.
func TestAFunctionStepThatDoesNotRenderRecordsFailedAtItsEnd(t *testing.T) {
	rules := functionRouted(t)
	bad, err := crew.ParseParameterTemplate("title", "{{slice .Issue.Title 0 10}}")
	if err != nil {
		t.Fatal(err)
	}
	step, _ := rules[0].Routes[1].Steps[0].(crew.FunctionStep)
	step.Function.Texts = []crew.TextParameter{{Name: "title", Template: bad}}
	rules[0].Routes[1].Steps[0] = step
	d := startedDriverOf(t, rules)
	d.running(issue("1", 1, ready))

	cmds := d.ended("1", "acceptance", failed("tests fail"))

	wantStatistics(t, workSpansOf(statisticsOf(cmds)),
		d.routeOpened("1", crew.FailedRoute, d.now, "acceptance", crew.Failed),
		d.stepSpan(0, "check", d.now, crew.WorkFailed, crew.FunctionWork{Function: "pr-open"}))
}

// Covers R11, KTD2: a route of tracker steps alone records its open and
// its end, and no step span.
func TestARouteOfTrackerStepsRecordsNoStepSpan(t *testing.T) {
	d := startedDriverOf(t, draft())
	ending := implemented(d, failed("broke"))
	open := d.routeOpened("1", crew.FailedRoute, d.now, "development", crew.Failed)

	d.settle(ending)

	wantStatistics(t, workSpansOf(d.statistics), open, d.routeClosed(open, crew.OutcomeRouted),
		d.closed("1", crew.OutcomeRouted, crew.FailedRoute, ""))
}

// Covers KTD5: a route whose final move is dropped ends with
// route_dropped, and one whose final move is given up with route_given_up.
func TestARoutesEndRecordsTheRunsOutcome(t *testing.T) {
	for _, tc := range []struct {
		result  core.Result
		outcome crew.RunOutcome
	}{
		{core.ResultMovedMeanwhile, crew.OutcomeRouteDropped},
		{core.ResultRefused, crew.OutcomeRouteGivenUp},
	} {
		t.Run(string(tc.outcome), func(t *testing.T) {
			d := startedDriverOf(t, draft())
			ending := implemented(d, succeeded)
			open := d.routeOpened("1", crew.PassedRoute, d.now, "development", crew.Passed)

			d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: tc.result, Reason: "nope"})

			wantStatistics(t, workSpansOf(d.statistics), open, d.routeClosed(open, tc.outcome),
				d.closed("1", tc.outcome, crew.PassedRoute, ""))
		})
	}
}

// Covers KTD9: a rule without actions records its route under the rule
// run, with no verdict.
func TestTheRouteOfARuleWithoutActionsHangsFromTheRun(t *testing.T) {
	d := startedDriverOf(t, promoted())
	take, _ := d.poll(issue("1", 1, triageDone))

	moved, _ := d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})
	open := d.routeOpened("1", crew.PassedRoute, d.now, "", "")
	d.settle(moved)

	wantStatistics(t, workSpansOf(d.statistics), open, d.routeClosed(open, crew.OutcomeRouted),
		d.closed("1", crew.OutcomeRouted, crew.PassedRoute, ""))
	if open.Parent() != crew.SpanID(open.Run) {
		t.Fatalf("parent %q, want the run's %q", open.Parent(), open.Run)
	}
}

// Covers KTD9: a run of its continued run's passed route alone records its
// route under the rule run, with no verdict: the action that chose it ran
// in the earlier run.
func TestThePassedRouteAloneHangsFromTheRun(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		moved := d.ended("9", "lfg", succeeded)
		d.send(core.CallResult{ID: moveID(t, moved, "9"), Result: core.ResultRefused, Reason: "nope"})
	})
	d := startedDriverOf(t, crewRules(), past...)
	take, _ := d.poll(issue("9", 1, readyForDev))

	moved, _ := d.send(core.CallResult{ID: moveID(t, take, "9"), Result: core.ResultDone})
	open := d.routeOpened("9", crew.PassedRoute, d.now, "", "")
	d.settle(moved)

	wantStatistics(t, workSpansOf(d.statistics), open, d.routeClosed(open, crew.OutcomeRouted),
		d.closed("9", crew.OutcomeRouted, crew.PassedRoute, ""))
}

// Covers KTD2: a run whose take does not land records no route span.
func TestARunNotTakenRecordsNoRouteSpan(t *testing.T) {
	d := startedDriverOf(t, draft())
	take, _ := d.poll(issue("1", 1, ready))

	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultRefused, Reason: "nope"})

	wantStatistics(t, workSpansOf(d.statistics), d.closed("1", crew.OutcomeNotTaken, "", ""))
}

// Covers KTD3: a run that waits for its pull request lookup before its
// first step opens its route when it chose it, and starts its first step
// when it asked for it.
func TestARouteThatWaitsForItsLookupStartsItsFirstStepWhenAsked(t *testing.T) {
	d := &driver{t: t, m: core.New(fullyRouted(t), 2, core.FindingPullRequests(),
		core.RecordingStatistics("v0.1.1", "/repo", tracker)), now: t0}
	d.send(core.Started{Repository: crew.Repository{ID: "R_1", Name: "owner/name"}})
	implemented(d, succeeded)
	chosen := d.now

	d.send(core.PullRequestFound{IssueID: issueID("1")})
	asked := d.now
	cmds, _ := d.send(core.StepShellEnded{IssueID: issueID("1"), Step: 0, Outcome: exited(0)})

	shell := d.stepSpan(0, "notify", asked, crew.WorkSucceeded, exitedWith(0))
	wantStatistics(t, workSpansOf(statisticsOf(cmds)), shell)
	open := d.routeOpened("1", crew.PassedRoute, chosen, "development", crew.Passed)
	wantStatistics(t, workSpansOf(d.statistics), open, shell)
}

// Covers KTD2: the runs a journal replays at the start record no route or
// step span.
func TestAReplayedJournalRecordsNoRouteOrStepSpan(t *testing.T) {
	rules := crewRules()
	notify := crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}}
	rules[0].Routes[1].Steps = append([]crew.Step{notify}, rules[0].Routes[1].Steps...)
	past := journaled(t, rules, nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		d.ended("9", "lfg", failed("broke"))
		cmds, _ := d.send(core.StepShellEnded{IssueID: issueID("9"), Step: 0, Outcome: exited(0)})
		d.settle(cmds)
		wantHeld(t, d.m)
	})

	d := startedDriverOf(t, rules, past...)
	d.poll()

	wantStatistics(t, workSpansOf(d.statistics))
}
