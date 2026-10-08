package screen

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// blocker is the title of the issue that blocks another, and filter the
	// title of a second ready issue.
	blocker = "Design the search page"
	filter  = "Add a filter"

	// blocked is what a blocked issue's card and box show.
	blocked = "blocked"
	// idle is what the card of an issue crew does not hold shows when nothing
	// blocks it, as issue #229's AE2 says.
	idle = "idle"
)

// fastConfig is config with a poll every second, so the board shows a change
// on GitHub at once. It keeps the default two slots: with every slot busy,
// crew skips its polls, so the board would not change.
const fastConfig = "poll_interval_seconds: 1\n" + config

// parked is the label of a board column that no rule takes from, so crew
// never holds an issue there.
const parked = "parked"

// parkedConfig is fastConfig with a board of one column, parked, for the
// label parked.
const parkedConfig = fastConfig + "board:\n  parked: parked\n"

// headerRow matches a row of a box's header that says whether the issue is
// blocked, as the box did before issue #229: a row whose name is blocked.
var headerRow = regexp.MustCompile(`^\s*blocked\b`)

// TestScreenBlockedCard checks the card of a blocked issue crew does not hold.
//
// README: "`run` (the action its run is on and how long it has run, ... or its
// state with none: `blocked` when crew does not hold it and an open issue
// blocks it)".
// An issue in the rule's ready label that an open issue blocks has a card in
// the development column whose run row says blocked, not idle.
func TestScreenBlockedCard(t *testing.T) {
	sc := newEmptyScenario(t, config)
	first := sc.GitHub.AddIssue(fakegithub.Issue{Title: blocker, Author: owner})
	sc.GitHub.AddIssue(fakegithub.Issue{
		Title: title, Author: owner, Labels: []string{ready}, BlockedBy: []int{first},
	})
	sc.Start()
	sc.Screen().WaitFor(t, func(text string) bool { return inColumn(text, "development", blocked) }, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	if !inColumn(text, "development", title) || !inColumn(text, "development", blocked) {
		t.Errorf("the development column has no blocked card for %q:\n%s", title, text)
	}
	if inColumn(text, "development", idle) {
		t.Errorf("the blocked issue's card says %s:\n%s", idle, text)
	}
	harness.MatchSnapshot(t, "board-blocked-issue", text)
	stop(sc)
}

// TestScreenBlockedCardUnblocked checks a card once its blocker closes.
//
// README: "`run` (the action its run is on and how long it has run, ... or its
// state with none: `blocked` when crew does not hold it and an open issue
// blocks it)".
// An issue in the board column parked, which no rule takes from, has a card
// that says blocked while an open issue blocks it. Once that issue is closed,
// no open issue blocks it, so its card no longer says blocked and, as issue
// #229's AE2 says, shows idle again.
func TestScreenBlockedCardUnblocked(t *testing.T) {
	sc := newEmptyScenario(t, parkedConfig)
	first := sc.GitHub.AddIssue(fakegithub.Issue{Title: blocker, Author: owner})
	sc.GitHub.AddIssue(fakegithub.Issue{
		Title: filter, Author: owner, Labels: []string{parked}, BlockedBy: []int{first},
	})
	sc.Start()
	sc.Screen().WaitFor(t, func(text string) bool {
		return inColumn(text, parked, filter) && inColumn(text, parked, blocked)
	}, timeout)
	sc.GitHub.SetState(first, fakegithub.Closed)
	text := sc.Screen().WaitFor(t, func(text string) bool {
		return !inColumn(text, parked, blocked) && inColumn(text, parked, idle)
	}, timeout)
	if !inColumn(text, parked, filter) {
		t.Errorf("the parked column has no card for %q:\n%s", filter, text)
	}
	stop(sc)
}

// TestScreenBlockedIssueBox checks the box of a blocked issue crew does not
// hold.
//
// README: "Enter opens a box over the dimmed view with that issue's rule, its
// labels as chips with a `blocked` chip after them when an open issue blocks
// it, its kind, priority and URL". The box's labels row shows dev:ready, then
// a blocked chip, and its header has no row of its own that says blocked.
func TestScreenBlockedIssueBox(t *testing.T) {
	sc := newEmptyScenario(t, config)
	first := sc.GitHub.AddIssue(fakegithub.Issue{Title: blocker, Author: owner})
	n := sc.GitHub.AddIssue(fakegithub.Issue{
		Title: title, Author: owner, Labels: []string{ready}, BlockedBy: []int{first},
	})
	sc.Start()
	sc.Screen().WaitFor(t, func(text string) bool { return inColumn(text, "development", blocked) }, timeout)
	sc.Screen().Send(t, enter)
	sc.Screen().WaitForText(t, issueURLOf(n), timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	wantText(t, text, title, "development", ready, issueURLOf(n))
	checkBlockedChip(t, text, issueURLOf(n), ready)
	harness.MatchSnapshot(t, "blocked-issue-box", text)
	stop(sc)
}

// TestScreenBlockedWhileRunning checks an issue that becomes blocked while
// crew runs it.
//
// README: "`run` (the action its run is on and how long it has run, ... or its
// state with none: `blocked` when crew does not hold it and an open issue
// blocks it)",
// and the box shows "its labels as chips with a `blocked` chip after them when
// an open issue blocks it". The issue's blocker is closed when crew takes it
// and reopened while its session runs: its box then shows the blocked chip
// after its labels, and its card, since crew holds it, still shows its action
// implement, not blocked.
func TestScreenBlockedWhileRunning(t *testing.T) {
	sc := newEmptyScenario(t, fastConfig)
	first := sc.GitHub.AddIssue(fakegithub.Issue{Title: blocker, Author: owner, State: fakegithub.Closed})
	n := sc.GitHub.AddIssue(fakegithub.Issue{
		Title: title, Author: owner, Labels: []string{ready}, BlockedBy: []int{first},
	})
	release := make(chan struct{})
	sc.Claude.Script(prompt, heldSession(release))
	sc.Start()
	waitForLabel(sc, n, running)
	sc.Screen().WaitFor(t, func(text string) bool { return inColumn(text, "development", "implement") }, timeout)
	sc.GitHub.SetState(first, fakegithub.Open)
	sc.Screen().Send(t, enter)
	text := sc.Screen().WaitFor(t, func(text string) bool {
		header, found := boxHeader(text, issueURLOf(n))
		return found && strings.Contains(header, blocked)
	}, timeout)
	checkBlockedChip(t, text, issueURLOf(n), ready, running)
	sc.Screen().Send(t, esc)
	text = sc.Screen().WaitFor(t, func(text string) bool { return !strings.Contains(text, issueURLOf(n)) }, timeout)
	if !inColumn(text, "development", "implement") {
		t.Errorf("the running issue's card no longer shows its action implement:\n%s", text)
	}
	if inColumn(text, "development", blocked) {
		t.Errorf("the card of an issue crew holds says %s:\n%s", blocked, text)
	}
	close(release)
	waitForLabel(sc, n, success)
	stop(sc)
}

// issueURLOf is the part of the issue number's URL that its box shows.
func issueURLOf(number int) string {
	return fmt.Sprintf("acme/widgets/issues/%d", number)
}

// checkBlockedChip fails the test unless the header of the box that shows url
// has a labels row holding each of labels and then a blocked chip after them,
// and no row of its own that says blocked.
func checkBlockedChip(t *testing.T, screen, url string, labels ...string) {
	t.Helper()
	header, found := boxHeader(screen, url)
	if !found {
		t.Errorf("the screen shows no box for %s:\n%s", url, screen)
		return
	}
	rows := strings.Split(header, "\n")
	row := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, labels[0]) })
	if row < 0 {
		t.Errorf("the box's header has no labels row with %s:\n%s", labels[0], header)
		return
	}
	last := 0
	for _, label := range labels {
		last = max(last, strings.Index(rows[row], label)+len(label))
	}
	if !strings.Contains(rows[row][last:], blocked) {
		t.Errorf("the box's labels row has no %s chip after %v:\n%s", blocked, labels, header)
	}
	checkNoBlockedRow(t, header)
}

