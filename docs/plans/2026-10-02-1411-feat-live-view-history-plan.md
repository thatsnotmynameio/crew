---
title: Live View History - Plan
type: feat
date: 2026-10-02
topic: live-view-history
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Live View History - Plan

## Goal Capsule

- **Objective:** The boss comes back to a crew that ran for hours and, from the live view alone, knows which issues crew finished this run, which of them need them and why, how long crew has been up and how much run time is left. They open only the issues that need them.
- **Product authority:** this Product Contract, within `STRATEGY.md` and the engine's architecture plan (`docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md`). It covers #23, one part of the epic #16. Cost, token usage and pull requests (#24), scrolling and selecting an issue (#25), and `--plain` output (#26) are not active scope.
- **Open blockers:** none.

---

## Product Contract

### Summary

The live view gets a summary at the top: uptime, the time left when a run limit is set, and how many issues crew finished, counted by the state each went to. A new **Handled** region lists each issue crew finished this run on one line, with failures first and each failed action's reason under its issue. The view fits the window height: Recent events gives up its rows before Handled does.

### Problem Frame

The live view shows only the present (`internal/ui/tui/view.go`). Once a stage's verdict is in, the issue leaves the issues crew holds, and so the view's Issues and Actions regions. Recent events keeps the last 20 events (`recentEvents`, `internal/engine/stream.go`), so after 20 more events nothing on screen says the issue was ever handled.

