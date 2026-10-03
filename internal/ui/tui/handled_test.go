package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// entry is #key handled by stage into to, taken and ended the given minutes
// before start.
func entry(key, title, stage string, to crew.State, taken, ended int) core.HandledView {
	return core.HandledView{
		Issue: crew.Issue{Key: key, Ref: "#" + key, Title: title}, Stage: stage, To: to, Move: crew.MoveDone,
		Taken: start.Add(-time.Duration(taken) * time.Minute), Ended: start.Add(-time.Duration(ended) * time.Minute),
	}
}

// failedEntry is entry with its actions failed for reasons, in pairs of
// action and reason.
func failedEntry(key, title string, taken, ended int, failures ...string) core.HandledView {
	e := entry(key, title, "implement", "needs attention", taken, ended)
	for len(failures) >= 2 {
		e.Failures = append(e.Failures, crew.ActionFailure{Action: failures[0], Reason: failures[1]})
		failures = failures[2:]
	}
	return e
}

// givenUpEntry is entry whose verdict move crew gave up.
func givenUpEntry(e core.HandledView, reason string) core.HandledView {
	e.Move, e.DropReason = crew.MoveDropped, reason
	return e
}

// handledSnapshot is runningSnapshot 12 minutes into a one-hour run, with
// four issues handled: a failure whose code action never had a session, a
// given-up move and two successes, one of two actions. An earlier stage of
// #8 spent $2.00 this run.
func handledSnapshot() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{
		acted(
			failedEntry("5", "Parse the config once", 40, 30,
				"tests", "exited 1: tests fail", "code", "prompt did not render"),
			core.HandledAction{Name: "tests", Spend: spent(0.84, 1_200_000), PullRequest: noPullRequest},
			core.HandledAction{Name: "code"}),
		acted(givenUpEntry(entry("6", "Drop the old flag", "implement", "ready to review", 25, 20), "issue closed"),
			core.HandledAction{Name: "code", Spend: spent(3.1, 4_500_000), PullRequest: found("#44")}),
		acted(entry("8", "Trim the README", "review", "ready to merge", 9, 3),
			core.HandledAction{Name: "review", Spend: spent(0.42, 48_200), PullRequest: found("#46")}),
		acted(entry("7", "Log the poll interval", "implement", "ready to review", 15, 6),
			core.HandledAction{Name: "code", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")},
			core.HandledAction{Name: "tests", Spend: spent(1.1, 900_000), PullRequest: noPullRequest}),
	}
	u.Snapshot.Spent = spent(2, 300_000)
	for _, e := range u.Snapshot.Handled {
		u.Snapshot.Spent = u.Snapshot.Spent.Add(e.Spend())
	}
	return u
}

func line(t *testing.T, view string, n int) string {
	t.Helper()
	all := strings.Split(view, "\n")
	if n >= len(all) {
		t.Fatalf("view has %d lines, want line %d:\n%s", len(all), n, view)
	}
	return all[n]
}

func TestHandledIssuesRenderTheGoldenView(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(handledSnapshot()))

	golden(t, "handled", h.view())
}

// Covers AE5.
func TestWithoutARunTimeLimitTheTopLineShowsTheUptimeAndNoTimeLeft(t *testing.T) {
	h := newHarness(t, 80)
	u := runningSnapshot()
	u.Snapshot.RunTimeLimit = 0

	h.send(updateMsg(u))

	if got, want := line(t, h.view(), 0), "crew: 2 issues held, up 12m (q or ctrl+c stops)"; got != want {
		t.Errorf("top line = %q, want %q", got, want)
	}
}

func TestBeforeTheFirstPollTheTopLineShowsNoUptime(t *testing.T) {
	h := newHarness(t, 80)

	if got, want := line(t, h.view(), 0), "crew: 0 issues held (q or ctrl+c stops)"; got != want {
		t.Errorf("top line = %q, want %q", got, want)
	}
}

func TestATickAMinuteLaterAdvancesTheUptimeAndLowersTheTimeLeft(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.clock = h.clock.Add(time.Minute)
	h.send(tickMsg{})

	if got, want := line(t, h.view(), 0), "crew: 2 issues held, up 13m, 47m left (q or ctrl+c stops)"; got != want {
		t.Errorf("top line = %q, want %q", got, want)
	}
}

