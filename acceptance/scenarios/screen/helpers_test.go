package screen

import (
	"context"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// timeout bounds every wait and every exit: generous, since a wait
	// returns as soon as its condition holds.
	timeout = time.Minute

	// settle is how long a masked screen must stay the same to be stable.
	settle = 3 * time.Second

	// cols and rows are the terminal's size: far taller than the layout
	// needs, so no section loses rows to the height.
	cols = 160
	rows = 80

	// owner is the account gh is logged in as, and the repository's code
	// owner.
	owner = "boss"

	// The labels of the development rule.
	ready   = "dev:ready"
	running = "dev:running"
	success = "dev:done"
	failure = "dev:failed"

	// title is the title of every scenario's issue, and said what its
	// session says while it works.
	title = "Add a search box"
	said  = "Writing the search box."
)

// config has one rule, development, with one action, implement, and no board,
// so the board has one column for the rule. It keeps the default poll interval,
// so a poll's event does not scroll the screen while a scenario waits for it to
// hold still: crew takes the issue at its first poll.
const config = `agents:
  developer:
    harness:
      name: claude
rules:
  development:
    labels:
      ready: dev:ready
      running: dev:running
      success: dev:done
      failure: dev:failed
    actions:
      implement:
        prompt: |-
          Implement "{{.Issue.Title}}".
`

// prompt is the implement action's prompt for the scenarios' issue.
const prompt = `Implement "` + title + `".`

// newScenario builds a screen scenario with config, the board crew draws
// without a board key.
func newScenario(t *testing.T) (*harness.Scenario, int) {
	t.Helper()
	return newScenarioWith(t, config)
}

// newScenarioWith builds a screen scenario whose repository holds cfg as its
// config, owner as its code owner, the rule's labels, and one issue titled
// title with the ready label. It returns the scenario and the issue's number.
func newScenarioWith(t *testing.T, cfg string) (*harness.Scenario, int) {
	t.Helper()
	sc := harness.New(t, harness.Options{Config: cfg, Screen: true, Size: harness.Size{Cols: cols, Rows: rows}})
	sc.GitHub.SetFile(".github/CODEOWNERS", "* @"+owner+"\n")
	sc.GitHub.AddLabel(ready, running, success, failure)
	n := sc.GitHub.AddIssue(fakegithub.Issue{Title: title, Author: owner, Labels: []string{ready}})
	return sc, n
}

// heldSession is a session that says said and then works until release is
// closed, when it succeeds, or until crew stops it.
func heldSession(release <-chan struct{}) fakeclaude.ScriptFunc {
	return func(ctx context.Context, s *fakeclaude.Session) int {
		_ = s.Emit(s.Init(), s.Said(said))
		select {
		case <-release:
			_ = s.Emit(s.Success("Added the search box."))
			return 0
		case <-ctx.Done():
			return 1
		}
	}
}

// masks are the default masks, then one for a clock that a box drawn over
// the Events section cuts after its first digits, such as 01:2.
func masks() []harness.Mask {
	return append(harness.DefaultMasks(), harness.Mask{
		Pattern: regexp.MustCompile(`\b[0-9]{2}:[0-9]\b`), Placeholder: "HH:M",
	})
}

// waitForLabel waits until the issue number carries label.
func waitForLabel(sc *harness.Scenario, number int, label string) {
	sc.Wait(func() bool {
		issue, _ := sc.GitHub.Issue(number)
		return slices.Contains(issue.Labels, label)
	}, timeout)
}

// stop stops crew as Ctrl+C does and waits for it to exit.
func stop(sc *harness.Scenario) {
	sc.Stop()
	sc.Exit(timeout)
}

// wantText fails the test for each of wants the screen text does not show.
func wantText(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("the screen does not show %q:\n%s", want, text)
		}
	}
}

// inColumn reports whether the cards of the board's column header hold the
// scenarios' issue title, ignoring case. It reads only the board: the rows between the column headers
// and the Queues and Events sections, which the README puts under the board,
// and in those rows only the cells from the header's first cell to the next
// column's, so neither an event line nor a card of another column counts.
func inColumn(screen, header string) bool {
	cards, found := columnCards(screen, header)
	return found && strings.Contains(strings.ToLower(cards), strings.ToLower(title))
}

// columnCards returns the text of the cards in the board's column header, one
// row per line, and false when the screen has no Board section, no such column
// under it, or no Queues or Events section under the board to end it.
func columnCards(screen, header string) (string, bool) {
	lines := strings.Split(screen, "\n")
	board := slices.IndexFunc(lines, func(line string) bool { return isSection(line, "Board") })
	if board < 0 {
		return "", false
	}
	start, end, row := -1, 0, board+1
	for ; row < len(lines) && start < 0; row++ {
		if isSection(lines[row], "Queues") || isSection(lines[row], "Events") {
			return "", false
		}
		if start = cell(lines[row], header); start >= 0 {
			end = nextColumn(lines[row], start+len([]rune(header)))
		}
	}
	var cards []string
	for _, line := range lines[row:] {
		if isSection(line, "Queues") || isSection(line, "Events") {
			return strings.Join(cards, "\n"), true
		}
		cards = append(cards, cells(line, start, end))
	}
	return "", false
}

