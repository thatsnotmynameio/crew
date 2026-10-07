---
title: Tag blocked issues as blocked on the card and in the popup - Plan
type: feat
date: 2026-10-06
topic: blocked-tag
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #229 (part 1 of 2 of #228)
execution: code
---

# Tag blocked issues as blocked on the card and in the popup - Plan

This file is the body of issue #229, as `/cw-split-plan` wrote it from the plan of #228, followed by the sections of #228's plan that belong to this part: KTD4, U1 to U3, the Verification Contract and the Definition of Done.

## Goal Capsule

- **Objective:** a person watching the live view sees each issue once, where its labels put it, and can tell at a glance which issues crew will not take because an open issue blocks them.
- **Means:** the TUI reads the `Blocked` flag every board card's issue already carries and draws it on the card and in the popup (KTD1 to KTD3); the board stops drawing the Handled column (KTD5).
- **Product authority:** the boss, through the brainstorm of #228 and the planning session that added the Handled column's removal. The Product Contract wins on behaviour; the KTDs win on mechanism.
- **Split candidate:** this plan holds two parts that each merge alone, the blocked tag and the Handled column's removal. Refinement should split it along How This Work Fits Together.
- **Stop conditions:** stop and report when a settled Key Decision proves unworkable, or when a gate in the Verification Contract cannot pass without changing a requirement.
- **Execution profile:** one branch per part, or one branch for both when refinement keeps them together. Each pull request carries `Closes` for its issue. Changed acceptance snapshots are accepted by the tester, not the developer (KTD4).
- **Part:** this issue is part 1 of 2 of #228. It ships alone because the blocked tag on the card and in the popup works by itself and the README describes it, whether or not the Handled column is still drawn.

### Summary

A blocked issue that crew does not hold shows a yellow `⊘ blocked` on its card's `run` row, in place of `○ idle`. Its popup shows a `blocked` chip, with yellow text, among the label chips, and the popup's `blocked yes/no` row goes away.

### Problem Frame

crew does not take an issue while an open issue blocks it. The live view does not show this. The card shows `○ idle`, the same as any issue crew does not hold, which reads as "crew could take this and has not yet". The popup states it only as a `blocked  yes` row among the header's other rows, where nothing sets it apart. Someone watching the board then waits on an issue that will not move, or goes looking for why.

The Handled column adds a second card for each issue whose rule ended this run, beside the card its new label already gets in its own column. The board then shows the same issue twice.

### Key Decisions