// Covers AE3 (TUI side).
func TestAGivenUpMoveIsCountedOnItsOwnWithItsReason(t *testing.T) {
	h := newHarness(t, 80)
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{
		givenUpEntry(entry("6", "Drop the old flag", "implement", "ready to review", 25, 20), "issue closed"),
		entry("7", "Log the poll interval", "implement", "ready to review", 15, 6),
	}

	h.send(updateMsg(u))
	view := h.view()

	if got, want := line(t, view, 1), "handled 2: 1 move given up, 1 ready to review"; got != want {
		t.Errorf("counts line = %q, want %q", got, want)
	}
	if !strings.Contains(view, "\n    move to ready to review given up: issue closed\n") {
		t.Errorf("view lacks the give-up reason line:\n%s", view)
	}
}

func TestAFailedStageWhoseMoveWasGivenUpShowsItsFailuresThenTheGiveUp(t *testing.T) {
	h := newHarness(t, 80)
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{
		givenUpEntry(failedEntry("5", "Parse the config once", 40, 30, "tests", "exited 1"), "label missing"),
	}

	h.send(updateMsg(u))
	view := h.view()

	if got, want := line(t, view, 1), "handled 1: 1 move given up"; got != want {
		t.Errorf("counts line = %q, want %q", got, want)
	}
	want := "\n  move given up  #5  implement 10m00s  Parse the config once\n" +
		"    tests failed: exited 1\n" +
		"    move to needs attention given up: label missing\n"
	if !strings.Contains(view, want) {
		t.Errorf("view lacks the entry and its two reason lines:\n%s", view)
	}
}

func TestANarrowWindowCutsTheSuccessCountsBeforeTheFailures(t *testing.T) {
	// 75 columns leave the counts, past this run's spend, 41 columns.
	h := newHarness(t, 75)

	h.send(updateMsg(handledSnapshot()))

	got := line(t, h.view(), 1)
	for _, want := range []string{"1 move given up", "1 needs attention"} {
		if !strings.Contains(got, want) {
			t.Errorf("counts line %q lacks %q", got, want)
		}
	}
}

func TestAReasonWithNewlinesTakesOneLine(t *testing.T) {
	h := newHarness(t, 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{failedEntry("5", "Parse", 40, 30, "tests", "exited 1:\n  tests fail\n")}

	h.send(updateMsg(u))
	view := h.view()

	if !strings.Contains(view, "\n    tests failed: exited 1: tests fail\n") {
		t.Errorf("view lacks the reason on one line:\n%s", view)
	}
	if n := strings.Count(view, "\n") + 1; n > 24 {
		t.Errorf("view has %d lines, over the window's 24", n)
	}
}

func TestARecentEventWithAMultiLineReasonTakesOneRow(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Recent = append(u.Snapshot.Recent, core.ActionEnded{
		At: start, IssueRef: "#1", Stage: "implement", Action: "code",
		Outcome: crew.Outcome{Reason: "git fetch: exit status 128: ssh: Could not resolve hostname\n" +
			"fatal: Could not read from remote repository."},
	})
	full := fitted(t, 200, 0, u)
	rows := strings.Count(full, "\n") + 1

	// A window two rows short of everything leaves Recent events four of its
	// six rows; a reason that broke onto two rows would push the top line off.
	view := fitted(t, 200, rows-2, u)

	if got := line(t, view, 0); !strings.HasPrefix(got, "crew: 2 issues held") {
		t.Errorf("first line = %q, want the top line", got)
	}
	if !strings.Contains(view, "ssh: Could not resolve hostname fatal: Could not read") {
		t.Errorf("view lacks the reason on one row:\n%s", view)
	}
}

// manySnapshot is runningSnapshot with ten issues handled: #11 and #12
// failed, and the successes #13 to #20 in crew:waiting review, the higher
// the number the more recent.
func manySnapshot() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{
		acted(failedEntry("11", "Parse the config once", 90, 50, "lfg", `exited 1: "tests fail on Go 1.27"`),
			core.HandledAction{Name: "lfg", Spend: spent(4.05, 6_100_000), PullRequest: noPullRequest}),
		acted(failedEntry("12", "Drop the old --dry flag", 60, 59, "lfg", `prompt did not render: no field "Body"`),
			core.HandledAction{Name: "lfg"}),
	}
	for n := 13; n <= 20; n++ {
		u.Snapshot.Handled = append(u.Snapshot.Handled,
			acted(entry(strconv.Itoa(n), fmt.Sprintf("Success number %d", n), "development", "crew:waiting review", 60, 40-n),
				core.HandledAction{
					Name: "lfg", Spend: spent(float64(n)/2, int64(n)*1_000_000), PullRequest: found("#" + strconv.Itoa(n+30)),
				}))
	}
	for _, e := range u.Snapshot.Handled {
		u.Snapshot.Spent = u.Snapshot.Spent.Add(e.Spend())
	}
	return u
}

