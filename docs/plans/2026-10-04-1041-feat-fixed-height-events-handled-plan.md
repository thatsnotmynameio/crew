---
title: Events and Handled take a fixed height - Plan
type: feat
date: 2026-10-04
topic: fixed-height-events-handled
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #108
execution: code
---

# Events and Handled take a fixed height - Plan

## Goal Capsule

- **Objective:** in a window with room for the whole live view, the view holds still while events arrive and issues are handled. A section moves only when the board, Actions or the warnings change.
- **Means:** Events and Handled each take a fixed 5 rows and scroll through the rest (R1, R4). The layout's height budget starts both sections at 5 rows instead of at all their rows (KTD1), and each section pads itself to the rows it was given (KTD2).
- **Product authority:** the boss, through the brainstorm of #108, whose body is the Product Contract below. For Events and Handled, this work replaces the rule in `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md` (KTD8) that sections take their natural height. The order in which a short window takes rows back stays as KTD8 set it.
- **Open blockers:** none. #109, which #108's triage recorded as its blocker, is closed (#115).
- **Execution profile:** the TUI renderer only (`internal/ui/tui`), its tests and golden files, and the "Run it" section of `docs/guide/crew.mdx`. No change to the engine, the snapshot or the config.
- **Stop conditions:** stop and report if a fixed height needs anything the engine's snapshot does not already carry.
- **Who ships:** the implementer opens one pull request that closes #108. Merging is the boss's.

---

## Product Contract

Product Contract preservation: carried from #108's body with its IDs and meaning unchanged. AE6's "each shrink to 2 rows" is read through R7, which governs it: see Assumptions.

### Summary

Events and Handled each take 5 rows under their title. Their rows fill from the top, with blank rows below until there are 5. Once a section has more than 5 rows it scrolls as it does today. A short window still takes rows from Events first, then Handled, down to 2 rows each.

### Problem Frame

The boss watches crew work in the live view. Events has one row per recent event, up to 100, and Handled has one row per handled issue plus its reasons and pull requests. Both start at one "none" row and grow as crew runs. Events grows on every event, so the key-help line under it keeps moving down. Handled sits above Events, so each handled issue moves Events and everything under it. The view only holds still once it fills the window and the height budget starts taking rows back.

### Key Decisions