- **On the card, `⊘ blocked` replaces `○ idle`, and shows only where `○ idle` would.** (session-settled: user-directed — chosen over keeping `○ idle` and adding a `blocked` chip beside it on the card.) A held issue's card keeps showing its actions or claim as today, even when the issue is blocked. Governs R1, R2.
- **In the popup, a `blocked` chip joins the label chips, and the `blocked yes/no` row is removed.** (session-settled: user-directed — chosen over keeping the row with a highlighted `yes` and no chip.) Governs R3, R4.
- **The color is the view's yellow warning color, in both places.** It already means "waiting on something" for owed claims, waiting actions and bots that cannot act. The popup's chip keeps the usual chip background, with yellow text. (session-settled: user-directed — chosen over a filled yellow pill like the header's `STOPPING`, which is louder than the label chips, and over the red error color, which means failure.) Governs R5.
- KTD1. **The card reads `c.issue.Blocked` in `claimState`, in the branch for an issue crew does not hold.** A card whose issue crew does not hold is always a board card, whose issue comes from the board read (`ListBoard` fills `Blocked` from GitHub's `issueDependenciesSummary.blockedBy`). No core, engine or adapter change is needed. Governs R1, R2.
- KTD2. **The `blocked` chip is appended by `chips`, after the label chips, with a new chip style: the chip's background and the warning foreground.** The chip is not a label, so it is added after the de-duplicated labels rather than joining them. An issue with no labels but blocked shows the `blocked` chip alone instead of `none`. Governs R3, R5.
- KTD3. **The popup's `blocked` chip reads `Blocked` from the card's board item when the issue is on the board, and from `c.issue.Blocked` only when it is not.** `chips` already looks that item up for its labels. A held issue's view (`IssueView.Issue`) and a Handled entry's issue (`HandledView.Issue`) are the copy core made when it took the issue (`internal/core/update.go`, `take`), and core takes only unblocked issues, so their `Blocked` is always false. The board item is fresh on every poll. Governs R3.

### Requirements

**Card**

- R1. When crew does not hold an issue and the issue is blocked, its card's `run` row shows `⊘ blocked` instead of `○ idle`.
- R2. A card whose issue crew holds shows its actions or claim as today, whether or not the issue is blocked.

**Popup**

- R3. The popup of a blocked issue shows a `blocked` chip after its label chips, whether or not crew holds the issue.
- R4. The popup's header no longer has a `blocked` row. A popup of an issue that is not blocked shows no `blocked` chip.

**Look**

- R5. The card's `⊘ blocked` and the chip's text use the view's warning color, in the dark and the light theme. The chip keeps the other chips' background.

**Docs**

- R6. The README's description of the live view says that a blocked issue's card shows `blocked` in its `run` row and that its box shows a `blocked` chip with its labels, in place of "whether it is blocked".

### Acceptance Examples

- AE1. **Covers R1, R5.** **Given** an open issue in a rule's ready label that another open issue blocks, **when** the live view shows the board, **then** that issue's card shows `⊘ blocked` in yellow on its `run` row and no `idle`.
- AE2. **Covers R1.** **Given** the same issue once the issue blocking it is closed, and crew has not taken it yet, **when** the view refreshes, **then** its card shows `○ idle` again.
- AE3. **Covers R3, R4.** **Given** the blocked issue of AE1, **when** the person opens its popup, **then** the labels row shows its label chips followed by a `blocked` chip, and the header has no `blocked` row.
- AE4. **Covers R4.** **Given** an issue that nothing blocks, **when** the person opens its popup, **then** it shows no `blocked` chip and no `blocked` row.
- AE5. **Covers R2, R3.** **Given** an issue crew is running that becomes blocked mid-run, **when** the view refreshes, **then** its card still shows its running actions, and its popup shows the `blocked` chip.

### Scope Boundaries

- Naming the issues that block an issue, on the card or in the popup, is deferred.
- The `--plain` output does not change.
- When crew takes an issue and what blocks it do not change: this is the live view only.
- Core keeps recording the issues it handled (`Snapshot.Handled`): the desktop notifications read them. Only the TUI stops drawing them as a column.
- The Handled column's removal (R7 to R11, AE6 to AE8) is built in its own issue, the other part of #228.

### Sources / Research

Split from #228.

- `internal/ui/tui/card.go` (`claimState`, `runItems`, `handledCard`): the `○ idle` branch, the yellow `! owed` to mirror, and the Handled card face.
- `internal/ui/tui/detail.go` (`popupHeader`, `chips`, `popupActions`, `issueCost`): the `blocked` row, the label chips and the Handled popup's parts.
- `internal/ui/tui/board.go` (`cards`, `handledCards`, `handledColumn`, the column titles): where each card comes from and where the Handled column and its count are drawn.
- `internal/ui/tui/band.go` and `internal/ui/tui/outside.go`: the Handled helpers and the notifications that keep using core's handled list.
- `internal/ui/tui/selection.go` (`repaired`): the selection's Handled-card preference.
- `internal/ui/tui/styles.go`: the `warning`, `chipBack` and `chip` styles in both palettes.
- `docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md`: how `Blocked` is read from GitHub.

---

## Planning Contract (from #228, Part A)

- KTD4. **Only the tester rewrites acceptance snapshots (`AGENTS.md`, Tests).** Part A changes `acceptance/scenarios/screen/testdata/issue-box.snapshot`, which shows the `blocked  no` row R4 removes. Part B changes `board-running-issue.snapshot` and `handled-failed-issue.snapshot`, which show the Handled column. The developer leaves the scenarios, their snapshots and their comments in `acceptance/scenarios/screen/screen_test.go` unchanged. Each pull request names the snapshots it breaks and asks for `/cw-tester` to accept or rewrite them.

---

## Implementation Units

### U1. The card shows blocked in place of idle

- **Goal:** a blocked issue crew does not hold reads `⊘ blocked` on its card's `run` row.
- **Requirements:** R1, R2, R5, KTD1.
- **Dependencies:** none.
- **Files:** `internal/ui/tui/card.go`, `internal/ui/tui/card_test.go`, `internal/ui/tui/testdata/*.golden` only if a fixture there has a blocked issue.
- **Approach:** in `claimState`, an issue crew does not hold and that is blocked renders `⊘ blocked` in the warning style; the existing `○ idle` stays for the rest. Update the function's comment to name the new case.
- **Patterns to follow:** the `! owed` branch of `claimState`; `TestACardsBorderIsStrongOnlyWhileItsIssueRuns` in `card_test.go`, which checks a colour by looking for the style's rendered text in the raw view.
- **Test scenarios:**
  - Covers AE1. A board issue with `Blocked: true` that crew does not hold: its card's `run` row reads `⊘ blocked`, not `idle`, drawn in the dark palette's warning colour.
  - Covers AE2. The same issue with `Blocked: false`: the row reads `○ idle`.
  - Covers AE5. A held, running issue with `Blocked: true`: the row shows its running action, with no `blocked`.
- **Verification:** the scenarios pass; any golden file changes only where a blocked card is drawn, reviewed after `go test ./internal/ui/tui -update`.

### U2. The popup shows a blocked chip and drops the blocked row

- **Goal:** a blocked issue's popup shows a yellow `blocked` chip after its labels, and no popup has a `blocked` row.
- **Requirements:** R3, R4, R5, KTD2, KTD3.
- **Dependencies:** none.
- **Files:** `internal/ui/tui/detail.go`, `internal/ui/tui/styles.go`, `internal/ui/tui/popup_test.go`.
- **Approach:**
  1. Add a blocked chip style in `styles.go`, built like `chip` with the palette's warning foreground, so both themes get it.
  2. In `chips`, append the blocked chip after the label chips when the issue is blocked, read as KTD3 says; return it alone instead of `none` when there are no labels.
  3. In `popupHeader`, remove the `blocked` label and value, and update both functions' comments.
- **Patterns to follow:** the `chip` style and the existing `TestThePopupHeaderShowsTheIssuesFields`, whose `headerIssue` is already blocked.
- **Test scenarios:**
  - Covers AE3. The blocked `headerIssue` with labels `in progress` and `bug`: the labels row reads `in progress`, `bug`, then `blocked`, and the raw output has the warning foreground on the chip background.
  - Covers AE3. The header has no `blocked` row: no header row starts with `blocked `. The `field` helper fails the test on a missing row, so this check scans the rows itself.
  - Covers AE4. The same issue with `Blocked: false`: no `blocked` chip and no `blocked` row.
  - A blocked issue with no labels: the labels row shows the `blocked` chip alone, not `none`.
  - Covers AE5. A held issue whose view's issue is unblocked, as core always records it, while its board item is blocked: its popup shows the chip.
  - A Handled card whose entry's issue is unblocked while the same issue on the board is blocked: its popup shows the chip. Drop this scenario when U4 is on the same branch.
- **Verification:** the scenarios pass; the test that today reads `blocked` as `yes`/`no` is rewritten to the chip, not deleted.

### U3. The README describes the tag

- **Goal:** the README's live view paragraph says what a blocked issue shows.
- **Requirements:** R6.
- **Dependencies:** U1, U2.
- **Files:** `README.md`.
- **Approach:** in the live view paragraph, say that `run` shows `blocked` for an issue another open issue blocks, and replace "whether it is blocked" in the box's list with a `blocked` chip among its labels.
- **Test expectation:** none -- documentation only.
- **Verification:** the paragraph matches what U1 and U2 draw.

## Verification Contract

| Gate | Command | Applies to |
| --- | --- | --- |
| Format | `gofmt -l cmd internal tools acceptance` prints nothing | U1, U2, U4 |
| Vet | `go vet ./...` | U1, U2, U4 |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | U1, U2, U4 |
| Tests | `go test -race ./...` | U1, U2, U4 |
| Coverage | the total floor (`.testcoverage.yml`) and `tools/diffcover` on changed lines, both at least 90% | U1, U2, U4 |
| Acceptance | `go -C acceptance run ./cmd/acceptance -count=1`: every scenario passes except those whose snapshots KTD4 names for the part, which wait for the tester | each branch |

---

## Definition of Done

- The part's units are done and every gate above passes, with only the snapshots KTD4 names for that part failing in the acceptance suite, each named in the pull request.
- Each pull request body carries `Closes` for its issue and asks for `/cw-tester` to accept or rewrite the snapshots it breaks.
- No acceptance scenario or snapshot is edited by the developer.
- No leftover code from abandoned attempts remains in the diff.