func fitted(t *testing.T, width, height int, u engine.Update) string {
	t.Helper()
	h := newHarness(t, width)
	h.send(tea.WindowSizeMsg{Width: width, Height: height})
	h.send(updateMsg(u))
	view := h.view()
	if n := strings.Count(view, "\n") + 1; height > 0 && n > height {
		t.Errorf("view has %d lines, over the window's %d:\n%s", n, height, view)
	}
	for l := range strings.SplitSeq(view, "\n") {
		if n := utf8.RuneCountInString(l); n > width {
			t.Errorf("line is %d columns wide, over %d: %q", n, width, l)
		}
	}
	return view
}

// Covers AE4.
func TestA24RowWindowGivesUpRecentEventsAndCollapsesOldSuccesses(t *testing.T) {
	view := fitted(t, 80, 24, manySnapshot())

	golden(t, "fit-24-rows", view)
	if n := strings.Count(view, "\n") + 1; n != 24 {
		t.Errorf("view has %d lines, want the window's 24", n)
	}
	if strings.Contains(view, "Recent events") {
		t.Errorf("view still shows Recent events:\n%s", view)
	}
	for _, want := range []string{
		"#11", "tests fail on Go 1.27", "#12", `no field "Body"`, "  … and 8 more in crew:waiting review",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestCollapsedSuccessesTakeOneLinePerState(t *testing.T) {
	u := manySnapshot()
	u.Snapshot.Handled[2].To = "crew:merged" // #13, the oldest success
	view := fitted(t, 80, 28, u)

	for _, want := range []string{"  … and 4 more in crew:waiting review", "  … and 1 more in crew:merged"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestATallerWindowShowsEveryEntryAndTheNewestRecentEvents(t *testing.T) {
	// 19 fixed lines, 2 failed entries with a reason each, 8 successes, and
	// the Recent events header: 2 rows of events are left at 35.
	view := fitted(t, 80, 35, manySnapshot())

	if strings.Contains(view, "more in") {
		t.Errorf("view collapsed entries although they fit:\n%s", view)
	}
	if !strings.HasSuffix(view, "Recent events\n"+
		"  14:29:50 poll: listed 2 issues, took 1\n"+
		`  14:29:50 review took #2 "Fix the flaky stream test" (ready to review -> in re…`) {
		t.Errorf("view does not end with the two newest recent events:\n%s", view)
	}
}

func TestAWindowTooShortForTheFailuresIsCutAtTheBottom(t *testing.T) {
	view := fitted(t, 80, 21, manySnapshot())

	if n := strings.Count(view, "\n") + 1; n != 21 {
		t.Errorf("view has %d lines, want the window's 21", n)
	}
	// 19 fixed lines, 4 for the failures and 1 collapsed line: 4 are cut.
	if got, want := line(t, view, 20), "… 4 lines cut"; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}

func TestBeforeAWindowSizeTheViewShowsEverything(t *testing.T) {
	view := fitted(t, 80, 0, manySnapshot())

	if strings.Contains(view, "more in") || !strings.Contains(view, "Recent events") {
		t.Errorf("view without a height was fitted:\n%s", view)
	}
}

func TestANarrowShortWindowRendersWithoutPanicking(t *testing.T) {
	for _, size := range [][2]int{{12, 5}, {1, 1}, {40, 2}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			fitted(t, size[0], size[1], manySnapshot())
		})
	}
}

// spent is one session that reported cost dollars and tokens tokens.
func spent(cost float64, tokens int64) crew.Spend {
	return crew.Usage{Cost: cost, HasCost: true, Tokens: crew.Tokens{Output: tokens}, HasTokens: true}.Spend()
}

// found is the pull request ref, as a lookup found it.
func found(ref string) crew.PullRequest {
	return crew.PullRequest{Lookup: crew.PullRequestFound, Ref: ref, URL: "https://github.com/o/r/pull/" + ref[1:]}
}

var noPullRequest = crew.PullRequest{Lookup: crew.PullRequestNone}

// acted is e with its stage's actions.
func acted(e core.HandledView, actions ...core.HandledAction) core.HandledView {
	e.Actions = actions
	return e
}

// handledView renders a snapshot handling entries, in a window wide enough
// for their whole lines.
func handledView(t *testing.T, entries ...core.HandledView) string {
	t.Helper()
	u := runningSnapshot()
	u.Snapshot.Handled = entries
	return fitted(t, 120, 40, u)
}

// Covers AE1.
func TestAnEntryShowsItsActionsCostTokensAndPullRequest(t *testing.T) {
	view := handledView(t, acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")}))

	want := "\n  ready to review  #31  development 10m00s  $12.40, 17.2M tokens  #45  Add login form\n"
	if !strings.Contains(view, want) {
		t.Errorf("view lacks the entry with its cost, tokens and pull request:\n%s", view)
	}
}

// Covers AE2.
func TestAnEntryWhoseActionOpenedNoPullRequestSaysSo(t *testing.T) {
	view := handledView(t, acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: noPullRequest}))

	want := "\n  ready to review  #31  development 10m00s  $12.40, 17.2M tokens  no pull request  Add login form\n"
	if !strings.Contains(view, want) {
		t.Errorf("view lacks the entry saying it opened no pull request:\n%s", view)
	}
}

// Covers AE4.
func TestATwoActionEntryWithOneCostNotReportedShowsThePartialCostAndEachPullRequest(t *testing.T) {
	view := handledView(t, acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")},
		core.HandledAction{Name: "docs", Spend: crew.Usage{}.Spend(), PullRequest: noPullRequest}))

	want := "\n  ready to review  #31  development 10m00s  $12.40 (partial), 17.2M tokens (partial)  Add login form\n" +
		"    lfg: pull request #45\n" +
		"    docs: no pull request\n"
	if !strings.Contains(view, want) {
		t.Errorf("view lacks the partial entry and its two pull request lines:\n%s", view)
	}
}