- **Events and Handled have a fixed height of 5 rows and scroll.** Governs R1, R3, R4, R6. (session-settled: user-directed — chosen over one row per entry, today's behavior, which #108 describes: a section that changes height moves everything under it.)
- **Rows fill from the top, with blank rows below.** Governs R2, R5. (session-settled: user-directed — chosen over pinning the newest event to the last row with the blank rows above, and over growing up to 5 rows: it reads like a log being filled in, and the section's height never changes.)
- **A short window still takes rows from Events first.** Governs R7. (session-settled: user-directed — chosen over Events giving way last and over Events never shrinking: Events sits right above the key-help line, so when it shrinks nothing under it moves.)
- **Handled gets the same treatment as Events.** Governs R4, R5, R6. (session-settled: user-directed — chosen over changing Events only: Handled sits above Events, so its growth still moved the view.)
- **Handled has 5 rows, like Events.** Governs R4. (session-settled: user-directed — chosen over 3 and 8 rows: both scroll sections are the same size.)
- **The 5 rows are set in code.** Governs R8. (session-settled: user-directed — chosen over a `.crew/config.yaml` key: nobody has needed another height, so a config key would be unused surface.)

### Requirements

**Events**

- R1. Events takes 5 rows under its title whenever the window has room for them, whatever the number of events.
- R2. While Events has fewer than 5 events, they fill the rows from the top, oldest first and newest last, and the rows below them are blank. With no events, the first row says "none" and the other 4 are blank.
- R3. With more than 5 events, Events shows 5 of the latest 100 and scrolls as it does today: it follows the newest event until the boss scrolls up, and the title says `↑↓ scroll`.

**Handled**

- R4. Handled takes 5 rows under its title whenever the window has room for them, whatever the number of handled issues. The Queues and Handled band is as tall as the taller of the two.
- R5. While Handled has fewer than 5 rows, they fill from the top in today's order, issues that need attention first, and the rows below them are blank. With no handled issue, the first row says "none" and the other 4 are blank.
- R6. With more than 5 rows, Handled shows 5 and scrolls as it does today.

**Short window**

- R7. When the window is too short for the whole view, rows are taken back in KTD8's order: from Events first, then Handled, then the session lines under Actions, then the board's cards, and only then is the view cut. Events and Handled give up only as many rows as the window needs, and keep at least 2 each.

**Height and docs**

- R8. Both heights are 5 rows, set in code. `.crew/config.yaml` gets no key for them.
- R9. The "Run it" section of `docs/guide/crew.mdx` says Events and Handled each take 5 rows, filled from the top with blank rows below, and scroll past that. Its example view and its text on short windows match.

### Acceptance Examples

- AE1. **Covers R1, R2.**
  - **Given:** a tall window and 2 events.
  - **When:** the view is drawn.
  - **Then:** Events shows its title, the 2 events, then 3 blank rows, then the key-help line. When a third event arrives it takes the third row, and the key-help line does not move.
- AE2. **Covers R2.**
  - **Given:** a tall window, just after crew starts, with no events.
  - **Then:** Events shows "none" on its first row and 4 blank rows.
- AE3. **Covers R3.**
  - **Given:** a tall window and 30 events.
  - **Then:** Events shows the newest 5, its title reads `30 events · ↑↓ scroll`, and its height is the same as in AE1.
- AE4. **Covers R4, R5.**
  - **Given:** a tall window, 2 queues, and one handled issue whose failed action adds a reason row.
  - **Then:** Handled shows the issue's row, its reason row and 3 blank rows. The band is 6 rows tall (title and 5 rows), and stays 6 when a second issue is handled.
- AE5. **Covers R4.**
  - **Given:** a configuration with 7 queues.
  - **Then:** Queues sets the band's height at 8 rows (title and 7 queues). Handled shows its 5 rows at the top of its side, and the band does not grow when issues are handled.
- AE6. **Covers R7.**
  - **Given:** an 80×24 window with many handled issues and 30 events.
  - **Then:** Events and Handled each shrink to 2 rows and scroll, and the view is 24 lines, as today.
- AE7. **Covers R7.**
  - **Given:** a window one row too short for the whole view.
  - **Then:** Events shows 4 rows and Handled keeps 5.

### Scope Boundaries

- A config key for the number of rows is deferred until someone needs another height.
- The board, Actions, Queues and the warnings keep their natural height. They still move what is under them when they change.
- Pinning the key-help line to the bottom of the window, with the view filling every row, is not part of this work.
- How many events crew keeps (the latest 100) stays as it is.
- While the boss is scrolled up in Events, each new event still shifts the rows shown by one, as it does today. Pinning the scrolled view to the same events is not part of this work.
- `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md` is not edited: a plan is a point-in-time record, and this plan's Goal Capsule records that KTD8's natural height no longer holds for Events and Handled.

### Success Criteria

- In a window tall enough for the whole view, the key-help line stays on the same row from the moment crew starts while events arrive and issues are handled.

### Sources / Research

- `internal/ui/tui/events.go`: `eventCount` and `eventsSection`, the rows Events takes today.
- `internal/ui/tui/layout.go`: `budget`, `rows` and `band`, the height budget, the order of giving way and `minScroll`.
- `internal/ui/tui/band.go`: `handledSection`, where one handled issue can take several rows.
- `internal/ui/tui/keys.go`: `scrollLimit`, which reads the budget to clamp the scroll offsets.
- `internal/ui/tui/testdata/`: every golden view shows Events; `resuming.golden` and `winding-down.golden` show fewer than 5 events, and `fit-24-rows.golden` shows the short window.
- `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md`: R21, KTD8 and KTD11, the scrolling and height rules this work changes.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The budget starts Events and Handled at 5 rows, and gives rows back from there.** A `scrollRows = 5` constant sits beside `minScroll` in `internal/ui/tui/layout.go`. `budget()` starts `events` and `handled` at `scrollRows` instead of `-1` ("all"), and a short window lowers each to `max(minScroll, scrollRows - over)`, Events first, then Handled, then the said lines and the cards as today (R7). The `min(minScroll, count)` guard goes, because a section's height no longer depends on its row count. This instantiates the fixed-height and short-window Key Decisions (R1, R4, R7, R8).
- KTD2. **Each section pads itself to the rows the budget gave it.** `eventsSection(n)` and the Handled side of `band(n)` always return exactly `n` rows: the shown rows first, then blank rows (R2, R5). The "none" row counts as the first row. The band's height then follows from `max(len(lefts), len(rights))` with no other change (R4, AE5). Padding inside the section, rather than in `rows`, keeps the scroll slicing and the padding in one place per section.
- KTD3. **The fixed height applies whether or not the window's height is known.** When the height is unknown (`m.height <= 0`, before the first `WindowSizeMsg`), the budget skips only the shrinking and the cut; Events and Handled still take their 5 rows. Rejected: keeping "everything" before a window size, which would draw a first frame of up to 100 events that then jumps to 5, the movement #108 removes. Tests that render at height 0 and assert Handled rows past the fifth (for example `TestHandledPutsAttentionFirstWithAPillPerEnding`, `TestWithoutColourSectionsAndStatesStillReadApart`) move to asserting on `handledSection`'s full rows, or on fewer entries, rather than on the view. `TestBeforeAWindowSizeTheViewShowsEverything` becomes a test that the view is not cut before a window size and that Events shows its 5 rows.
- KTD4. **`scrollLimit` reads the budget's counts directly.** With the budget's counts never negative, `scrollLimit` in `internal/ui/tui/keys.go` drops its `>= 0` guards and returns the section's rows past its budget (R3, R6). `eventCount` stays only as the count of event rows (or 1 for "none") for that limit, or is folded into it.

### Assumptions

- AE6 reads through R7. In the 80×24 window of `fit-24-rows.golden`, starting both sections at 5 rows makes the view 4 rows too tall, so Events gives up 3 rows (to 2) and Handled 1 (to 4). That is the view the golden file already holds, so `fit-24-rows.golden` is expected unchanged, and the test checks Events at 2 rows and Handled at 4. AE6's "each shrink to 2" holds only in a window short enough to need it.
- Blank rows are empty strings in Events and a padded empty cell on Handled's side of the band, as the band already draws when Queues is taller than Handled.

### Sequencing

U1 changes the layout and its tests. U2 regenerates the golden files and checks the diff. U3 updates the guide from the regenerated views.

---

## Implementation Units

### U1. Fixed-height Events and Handled in the layout

- **Goal:** Events and Handled take 5 rows each, padded from the top, and a short window takes rows back in KTD8's order down to 2 each.
- **Requirements:** R1, R2, R3, R4, R5, R6, R7, R8; AE1 to AE7; KTD1, KTD2, KTD3, KTD4.
- **Dependencies:** none.
- **Files:**
  - Modify: `internal/ui/tui/layout.go`, `internal/ui/tui/events.go`, `internal/ui/tui/keys.go`
  - Test: `internal/ui/tui/layout_test.go`, `internal/ui/tui/band_test.go`, `internal/ui/tui/board_test.go`, `internal/ui/tui/helpers_test.go`
- **Approach:**
  1. Add `scrollRows` and start the budget from it (KTD1); keep the order of giving way.
  2. Pad `eventsSection` and Handled's rows in `band` to the budget's `n` (KTD2).
  3. Drop the `-1` paths for Events and Handled in `rows`, `band` and `scrollLimit` (KTD3, KTD4).
  4. Move the height-0 tests that read Handled past its fifth row to `handledSection` or fewer entries (KTD3).
  5. Move `TestTheCardCapCountsOnlyTheDrawnColumns` in `board_test.go` from heights 20 and 21 to 22 and 23: an empty Events or Handled now keeps `minScroll` (2) rows in a short window where it kept 1, so the same card-cap boundary sits 2 rows lower (KTD1).
- **Patterns to follow:** the existing `fitted`, `eventful`, `manySnapshot` and `harness` helpers; tests named as sentences with a `// Covers R…` / `AE…` comment, as in `layout_test.go`.
- **Test scenarios:**
  - Covers AE1. A 40-row window with 2 events: Events shows the 2 events then 3 blank rows before the blank line and the key-help line. Adding a third event leaves the key-help line on the same row and the view the same number of lines.
  - Covers AE2. A 40-row window with no events: Events' first row is "none" and the next 4 are blank.
  - Covers AE3. A 40-row window with 30 events: Events shows `listed 26 issues` to `listed 30 issues`, not `listed 25`, its title reads `30 events · ↑↓ scroll`, and the view has as many lines as with 2 events.
  - Covers AE4. A 40-row window with 2 queues and one failed entry with one reason: Handled shows the entry, its `×` reason row and 3 blank rows, and the band is 6 rows. Adding a second handled entry keeps the band at 6 rows.
  - Covers AE5. Seven queues: the band is 8 rows, Handled's 5 rows sit at its top, and handling more issues does not grow it.
  - Covers AE6. The existing 80×24 test with `eventful()`: the view is 24 lines, Events shows 2 rows and Handled 4, and both titles say `↑↓ scroll`.
  - Covers AE7. Render a snapshot at height 0 to get its full line count L, then at L−1: Events shows 4 rows and Handled 5.
  - Before a window size, the view is not cut and Events shows 5 of 30 events with `↑↓ scroll`.
  - Scrolling still works: `TestFocusAndScrollMoveHandledAndEvents` and `TestPageKeysScrollTheFocusedSectionByAPage` pass, and in a tall window `home` on Events reaches `listed 1 issue,`.
  - A section with fewer rows than its budget does not scroll: with 2 events and Events focused, `↑` changes nothing.
  - The narrow and tiny windows of `TestANarrowShortWindowRendersWithoutPanicking` still render without panicking and fit.
- **Verification:** `go test -race ./internal/ui/tui` passes apart from the golden files U2 regenerates.

### U2. Regenerate the golden views

- **Goal:** the golden files show the fixed heights.
- **Requirements:** R1, R2, R4, R5, R6, R7.
- **Dependencies:** U1.
- **Files:**
  - Modify: `internal/ui/tui/testdata/running.golden`, `warning.golden`, `resuming.golden`, `winding-down.golden`, `handled.golden`
  - Expected unchanged: `internal/ui/tui/testdata/fit-24-rows.golden`
- **Approach:** regenerate with `go test ./internal/ui/tui -update` and review the diff against these expectations:
  1. `running`, `warning`: Handled "none" plus 4 blank rows; Events unchanged at 5 events.
  2. `resuming`: Events 2 events plus 3 blank rows; Handled "none" plus 4 blank rows.
  3. `winding-down`: Events 1 event plus 4 blank rows; Handled "none" plus 4 blank rows.
  4. `handled`: Handled shows its first 5 rows with `↑↓ scroll` in its title; Events unchanged.
  5. `fit-24-rows`: no change (see Assumptions).
- **Test scenarios:** Test expectation: none -- the golden tests in U1's packages are the checks; this unit is their regenerated fixtures.
- **Verification:** `go test -race ./internal/ui/tui` passes, and the golden diff shows only the changes listed above.

### U3. The guide's "Run it" section

- **Goal:** the guide describes the fixed heights and shows them in its example view.
- **Requirements:** R9.
- **Dependencies:** U2.
- **Files:** Modify: `docs/guide/crew.mdx`
- **Approach:**
  1. In the example view, pad Handled to 5 rows (its 4 rows plus 1 blank) and Events to 5 rows (its 3 events plus 2 blank rows).
  2. Replace "Events shows the latest 100 events, the newest at the bottom." with text saying Events and Handled each take 5 rows, filled from the top with blank rows below, and scroll once they have more; Events keeps the latest 100 events, the newest at the bottom.
  3. Change the short-window paragraph so Events and then Handled give up rows, down to two each, and scroll.
- **Test scenarios:** Test expectation: none -- documentation; `pnpm docs:check` checks its links and MDX.
- **Verification:** the example view matches the shape of the regenerated golden files, and `pnpm docs:check` passes.

---

## Verification Contract

| Gate | Command | Applies to |
| --- | --- | --- |
| Tests | `go test -race ./...` | U1, U2 |
| Golden files | `go test ./internal/ui/tui -update`, then review the diff | U2 |
| Format | `gofmt -l cmd internal tools` prints nothing | U1 |
| Vet | `go vet ./...` | U1 |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | U1 |
| Coverage | `go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...`, then `go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml` (total ≥ 90%) and `git diff -U0 origin/main...HEAD \| go run ./tools/diffcover -profile coverage.out` (changed lines ≥ 90%) | U1 |
| Docs | `pnpm docs:check` | U3 |

---

## Definition of Done

- Every scenario in U1 has a test, and AE1 to AE7 each have a test that names them.
- The golden diff matches U2's list, and `fit-24-rows.golden` is unchanged.
- The guide's "Run it" section matches R9.
- Every gate in the Verification Contract passes with zero findings.
- No dead code from abandoned attempts is left in the diff, and nothing outside `internal/ui/tui`, its testdata and `docs/guide/crew.mdx` changed apart from this plan.
