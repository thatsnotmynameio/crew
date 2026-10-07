package lines_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/ui/lines"
)

// source is a closed, prefilled subscription with a fixed drop count.
type source struct {
	ch      chan engine.Update
	dropped int
}

func newSource(dropped int, updates ...engine.Update) *source {
	ch := make(chan engine.Update, len(updates))
	for _, u := range updates {
		ch <- u
	}
	close(ch)
	return &source{ch: ch, dropped: dropped}
}

func (s *source) Updates() <-chan engine.Update { return s.ch }
func (s *source) Dropped() int                  { return s.dropped }

// zone is a fixed location three hours west of UTC, so a test proves the
// stamps are in the given location, not in UTC or the machine's zone.
var zone = time.FixedZone("test", -3*60*60)

func at(hms string) time.Time {
	t, err := time.ParseInLocation("15:04:05", hms, zone)
	if err != nil {
		panic(err)
	}
	return time.Date(2026, 10, 1, t.Hour(), t.Minute(), t.Second(), 0, zone).UTC()
}

func render(t *testing.T, src lines.Source, now time.Time) []string {
	t.Helper()
	var out strings.Builder
	if err := lines.Run(src, &out, zone, func() time.Time { return now }); err != nil {
		t.Fatalf("Run: %v", err)
	}
	text := strings.TrimSuffix(out.String(), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func equalLines(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("output:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestATakenStartedEndedRoutedSequencePrintsStampedLinesInOrder(t *testing.T) {
	issue := crew.IssueData{ID: issueID("1"), Ref: "#1", Title: "Add login form"}
	head := func(hms string) crew.EventHead {
		return crew.EventHead{At: at(hms), IssueID: issueID("1"), IssueRef: "#1", Rule: "implement"}
	}
	src := newSource(0,
		engine.Update{Events: []core.Published{
			crew.RunTaken{EventHead: head("09:00:01"), Issue: issue, From: "ready", To: "in progress"},
		}},
		engine.Update{Events: []core.Published{
			crew.WorkspaceOpened{EventHead: head("09:00:02"),
				Workspace: crew.Workspace{Name: "issue-1-implement", Branch: "crew/issue-1-implement"},
				Log:       ".crew/logs/issue-1-implement.log"},
			crew.ActionSessionStarted{EventHead: head("09:00:02"), Action: "code"},
		}},
		engine.Update{Events: []core.Published{
			crew.ActionEnded{EventHead: head("09:12:30"), Action: "code", Verdict: crew.Passed,
				End: crew.EndSucceeded{Reason: crew.NewSessionText("Opened pull request #7")}},
			crew.RouteChosen{EventHead: head("09:12:30"), Route: crew.PassedRoute, Action: "code"},
			core.RouteStepEnded{At: at("09:12:31"), IssueID: issueID("1"), IssueRef: "#1", Rule: "implement",
				Route: crew.PassedRoute, Plan: crew.StepPlan{Kind: crew.StepMove, To: "ready to review"},
				From: "in progress", Outcome: crew.StepLanded{}},
		}},
	)

	got := render(t, src, at("09:13:00"))

	equalLines(t, got, []string{
		`09:00:01 crew: implement took #1 "Add login form" (ready -> in progress)`,
		`09:00:02 crew: #1 implement works in worktree issue-1-implement on branch crew/issue-1-implement, ` +
			`log .crew/logs/issue-1-implement.log`,
		`09:00:02 crew: #1 implement/code started its session`,
		`09:12:30 crew: #1 implement/code passed: Opened pull request #7`,
		`09:12:30 crew: #1 implement ends through passed`,
		`09:12:31 crew: #1 moved from in progress to ready to review`,
	})
}

// The tracker calls the sentences below describe, all of issue #2.
var (
	two     = issueID("2")
	move    = core.Call{Kind: core.CallMove, IssueID: two, IssueRef: "#2", From: "in review", To: "needs attention"}
	report  = core.Call{Kind: core.CallReport, IssueID: two, IssueRef: "#2"}
	prs     = core.Call{Kind: core.CallPullRequests, IssueID: two, IssueRef: "#2", To: "needs attention"}
	closed  = core.Call{Kind: core.CallPullRequests, IssueID: two, IssueRef: "#2"}
	comment = core.Call{Kind: core.CallComment, IssueID: two, IssueRef: "#2"}
	closing = core.Call{Kind: core.CallClose, IssueID: two, IssueRef: "#2", From: "in review"}
)

// The run events below name these heads and workspace.
var (
	lfgHead     = crew.EventHead{At: at("10:00:00"), IssueID: issueID("9"), IssueRef: "#9", Rule: "development"}
	runWorktree = crew.Workspace{Name: "issue-9-development", Branch: "crew/issue-9-development"}
)

// twoHead is the head of an event of rule's run on #2.
func twoHead(rule crew.RuleName) crew.EventHead {
	return crew.EventHead{At: at("10:00:00"), IssueID: two, IssueRef: "#2", Rule: rule}
}

// stepEnded is #9's development route blocked's step plan, which settled as
// outcome.
func stepEnded(plan crew.StepPlan, outcome crew.StepOutcome) core.RouteStepEnded {
	return core.RouteStepEnded{
		At: at("10:00:00"), IssueID: issueID("9"), IssueRef: "#9", Rule: "development", Route: "blocked",
		Plan: plan, From: "in progress", Outcome: outcome,
	}
}

// notify is a route's shell step.
var notify = crew.StepPlan{Kind: crew.StepShell, Shell: "notify"}

// sentences pairs each kind of event with the sentence Text gives it.
var sentences = []struct {
	event core.Published
	want  string
}{
	{crew.WorkspaceOpened{EventHead: lfgHead, Workspace: runWorktree, Log: ".crew/logs/issue-9-development.log"},
		"#9 development works in worktree issue-9-development on branch crew/issue-9-development, " +
			"log .crew/logs/issue-9-development.log"},
	{crew.WorkspaceOpened{EventHead: lfgHead, Workspace: runWorktree, Log: ".crew/logs/issue-9-development.log",
		Resumed: true},
		"#9 development resumed in worktree issue-9-development on branch crew/issue-9-development, " +
			"log .crew/logs/issue-9-development.log"},
	{crew.WorkspaceOpened{EventHead: lfgHead, Workspace: runWorktree},
		"#9 development works in worktree issue-9-development on branch crew/issue-9-development"},
	{crew.WorkspaceMissing{EventHead: lfgHead, Workspace: runWorktree},
		"#9 development: worktree issue-9-development is gone, so it does not resume there"},
	{crew.ActionSessionStarted{EventHead: lfgHead, Action: "lfg"}, "#9 development/lfg started its session"},
	{crew.ActionShellAsked{EventHead: lfgHead, Action: "judge"}, "#9 development/judge started its script"},
	{core.RunNotRecorded{At: at("10:00:00"), IssueID: issueID("9"), IssueRef: "#9", Rule: "development", Action: "lfg",
		What: "the start of lfg", Reason: "disk full"},
		"#9 development: could not record the start of lfg, so a restart may not resume it: disk full"},
	{crew.ActionEnded{EventHead: twoHead("review"), Action: "check", Verdict: crew.Failed,
		End: crew.EndFailed{Reason: crew.NewSessionText("session exited with status 1"), Cause: crew.CauseSession}},
		"#2 review/check failed: session exited with status 1"},
	{crew.ActionEnded{EventHead: twoHead("review"), Action: "check", Verdict: crew.Passed, End: crew.EndSucceeded{}},
		"#2 review/check passed"},
	{crew.ActionEnded{EventHead: lfgHead, Action: "lfg", Verdict: "blocked",
		End: crew.EndSucceeded{Reason: crew.NewSessionText("waiting on #12")}},
		"#9 development/lfg ended with blocked: waiting on #12"},
	{crew.ActionEnded{EventHead: twoHead("implement"), Action: "judge", Verdict: crew.Failed,
		End: crew.EndFailed{
			Reason: crew.NewSessionText("the shell action judge exited with status 1: no open pull request"),
			Cause:  crew.CauseShell,
		}},
		"#2 implement/judge failed: the shell action judge exited with status 1: no open pull request"},
	{crew.TakeMoved{EventHead: twoHead("review"), From: "ready to review", To: "in review"},
		"#2 moved from ready to review to in review"},
	{crew.RouteChosen{EventHead: lfgHead, Route: "blocked", Action: "lfg"}, "#9 development ends through blocked"},
	{stepEnded(crew.StepPlan{Kind: crew.StepMove, To: "blocked"}, crew.StepLanded{}),
		"#9 moved from in progress to blocked"},
	{stepEnded(crew.StepPlan{Kind: crew.StepClose}, crew.StepLanded{}), "closed #9"},
	{stepEnded(crew.StepPlan{Kind: crew.StepComment}, crew.StepLanded{}), "commented on #9"},
	{stepEnded(crew.StepPlan{Kind: crew.StepReport}, crew.StepLanded{}), "posted the report on #9"},
	{stepEnded(notify, crew.StepRan{Reason: crew.NewShellReason("the route's shell step notify exited with status 0")}),
		"#9 development through blocked: the shell step notify ran: the route's shell step notify exited with status 0"},
	{stepEnded(notify, crew.StepFailed{Reason: crew.NewShellReason("the route's shell step notify exited with status 1")}),
		"#9 development through blocked: the shell step notify failed: " +
			"the route's shell step notify exited with status 1"},
	{stepEnded(crew.StepPlan{Kind: crew.StepComment}, crew.StepFailed{Reason: crew.NewShellReason("no .Foo")}),
		"#9 development through blocked: the comment failed: no .Foo"},
	{stepEnded(notify, crew.StepStopped{Reason: crew.NewShellReason("the route's shell step notify was stopped")}),
		"#9 development through blocked: crew stopped the shell step notify"},
	{stepEnded(notify, crew.StepSkipped{}),
		"#9 development through blocked: crew skipped the shell step notify, as it was stopping"},
	{core.IssueSkipped{At: at("10:00:00"), IssueRef: "#3", States: []crew.State{"ready", "in progress"}},
		"skipped #3: it carries 2 crew labels (ready, in progress)"},
	{core.IssueOfOtherKind{At: at("10:00:00"), IssueID: issueID("90"), IssueRef: "#90", Kind: crew.KindPullRequest,
		Label: "crew:development:ready", Rule: "development", Takes: crew.KindIssue},
		"left #90 alone: it is a pull request, and crew:development:ready is the label of development, which takes issues"},
	{core.IssueOfOtherKind{At: at("10:00:00"), IssueID: issueID("42"), IssueRef: "#42", Kind: crew.KindIssue,
		Label: "fix review ready", Rule: "fix review", Takes: crew.KindPullRequest},
		"left #42 alone: it is an issue, and fix review ready is the label of fix review, which takes pull requests"},
	{core.PollDone{At: at("10:00:00"), Listed: 3, Taken: 1},
		"poll: listed 3 issues, took 1"},
	{core.PollDone{At: at("10:00:00"), Listed: 1, Taken: 0},
		"poll: listed 1 issue, took 0"},
	{core.PollSkipped{At: at("10:00:00"), Busy: 2, Slots: 2},
		"poll: skipped, 2 of 2 slots busy"},
	{core.PollSkipped{At: at("10:00:00"), Busy: 1, Slots: 1},
		"poll: skipped, 1 of 1 slot busy"},
	{core.ListingFailed{At: at("10:00:00"), Reason: "gh: rate limited"},
		"listing issues failed: gh: rate limited"},
	{core.CallOwed{At: at("10:00:00"), Call: move, Reason: "timeout"},
		"moving #2 from in review to needs attention failed, retrying at the next tick: timeout"},
	{core.CallOwed{At: at("10:00:00"), Call: report, Reason: "timeout"},
		"posting the report on #2 failed, retrying at the next tick: timeout"},
	{core.CallOwed{At: at("10:00:00"), Call: comment, Reason: "timeout"},
		"commenting on #2 failed, retrying at the next tick: timeout"},
	{core.CallOwed{At: at("10:00:00"), Call: closing, Reason: "timeout"},
		"closing #2 failed, retrying at the next tick: timeout"},
	{core.CallOwed{At: at("10:00:00"), Call: prs, Reason: "gh: HTTP 502"},
		"updating the pull requests of #2 to needs attention failed, retrying at the next tick: gh: HTTP 502"},
	{core.CallOwed{At: at("10:00:00"), Call: closed, Reason: "gh: HTTP 502"},
		"telling the pull requests of #2 it was closed failed, retrying at the next tick: gh: HTTP 502"},
	{core.CallDropped{At: at("10:00:00"), Call: prs, Result: core.ResultRefused, Reason: "pull request is locked"},
		"gave up updating the pull requests of #2 to needs attention: the tracker refused: pull request is locked"},
	{core.CallDropped{At: at("10:00:00"), Call: move, Result: core.ResultMovedMeanwhile},
		"gave up moving #2 from in review to needs attention: the issue moved meanwhile"},
	{core.CallDropped{At: at("10:00:00"), Call: report, Result: core.ResultRefused, Reason: "issue is locked"},
		"gave up posting the report on #2: the tracker refused: issue is locked"},
	{core.CallDropped{At: at("10:00:00"), Call: move, Result: core.ResultFailed, Reason: "timeout"},
		"gave up moving #2 from in review to needs attention: it failed: timeout"},
	{core.StatusFailed{At: at("10:00:00"), IssueRef: "#2", Result: core.ResultFailed, Reason: "timeout"},
		"could not update the status comment on #2: it failed: timeout"},
	{core.StatusFailed{At: at("10:00:00"), IssueRef: "#2", Result: core.ResultRefused, Reason: "issue is locked"},
		"could not update the status comment on #2: the tracker refused: issue is locked"},
	{core.WindingDown{At: at("10:00:00"), Limit: time.Hour},
		"run time of 1h0m0s is up: taking no new issues, winding down"},
	// Covers AE4 (lines side): the line names the bot and its full warning.
	{core.BotStopped{At: at("10:00:00"), Bot: "clerk", Reason: "writes as you",
		Warning: "crew's writes as bot clerk went back to you: HTTP 401"},
		"bot clerk stopped acting: crew's writes as bot clerk went back to you: HTTP 401"},
	{core.BotActsAgain{At: at("10:00:00"), Bot: "developer"},
		"bot developer acts again: its token renewed"},
	{core.Stopped{At: at("10:00:00")},
		"stopped"},
}

func TestEveryEventPrintsAnEnglishSentence(t *testing.T) {
	for _, tt := range sentences {
		t.Run(tt.want, func(t *testing.T) {
			if got := lines.Text(tt.event); got != tt.want {
				t.Errorf("Text = %q, want %q", got, tt.want)
			}
		})
	}
}

// The core publishes only the run events the views word (KTD-P6); the rest,
// such as a workspace asked for or a run released, have no sentence, and a
// route's tracker step given up or dropped has its CallDropped's.
func TestASilentEventHasNoSentence(t *testing.T) {
	for _, e := range []core.Published{
		crew.WorkspaceAsked{EventHead: lfgHead},
		crew.ActionSessionEnded{EventHead: lfgHead, Action: "lfg"},
		crew.StepEnded{EventHead: lfgHead, Outcome: crew.StepLanded{}},
		crew.RunReleased{EventHead: lfgHead},
		stepEnded(crew.StepPlan{Kind: crew.StepComment}, crew.StepGivenUp{Reason: "refused"}),
		stepEnded(crew.StepPlan{Kind: crew.StepClose}, crew.StepDropped{Reason: "closed"}),
	} {
		if got := lines.Text(e); got != "" {
			t.Errorf("Text(%T) = %q, want none", e, got)
		}
	}
}

// Covers R17 (lines side): no sentence says boss, mate, stage or workflow.
func TestNoSentenceSaysAnOldWord(t *testing.T) {
	old := regexp.MustCompile(`(?i)\b(boss(es)?|mates?|stages?|workflows?)\b`)
	for _, tt := range sentences {
		if found := old.FindAllString(lines.Text(tt.event), -1); len(found) > 0 {
			t.Errorf("%q says %q", lines.Text(tt.event), found)
		}
	}
}

func TestADropCountPrintsOneLineSayingHowManyEventsWereDropped(t *testing.T) {
	poll := func(hms string) engine.Update {
		return engine.Update{Events: []core.Published{core.PollDone{At: at(hms), Listed: 0}}}
	}
	src := newSource(3, poll("11:00:00"), poll("11:00:30"))

	got := render(t, src, at("11:01:05"))

	equalLines(t, got, []string{
		"11:00:00 crew: poll: listed 0 issues, took 0",
		"11:00:30 crew: poll: listed 0 issues, took 0",
		"11:01:05 crew: 3 events were dropped because this output fell behind",
	})
}

func TestNoDropsPrintNoDropLine(t *testing.T) {
	src := newSource(0, engine.Update{Events: []core.Published{core.Stopped{At: at("12:00:00")}}})

	got := render(t, src, at("12:00:01"))

	equalLines(t, got, []string{"12:00:00 crew: stopped"})
}

// Covers #92 (lines side): the notice for an item of the other kind is
// stamped with the time of the poll that found it, not the time it prints.
func TestANoticeOfAnItemOfTheOtherKindPrintsAtItsPollsTime(t *testing.T) {
	src := newSource(0, engine.Update{Events: []core.Published{
		core.IssueOfOtherKind{At: at("12:30:00"), IssueID: issueID("90"), IssueRef: "#90", Kind: crew.KindPullRequest,
			Label: "ready", Rule: "implement", Takes: crew.KindIssue},
	}})

	got := render(t, src, at("12:30:05"))

	equalLines(t, got, []string{
		"12:30:00 crew: left #90 alone: it is a pull request, and ready is the label of implement, which takes issues",
	})
}

// Covers AE10 (lines side): each startup warning prints once, before the
// first event.
func TestEachStartupWarningPrintsOnceBeforeTheFirstEvent(t *testing.T) {
	src := newSource(0,
		engine.Update{Events: []core.Published{core.PollDone{At: at("12:00:02"), Listed: 0}}},
		engine.Update{Events: []core.Published{core.Stopped{At: at("12:00:03")}}},
	)
	var out strings.Builder
	warning := "bot ops has no key on this machine for thatsnotmynameio; " +
		"run `crew bots create ops` in this repository"
	if err := lines.Run(src, &out, zone, func() time.Time { return at("12:00:01") }, warning); err != nil {
		t.Fatalf("Run: %v", err)
	}
	equalLines(t, strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), []string{
		"12:00:01 crew: warning: " + warning,
		"12:00:02 crew: poll: listed 0 issues, took 0",
		"12:00:03 crew: stopped",
	})
}

func TestLinePrintsOneStampedLineInItsTimesLocation(t *testing.T) {
	var out strings.Builder

	if err := lines.Line(&out, at("09:00:01").In(zone), "checking the gh login"); err != nil {
		t.Fatalf("Line: %v", err)
	}

	if want := "09:00:01 crew: checking the gh login\n"; out.String() != want {
		t.Errorf("Line printed %q, want %q", out.String(), want)
	}
}

// issueID returns the id of the issue keyed key, in no repository.
func issueID(key string) crew.IssueID { return crew.IssueID{Key: key} }
