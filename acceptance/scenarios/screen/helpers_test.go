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

// newScenario builds a screen scenario whose repository has owner as its code
// owner, the rule's labels, and one issue titled title with the ready label.
// It returns the scenario and the issue's number.
func newScenario(t *testing.T) (*harness.Scenario, int) {
	t.Helper()
	sc := harness.New(t, harness.Options{Config: config, Screen: true, Size: harness.Size{Cols: cols, Rows: rows}})
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

// inColumn reports whether the cards of the board's column header hold text,
// ignoring case. It reads only the board: the rows between the column headers
// and the Queues and Events sections, which the README puts under the board,
// and in those rows only the cells from the header's first cell to the next
// column's, so neither an event line nor a card of another column counts.
func inColumn(screen, header, text string) bool {
	cards, found := columnCards(screen, header)
	return found && strings.Contains(strings.ToLower(cards), strings.ToLower(text))
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
// separates the words of a single header, such as "Handled 1 · $0.25".
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
