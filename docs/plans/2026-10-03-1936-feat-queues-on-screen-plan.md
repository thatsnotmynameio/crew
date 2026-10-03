---
title: Show the queues on screen - Plan
type: feat
date: 2026-10-03
topic: queues-on-screen
origin: GitHub issue #94
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Show the queues on screen - Plan

## Goal Capsule

- **Objective:** while crew runs, the boss sees on its screen how many slots each queue has, how many are busy and free, and which queue each running job uses.
- **Means:** the core's view gains one entry per queue and names each held issue's queue (KTD1, KTD2); the TUI renders a Queues section and a queue column in Actions (KTD3, KTD4).
- **Product authority:** the boss, through the brainstorm of #94, whose Product Contract is the body of issue #94 and is carried here unchanged.
- **Stop conditions:** stop if showing the queues needs a change to how crew takes issues or assigns slots, to the `lines` renderer, or to the `poll: skipped` line.
- **Who finishes:** the pull request ships U1 to U3 and carries `Closes #94`.
- **Open blockers:** none.

---

## Product Contract

Product Contract preservation: Product Contract unchanged (copied from the body of issue #94).

### Summary

The TUI gets a Queues section with one line per queue: its name, its slots from the config, how many are busy and how many are free. Each row of the Actions section also names the queue its issue's stage runs in.

### Problem Frame

Since #76, stages run in queues with their own slots, such as `clerk` with 1 slot for triage and audit ci in this repository. The TUI groups the held issues by stage and lists the actions, but says nothing about queues. The boss cannot tell from the screen which queue is full, which has room, or which queue a running job holds a slot in. The only hint is the `poll: skipped, 2 of 3 slots busy` event line, which covers all queues together.

### Key Decisions

- **The size of a queue is its slots, not the issues waiting for it.** crew does not keep the issues it listed but could not take, and skips the listing while every queue is full, so a waiting count would be stale or need extra polling. (session-settled: user-directed — chosen over counting the issues waiting from the last poll: the size the boss wants is the queue's maximum from the config, with busy and free slots.) Governs R2.
- **Only the TUI changes.** The request is about the screen; the `lines` renderer prints events, not a standing view. Governs R1, R5.
- **Only queues some stage runs in get a line.** A queue no stage names can never be busy, and this matches the slots the `poll: skipped` line counts. Governs R1.

### Requirements

**Queues section**

- R1. The TUI shows a Queues section with one line for each queue some stage runs in.
- R2. Each queue line shows the queue's name, its size (the slots it has from the config), how many slots are busy and how many are free.
- R3. A queue's busy count is the number of issues crew holds whose stage runs in that queue, in any claim, the same count crew uses to decide whether the queue can take another issue.
- R4. A queue with 0 slots, such as a `default` the other queues left empty, still gets its line, showing a size of 0.

**Actions section**

- R5. Each row of the Actions section names the queue the row's issue holds its slot in.

### Acceptance Examples

- AE1. **Covers R2, R3, R5.** Given this repository's config (`max_parallel_issues: 3`, `clerk` with 1 slot for triage and audit ci, `default` with 2 for development, fix and knowledge base), when crew runs a triage on #7 and a development on #1, then the Queues section shows `clerk` with size 1, 1 busy, 0 free, and `default` with size 2, 1 busy, 1 free, and the Actions rows of #7 and #1 name `clerk` and `default`.
- AE2. **Covers R3.** Given an issue crew has taken but whose action has not started yet, its queue counts that slot as busy.
- AE3. **Covers R1.** Given a config where no stage names a queue, the Queues section shows only `default`, not `clerk`.
- AE4. **Covers R4.** Given a config where the other queues take every slot and a stage runs in `default`, the Queues section shows `default` with size 0, 0 busy, 0 free.

### Scope Boundaries

- No count of the issues waiting for a queue, and no change to when crew polls.
- No change to the `lines` renderer or to the `poll: skipped` event line.
- No change to how crew assigns slots or takes issues.
- Considered and not built: a special rendering for the unnamed queue the core gives stages whose `crew.Queue` is zero. `config` always gives a stage a named queue (`default` when it names none), so only core and TUI tests build such stages. A real config producing one would change this.

### Outstanding Questions

**Resolved in planning**

- Where the Queues section sits and how its lines and the Actions rows' queue are laid out within the 24-row fit: KTD3 and KTD4.

### Sources / Research

- `internal/core/update.go`: `full` and `queueFull`, the busy count R3 matches.
- `internal/core/model.go`: `queues`, which keeps only the queues some stage runs in.
- `internal/ui/tui/testdata/running.golden`: the current Issues and Actions sections.
- `docs/guide/crew.mdx`, section Queues: the user-facing rules for queues and slots, which the TUI description there should follow.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The core reports the queues in its `View`.** `core.View` gains `Queues`, one `QueueView` per queue some stage runs in, with its name, its slots and its busy count, in the order the core already keeps them (the first stage that runs in each, config order). `QueueView` has a `Free` method, its slots minus its busy count, never below 0. The engine's `Snapshot` embeds `core.View`, so the TUI receives the queues with no engine change, and layering stays as it is: the TUI reads only engine updates. The core is the only place that knows each stage's queue and effective slots (the zero `crew.Queue` gets `max_parallel_issues`), so computing them anywhere else would duplicate `queues`.
- KTD2. **One busy count for the view and for taking.** A helper on the model counts the held issues whose stage runs in queue `q`; `queueFull` and `View` both call it, so R3's "the same count crew uses" holds by construction. To name each queue, the model keeps each queue's name next to its slots: `queues` returns the names with the slots, replacing the slots-only slice, or a parallel slice of names; the implementer picks.
- KTD3. **The Queues section sits between the counts line and Issues.** It reads as part of the run's summary, above what fills the slots. It joins the fixed rows of the fit, like Issues and Actions, so it never collapses: with two queues it costs four rows, which the existing fitting rules take from Recent events first, then from the oldest successes in Handled once Recent events is gone. One row per queue: the name, the size as `N slot` or `N slots`, `N busy` and `N free`, each column padded to its widest cell so the lines align. With no queue, as before the first update, it shows `none` like the other sections.
- KTD4. **The Actions rows get a queue column between the action's name and its state.** `IssueView` gains `Queue`, the name of the queue its stage runs in. The column is padded to the widest queue name among the rows, so the state and elapsed time stay aligned. The name stays first, so a narrow window cuts the elapsed time before the queue.

### Assumptions

- The Queues lines read `  clerk    1 slot   1 busy  0 free`, with the size pluralized through `lines.Plural`. The exact spacing is the implementer's, and the golden files record it.
- The busy count can never exceed a queue's slots, since the one take path checks `queueFull`. `Free` still floors at 0 so the screen never shows a negative count.

---

## Implementation Units

### U1. The core's view reports queues and each issue's queue

- **Goal:** `core.View` carries every queue some stage runs in, with its slots and busy count, and each `IssueView` names its queue.
- **Requirements:** R1, R2, R3, R4, R5; KTD1, KTD2.
- **Dependencies:** none.
- **Files:** `internal/core/model.go`, `internal/core/update.go`, `internal/core/queue_test.go`, `internal/core/call_test.go`, `internal/core/update_test.go`.
- **Approach:**
  1. Keep each queue's name with its effective slots where `queues` builds them.
  2. Add the busy-count helper and make `queueFull` use it (KTD2).
  3. Add `QueueView` with `Name`, `Slots`, `Busy` and `Free`, and `View.Queues`; fill it in `Model.View` in queue order.
  4. Add `IssueView.Queue`, set from the held issue's stage.
  5. Update the two tests that compare a whole `View` (`call_test.go`, `update_test.go`) to expect the new fields.
- **Patterns to follow:** `HandledView.Duration` and `NeedsAttention` for a method on a view type; the `queued`, `inQueues`, `clerk` and `defaultQueue` helpers in `queue_test.go`.
- **Test scenarios:**
  - Covers AE1. Triage in `clerk` (1 slot), development in `default` (2 slots), `max_parallel_issues` 3: take #7 for triage and #1 for development; `View().Queues` is `clerk` 1 slot 1 busy 0 free, then `default` 2 slots 1 busy 1 free, and the issues' `Queue` are `clerk` and `default`.
  - Covers AE2. An issue whose take move is in flight, and one whose take is owed, each count as busy in their queue.
  - Covers AE3. Every stage in `default`, with `clerk` declared by no stage: `View().Queues` holds only `default`.
  - Covers AE4. A stage in a `default` of 0 slots: its line is `default` 0 slots 0 busy 0 free.
  - Once an issue's verdict calls settle and the core releases it, its queue's busy count drops by one.
  - Before any take, every queue shows 0 busy and all its slots free.
  - Stages with the zero `crew.Queue` share one queue of `max_parallel_issues` slots with an empty name.
- **Verification:** the core tests pass, the existing queue-taking tests still pass unchanged, and `queueFull` and `View` share the one busy count.

### U2. The TUI shows the Queues section and the queue of each action

- **Goal:** the live view renders R1 to R5 from the snapshot.
- **Requirements:** R1, R2, R4, R5; KTD3, KTD4.
- **Dependencies:** U1.
- **Files:** `internal/ui/tui/view.go`, `internal/ui/tui/model_test.go`, `internal/ui/tui/handled_test.go`, `internal/ui/tui/testdata/*.golden`.
- **Approach:**
  1. Add a `queues` region beside `issues` and `actions`, and put it in `fitted`'s fixed rows between the counts line and Issues (KTD3).
  2. Add the queue column to `actions` (KTD4).
  3. Give the test snapshots queues: `runningSnapshot` gets `implement` in `default` and `review` in `clerk`, with their `Queues`, so every golden built on it shows both.
  4. Rewrite the golden files with `go test ./internal/ui/tui -update` and review the diff.
  5. Adjust the fitting tests whose expectations count rows: the 24-row golden test's collapsed line, the tests at heights 31 and 17, and the two other 24-row tests, `TestFittingCountsThePullRequestLines` and `TestCollapsedSuccessesTakeOneLinePerState`. Keep what each proves: raise those two windows by the four Queues rows (24 to 28), so the collapse they assert still happens instead of a cut at the bottom, and update the row counts in their comments.
- **Patterns to follow:** `columnsOf` and `pad` for padded columns; `issues` for a region with its `none` line.
- **Test scenarios:**
  - Covers AE1 (TUI side). A snapshot with `clerk` (1 slot, 1 busy) and `default` (2 slots, 1 busy) renders the Queues section with `clerk` 1 slot 1 busy 0 free and `default` 2 slots 1 busy 1 free, and the Actions rows name `clerk` and `default`; the `running` golden records it.
  - Covers AE4 (TUI side). A queue of 0 slots renders `0 slots  0 busy  0 free`.
  - A snapshot with no queues shows `Queues` then `none`.
  - Queue names of different lengths keep the Actions states aligned in one column.
  - A 24-row window keeps the Queues section whole, shows no Recent events, and collapses more old successes than before; the `fit-24-rows` golden and the test's collapsed-line assertion record how many.
  - A narrow window cuts the Queues and Actions lines to its width without panicking (the existing narrow-window tests, with queues in the snapshot).
- **Verification:** every TUI test passes, and the golden diff shows only the new section and the new column.

### U3. The docs describe the Queues section

- **Goal:** the guide and the architecture page say what the live view shows about queues.
- **Requirements:** R1 to R5 (documentation of them).
- **Dependencies:** U2.
- **Files:** `docs/guide/crew.mdx`, `docs/develop/architecture.mdx`.
- **Approach:**
  1. In `docs/guide/crew.mdx`, section Run it: the live view's description names the queues, and a short paragraph with a `text` example shows the Queues lines and says busy counts every issue crew holds in that queue, whether its action has started or not. Link to the Queues section; a line in Queues may point back to the live view.
  2. In `docs/develop/architecture.mdx`, next to the `View.Handled` paragraph: `View.Queues` and `IssueView.Queue`, and that the busy count is the one the core takes by.
- **Patterns to follow:** the Handled paragraph and its `text` example in `docs/guide/crew.mdx`.
- **Test expectation:** none -- documentation only; `pnpm docs:check` covers links and MDX.
- **Verification:** `pnpm docs:check` passes and the example matches the `running` golden's format.

---

## Verification Contract

| Check | Command | Proves |
|---|---|---|
| Core | `go test -race ./internal/core` | U1: AE1 to AE4 on the view; taking unchanged |
| TUI | `go test -race ./internal/ui/tui` | U2: the goldens and the fitting tests |
| Whole suite | `go test -race ./...` | nothing else breaks on the new view fields |
| Format | `gofmt -l cmd internal tools` | prints nothing |
| Vet and lint | `go vet ./...` and `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | zero findings, `funlen` and `cyclop` included (see `docs/solutions/tooling-decisions/codacy-lizard-and-golangci-lint-limits.md`) |
| Coverage | the coverage profile, then `go-test-coverage` and `tools/diffcover` as in `AGENTS.md` | total and changed lines at or above 90% |
| Docs | `pnpm docs:check` | U3: links and MDX |

## Definition of Done

- U1 to U3 are committed.
- The Verification Contract's checks pass.
- The golden files are rewritten and their diff shows only the Queues section and the Actions queue column.
- The `lines` renderer, the `poll: skipped` line and how crew takes issues are unchanged.
- No dead-end code from abandoned attempts remains in the diff.