// isSection reports whether line opens the section name: its title first,
// after the marker of the section that has focus.
func isSection(line, name string) bool {
	title := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "▸"))
	return title == name || strings.HasPrefix(title, name+" ")
}

// nextColumn is the first cell, from the cell from on, of the next column's
// header in the header row line: the first text after a gap of two or more
// spaces. It is math.MaxInt when no column follows, since a card may be wider
// than its header row.
func nextColumn(line string, from int) int {
	runes := []rune(line)
	gap := 0
	for x := from; x < len(runes); x++ {
		switch {
		case runes[x] == ' ':
			gap++
		case gap >= columnGap:
			return x
		default:
			gap = 0
		}
	}
	return math.MaxInt
}

// columnGap is the fewest spaces between two column headers. One space
// separates the words of a single header, such as "Not on board".
const columnGap = 2

// cells is the text of line from the cell start up to, not including, the
// cell end.
func cells(line string, start, end int) string {
	runes := []rune(line)
	start, end = min(start, len(runes)), min(end, len(runes))
	return string(runes[start:end])
}

// cell is the column, counted in runes, where line holds text, or -1.
func cell(line, text string) int {
	before, _, found := strings.Cut(line, text)
	if !found {
		return -1
	}
	return len([]rune(before))
}

// hasColumn reports whether the board draws a column titled header.
func hasColumn(screen, header string) bool {
	_, found := columnCards(screen, header)
	return found
}

// headerBefore reports whether the board's header row draws the column first
// left of the column second.
func headerBefore(screen, first, second string) bool {
	lines := strings.Split(screen, "\n")
	board := slices.IndexFunc(lines, func(line string) bool { return isSection(line, "Board") })
	if board < 0 {
		return false
	}
	for _, line := range lines[board+1:] {
		if isSection(line, "Queues") || isSection(line, "Events") {
			return false
		}
		if a, b := cell(line, first), cell(line, second); a >= 0 && b >= 0 {
			return a < b
		}
	}
	return false
}

// eventsTitle matches the row that opens the Events section, which may share
// its row with the Queues section on its left.
var eventsTitle = regexp.MustCompile(`(^|[\s▸])Events(\s|$)`)

// inEvents reports whether one row of the Events section holds every one of
// wants, ignoring case. It reads only the rows under the Events title that
// follows the board, and in them only the cells from the title's first cell
// on, so neither a card nor the Queues section beside Events counts.
func inEvents(screen string, wants ...string) bool {
	lines := strings.Split(screen, "\n")
	title, start := eventsTitleCell(lines)
	if title < 0 {
		return false
	}
	for _, line := range lines[title+1:] {
		row := strings.ToLower(cells(line, start, math.MaxInt))
		if !slices.ContainsFunc(wants, func(want string) bool { return !strings.Contains(row, strings.ToLower(want)) }) {
			return true
		}
	}
	return false
}

// eventsTitleCell returns the row of the Events title that follows the board
// in lines, and the cell where that title starts; the row is -1 when there is
// none.
func eventsTitleCell(lines []string) (int, int) {
	board := slices.IndexFunc(lines, func(line string) bool { return isSection(line, "Board") })
	if board < 0 {
		return -1, 0
	}
	title := slices.IndexFunc(lines[board:], eventsTitle.MatchString)
	if title < 0 {
		return -1, 0
	}
	return board + title, cell(lines[board+title], "Events")
}

// reportedEvent and movedEvent are the two events a failed rule records for
// the scenarios' issue: crew reports the failure and moves the issue to the
// failure label at once, and Events lists each when it is done, so they come
// in either order.
const (
	reportedEvent = "reported the failure on #1"
	movedEvent    = "#1 moved from " + running + " to " + failure
)

// failureEventsInOrder is screen with the Events rows of reportedEvent and
// movedEvent, when movedEvent comes right before reportedEvent, swapped into
// the order reportedEvent, movedEvent, so a snapshot does not pin an order
// the README leaves open. Only the Events cells of the two rows swap: the
// Queues section beside them stays as it is.
func failureEventsInOrder(screen string) string {
	lines := strings.Split(screen, "\n")
	title, start := eventsTitleCell(lines)
	if title < 0 {
		return screen
	}
	for row := title + 1; row+1 < len(lines); row++ {
		moved, reported := []rune(lines[row]), []rune(lines[row+1])
		if !strings.Contains(lines[row], movedEvent) || !strings.Contains(lines[row+1], reportedEvent) ||
			len(moved) < start || len(reported) < start {
			continue
		}
		lines[row] = string(moved[:start]) + string(reported[start:])
		lines[row+1] = string(reported[:start]) + string(moved[start:])
		row++
	}
	return strings.Join(lines, "\n")
}

// wantOneFrame fails the test unless the screen draws the view's header and
// its Bots section once each: rows a previous frame left behind, such as a
// second header above the view, are not part of the live view.
func wantOneFrame(t *testing.T, text string) {
	t.Helper()
	headers, bots := 0, 0
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "crew ") && strings.Contains(line, harness.RepositoryName) {
			headers++
		}
		if isSection(line, "Bots") {
			bots++
		}
	}
	if headers != 1 || bots != 1 {
		t.Errorf("the screen draws %d headers and %d Bots sections, not one of each:\n%s", headers, bots, text)
	}
}
