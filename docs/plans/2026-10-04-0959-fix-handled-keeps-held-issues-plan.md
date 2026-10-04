---
title: Handled keeps an issue a stage holds again - Plan
type: fix
date: 2026-10-04
topic: handled-keeps-held-issues
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-plan-bootstrap
origin: GitHub issue #109
execution: code
---

# Handled keeps an issue a stage holds again - Plan

## Goal Capsule

- **Objective:** an issue crew has handled stays in the live view's Handled section for the rest of the run, so the boss no longer watches entries vanish one poll after they appear.
- **Means:** the core's `View` stops leaving out the entries of issues held again and marks each with the stage holding it (KTD1), and a hidden stage's successful end keeps the entry it found (KTD4). The TUI uses that mark to keep the board from drawing the issue twice, to leave a retried failure out of the attention count, and to show where the issue is now (KTD2, KTD3).
- **Product authority:** the boss, through #109 and its move from `crew:fix:failed` to `crew:development:ready` after the diagnosis comment. The open product questions that comment raised are answered under Assumptions.
- **Open blockers:** none.
- **Execution profile:** one field, one filter change and one `release` rule in the pure core, three small TUI changes, the guide and the architecture page. No port, engine, adapter or config change.
- **Stop conditions:** stop and report if keeping held entries in `View.Handled` makes any consumer other than the TUI show an issue twice or count it wrongly.
- **Who ships:** the implementer opens one pull request that closes #109. Merging is the boss's.

---

## Product Contract

### Summary

Handled shows every issue whose stage ended this run, including one a later stage has taken again. Such an entry keeps showing how its last stage ended and says which stage holds the issue now. A hidden stage that ends well keeps the entry it found. The board still draws the issue once, and a failure the boss already sent back for a retry no longer counts as needing them.

### Problem Frame

Handled leaves out an issue while a stage holds it again (`internal/core/model.go`, `View`). On a workflow where most stages hand the issue on, such as crew's own (`triage` ends in `crew:triage:done`, `promote triage` takes it at the next poll, then `development` takes it and holds it for hours), each entry shows for at most one poll interval and then disappears. The boss reads that as crew forgetting work it did (#109). The rule was deliberate (R3 of `docs/plans/2026-10-02-1437-feat-live-view-handled-history-plan.md`, the guide's "Run it"), and #109 overturns it.

### Requirements

**Handled**

- R1. Handled lists an entry for each issue whose stage ended this run, whether or not a stage holds the issue again.
- R2. An issue keeps one entry, holding its latest ended stage. A new stage's end replaces it, as today.
- R3. An entry whose issue a stage holds again says which stage holds it, as `now in <stage>` after its details.
- R7. A successful end of an `on_board: false` stage does not replace the issue's earlier entry: the earlier entry stays. A failed or given-up end of such a stage replaces it, and with no earlier entry the stage's own entry is added, as today.

**Board and attention**

- R4. The board draws no waiting card for an issue a stage holds again. The held issue's own card, when its stage is on the board, is its only card.
- R5. An entry whose issue a stage holds again does not count toward the tab title's `needs attention`, does not turn the tab's progress indicator to an error, and does not sort first in Handled.

**Docs**

- R6. The guide's "Run it" section and the architecture page describe the new rule.

### Key Decisions

- **Handled keeps an entry while a stage holds its issue again.** (session-settled: user-directed — chosen over the rule "an issue leaves Handled while a stage holds it again": entries vanish within one poll on a workflow that hands issues on, which reads as lost work.) Governs R1.
- **One entry per issue, not one line per stage run.** The handled-history plan rejected a line per stage run. A held issue's latest stage already shows on the board and in Actions. Governs R2.

### Assumptions

These answer the questions the diagnosis comment on #109 raised. The boss did not answer them, so each is the plan's default.

- A kept entry still shows how its stage ended and gains the `now in <stage>` marker (R3). A failed one shows the last part of the label it moved to, in the error style, instead of `NEEDS ATTENTION`, because R5 no longer counts it as needing the boss. `GIVEN UP` is unchanged.
- A hidden stage's successful end keeps the earlier entry (R7). On crew's own workflow, `promote triage` ends seconds after it takes the issue, and replacing triage's entry with it would drop triage's outcome, time and cost one poll after they appear, the symptom #109 reports.
- A kept `NEEDS ATTENTION` entry stops counting as needing the boss while a stage holds the issue again (R5): the boss has already relabelled the issue for a retry, so a red tab would ask for an action they took. It counts again if the take is given up and the stage releases the issue without a new entry.
- The marker names any stage that holds the issue, including one with `on_board: false` such as `promote triage`. Hiding it would leave the entry looking idle while crew acts on the issue.