// Covers AE6.
func TestAnEntryWithNoCostReportedSaysSo(t *testing.T) {
	tokens := crew.Usage{Tokens: crew.Tokens{Input: 300, CacheRead: 17_000_000}, HasTokens: true}.Spend()
	view := handledView(t, acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: tokens, PullRequest: found("#45")}))

	if !strings.Contains(view, "  #31  development 10m00s  cost not reported, 17M tokens  #45  Add login form\n") {
		t.Errorf("view lacks the entry saying its cost was not reported:\n%s", view)
	}
}

func TestAnActionThatNeverHadASessionShowsNoPullRequest(t *testing.T) {
	view := handledView(t,
		acted(failedEntry("5", "Parse the config once", 40, 30, "code", "prompt did not render"),
			core.HandledAction{Name: "code"}),
		acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
			core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")},
			core.HandledAction{Name: "docs"}))

	want := "\n  needs attention  #5   implement 10m00s                          Parse the config once\n" +
		"    code failed: prompt did not render\n" +
		"  ready to review  #31  development 10m00s  $12.40, 17.2M tokens  Add login form\n" +
		"    lfg: pull request #45\n" +
		"\n"
	if !strings.Contains(view, want) {
		t.Errorf("view shows a spend or pull request for an action without a session:\n%s", view)
	}
}

// Covers AE7.
func TestTheCountsLineShowsThisRunsSpendIncludingReplacedEntries(t *testing.T) {
	u := runningSnapshot()
	// #31's earlier stage spent $3.00; its entry now holds its later stage.
	u.Snapshot.Spent = spent(3, 800_000).Add(spent(12.4, 17_200_000))
	u.Snapshot.Handled = []core.HandledView{acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")})}

	view := fitted(t, 80, 40, u)

	if got, want := line(t, view, 1), "handled 1 ($15.40, 18M tokens): 1 ready to review"; got != want {
		t.Errorf("counts line = %q, want %q", got, want)
	}
}

func TestANarrowWindowCutsTheStateCountsBeforeTheSpend(t *testing.T) {
	u := handledSnapshot()
	u.Snapshot.Spent = spent(15.4, 18_000_000)

	got := line(t, fitted(t, 40, 40, u), 1)

	if !strings.HasPrefix(got, "handled 4 ($15.40, 18M tokens): ") {
		t.Errorf("counts line = %q, want this run's spend before the state counts", got)
	}
}

// Covers R12 and R13 for the fitting: each pull request line takes a row.
func TestFittingCountsThePullRequestLines(t *testing.T) {
	u := manySnapshot()
	for i := 2; i < len(u.Snapshot.Handled); i++ {
		u.Snapshot.Handled[i].Actions = []core.HandledAction{
			{Name: "lfg", Spend: spent(1, 1_000), PullRequest: found("#4" + strconv.Itoa(i))},
			{Name: "docs", Spend: spent(1, 1_000), PullRequest: noPullRequest},
		}
	}

	// 19 fixed lines and 4 for the failures leave 5 rows: one success
	// with its two pull request lines, and one collapsed line.
	view := fitted(t, 80, 28, u)

	if !strings.HasSuffix(view, "    docs: no pull request\n  … and 7 more in crew:waiting review") {
		t.Errorf("view does not end with one success and the other seven collapsed:\n%s", view)
	}
}
