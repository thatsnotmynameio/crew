---
title: The board's highlight kept a stale scroll offset when its card moved up its column
date: 2026-10-06
category: logic-errors
module: internal/ui/tui
problem_type: logic_error
component: frontend
symptoms:
  - "After crew takes the highlighted issue, its card jumps to the top of a tall column, and the next ↓ scrolls that card out of sight"
  - "↓ scrolls the column by one row even though the next card was already drawn"
root_cause: logic_error
resolution_type: code_fix
severity: low
tags: [live-view, tui, board, selection, highlight, scroll, shownfrom, card-order, held-first]
---

# The board's highlight kept a stale scroll offset when its card moved up its column

## Problem

The highlight on the live view's board stores two numbers per column: the highlighted card's `row` and `top`, the first card the column shows (`internal/ui/tui/selection.go:16`). On each snapshot, `selection.repaired` finds the highlighted card again by its key and updates `row`, but it leaves `top` alone (`internal/ui/tui/selection.go:40-44`). Once the change for #231 put the cards of held issues first in each column, a highlighted card deep in a tall column jumped to row 0 whenever crew took its issue. The next ↓ then scrolled the column and hid the card the user had just left.

## Symptoms

- In a column taller than the cards it shows (at most five), highlight a card near the bottom. When crew takes its issue, the card moves to the top and stays highlighted, as it should.
- Press ↓. The highlight moves to the next card, but the column scrolls by one row, so the card that just moved up is no longer drawn.

## What Didn't Work

- The plan for #231 decided that no selection code had to change (its KTD3). It reasoned that `shownCards` already starts the highlighted column at `shownFrom(sel.top, sel.row, …)` (`internal/ui/tui/board.go:218`), which keeps the highlighted card drawn at its new row. That holds for drawing, so every test that only looked at the screen after a snapshot passed. The stale value lives in `sel.top`, and drawing never writes it back.
- The existing highlight tests, from #151, covered a card moving to another column, a card leaving the board, and a column emptying. None of them moved a card up within a column taller than it shows, so none fed the stale `top` into the next key press.

## Solution

`Model.updated` now re-derives the column's scroll offset right after repairing the highlight (`internal/ui/tui/model.go:191`):

```go
m.sel = m.sel.repaired(cards)
// ...
// The highlighted card's column scrolls to its row now, which moves
// when crew takes or lets go of its issue (R5 of #231).
if m.sel.key != "" {
	m.sel.top = shownFrom(m.sel.top, m.sel.row, len(byColumn(cards)[m.sel.column]), m.budget().cards)
}
```

`TestATakenCardFromDeepInAColumnStaysDrawnAfterDown` in `internal/ui/tui/selection_test.go` pins it down. Ten cards share one column. The test highlights #9, has crew take it, presses ↓, and checks that the highlight is on #1 and #9 is still drawn.

## Why This Works

`shownFrom(top, row, n, limit)` moves `top` only as far as it must to keep `row` drawn (`internal/ui/tui/selection.go:82-83`). Drawing calls it with the stored `top` and throws the result away, so a stale `top` never shows on screen. `moveRow` calls it with the stored `top` and saves the result (`internal/ui/tui/selection.go:157`). With `top` still at 4 and the card now at row 0, ↓ to row 1 gives `shownFrom(4, 1, 10, 5) = 1`, which scrolls row 0 out. Re-deriving `top` from the new row on every snapshot saves what drawing already showed (rows 0 to 4), so the next ↓ starts from the screen the user actually sees.

## Prevention

- Treat `selection.top` as state that must follow `row`. Any change that can move the highlighted card's row in a snapshot (reordering a column, filtering cards, a new sort) needs `top` re-derived, not just `row`. `Model.updated` now does this for every snapshot, so keep that call when refactoring `updated` or `repaired`.
- When a change reorders cards inside a column, test a key press after the reorder in a column taller than the cards it shows. A test that only checks the screen right after the snapshot passes with a stale `top`.

## Related Issues

- #231: held issues first in each column, the change that exposed this (its pull request also carries the fix). Its plan, `docs/plans/2026-10-06-2133-feat-held-cards-first-plan.md`, still records the overturned "no change to selection code" decision (KTD3).
- #151: introduced the highlight, card limits and column scrolling.
- `docs/solutions/design-patterns/live-view-scroll-sections-take-a-fixed-height.md`: how many cards a column shows, the `limit` in `shownFrom`.