### Scope Boundaries

- Notifications are unchanged: they are keyed by issue, stage and `Ended`, and a kept entry was already seen. Keeping the entry closes the accepted gap in KTD6 of `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md`, where an entry taken again between two received snapshots was never notified.
- The `--plain` line renderer does not read `View.Handled`, so it is untouched.
- Not built: a history of every stage run per issue (Key Decisions).

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The core marks a kept entry with the stage holding its issue.** `HandledView` gains a field, `HeldBy`, set in `View` to the name of the stage of `m.held(key)` and empty when no stage holds the issue. `View` no longer filters those entries out. The core already knows who holds the issue, so the TUI does not rebuild that join from `View.Issues`. Governs R1, R3, R4, R5.
- KTD2. **The board's `waits` returns false for an entry with `HeldBy` set.** Today `gone` skips held entries, so a held issue's entry keeps `Gone` false and `waits` would draw a waiting card beside the held card. Checking `HeldBy` first keeps `gone` as it is. The slide from the waiting card to the new column still works, because the waiting card disappears and the held card appears in the same snapshot, as when the entry used to drop out. Governs R4.
- KTD3. **One TUI predicate decides whether an entry needs the boss: `NeedsAttention()` and no `HeldBy`.** `attention` (tab title and progress) and `byAttention` (Handled order) use it. The pill uses it too: an entry with failures and `HeldBy` set shows the last part of its `To` in the error style, not `NEEDS ATTENTION` (Assumptions). `core.HandledView.NeedsAttention` keeps its meaning: it describes the stage's ending, not the boss's queue. Governs R5.
- KTD4. **`release` keeps the earlier entry when an off-board stage ends well, and marks it gone.** The core already holds `[]crew.Stage`, so `release` reads the ended stage's `OffBoard`. When it is set and the verdict does not need attention, and the issue has an earlier entry, `release` leaves that entry in place and sets its `Gone`: the hidden stage's move took the issue out of the entry's `To`, and without the mark the board would draw a stale waiting card until the next listing. Later listings decide `Gone` anew, as today. Governs R7.

### Sequencing

U1 adds the field and drops the filter. U2 builds on the field in the TUI. U3 documents both. Between U1 and U2 the board can draw a held issue twice, so U1 and U2 land in the same pull request.

---

## Implementation Units

### U1. Keep held issues' entries in the core's view

- **Goal:** `View.Handled` lists every handled entry and marks those whose issue a stage holds again (R1, R2, KTD1), and a hidden stage's successful end keeps the earlier entry (R7, KTD4).
- **Requirements:** R1, R2, R3, R7.
- **Dependencies:** none.
- **Files:**
  - `internal/core/model.go`
  - `internal/core/update.go`
  - `internal/core/gone.go`
  - `internal/core/handled_test.go`
  - `internal/core/gone_test.go`
- **Approach:**
  1. Add `HeldBy string` to `HandledView`, with a comment saying it names the stage holding the issue again and is empty otherwise.
  2. In `View`, append every entry's clone and set `HeldBy` from `m.held(key)` and `m.stages[h.stage].Name`.
  3. In `release` (`update.go`), apply KTD4 before replacing the issue's entry, and rewrite its comment.
  4. Rewrite the comments that state the old rule: `View.Handled` and `View.Spent` in `model.go`, and the last sentences of `gone`'s comment in `gone.go`.
- **Patterns to follow:** `View` already builds `IssueView.Stage` from `m.stages[h.stage].Name`. The driver helpers in `handled_test.go` (`onlyEntry`, `handled`, `d.poll`, `d.settle`).
- **Test scenarios:**
  - Rename and rewrite `TestAnIssueTakenAgainLeavesHandledUntilItsNewStageEnds`: after #1's `implement` stage ends and a poll takes it for `review`, `onlyEntry` is the `implement` entry with `HeldBy` `review`. When `review` ends failed, the single entry is the `review` entry, with `HeldBy` empty and the review failure.
  - An entry whose issue nobody holds has `HeldBy` empty (assert in an existing single-stage test).
  - `TestATakeGivenUpOnAHandledIssueShowsItsEarlierEntryAgain`: the earlier entry shows throughout, with `HeldBy` set while the take is pending and empty once the take is given up. Rename it to say so.
  - `TestAnIssueTakenAgainLeavesHandledAndItsNextEntryStartsNotGone` in `gone_test.go`: while #1 is held again its entry is listed with `HeldBy` set, and its next entry still starts not gone. Rename it to say so.
  - Hidden stage, success: on a workflow where an on-board stage ends in the label of an `on_board: false` stage, which ends in a third stage's label, #1's on-board stage ends, the hidden stage takes #1 and succeeds, then the third stage takes it. There is one entry throughout, the on-board stage's. It has `Gone` true once the hidden stage ended, and `HeldBy` naming the third stage while that stage holds #1.
  - Hidden stage, failure: the same, but the hidden stage's action fails. Its entry replaces the on-board stage's and needs attention.
  - Hidden stage, no earlier entry: an `on_board: false` stage that ends well on an issue with no entry adds its own entry.
