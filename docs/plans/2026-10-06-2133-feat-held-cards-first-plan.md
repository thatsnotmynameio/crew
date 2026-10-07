---
title: Keep held items at the top of the board's columns - Plan
type: feat
date: 2026-10-06
topic: held-cards-first
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #231
execution: code
---

# Keep held items at the top of the board's columns - Plan

## Goal Capsule

- **Objective:** in the live view, the issues crew is working on are always at the top of their board column, so the boss sees them without scrolling past idle cards.
- **Means:** `cards()` lists the configured columns' cards of held items before the others, keeping board order inside each group (KTD1).
- **Product authority:** the boss, through the brainstorm of #231. The Product Contract wins on behaviour; the KTDs win on mechanism.
- **Stop conditions:** stop and report if a settled Key Decision proves unworkable, or if a gate in the Verification Contract cannot pass without changing a requirement.
- **Execution profile:** one branch, one pull request carrying `Closes #231`. Changed acceptance snapshots are accepted by the tester, not the developer.

---

## Product Contract

Product Contract preservation: changed: R4 — #230 merged (#233) and removed the Handled column, so R4 now names only the Not on board column. No other change.

### Summary

Within each configured board column, the cards of items crew holds come first, then the other cards. Each group keeps today's order: board order, oldest first. Nothing else about the board changes.

### Problem Frame

`cards()` lays out each column item by item in board order, oldest first, whether or not crew holds the item. A held card can sit below cards where nothing is happening, and when a column holds more cards than fit, the held card is scrolled out of sight.

### Key Decisions

- **"Running" means held by crew, not only the running claim.** A card moves once when crew takes the item and once when crew lets it go, not on every claim change. Governs R3. (session-settled: user-directed — chosen over only the `running` claim: a card would move several times in one run.)
- **Stable partition, no new ordering.** The boss asked to keep everything else as it is. Governs R1, R2. (session-settled: user-directed — chosen over a new sort key or a config toggle: the boss asked to keep everything else as it is.)

### Requirements

**Order**

- R1. In each configured column, every card of an item crew holds comes before every card of an item it does not hold.
- R2. Within the held group and within the not-held group, cards keep the order the board uses today: item by item in board order, oldest first.
- R3. A card counts as held whenever crew holds its issue, whatever its claim: taking, running, stopping, judging or waiting to retry.
- R4. The Not on board column keeps its current order.

**Live updates**

- R5. When crew takes or lets go of an item, its card moves to its new place on the next refresh, and the highlight stays on the card it was on.

**Docs**

- R6. The README's description of the board says that held cards come first in each column.

### Acceptance Examples

- AE1. **Covers R1, R2.** **Given** a column with #10, #12 and #15 in board order, oldest first, and crew holding #15 and #12, **then** the column shows #12, #15, #10.
- AE2. **Covers R3.** **Given** crew is judging #15's verdict, **then** #15's card is still in the held group.
- AE3. **Covers R5.** **Given** the highlight is on #10 and crew takes #10, **then** #10 moves to the top of the held group and stays highlighted.
- AE4. **Covers R5.** **Given** crew lets go of #12 and #12 keeps a label of the same column, **then** its card returns to its oldest-first place among the not-held cards.

### Scope Boundaries

- No new sort keys: no ordering by priority, by start time or by claim.
- No config option to turn the ordering off.
- Considered and not built: a scroll-to-top of the highlighted column when its card moves up. `shownCards` already starts the highlighted column from `shownFrom(sel.top, sel.row, …)`, which keeps the highlighted card drawn at its new row. Evidence that would change this: a test showing the moved, highlighted card scrolled out of sight.

#### Deferred to Follow-Up Work

- A screen scenario in `acceptance/scenarios/screen/` for the new order belongs to the tester (`/cw-tester`), who owns the scenarios and their snapshots.

### Sources / Research

- `internal/ui/tui/board.go` (`cards()`): builds each column's cards item by item in board order. The `held` field already marks held cards (it is set from `m.snap.Issues`, whatever the claim, which is R3).
- `internal/ui/tui/selection.go` (`repaired`): finds the highlighted card again by its key in its column and takes its new row. This is the mechanism R5 relies on.
- `internal/core/model.go`: the `Claim` states R3 lists (`ClaimTaking`, `ClaimRunning`, `ClaimStopping`, `ClaimJudging`, `ClaimOwed`).

---

## Planning Contract

### Key Technical Decisions

- KTD1. **Partition in `cards()`, before the Not on board cards are appended.** Every consumer of the board's order (`byColumn`, `boardRows`, `selection.repaired`, `moveRow`, `moveColumn`, the popup's `walk`, `selected`) reads `cards()`, so one change there reorders the drawing, the highlight's rows and the popup's ←→ walk alike. Partitioning the whole configured-column slice stably by `held` gives a per-column partition because `byColumn` keeps slice order. The Not on board cards are appended after, untouched, which is R4. Governs R1, R2, R4.
- KTD2. **`held` alone decides the group.** The card's `held` is true for any issue in `m.snap.Issues`, whatever its claim, so the partition needs no claim check. All of one issue's cards share `held`, so an issue's cards keep their column order and `boardMemory.moved` still pairs the same columns for slides. Governs R3 through the Key Decision on "held".
- KTD3. **No change to selection code.** `repaired` already finds the highlighted key in its column at its new row. `updated` calls it on every snapshot with the reordered cards. `shownCards` scrolls the column to the new row. R5 then holds with no new mechanism. Governs R5.