Today the boss opens each issue on GitHub and reads its status comment (#7). That works for one issue. After hours away, they first have to know which issues to open, and nothing on screen points to a failure. Issue #9 went to `in review` without a pull request (#13), and the view never showed it.

### Key Decisions

- **Handled is an index, not a full record.** One line per issue, plus each failed action's reason; the status comment on GitHub keeps the detail. Governs R9, R10. (session-settled: user-directed — chosen over full per-action detail with label moves and log paths, and over an index with the reason cut into the issue's line, after seeing all three on screen: the view's job is to say which issues to open)
- **One entry per issue, holding its latest stage.** Counts are counts of issues. Governs R2, R3. (session-settled: user-directed — chosen over one line per stage run: an append-only log would list an issue twice and mix issues with stage runs in the counts)
- **The history covers this run only.** The status comments on GitHub are the record across runs. Governs R4. (session-settled: user-directed — chosen over writing the history under `.crew/` and reloading it on start, which needs retention and pruning rules)
- **Handled keeps its rows before Recent events does.** The boss comes back for what happened, and Recent events is the live feed. Governs R13, R14. (session-settled: user-directed — chosen over keeping 3 lines of Recent events, and over cutting the screen at the bottom, which can hide a failure when many issues are held)
- **Needing attention follows the stage's outcome, not a label's name.** Each stage names its own `on_failure` state, and crew's own workflow uses `crew:failed`, not `needs attention`. Governs R6, R8. (session-settled: user-directed — chosen over keying on the `needs attention` label: labels are configurable per stage)
- **A verdict move crew gave up on needs attention too.** The issue did not end up where crew meant it to. Governs R6, R10. (session-settled: user-approved — chosen over counting only failed stages: a given-up move also leaves the issue somewhere crew did not mean)
- **An issue's time is its last stage's, from the take to the verdict.** Governs R1. (session-settled: user-approved — chosen over adding up every stage's time: the line describes the last stage)
- **The engine keeps the record, not the TUI.** The TUI's subscription is latest-wins and drops the events of updates it skipped, and #25 and #26 need the same record. Governs R5.

### Requirements

**The record**

- R1. For each issue whose stage ended this run, crew records: the issue's ref and title, the stage, the state the issue was moved to, whether the stage failed, the stage's duration from the take to the verdict, and each failed action's name and reason.
- R2. The record holds one entry per issue. When another stage ends for the same issue, that stage's entry replaces the earlier one.
- R3. While a stage holds an issue again, the issue shows under Issues and not under Handled. It returns to Handled when that stage ends.
- R4. The record lasts for the run: it is empty when crew starts, and nothing of it is written to disk.
- R5. The live view always shows the whole record, including entries whose events arrived in updates the view skipped.

**Needing attention**

- R6. An entry needs attention when its stage failed, so the issue went to that stage's `on_failure` state, or when crew gave up the stage's verdict move (the issue was closed or moved meanwhile, or the tracker refused).

**The summary**

- R7. The view's top line also shows the uptime since crew started and, when a run time limit is set, the time left. Without a limit it shows no time left. Once time is up, it keeps today's winding-down wording.
- R8. A second line counts the entries: the total, then one count per state the issues went to, named as the workflow names them (for crew's own workflow, `crew:waiting review` and `crew:failed`). Given-up verdict moves get their own count.

**The Handled region**

- R9. A Handled region sits between Actions and Recent events. Each entry takes one line: the state the issue went to, the issue's ref and title, the stage and its duration.
- R10. An entry that needs attention adds one line per failed action, with the action's name and reason. A given-up verdict move adds one line with the reason crew gave it up.
- R11. Entries that need attention come first, then the rest; within each group, the most recently ended comes first.
- R12. With no entry, Handled shows `none`, like the other regions.

**Fitting the window**

- R13. The view fits the window's height. When rows run out, Recent events gives up its rows first. Then the oldest entries that do not need attention collapse, one line per state, such as `… and 3 more in crew:waiting review`.
- R14. Entries that need attention, and their reason lines, never collapse. When even they do not fit, the view is cut at the bottom with a line saying how many lines were cut.
- R15. Every line is still cut to the window's width, as today.

**Docs**

- R16. The live view's description in `docs/guide/crew.mdx` is updated to cover the summary and Handled.

An example at 80 columns and 24 rows, for crew's own workflow (illustrative, not a layout commitment):

```text
crew: 2 issues held, up 3h12m, 48m left (q or ctrl+c stops)
handled 10: 8 crew:waiting review, 2 crew:failed

Issues
  development
    running   #31 Add retry to the poller
  fix
    taking    #32 Rename the status kinds

Actions
  #31 development/lfg  running 12m03s
  #32 fix/lfg          waiting

Handled
  crew:failed          #28 Parse the config once      development 41m10s
    lfg failed: exited 1: "tests fail on Go 1.27"
  crew:failed          #27 Drop the old --dry flag    fix 0m02s
    lfg failed: prompt did not render: no field "Body"
  crew:waiting review  #36 Log the poll interval      development 8m12s
  crew:waiting review  #35 Trim the README            knowledge base 4m40s
  crew:waiting review  #34 Show the uptime            development 22m48s
  crew:waiting review  #33 Add a --version flag       development 9m15s
  crew:waiting review  #30 Plain summary              development 18m40s
  … and 3 more in crew:waiting review
```

### Acceptance Examples

- AE1. **Covers R2, R3.** **Given** #30's `development` stage ended in `crew:waiting review`, **when** another stage takes #30, **then** #30 leaves Handled and shows under Issues; **when** that stage ends in `crew:failed`, **then** #30's single Handled entry shows `crew:failed`, that stage, its duration and its failed actions.
- AE2. **Covers R6, R10.** **Given** a stage with actions `code` and `tests`, where `tests` failed, **when** the stage ends, **then** the issue's entry needs attention and shows one reason line, for `tests` only.
- AE3. **Covers R6, R8, R10.** **Given** a stage succeeded, **when** the issue is closed before crew moves it and crew gives the move up, **then** the entry needs attention, shows the reason crew gave the move up, and is counted as a given-up move, not under the stage's `on_success` state.
- AE4. **Covers R13, R14.** **Given** a 24-row window, 2 held issues and 10 entries, 2 of them failed, **when** the view renders, **then** Recent events shows no rows, both failed entries show with their reason lines, and the successes that do not fit collapse into one line per state.
- AE5. **Covers R7.** **Given** no run time limit, **when** the view renders, **then** the top line shows the uptime and no time left.
- AE6. **Covers R4.** **Given** crew handled 6 issues and was stopped, **when** it starts again, **then** Handled shows `none` and the counts line shows no entries.

### Scope Boundaries

- No change to the status comment, the failure report, or `--plain` output.
- No cost, token usage or pull request in Handled (#24).
- No scrolling, selection or detail view (#25).
- No record across restarts, and nothing printed when the live view exits.

<!-- ce-section: work-relationships -->
### How This Work Fits Together

This plan covers #23, the record and the summary. The rest of the epic #16 is the current breakdown, not a committed roadmap:

- #24, cost, token usage and pull requests: can proceed independently of this plan. Its data would add to Handled's entries. It shares the pull request check with #14's outcome verification.
- #25, scrolling and selecting an issue: depends on this plan's record. It would lift the collapse in R13.
- #26, `--plain` output: depends on this plan's record. Still to decide: whether it prints a summary when the run ends.

### Dependencies / Assumptions

- The engine records no start time today: the run limit is counted by a timer started at the first poll (`internal/engine/engine.go`). The run limit reaches the view only as the `TimeUp` flag, and its value only in the `WindingDown` event after it has passed. R7 needs both published.
- The view ignores the window's height today; it stores only the width (`internal/ui/tui/model.go`). R13 and R14 need the height.
- An issue leaves the core's view only after all its calls settle, including a failed stage's failure report, so an entry may appear in Handled a little after the stage ended.
- The domain events already carry what R1 needs: the take with the issue's title and stage, each action's end with its outcome and reason, and the moves with their states (`internal/core/event.go`).

### Outstanding Questions

**Deferred to Planning**

- How crew tells a given-up verdict move from a given-up take move or failure report. `CallDropped` fires for all three, and only the call's states tell them apart.
- Whether the record lives in the core's view or beside `Recent` in the engine's snapshot.

### Sources

- Issue #16 (epic) and #23; #7 (status comment); #13 and #14 (the #9 failure).
- `internal/ui/tui/view.go`, `internal/ui/tui/model.go`: today's regions and window handling.
- `internal/engine/stream.go`, `internal/engine/engine.go`: `Snapshot`, `recentEvents`, `SubscribeLatest`, the run limit timer.
- `internal/core/event.go`, `internal/core/update.go`: the events and when an issue is released.
- `.crew/config.yaml`: crew's own workflow and its `on_success` and `on_failure` labels.
