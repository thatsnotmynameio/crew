---
title: The live view's Events takes a fixed height, and a short window gives up whole board cards next
date: 2026-10-04
last_updated: 2026-10-05
category: design-patterns
module: internal/ui/tui
problem_type: design_pattern
component: frontend
severity: medium
applies_when:
  - "Changing how tall a section of the live view is, or the order in which a short window takes rows back"
  - "Adding a section to the live view that can grow while crew runs"
  - "Changing how many rows a board card takes, or how many cards a column shows"
  - "Writing a TUI test that renders at height 0 or at a small fixed height and asserts what Events or the board show"
tags: [live-view, tui, events, board, cards, height-budget, scroll, fixed-height, golden, layout]
---

# The live view's Events takes a fixed height, and a short window gives up whole board cards next

## Context

The live-view dashboard plan gave every section its natural height and let a short window take rows back, Events first, then Handled (KTD8 in `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md`). Events has one row per recent event and Handled had one or more rows per handled issue, so both grew while crew ran, and everything under them moved: the key-help line crept down on every event. #108 overturned KTD8 for these two sections: each took 5 rows, filled from the top with blank rows below, and scrolled past 5.

#151 then removed the Handled section, and the Actions section with it. Handled is now the board's last column, Queues sits beside Events in one band, and each board card is a bordered card of 6 rows (`cardRows`, `internal/ui/tui/layout.go:23`). The fixed height now covers Events alone, and the board's cards became the next thing a short window gives up (KTD10 in `docs/plans/2026-10-05-1450-feat-board-cards-plan.md`). Neither earlier plan is updated after it ships, so this note is where the rule lives.

## Guidance

- **Start the budget from the fixed height.** `budget()` starts Events at `scrollRows` (5) and each board column at `maxCards` (5) cards (`internal/ui/tui/layout.go:80-81`). With the height unknown (`m.height <= 0`) it stops there and shrinks nothing, so the first frame is not up to 100 events that then jumps to 5.
- **A short window gives rows back in this order** (`budget()`, `layout.go:80-108`, then `fitted()`, `layout.go:65-77`, for the cut):
  1. Events, from 5 rows down to `minScroll` (2), one row at a time: `max(minScroll, scrollRows-over)`.
  2. The board's cards, from 5 a column down to 1, in whole cards. A capped column saves `cardRows` rows per card it drops and gains a `+N more` row, unless `maxCards` already gave it one, so the cap is `max(shown-(over+more+cardRows-1)/cardRows, 1)`. One row over the window takes a whole card, 6 rows.
  3. The Bots cards collapse to their one-row strip (`botCards = false`).
  4. Only then is the view cut, with the `… N lines cut` row and the key-help line.
  `budget()` measures how far over the window the view is once, and again only after a step changed the budget. Each measure renders every section.
- **Pad inside the section.** `eventsSection(n)` (`internal/ui/tui/events.go:14`) returns exactly `n` rows: it slices when there are more and pads with `filled` (`layout.go`) when there are fewer. `band(n)` draws Queues at its natural width beside Events (`layout.go:139`), so the band is as tall as the taller of the two. Queues has one row per queue and does not scroll, so it only sets the band's height when a config declares more queues than Events has rows.
- **An empty Events keeps `minScroll` rows in a short window**: "none" and a blank. Starting from a fixed height dropped the old `max(min(minScroll, count), count-over)` guard, which let an empty section keep 1 row.

## Why This Matters

The point of the fixed height is that the key-help line stays on one row from the moment crew starts. Any section that sizes itself by its content brings the jumping back. A board column's cards also grow while crew runs, but they all have the same height and are capped at `maxCards`, so the board grows in known steps that the budget counts.

The consequences are easy to miss because they only show in tests. Every test that pins a window height near a budget boundary, and every golden file of a fitted view, moves when a section's rows or the give-up order change:

- #108: `TestTheCardCapCountsOnlyTheDrawnColumns` moved from heights 20 and 21 to 22 and 23, because Events and Handled each kept one row more. The plan's feasibility review found this by prototyping the change and running the suite; the plan itself had missed it.
- #151: that test now sits at 21 and 22 (`internal/ui/tui/board_test.go:425`; it was at 25 and 26 just before #151). `TestAShortWindowTakesOneCardOffACappedColumn` (`board_test.go:359`) now derives its height from the budget, `len(rows(least)) - cardRows`, rather than pinning a number. The fitted-view golden moved with its window: `fit-33-rows.golden` became `fit-51-rows.golden`, rendered by `TestA51RowWindowShrinksEventsToTheirMinimum` (`internal/ui/tui/layout_test.go:62`), a window 3 rows short of everything, so Events sits at its minimum. `TestAShrinkingWindowGivesRowsUpInOrder` (`internal/ui/tui/budget_test.go:43`) walks each step's boundary from `eventfulRows`, which is built from the same constants (`budget_test.go:19`).

Deriving a test's height from the budget's constants, as the last two do, keeps it on its boundary through the next layout change. A literal height silently becomes a test of a different boundary.

## When to Apply

- Before adding a section whose row count grows at run time: give it a fixed height in the budget, or accept that it moves what is under it.
- Before changing `scrollRows`, `minScroll`, `cardRows` or `maxCards`, or the give-up order: every test that pins a window height near a budget boundary, and every golden file, moves with them. Prefer heights computed from those constants.
- Before putting a second section beside Events in the band: the band is as tall as its taller side, so that side needs a fixed height too, or a short window cannot win its rows back.

## Examples

Before #108, with two events in a tall window, Events was 2 rows tall and a third event pushed the key-help line down a row. Now the same window, 72 columns wide, shows Events at 5 rows, with Queues beside it:

```text
Queues ─── 0 of 3 busy   Events ─────────────────────────────── 2 events
 default  □□  0/2         14:29:58 poll: listed 1 issue, took 0
 clerk    □   0/1         14:29:59 poll: listed 2 issues, took 0



q stop · tab focus · ←→↑↓ move · enter open · ? help
```

A third event takes the third row, and the key-help line stays where it was. In a window 4 rows short of a board whose tallest column holds more than five cards, Events gives 3 rows (down to 2) and that column drops one card, 6 rows, so the view has 5 rows to spare: the budget cannot take back part of a card. With five cards or fewer the column also gains its `+N more` row, and 4 rows are left.

## Related

- `docs/solutions/logic-errors/handled-drops-issue-a-stage-holds-again.md`: which entries Handled keeps (#109). That doc covers which issues Handled shows; this one covers how many rows the view gives each section. Both are dashboard-plan decisions an issue overturned.
- `docs/plans/2026-10-04-1041-feat-fixed-height-events-handled-plan.md`: the plan for #108, with the Product Contract and KTD1 to KTD4.
- `docs/plans/2026-10-05-1450-feat-board-cards-plan.md`: the plan for #151, whose KTD1 sets the card's 6 rows and KTD10 the give-up order.
- #108, #151, and #33, the issue the dashboard plan came from.