// checkNoBlockedRow fails the test when a box's header has a row of its own
// that says blocked.
func checkNoBlockedRow(t *testing.T, header string) {
	t.Helper()
	for row := range strings.SplitSeq(header, "\n") {
		if headerRow.MatchString(row) {
			t.Errorf("the box's header has a %s row: %q\n%s", blocked, row, header)
		}
	}
}

// boxHeader returns the inside of the header of the box that shows url: its
// rows from the one under its top border down to the one that holds url,
// without its borders, one per line. It returns false when the screen shows
// url in no box.
func boxHeader(screen, url string) (string, bool) {
	lines := strings.Split(screen, "\n")
	row := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, url) })
	if row < 0 {
		return "", false
	}
	left := lastRune([]rune(lines[row])[:cell(lines[row], url)], '│')
	top := row
	for top >= 0 && runeAt(lines[top], left) == '│' {
		top--
	}
	if left < 0 || top < 0 || runeAt(lines[top], left) != '╭' {
		return "", false
	}
	right := cell(cells(lines[top], left, len([]rune(lines[top]))), "╮")
	if right < 0 {
		return "", false
	}
	right += left
	var header []string
	for _, line := range lines[top+1 : row+1] {
		header = append(header, cells(line, left+1, right))
	}
	return strings.Join(header, "\n"), true
}

// lastRune is the index of the last r in runes, or -1.
func lastRune(runes []rune, r rune) int {
	for i, got := range slices.Backward(runes) {
		if got == r {
			return i
		}
	}
	return -1
}

// runeAt is the rune of line at the cell x, or 0 when line is shorter or x is
// negative.
func runeAt(line string, x int) rune {
	runes := []rune(line)
	if x < 0 || x >= len(runes) {
		return 0
	}
	return runes[x]
}
