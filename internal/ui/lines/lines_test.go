package lines_test

import (
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

func TestATakenStartedEndedMovedSequencePrintsFourStampedLinesInOrder(t *testing.T) {
	issue := crew.Issue{Key: "1", Ref: "#1", Title: "Add login form"}
	src := newSource(0,
		engine.Update{Events: []core.Event{
			core.IssueTaken{At: at("09:00:01"), Issue: issue, Stage: "implement", From: "ready", To: "in progress"},
		}},
		engine.Update{Events: []core.Event{
			core.ActionStarted{At: at("09:00:02"), IssueKey: "1", IssueRef: "#1", Stage: "implement", Action: "code",
				Workspace: "1-code", Branch: "crew/1-code", Log: ".crew/logs/1-code.log"},
		}},
		engine.Update{Events: []core.Event{
			core.ActionEnded{At: at("09:12:30"), IssueKey: "1", IssueRef: "#1", Stage: "implement", Action: "code",
				Outcome: crew.Outcome{Succeeded: true, Reason: "Opened pull request #7"}},
			core.IssueMoved{At: at("09:12:31"), IssueKey: "1", IssueRef: "#1", From: "in progress", To: "ready to review"},
		}},
	)

	got := render(t, src, at("09:13:00"))

	equalLines(t, got, []string{
		`09:00:01 crew: implement took #1 "Add login form" (ready -> in progress)`,
		`09:00:02 crew: #1 implement/code started on branch crew/1-code, log .crew/logs/1-code.log`,
		`09:12:30 crew: #1 implement/code succeeded: Opened pull request #7`,
		`09:12:31 crew: #1 moved from in progress to ready to review`,
	})
}

// The tracker calls the sentences below describe.
var (
	move   = core.Call{Kind: core.CallMove, IssueKey: "2", IssueRef: "#2", From: "in review", To: "needs attention"}
	report = core.Call{Kind: core.CallReport, IssueKey: "2", IssueRef: "#2"}
	prs    = core.Call{Kind: core.CallPullRequests, IssueKey: "2", IssueRef: "#2", To: "needs attention"}
)

// sentences pairs each kind of event with the sentence Text gives it.
var sentences = []struct {
	event core.Event
	want  string
}{
	{core.ActionStarted{At: at("10:00:00"), IssueKey: "9", IssueRef: "#9", Stage: "development", Action: "lfg",
		Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg", Log: ".crew/logs/issue-9-lfg.log"},
		"#9 development/lfg started on branch crew/issue-9-lfg, log .crew/logs/issue-9-lfg.log"},
	{core.ActionStarted{At: at("10:00:00"), IssueKey: "9", IssueRef: "#9", Stage: "development", Action: "lfg",
		Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg", Log: ".crew/logs/issue-9-lfg.log", Resumed: true},
		"#9 development/lfg resumed in worktree issue-9-lfg on branch crew/issue-9-lfg, log .crew/logs/issue-9-lfg.log"},
	{core.WorkspaceMissing{At: at("10:00:00"), IssueKey: "9", IssueRef: "#9", Stage: "development", Action: "lfg",
		Workspace: "issue-9-lfg"},
		"#9 development/lfg: worktree issue-9-lfg is gone, creating a new one"},
	{core.RunNotRecorded{At: at("10:00:00"), IssueKey: "9", IssueRef: "#9", Stage: "development", Action: "lfg",
		Reason: "disk full"},
		"could not record #9 development/lfg's run, so a restart may not resume it: disk full"},
	{core.ActionEnded{At: at("10:00:00"), IssueRef: "#2", Stage: "review", Action: "check",
		Outcome: crew.Outcome{Reason: "session exited with status 1"}},
		"#2 review/check failed: session exited with status 1"},
	{core.ActionEnded{At: at("10:00:00"), IssueRef: "#2", Stage: "review", Action: "check",
		Outcome: crew.Outcome{Succeeded: true}},
		"#2 review/check succeeded"},
	{core.ActionEnded{At: at("10:00:00"), IssueRef: "#2", Stage: "implement", Action: "lfg",
		Outcome: crew.Outcome{Reason: "the check failed: no open pull request from crew/issue-2-lfg"}},
		"#2 implement/lfg failed: the check failed: no open pull request from crew/issue-2-lfg"},
	{core.FailureReported{At: at("10:00:00"), IssueRef: "#2"},
		"reported the failure on #2"},
	{core.IssueSkipped{At: at("10:00:00"), IssueRef: "#3", States: []crew.State{"ready", "in progress"}},
		"skipped #3: it carries 2 crew labels (ready, in progress)"},
	{core.IssueOfOtherKind{At: at("10:00:00"), IssueKey: "90", IssueRef: "#90", Kind: crew.KindPullRequest,
		Label: "crew:development:ready", Stage: "development", Takes: crew.KindIssue},
		"left #90 alone: it is a pull request, and crew:development:ready is the label of development, which takes issues"},
	{core.IssueOfOtherKind{At: at("10:00:00"), IssueKey: "42", IssueRef: "#42", Kind: crew.KindIssue,
		Label: "fix review ready", Stage: "fix review", Takes: crew.KindPullRequest},
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
		"reporting the failure on #2 failed, retrying at the next tick: timeout"},
	{core.CallOwed{At: at("10:00:00"), Call: prs, Reason: "gh: HTTP 502"},
		"updating the pull requests of #2 to needs attention failed, retrying at the next tick: gh: HTTP 502"},
	{core.CallDropped{At: at("10:00:00"), Call: prs, Result: core.ResultRefused, Reason: "pull request is locked"},
		"gave up updating the pull requests of #2 to needs attention: the tracker refused: pull request is locked"},
	{core.CallDropped{At: at("10:00:00"), Call: move, Result: core.ResultMovedMeanwhile},
		"gave up moving #2 from in review to needs attention: the issue moved meanwhile"},
	{core.CallDropped{At: at("10:00:00"), Call: report, Result: core.ResultRefused, Reason: "issue is locked"},
		"gave up reporting the failure on #2: the tracker refused: issue is locked"},
	{core.CallDropped{At: at("10:00:00"), Call: move, Result: core.ResultFailed, Reason: "timeout"},
		"gave up moving #2 from in review to needs attention: it failed: timeout"},
	{core.StatusFailed{At: at("10:00:00"), IssueRef: "#2", Result: core.ResultFailed, Reason: "timeout"},
		"could not update the status comment on #2: it failed: timeout"},
	{core.StatusFailed{At: at("10:00:00"), IssueRef: "#2", Result: core.ResultRefused, Reason: "issue is locked"},
		"could not update the status comment on #2: the tracker refused: issue is locked"},
	{core.WindingDown{At: at("10:00:00"), Limit: time.Hour},
		"run time of 1h0m0s is up: taking no new issues, winding down"},
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

func TestADropCountPrintsOneLineSayingHowManyEventsWereDropped(t *testing.T) {
	poll := func(hms string) engine.Update {
		return engine.Update{Events: []core.Event{core.PollDone{At: at(hms), Listed: 0}}}
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
	src := newSource(0, engine.Update{Events: []core.Event{core.Stopped{At: at("12:00:00")}}})

	got := render(t, src, at("12:00:01"))

	equalLines(t, got, []string{"12:00:00 crew: stopped"})
}

// Covers AE10 (lines side): each startup warning prints once, before the
// first event.
func TestEachStartupWarningPrintsOnceBeforeTheFirstEvent(t *testing.T) {
	src := newSource(0,
		engine.Update{Events: []core.Event{core.PollDone{At: at("12:00:02"), Listed: 0}}},
		engine.Update{Events: []core.Event{core.Stopped{At: at("12:00:03")}}},
	)
	var out strings.Builder
	warning := "mate ops has no key on this machine for thatsnotmynameio; " +
		"run `crew mates create ops` in this repository"
	if err := lines.Run(src, &out, zone, func() time.Time { return at("12:00:01") }, warning); err != nil {
		t.Fatalf("Run: %v", err)
	}
	equalLines(t, strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), []string{
		"12:00:01 crew: warning: " + warning,
		"12:00:02 crew: poll: listed 0 issues, took 0",
		"12:00:03 crew: stopped",
	})
}