### Assumptions

- The README is the only user-facing description of the board's order. No config reference describes card order.
- TUI golden files in `internal/ui/tui/testdata/` may change if one draws a held card below an idle card in the same column. Such a change is the intended order and is reviewed, not avoided.

---

## Implementation Units

### U1. Held cards first in each configured column

- **Goal:** `cards()` returns the configured columns' cards of held items before those of other items, each group in board order, then the Not on board cards.
- **Requirements:** R1, R2, R3, R4, R5 (KTD1, KTD2, KTD3).
- **Dependencies:** none.
- **Files:**
  - `internal/ui/tui/board.go` (`cards()` and its doc comment)
  - `internal/ui/tui/selection.go` (the `byColumn` comment says "each in board order"; make it say the order `cards()` gives)
  - `internal/ui/tui/board_test.go`
  - `internal/ui/tui/selection_test.go`
  - `internal/ui/tui/popup_test.go` (only if its walk test is affected)
  - `internal/ui/tui/testdata/*.golden` (only if regenerated)
- **Approach:**
  1. In `cards()`, split the configured-column cards into held and not held while building them, or stably partition the built slice. Then append the Not on board cards as today.
  2. Update the doc comments that state the order, citing #231.
  3. Run the TUI tests; regenerate any golden that changes with `-update` and check that each diff only moves held cards up.
- **Patterns to follow:** test helpers `held`, `onBoard`, `labeled`, `item`, `boardOf`, `wantLit` and `newBoardHarness` in `internal/ui/tui/board_test.go` and `selection_test.go`. A snapshot with several held issues needs `View.Issues` with one `core.IssueView` per held issue; add a small helper beside `held` if none fits.
- **Test scenarios:**
  - Covers AE1. Board items #10, #12, #15 all labeled for one column, in that order. Crew holds #12 and #15. The column draws #12, then #15, then #10.
  - Covers AE2. Crew holds #15 in `ClaimJudging`, with #10 and #12 not held and earlier in board order. #15's card is drawn first in the column.
  - R3 across claims: the same check holds for `ClaimTaking`, `ClaimStopping` and `ClaimOwed` (one table test over the claims is enough).
  - R2 within the not-held group: with nothing held, the column order is unchanged (the existing `TestAColumnsCardsGoOldestFirstAndTheNewestAreCut` still passes).
  - R1 per column: an issue held with labels for two columns comes first in both, while an idle item earlier in board order comes second in both.
  - R4: a held issue with no column card still shows in Not on board, after the configured columns, and the Not on board column's order is unchanged.
  - Covers AE3. Snapshot with #10, #12 idle in one column, highlight moved to #12 with ↓. Next snapshot holds #12. #12 is drawn first and `wantLit` still finds #12 highlighted in that column.
  - Covers AE4. Snapshot with #10 idle and #12 held, #12 drawn first and highlighted. Next snapshot no longer holds #12, which keeps its label. #12 is drawn after #10 and stays highlighted.
  - The popup's → walk follows the new column order: with #12 held and #10 idle in one column, → from #12's popup opens #10's.
- **Verification:** the scenarios above pass, the rest of `internal/ui/tui` passes, and any regenerated golden diff only moves held cards above idle cards in the same column.

### U2. README says held cards come first

- **Goal:** the README's board description says that within each column the cards of issues crew holds come first, each group oldest first.
- **Requirements:** R6.
- **Dependencies:** U1.
- **Files:** `README.md` (the live view paragraph that begins "crew shows a live view").
- **Approach:** add one sentence after "Below them, the board has a card for each issue in each of its columns: …" that states the order. Keep the README's existing style: plain, present tense, no new section.
- **Test expectation:** none -- documentation only.
- **Verification:** the sentence states R1 and R2 and matches the code from U1.

---

## Verification Contract

| Gate | Command | Applies to |
|---|---|---|
| Format | `gofmt -l cmd internal tools acceptance` prints nothing | U1 |
| Vet | `go vet ./...` | U1 |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | U1 |
| Tests | `go test -race ./...` | U1 |
| Coverage | `go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...`, then `go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml` (total ≥ 90%) and `git diff -U0 origin/main...HEAD \| go run ./tools/diffcover -profile coverage.out` (changed lines ≥ 90%) | U1 |
| Goldens | `go test ./internal/ui/tui -update` only when a golden changes; review the diff | U1 |
| Acceptance | `go -C acceptance run ./cmd/acceptance -count=1`. A failing screen snapshot from the new order is reported to the tester, not rewritten by the developer | U1 |

## Definition of Done

- AE1 to AE4 each have a passing test in `internal/ui/tui`.
- Every gate in the Verification Contract passes, apart from acceptance snapshots left to the tester, which the pull request names.
- The README states the order (R6).
- No dead or experimental code from abandoned attempts remains in the diff.
- The pull request body contains `Closes #231`.
