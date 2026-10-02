package tui

import (
	"fmt"
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
	for i := 0; i < len(failures); i += 2 {
		e.Failures = append(e.Failures, crew.ActionFailure{Action: failures[i], Reason: failures[i+1]})
	}
	return e
}

// givenUpEntry is entry whose verdict move crew gave up.
func givenUpEntry(e core.HandledView, reason string) core.HandledView {
	e.Move, e.DropReason = crew.MoveDropped, reason
	return e
}

// handledSnapshot is runningSnapshot 12 minutes into a one-hour run, with
// four issues handled: a failure, a given-up move and two successes.
func handledSnapshot() engine.Update {
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{
		failedEntry("5", "Parse the config once", 40, 30, "tests", "exited 1: tests fail", "code", "prompt did not render"),
		givenUpEntry(entry("6", "Drop the old flag", "implement", "ready to review", 25, 20), "issue closed"),
		entry("8", "Trim the README", "review", "ready to merge", 9, 3),
		entry("7", "Log the poll interval", "implement", "ready to review", 15, 6),
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
	h := newHarness(t, 52)

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
	h.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{failedEntry("5", "Parse", 40, 30, "tests", "exited 1:\n  tests fail\n")}

	h.send(updateMsg(u))
	view := h.view()

	if !strings.Contains(view, "\n    tests failed: exited 1: tests fail\n") {
		t.Errorf("view lacks the reason on one line:\n%s", view)
	}
	if n := strings.Count(view, "\n") + 1; n > 20 {
		t.Errorf("view has %d lines, over the window's 20", n)
	}
}

func TestARecentEventWithAMultiLineReasonTakesOneRow(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Recent = append(u.Snapshot.Recent, core.ActionEnded{
		At: start, IssueRef: "#1", Stage: "implement", Action: "code",
		Outcome: crew.Outcome{Reason: "git fetch: exit status 128: ssh: Could not resolve hostname\nfatal: Could not read from remote repository."},
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
		failedEntry("11", "Parse the config once", 90, 50, "lfg", `exited 1: "tests fail on Go 1.27"`),
		failedEntry("12", "Drop the old --dry flag", 60, 59, "lfg", `prompt did not render: no field "Body"`),
	}
	for n := 13; n <= 20; n++ {
		u.Snapshot.Handled = append(u.Snapshot.Handled,
			entry(fmt.Sprint(n), fmt.Sprintf("Success number %d", n), "development", "crew:waiting review", 60, 40-n))
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
	for _, l := range strings.Split(view, "\n") {
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
	for _, want := range []string{"#11", "tests fail on Go 1.27", "#12", `no field "Body"`, "  … and 4 more in crew:waiting review"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestCollapsedSuccessesTakeOneLinePerState(t *testing.T) {
	u := manySnapshot()
	u.Snapshot.Handled[2].To = "crew:merged" // #13, the oldest success
	view := fitted(t, 80, 24, u)

	for _, want := range []string{"  … and 4 more in crew:waiting review", "  … and 1 more in crew:merged"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestATallerWindowShowsEveryEntryAndTheNewestRecentEvents(t *testing.T) {
	// 15 fixed lines, 2 failed entries with a reason each, 8 successes, and
	// the Recent events header: 2 rows of events are left at 31.
	view := fitted(t, 80, 31, manySnapshot())

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
	view := fitted(t, 80, 17, manySnapshot())

	if n := strings.Count(view, "\n") + 1; n != 17 {
		t.Errorf("view has %d lines, want the window's 17", n)
	}
	// 15 fixed lines, 4 for the failures and 1 collapsed line: 4 are cut.
	if got, want := line(t, view, 16), "… 4 lines cut"; got != want {
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
