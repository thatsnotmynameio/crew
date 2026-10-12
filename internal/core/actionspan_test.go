package core_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// actionSpansOf returns the action spans among sts, in order.
func actionSpansOf(sts []crew.Statistic) []crew.Statistic {
	var out []crew.Statistic
	for _, st := range sts {
		if sp, ok := st.(crew.ActionSpan); ok {
			out = append(out, sp)
		}
	}
	return out
}

// actionSpan is the span of action in issue key's last run, by d's
// process, from start to d.now, ended with outcome and verdict, having
// done work.
func (d *driver) actionSpan(key string, action crew.ActionName, start time.Time, outcome crew.WorkOutcome,
	verdict crew.Verdict, work crew.Work,
) crew.ActionSpan {
	d.t.Helper()
	taken := takenOf(d.t, d, key)
	return crew.ActionSpan{
		Issue: taken.IssueID, Rule: taken.Rule, Run: taken.Run, Action: action, Process: d.process(),
		Start: start, End: d.now, Outcome: outcome, Verdict: verdict, Work: work,
	}
}

// asked takes it on d, lands its take, readies its workspace and settles
// what follows, starting the session it asks for, and returns when crew
// asked for the run's first action: when its workspace was ready.
func (d *driver) asked(it crew.Issue) time.Time {
	d.t.Helper()
	d.takeIssue(it)
	cmds := d.ready(it.ID().Key)
	at := d.now
	d.settle(cmds)
	return at
}

// templateHash is the hash of a prompt template's text, as the store keeps
// it: hex SHA-256.
func templateHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// The prompts of the draft's sessions.
const (
	acceptanceTemplate  = "Implement test acceptance for issue {{.Issue.Ref}}"
	developmentTemplate = "Implement development for issue {{.Issue.Ref}}"
)

// claudeDraft is the draft rules whose acceptance session runs on the
// agent claude, through the harness claude, acting as developer.
func claudeDraft() []crew.Rule {
	return withSpec(draft(), 0, "acceptance", func(s *crew.SessionSpec) {
		s.Agent = crew.Agent{Name: "claude", Harness: "claude"}
		s.Bot = crew.Bot{Name: "developer"}
	})
}

// acceptanceWork is the work of a session of the draft's acceptance on claude
// as developer, started at started unless it is zero, with usage.
func acceptanceWork(started time.Time, usage crew.Usage) crew.SessionWork {
	w := crew.SessionWork{
		Agent: "claude", Harness: "claude", Bot: "developer", PromptHash: templateHash(acceptanceTemplate),
		Turns: usage.Turns, Tokens: usage.ByModel,
	}
	if !started.IsZero() {
		w.Started = crew.Some(started)
	}
	return w
}

// developmentWork is the work of the draft's development session, which
// never started.
var developmentWork = crew.SessionWork{PromptHash: templateHash(developmentTemplate)}

// twoModels is a session's usage through Opus and, by its subagents,
// Sonnet, in 12 turns.
var twoModels = crew.Usage{
	Tokens: crew.Some(crew.Tokens{Input: 110, Output: 220, CacheRead: 3300, CacheWrite: 440}),
	Turns:  crew.Some(12),
	Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
	ByModel: []crew.ModelTokens{
		{Model: "claude-opus-5-5", Tokens: crew.Tokens{Input: 100, Output: 200, CacheRead: 3000, CacheWrite: 400}},
		{Model: "claude-sonnet-5-5", Tokens: crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}},
	},
}

// Covers AE4, R10, R12, KTD7: a session that ends with passed and usage by
// two models records, once the action ends, its span with its agent,
// harness, bot, prompt hash, turns and one token record per model, from
// when crew asked for it to the session's end, under its run.
func TestAnEndedSessionRecordsItsSpanWithItsUsageByModel(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	asked := d.asked(issue("243", 1, ready))
	started := asked.Add(time.Second)

	d.send(core.SessionEnded{IssueID: issueID("243"), Action: "acceptance", Outcome: succeeded, Usage: twoModels})

	want := d.actionSpan("243", "acceptance", asked, crew.WorkSucceeded, crew.Passed, acceptanceWork(started, twoModels))
	wantStatistics(t, actionSpansOf(d.statistics), want)
	if want.Parent() != crew.SpanID(want.Run) {
		t.Fatalf("parent %q, want the run's span %q", want.Parent(), want.Run)
	}
}

