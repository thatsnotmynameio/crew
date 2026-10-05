---
title: The live view's Events and Handled take a fixed height, not their natural one
date: 2026-10-04
category: design-patterns
module: internal/ui/tui
problem_type: design_pattern
component: frontend
severity: medium
applies_when:
  - "Changing how tall a section of the live view is, or the order in which a short window takes rows back"
  - "Adding a section to the live view that can grow while crew runs"
  - "Writing a TUI test that renders at height 0 or at a small fixed height and asserts what Events or Handled show"
tags: [live-view, tui, events, handled, height-budget, scroll, fixed-height, golden, layout]
---

# The live view's Events and Handled take a fixed height, not their natural one

## Context

The live-view dashboard plan gave every section its natural height and let a short window take rows back, Events first, then Handled (KTD8 in `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md`). Events has one row per recent event and Handled one or more rows per handled issue, so both grew while crew ran, and everything under them moved: the key-help line crept down on every event. #108 overturned KTD8 for these two sections. Each now takes 5 rows, filled from the top with blank rows below, and scrolls past 5. The dashboard plan still states the old rule, and plans are not updated after they ship, so this note is where the change lives.

## Guidance

- **Start the budget from the fixed height.** `budget()` starts Events and Handled at `scrollRows` (5) rather than "all" (`internal/ui/tui/layout.go:16`, `:74`). A short window lowers each to `max(minScroll, scrollRows-over)`, Events first, then Handled (`layout.go:80`, `:83`). It still takes only what the window needs: in 80×24 with 30 events and ten handled issues, Events gives 3 rows (to 2) and Handled 1 (to 4). That is why `fit-24-rows.golden` did not change (#114's Mates section later added four rows above, and #136's bot cards five more, so that case is now 80×33 in `fit-33-rows.golden`), although #108's AE6 reads as if both go to 2.
- **Pad inside the section.** `eventsSection(n)` and the Handled side of `band(n)` return exactly `n` rows. They slice when there are more and pad with `filled` when there are fewer (`layout.go:148`). The band's height then follows from the taller of Queues and Handled with no other change.
- **The fixed height holds before the first window size too.** With the height unknown (`m.height <= 0`), the budget skips only the shrinking and the cut (`layout.go:75`). Showing everything until a size arrives would draw a first frame of up to 100 events that then jumps to 5.
- **An empty section keeps `minScroll` rows in a short window.** The old budget used `max(min(minScroll, count), count-over)`, so an empty Events or Handled kept 1 row ("none"). Starting from a fixed height drops that guard, so an empty section keeps 2 ("none" and a blank). The smallest view the budget reaches is therefore 2 rows taller than before, one for each section. Tests that probe a boundary of the budget by window height move with it.

## Why This Matters

The point of the change is that the key-help line stays on one row from the moment crew starts. Any section that sizes itself by its content brings the jumping back. The two height consequences are easy to miss because they only show in tests:

- A height-0 test that asserts something past Handled's fifth row now fails, because the view is no longer "everything".
- A board test that sits on a card-cap boundary at a given window height now finds the view cut, because the sections below the board kept one row more each.

The second was not found while writing the plan. The plan's feasibility review caught it, by prototyping the change and running the suite: `TestTheCardCapCountsOnlyTheDrawnColumns` (`internal/ui/tui/board_test.go:266`) had to move from heights 20 and 21 to 22 and 23 (`board_test.go:278`).

## When to Apply

- Before adding a section whose row count grows at run time: give it a fixed height in the budget, or accept that it moves what is under it.
- Before changing `scrollRows` or `minScroll`: every test that pins a window height near a budget boundary, and every golden file, moves with them.
- When a TUI test needs Handled's rows past the visible ones: read them from `handledSection`, as the `handledText` helper does (`internal/ui/tui/helpers_test.go:162`), rather than from the rendered view.

## Examples

Before #108, with two events in a tall window, Events was 2 rows tall and a third event pushed the key-help line down a row. Now the same window shows:

```text
Events ─────────────────────────────────────────── 2 events
 14:29:58 poll: listed 1 issue, took 0
 14:29:59 poll: listed 2 issues, took 0




q stop · tab focus · ↑↓ scroll · ←→ board · ? help
```

A third event takes the third row, and the key-help line stays where it was.

## Related

- `docs/solutions/logic-errors/handled-drops-issue-a-stage-holds-again.md`: which entries Handled keeps (#109). This doc covers how many rows Handled draws. It is another dashboard-plan decision that an issue overturned.
- `docs/plans/2026-10-04-1041-feat-fixed-height-events-handled-plan.md`: the plan for #108, with the Product Contract and KTD1 to KTD4.
- #108, and #33, the issue the dashboard plan came from.
