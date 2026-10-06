package screen

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// enter and esc are the keys that open and close an issue's box.
	enter = "\r"
	esc   = "\x1b"

	// issueURL is the part of the issue's URL that the box shows.
	issueURL = "acme/widgets/issues/1"
)

// TestScreenBoardRunningIssue checks the live view while a session runs.
//
// README: "Under the header, Bots shows a card for each bot crew acts as, then
// one for you", "the board has a card for each issue in each of its columns:
// the issue's reference and title, then `run` (its actions and how long each
// has run ...), `bots` (...) and `via` (the queue its actions run in). Its last
// column, Handled", and "Queues and Events sit under the board". Without a
// board key, `.crew/config.example.yaml` says "the board has one column per
// rule that has actions". So the screen shows Bots, a development column whose
// card holds the issue's reference, its title, its action implement and its
// queue default, the Handled column, Queues and Events.
func TestScreenBoardRunningIssue(t *testing.T) {
	sc, n := newScenario(t)
	release := make(chan struct{})
	sc.Claude.Script(prompt, heldSession(release))
	sc.Start()
	waitForLabel(sc, n, running)
	sc.Screen().WaitForText(t, title, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	wantText(t, text, "Bots", "development", "Handled", "Queues", "Events",
		"#1", title, "run", "implement", "bots", "via", "default")
	if !inColumn(text, "development", title) {
		t.Errorf("the development column has no card for %q:\n%s", title, text)
	}
	harness.MatchSnapshot(t, "board-running-issue", text)
	close(release)
	waitForLabel(sc, n, success)
	stop(sc)
}

// TestScreenIssueBox checks the box Enter opens over the board.
//
// README: "The board has focus when the view opens, with one card highlighted"
// and "Enter opens a box over the dimmed view with that issue's rule, labels,
// kind, priority, whether it is blocked and its URL, its actions with the bot,
// queue, state and branch of each and the last thing each said or why it
// failed", and "Esc closes it". With one issue on the board, Enter opens its
// box, which shows the rule development, the label dev:running, the URL, the
// action implement, its queue default and what its session said last; Esc
// closes it.
func TestScreenIssueBox(t *testing.T) {
	sc, n := newScenario(t)
	release := make(chan struct{})
	sc.Claude.Script(prompt, heldSession(release))
	sc.Start()
	waitForLabel(sc, n, running)
	sc.Screen().WaitForText(t, title, timeout)
	sc.Screen().Send(t, enter)
	sc.Screen().WaitForText(t, issueURL, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	wantText(t, text, title, "development", running, issueURL, "implement", "default", said)
	harness.MatchSnapshot(t, "issue-box", text)
	sc.Screen().Send(t, esc)
	sc.Screen().WaitFor(t, func(text string) bool { return !strings.Contains(text, issueURL) }, timeout)
	close(release)
	waitForLabel(sc, n, success)
	stop(sc)
}

// TestScreenHandledFailedIssue checks the Handled column after a rule failed.
//
// README: "Its last column, Handled, holds a card for each issue crew stopped
// handling, those needing you first, with why its rule ended." After the
// issue's session failed and it moved to the failure label, the Handled
// column holds a card with the issue's title that says it failed.
func TestScreenHandledFailedIssue(t *testing.T) {
	sc, n := newScenario(t)
	sc.Claude.Script(prompt, fakeclaude.Fail("The build is broken."))
	sc.Start()
	waitForLabel(sc, n, failure)
	sc.Screen().WaitFor(t, func(text string) bool { return inColumn(text, "Handled", title) }, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	if !inColumn(text, "Handled", title) {
		t.Errorf("the Handled column has no card for %q:\n%s", title, text)
	}
	if !inColumn(text, "Handled", "fail") {
		t.Errorf("the Handled column does not say the rule failed:\n%s", text)
	}
	harness.MatchSnapshot(t, "handled-failed-issue", text)
	stop(sc)
}

// TestScreenKeys checks the list of keys.
//
// README: "`?` lists every key", among them "Tab cycles the board, Bots and
// Events, `b` and `e` jump to Bots and Events, and Esc returns to the board".
// The board's own footer already names some keys, such as tab and enter, so
// the scenario reads only the rows `?` changed: they name Esc, b and e, which
// only the full list has, and the screen then names Enter and Tab too.
func TestScreenKeys(t *testing.T) {
	sc, n := newScenario(t)
	release := make(chan struct{})
	sc.Claude.Script(prompt, heldSession(release))
	sc.Start()
	waitForLabel(sc, n, running)
	sc.Screen().WaitForText(t, title, timeout)
	board := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	sc.Screen().Send(t, "?")
	text := sc.Screen().WaitFor(t, func(text string) bool {
		added := addedRows(board, harness.MaskText(text, masks()...))
		return namesKey(added, "esc") && namesKey(added, "b") && namesKey(added, "e")
	}, timeout)
	wantText(t, strings.ToLower(text), "enter", "tab")
	close(release)
	waitForLabel(sc, n, success)
	stop(sc)
}

// addedRows is the rows of after that before does not hold, one per line.
func addedRows(before, after string) string {
	rows := strings.Split(before, "\n")
	var added []string
	for row := range strings.SplitSeq(after, "\n") {
		if !slices.Contains(rows, row) {
			added = append(added, row)
		}
	}
	return strings.Join(added, "\n")
}

// namesKey reports whether text names key, ignoring case, as a word of its
// own: not inside a longer word, as the b of "Bots" is.
func namesKey(text, key string) bool {
	word := regexp.MustCompile(`(?i)(^|[^\pL\pN])` + regexp.QuoteMeta(key) + `([^\pL\pN]|$)`)
	return word.MatchString(text)
}