// Covers R10, KTD7: a Codex session, which reports a token sum and no
// model, records one token record with no model's name.
func TestASessionReportingATokenSumRecordsOneRecordWithNoModel(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	asked := d.asked(issue("1", 1, ready))
	sum := crew.Usage{Tokens: crew.Some(crew.Tokens{Input: 1, Output: 2}), Turns: crew.Some(3)}

	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded, Usage: sum})

	work := acceptanceWork(asked.Add(time.Second), sum)
	work.Tokens = []crew.ModelTokens{{Tokens: crew.Tokens{Input: 1, Output: 2}}}
	wantStatistics(t, actionSpansOf(d.statistics), d.actionSpan("1", "acceptance", asked, crew.WorkSucceeded,
		crew.Passed, work))
}

// Covers AE6, R14, KTD5: a session crew stopped while it ran, whose
// harness reported nothing, records outcome stopped, its session's start,
// no tokens and no turns; the next action, which the stop kept from
// starting, records stopped_before_start at its end.
func TestAStoppedSessionThatReportedNothingRecordsNoTokens(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	asked := d.asked(issue("1", 1, ready))
	d.send(core.StopRequested{})

	d.ended("1", "acceptance", failed("stopped"))

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "acceptance", asked, crew.WorkStopped, crew.Failed,
			acceptanceWork(asked.Add(time.Second), crew.Usage{})))
}

// Covers KTD5, KTD7: a session that ends on its own once crew's stop
// reached the run records stopped, and keeps the usage its harness
// reported.
func TestAStoppedSessionRecordsTheUsageItReported(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	asked := d.asked(issue("1", 1, ready))
	d.send(core.StopRequested{})

	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded, Usage: twoModels})

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "acceptance", asked, crew.WorkStopped, crew.Failed,
			acceptanceWork(asked.Add(time.Second), twoModels)))
}

// Covers KTD3, KTD5: a session that ends on its own once the run time is
// up keeps its own outcome; the next action, which the time limit kept
// from starting in the same decision, records run_time_limit, starting at
// its own end.
func TestTheRunTimeLimitEndsTheNextActionUnaskedAtItsEnd(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	asked := d.asked(issue("1", 1, ready))
	d.send(core.TimeUp{Limit: limit})

	d.ended("1", "acceptance", succeeded)

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "acceptance", asked, crew.WorkSucceeded, crew.Passed,
			acceptanceWork(asked.Add(time.Second), crew.Usage{})),
		d.actionSpan("1", "development", d.now, crew.WorkRunTimeLimit, crew.Failed, developmentWork))
}

// Covers KTD5: a stop that reaches a run while its take is in flight
// keeps its first action from starting: it records stopped_before_start,
// starting and ending when the take landed.
func TestAnActionAStopKeptFromStartingRecordsStoppedBeforeStart(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	take, _ := d.poll(issue("1", 1, ready))
	d.send(core.StopRequested{})

	d.send(core.CallResult{ID: moveID(t, take, "1"), Result: core.ResultDone})

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "acceptance", d.now, crew.WorkStoppedBeforeStart, crew.Failed,
			acceptanceWork(time.Time{}, crew.Usage{})))
}

// Covers KTD3, KTD7: a session that could not start records failed, no
// session start and no tokens, from when crew asked for it.
func TestASessionThatCouldNotStartRecordsFailed(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	d.takeIssue(issue("1", 1, ready))
	d.ready("1")
	asked := d.now

	d.send(core.SessionFailedToStart{
		IssueID: issueID("1"), Action: "acceptance", Reason: crew.NewSessionText("no claude"),
	})

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "acceptance", asked, crew.WorkFailed, crew.Failed, acceptanceWork(time.Time{}, crew.Usage{})))
}

// Covers KTD3: the action at the cursor when the run's workspace failed
// records failed, starting at its end.
func TestTheActionAtTheCursorOfAFailedWorkspaceStartsAtItsEnd(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	d.takeIssue(issue("1", 1, ready))

	d.send(core.WorkspaceFailed{IssueID: issueID("1"), Reason: crew.NewSessionText("disk full")})

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "acceptance", d.now, crew.WorkFailed, crew.Failed, acceptanceWork(time.Time{}, crew.Usage{})))
}

