package screen

import (
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

const (
	// broken is what the failing session says it failed on.
	broken = "The build is broken."

	// The columns of boardConfig, and the column the README names for a
	// held issue that no column shows.
	toDo       = "to do"
	doing      = "doing"
	failed     = "failed"
	notOnBoard = "Not on board"

	// handled is the column the board no longer draws.
	handled = "Handled"
)

// boardConfig is config with a board that has a column for each of the rule's
// labels but success, in the shape `.crew/config.example.yaml` shows.
const boardConfig = config + `board:
  to do: dev:ready
  doing: dev:running
  failed: dev:failed
`

// readyOnlyConfig is config with a board whose one column shows the ready
// label, so no column shows the issue while its rule runs.
const readyOnlyConfig = config + `board:
  to do: dev:ready
`

// TestScreenFailedRuleIssue checks the board after a rule failed, on the board
// crew draws without a board key.
//
// README: "An issue whose rule ended shows only in the columns its labels put
// it in, and Events says how the rule ended." Without a board key,
// `.crew/config.example.yaml` says "the board has one column per rule that has
// actions", and `schema/config.schema.json` that such a column holds "the
// items of the rule's kind that carry its ready or running label". After the
// session failed and the issue moved to dev:failed, no column shows that
// label: the development column holds no card for the issue, no Handled or
// Not on board column holds one, the board draws no Handled column, and a row
// of Events names #1 and says it failed.
//
// Edge case: the card leaving makes the board shorter, and the view redraws
// in place. README: "Under the header, Bots shows a card for each bot crew
// acts as, then one for you". So the screen draws one header and one Bots
// section, with no rows a taller frame left behind.
//
// crew reports the failure and moves the issue to dev:failed at once, and the
// README gives no order for their two Events rows, so the snapshot holds them
// in one order whichever came first (failureEventsInOrder).
func TestScreenFailedRuleIssue(t *testing.T) {
	sc, n := newScenario(t)
	sc.Claude.Script(prompt, fakeclaude.Fail(broken))
	sc.Start()
	waitForLabel(sc, n, failure)
	sc.Screen().WaitFor(t, func(text string) bool {
		return inEvents(text, "#1", "fail") && !inColumn(text, "development", title)
	}, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	wantText(t, text, "development", "Queues", "Events")
	if inColumn(text, "development", title) {
		t.Errorf("the development column still has a card for %q:\n%s", title, text)
	}
	if hasColumn(text, handled) {
		t.Errorf("the board draws a %s column:\n%s", handled, text)
	}
	if inColumn(text, notOnBoard, title) {
		t.Errorf("the %s column has a card for %q, whose rule ended:\n%s", notOnBoard, title, text)
	}
	if !inEvents(text, "#1", "fail") {
		t.Errorf("Events does not say the rule of #1 failed:\n%s", text)
	}
	wantOneFrame(t, text)
	harness.MatchSnapshot(t, "failed-rule-issue", failureEventsInOrder(text))
	stop(sc)
}

// TestScreenFailedColumnIssue checks the board after a rule failed, on a board
// with a column for the failure label.
//
// README: "An issue whose rule ended shows only in the columns its labels put
// it in, and Events says how the rule ended." `schema/config.schema.json`:
// "A column holds a card for each open issue that carries one of its labels".
// After the session failed and the issue moved to dev:failed, the failed
// column holds its card, to do and doing do not, the board draws no Handled
// column, and a row of Events names #1 and says it failed.
//
// crew reports the failure and moves the issue to dev:failed at once, and the
// README gives no order for their two Events rows, so the snapshot holds them
// in one order whichever came first (failureEventsInOrder).
func TestScreenFailedColumnIssue(t *testing.T) {
	sc, n := newScenarioWith(t, boardConfig)
	sc.Claude.Script(prompt, fakeclaude.Fail(broken))
	sc.Start()
	waitForLabel(sc, n, failure)
	sc.Screen().WaitFor(t, func(text string) bool {
		return inEvents(text, "#1", "fail") && inColumn(text, failed, title)
	}, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	wantText(t, text, toDo, doing, failed, "Queues", "Events")
	if !inColumn(text, failed, title) {
		t.Errorf("the %s column has no card for %q:\n%s", failed, title, text)
	}
	for _, column := range []string{toDo, doing, notOnBoard} {
		if inColumn(text, column, title) {
			t.Errorf("the %s column has a card for %q, which carries %s:\n%s", column, title, failure, text)
		}
	}
	if hasColumn(text, handled) {
		t.Errorf("the board draws a %s column:\n%s", handled, text)
	}
	if !inEvents(text, "#1", "fail") {
		t.Errorf("Events does not say the rule of #1 failed:\n%s", text)
	}
	wantOneFrame(t, text)
	harness.MatchSnapshot(t, "failed-column-issue", failureEventsInOrder(text))
	stop(sc)
}

// TestScreenNotOnBoardIssue checks the Not on board column.
//
// README: "A held issue that no column shows, such as a pull request, gets a
// card in a Not on board column after the others." The board's one column,
// to do, shows dev:ready; once crew took the issue it carries dev:running,
// which no column shows, while its session runs. So a Not on board column,
// drawn right of to do, holds its card, and to do does not.
func TestScreenNotOnBoardIssue(t *testing.T) {
	sc, n := newScenarioWith(t, readyOnlyConfig)
	release := make(chan struct{})
	sc.Claude.Script(prompt, heldSession(release))
	sc.Start()
	waitForLabel(sc, n, running)
	sc.Screen().WaitFor(t, func(text string) bool { return inColumn(text, notOnBoard, title) }, timeout)
	text := sc.Screen().WaitStable(t, settle, timeout, masks()...)
	if !inColumn(text, notOnBoard, title) {
		t.Errorf("the %s column has no card for %q:\n%s", notOnBoard, title, text)
	}
	if inColumn(text, toDo, title) {
		t.Errorf("the %s column has a card for %q, which carries %s:\n%s", toDo, title, running, text)
	}
	if !headerBefore(text, toDo, notOnBoard) {
		t.Errorf("the %s column is not drawn after the %s column:\n%s", notOnBoard, toDo, text)
	}
	wantOneFrame(t, text)
	harness.MatchSnapshot(t, "not-on-board-issue", text)
	close(release)
	waitForLabel(sc, n, success)
	stop(sc)
}
