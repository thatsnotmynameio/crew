package tui

import (
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestHandledIssuesRenderTheGoldenView(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(handledSnapshot()))

	golden(t, "handled", h.view())
}

// Covers R5 and R16: attention first, each entry with its pill, then its
// reasons after an ×.
func TestHandledPutsAttentionFirstWithAPillPerEnding(t *testing.T) {
	view := fitted(t, 160, 0, handledSnapshot())

	gu := strings.Index(view, " GIVEN UP ")
	na := strings.Index(view, " NEEDS ATTENTION ")
	rm := strings.Index(view, " READY TO MERGE ")
	if gu < 0 || na < 0 || rm < 0 || gu > rm || na > rm {
		t.Errorf("view does not put the given-up and failed entries before the successes:\n%s", view)
	}
	contains(t, view,
		"× move to ready to review given up: issue closed",
		"× tests failed: exited 1: tests fail",
		"× code failed: prompt did not render",
	)
}

// Covers R16: each entry shows its stage and time, spend and pull request.
func TestAnEntryShowsItsStageTimeCostTokensAndPullRequest(t *testing.T) {
	view := handledView(t, acted(entry("31", "Add login form", "development", "crew:development:waiting review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")}))

	contains(t, view, " WAITING REVIEW   #31 Add login form  development 10m00s · $12.40 · 17.2M tokens · #45")
}

func TestAnEntryWhoseActionOpenedNoPullRequestSaysSo(t *testing.T) {
	view := handledView(t, acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: noPullRequest}))

	contains(t, view, "development 10m00s · $12.40 · 17.2M tokens · no pull request")
}

func TestATwoActionEntryShowsThePartialCostAndEachPullRequest(t *testing.T) {
	view := handledView(t, acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")},
		core.HandledAction{Name: "docs", Spend: crew.Usage{}.Spend(), PullRequest: noPullRequest}))

	contains(t, view,
		"development 10m00s · $12.40 (partial) · 17.2M tokens (partial)\n",
		"    lfg: #45\n",
		"    docs: no pull request\n",
	)
}

func TestAnEntryWithNoCostReportedSaysSo(t *testing.T) {
	tokens := crew.Usage{Tokens: crew.Tokens{Input: 300, CacheRead: 17_000_000}, HasTokens: true}.Spend()
	view := handledView(t, acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: tokens, PullRequest: found("#45")}))

	contains(t, view, "development 10m00s · cost not reported · 17M tokens · #45")
}

func TestAnActionThatNeverHadASessionShowsNoSpendOrPullRequest(t *testing.T) {
	view := handledView(t, acted(failedEntry("5", "Parse the config once", 40, 30, "code", "prompt did not render"),
		core.HandledAction{Name: "code"}))

	contains(t, view, "#5 Parse the config once  implement 10m00s\n")
}

// A pull request found links to its page (R6).
func TestAFoundPullRequestLinksToItsPage(t *testing.T) {
	h := newHarness(t, 160)
	u := runningSnapshot()
	u.Snapshot.Handled = []core.HandledView{acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(1, 1), PullRequest: found("#45")})}

	h.send(updateMsg(u))

	contains(t, h.raw(), "https://github.com/o/r/pull/45")
}

func TestAReasonWithNewlinesTakesOneRow(t *testing.T) {
	view := handledView(t, failedEntry("5", "Parse", 40, 30, "tests", "exited 1:\n  tests fail\n"))

	contains(t, view, "× tests failed: exited 1: tests fail\n")
}

// Covers R16: the summary counts the entries and this run's spend,
// including that of entries Handled no longer shows.
func TestTheHandledSummaryShowsTheCountAndThisRunsCost(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Spent = spent(3, 800_000).Add(spent(12.4, 17_200_000))
	u.Snapshot.Handled = []core.HandledView{acted(entry("31", "Add login form", "development", "ready to review", 15, 5),
		core.HandledAction{Name: "lfg", Spend: spent(12.4, 17_200_000), PullRequest: found("#45")})}

	view := fitted(t, 120, 0, u)

	contains(t, view, "─ 1 · $15.40\n")
}

func TestWithNothingHandledHandledSaysNone(t *testing.T) {
	view := fitted(t, 80, 0, runningSnapshot())

	contains(t, view, "Handled ", "─ 0\n")
	if !strings.Contains(view, "   none") {
		t.Errorf("view lacks Handled's none:\n%s", view)
	}
}

// Covers R16: each queue's busy and free slots.
func TestTheQueuesSectionShowsEachQueuesBusyAndFreeSlots(t *testing.T) {
	view := fitted(t, 80, 0, runningSnapshot())

	contains(t, view, "Queues ─── 2 of 3 busy", "\n default  ■□  1/2", "\n clerk    ■   1/1")
}

func TestAQueueOfNoSlotsStillHasItsRow(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.Queues = []core.QueueView{{Name: "review", Slots: 2, Busy: 1}, {Name: crew.DefaultQueue}}

	view := fitted(t, 80, 0, u)

	contains(t, view, "\n default      0/0")
}

func TestBeforeTheFirstUpdateTheQueuesSectionShowsNone(t *testing.T) {
	h := newHarness(t, 80)

	contains(t, h.view(), "Queues ", "\n none ")
}