// Covers R10: a session whose verdict sends the run on to the next action
// records that verdict.
func TestASessionsSpanRecordsTheVerdictThatLedOn(t *testing.T) {
	rules := claudeDraft()
	rules[0].Actions[0].On = crew.On{"ready": crew.Next{}}
	d := startedDriverOf(t, rules)
	asked := d.asked(issue("1", 1, ready))

	d.send(core.SessionEnded{
		IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded, Report: crew.VerdictReported{Verdict: "ready"},
	})

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "acceptance", asked, crew.WorkSucceeded, "ready",
			acceptanceWork(asked.Add(time.Second), crew.Usage{})))
}

// Covers KTD2, KTD7: two actions of one run record two spans with ids of
// their own and one parent, the run's span.
func TestTwoActionsOfARunRecordTwoSpansUnderIt(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())
	d.asked(issue("1", 1, ready))
	d.ended("1", "acceptance", succeeded)
	d.send(core.SessionStarted{IssueID: issueID("1"), Action: "development"})

	d.ended("1", "development", succeeded)

	spans := actionSpansOf(d.statistics)
	if len(spans) != 2 {
		t.Fatalf("action spans %#v, want acceptance's and development's", spans)
	}
	first, _ := spans[0].(crew.ActionSpan)
	second, _ := spans[1].(crew.ActionSpan)
	run := crew.SpanID(d.run(issueID("1")))
	if first.ID() == second.ID() || first.Parent() != run || second.Parent() != run {
		t.Fatalf("spans %q under %q and %q under %q, want two ids under %q",
			first.ID(), first.Parent(), second.ID(), second.Parent(), run)
	}
}

// Covers KTD3: a session whose prompt does not render records failed,
// starting at its end, not at the start of the action before it, which
// ended in the same decision.
func TestASessionWhosePromptDoesNotRenderStartsAtItsEnd(t *testing.T) {
	rules := draft()
	rules[0].Actions = []crew.Action{
		shellAction("install", "make deps"), sessionAction("lfg", "Fix {{index .Issue.Title 11}}"),
	}
	d := startedDriverOf(t, rules)
	asked := d.asked(issue("1", 1, ready))

	d.send(core.ShellEnded{IssueID: issueID("1"), Action: "install", Outcome: exited(0)})

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "install", asked, crew.WorkSucceeded, crew.Passed, crew.ShellWork{ExitStatus: crew.Some(0)}),
		d.actionSpan("1", "lfg", d.now, crew.WorkFailed, crew.Failed,
			crew.SessionWork{PromptHash: templateHash("Fix {{index .Issue.Title 11}}")}))
}

// Covers AE7, R13: a shell action that exits 1 records failed, its exit
// status, from when crew asked for it to its end, and no tokens.
func TestAShellActionRecordsItsExitStatus(t *testing.T) {
	rules := draft()
	rules[0].Actions = []crew.Action{shellAction("lint", "make lint")}
	d := startedDriverOf(t, rules)
	asked := d.asked(issue("1", 1, ready))

	d.send(core.ShellEnded{IssueID: issueID("1"), Action: "lint", Outcome: exited(1)})

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "lint", asked, crew.WorkFailed, crew.Failed, crew.ShellWork{ExitStatus: crew.Some(1)}))
}

// Covers R10, KTD7: a function action that returns a verdict it declares
// records the function's registered name and that verdict, from when crew
// asked for it: when the take landed, as a rule without sessions needs no
// workspace.
func TestAFunctionActionRecordsItsFunctionAndVerdict(t *testing.T) {
	rules := draft()
	spec := checkSpec(t, checkUse)
	spec.Verdicts = []crew.Verdict{"open"}
	rules[0].Actions = []crew.Action{{
		Name: "check", Kind: spec, On: crew.On{"open": crew.ToRoute{Route: crew.PassedRoute}},
	}}
	d := startedDriverOf(t, rules)
	d.takeIssue(issue("1", 1, ready))
	asked := d.now

	d.send(core.FunctionEnded{IssueID: issueID("1"), Action: "check", Outcome: returned("open")})

	wantStatistics(t, actionSpansOf(d.statistics), d.actionSpan("1", "check", asked, crew.WorkSucceeded, "open",
		crew.FunctionWork{Function: "pr-open", Verdict: crew.Some[crew.Verdict]("open")}))
}