- **Verification:** `go test -race ./internal/core` passes, and no core test still asserts that `View.Handled` drops a held issue.

### U2. Draw a kept entry once, out of the attention count, with its marker

- **Goal:** the TUI shows a kept entry with `now in <stage>`, draws no waiting card for it, and does not count it as needing the boss (R3, R4, R5).
- **Requirements:** R3, R4, R5.
- **Dependencies:** U1.
- **Files:**
  - `internal/ui/tui/board.go`
  - `internal/ui/tui/band.go`
  - `internal/ui/tui/outside.go`
  - `internal/ui/tui/board_test.go`
  - `internal/ui/tui/band_test.go`
  - `internal/ui/tui/outside_test.go`
  - `internal/ui/tui/testdata/` (a golden file, if a view test covers a kept entry)
- **Approach:**
  1. `waits` returns false when `e.HeldBy` is set (KTD2).
  2. Add the predicate from KTD3 and use it in `byAttention`, `attention` and `pill`.
  3. `handledDetails` appends a muted `now in <stage>` part when `HeldBy` is set (R3).
- **Patterns to follow:** existing snapshot-built tests in `board_test.go`, `band_test.go` and `outside_test.go`, which build `core.View` values directly. Golden files are rewritten with `go test ./internal/ui/tui -update` and the diff reviewed.
- **Test scenarios:**
  - Board: a snapshot with #1 held by `review` and a Handled entry for #1 from `implement` whose `To` is `review`'s label draws one card for #1, the held one, in `review`'s column.
  - Board: the same entry without `HeldBy` still draws its waiting card in `implement`'s column (no regression).
  - Handled: an entry with `HeldBy` `review` renders `now in review` in its details. An entry without `HeldBy` renders no marker.
  - Order: a `NEEDS ATTENTION` entry with `HeldBy` set sorts with the other entries by `Ended`, not first. One without `HeldBy` still sorts first.
  - Pill: a failed entry with `HeldBy` set and `To` `crew:fix:failed` shows `FAILED` in the error style, with its `×` reasons. The same entry without `HeldBy` shows `NEEDS ATTENTION`. A given-up entry shows `GIVEN UP` either way.
  - Tab: with only a failed entry whose issue is held again, the title has no `needs attention` and the progress indicator is not an error. With the same entry not held, both show it as today.
- **Verification:** `go test -race ./internal/ui/tui` passes, with any golden diff reviewed and showing only the marker.

### U3. Document the new rule

- **Goal:** the guide and the architecture page say what Handled now does (R6).
- **Requirements:** R6.
- **Dependencies:** U1, U2.
- **Files:**
  - `docs/guide/crew.mdx`
  - `docs/develop/architecture.mdx`
- **Approach:**
  1. In the guide's "Run it", replace "and leaves Handled while a stage holds it again" with the new rule: the entry stays and says `now in <stage>`; while a stage holds the issue again, a failed entry shows its label instead of `NEEDS ATTENTION` and does not count as needing you; a stage with `on_board: false` that ends well keeps the earlier entry. Adjust the tab-title paragraph if it needs to say that.
  2. In `architecture.mdx`, replace "the view leaves it out while the issue is held again" with `HeldBy`. In the `SubscribeLatest` bullet, drop or narrow the remark that an entry that came and went between two updates sends no notification, since an entry no longer goes while its issue is held.
- **Test expectation:** none -- documentation only; `pnpm docs:check` covers links and MDX.
- **Verification:** `pnpm docs:check` passes, and no published page still says Handled leaves out a held issue.

---

## Verification Contract

| Gate | Command |
| --- | --- |
| Format | `gofmt -l cmd internal tools` prints nothing |
| Vet | `go vet ./...` |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` |
| Tests | `go test -race ./...` |
| Coverage | `go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...`, then `go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml` and `git diff -U0 origin/main...HEAD \| go run ./tools/diffcover -profile coverage.out` |
| Docs | `pnpm docs:check` |

---

## Definition of Done

- R1 through R7 hold, each proven by a U1 or U2 test scenario or by U3's docs check.
- No test, comment or published page still states that Handled leaves out an issue held again.
- Every gate in the Verification Contract passes.
- No dead or experimental code from abandoned approaches remains in the diff.