// Covers R10, KTD7: a rule's question action records kind question,
// succeeded, starting and ending at once, when the take landed.
func TestAQuestionActionRecordsAnInstantSpan(t *testing.T) {
	text, err := crew.ParseCommentTemplate("ask", "Does {{.Issue.Ref}} block #281?")
	if err != nil {
		t.Fatal(err)
	}
	rules := draft()
	rules[0].Actions = []crew.Action{{
		Name: "ask", Kind: crew.QuestionSpec{Question: crew.Ask{ID: "unsure", Text: text, Return: ready}},
		On: crew.On{crew.Asked: crew.ToRoute{Route: crew.PassedRoute}},
	}}
	d := startedDriverOf(t, rules)

	d.takeIssue(issue("1", 1, ready))

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "ask", d.now, crew.WorkSucceeded, crew.Asked, crew.QuestionWork{}))
}

// Covers R10, KTD7: the answered rule's check records kind check, from
// when crew asked for the read of the comments to the read.
func TestTheAnsweredRulesCheckRecordsItsSpan(t *testing.T) {
	rules := append(append(delegating(), answering()), depsAsking(t)...)
	m := core.New(rules, 2, core.Journaling(nil), core.WithBots(developerBots()),
		core.WithAnswerers(crewAnswerers()), core.Delegating("octocat"), core.RecordingStatistics("v0.1.1", "/repo", tracker))
	d := &driver{t: t, m: m, now: t0}
	d.send(core.Started{Repository: crew.Repository{ID: "R_1", Name: "owner/name"}})
	d.checking()
	asked := d.now

	d.send(returnRead(unsureQuestion("crew-clerk[bot]"), crew.Comment{Author: "alice", Body: "yes"}))

	wantStatistics(t, actionSpansOf(d.statistics),
		d.actionSpan("1", "answer", asked, crew.WorkSucceeded, crew.Passed, crew.CheckWork{}))
}

// Covers AE11, R10: a session that still runs records no action span.
func TestARunningSessionRecordsNoActionSpan(t *testing.T) {
	d := startedDriverOf(t, claudeDraft())

	d.asked(issue("1", 1, ready))

	wantStatistics(t, actionSpansOf(d.statistics))
}

// Covers AE10, KTD10: the actions that ended before a restart point to the
// first process, under the first run's span; after it, a second process
// takes a new run, whose span names the first, and its actions point to
// the second process, under the new run's span.
func TestActionsPointToTheProcessThatRanThem(t *testing.T) {
	first := &driver{t: t, now: t0, m: core.New(claudeDraft(), 2, core.Journaling(nil),
		core.RecordingStatistics("v0.1.1", "/repo", tracker))}
	first.send(core.Started{Repository: crew.Repository{ID: "R_1", Name: "owner/name"}})
	first.asked(issue("1", 1, ready))
	first.ended("1", "acceptance", succeeded)
	before := actionSpansOf(first.statistics)
	if len(before) != 1 {
		t.Fatalf("action spans before the restart %#v, want acceptance's", before)
	}
	acceptance, _ := before[0].(crew.ActionSpan)
	if acceptance.Process != first.process() || acceptance.Run != first.run(issueID("1")) {
		t.Fatalf("acceptance %#v, want it by %q under %q", acceptance, first.process(), first.run(issueID("1")))
	}

	second := startedDriverOf(t, claudeDraft(), first.recorded...)
	second.asked(issue("1", 1, ready))
	second.ended("1", "acceptance", succeeded)

	if second.process() == first.process() {
		t.Fatalf("both processes are %q", first.process())
	}
	if continues, _ := second.opened("1").Continues.Get(); continues != first.run(issueID("1")) {
		t.Fatalf("the new run continues %q, want %q", continues, first.run(issueID("1")))
	}
	after := actionSpansOf(second.statistics)
	if len(after) == 0 {
		t.Fatal("no action span after the restart")
	}
	for _, st := range after {
		sp, _ := st.(crew.ActionSpan)
		if sp.Process != second.process() || sp.Run != second.run(issueID("1")) {
			t.Fatalf("span %#v, want it by %q under %q", sp, second.process(), second.run(issueID("1")))
		}
	}
}

// Covers KTD2: the actions of the runs a journal replays at the start
// record no span.
func TestAReplayedJournalRecordsNoActionSpan(t *testing.T) {
	past := journaled(t, crewRules(), nil, func(d *driver) {
		d.running(issue("9", 1, readyForDev))
		d.settle(d.ended("9", "lfg", succeeded))
	})

	d := startedDriverOf(t, crewRules(), past...)
	d.poll()

	wantStatistics(t, actionSpansOf(d.statistics))
}
