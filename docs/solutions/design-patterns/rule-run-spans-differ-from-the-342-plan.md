---
title: "A rule run span's halt, open end and first end differ from the #342 plan"
date: 2026-10-09
category: design-patterns
module: internal/core, internal/adapter/sqlite, internal/engine
problem_type: design_pattern
component: statistics_adapter
related_components: [core_reducer]
severity: medium
applies_when:
  - "Reading docs/plans/2026-10-09-2214-issue-342-plan.md's KTD3, U1 or Assumptions on rule run spans"
  - "Adding span kinds to the spans table, such as #343's action, route and step spans"
  - "Changing keepHalt, or how a stopping run chooses its route"
  - "Reading statistics.db spans with no end, or counting runs crew's stop or run time limit failed"
  - "Adding a statistics store adapter other than sqlite"
tags: [statistics, spans, rule-run-span, halt, sqlite, first-end, stale-plan, run-time-limit]
---

# A rule run span's halt, open end and first end differ from the #342 plan

## Context

#342 records each rule run as a span: the core emits a `crew.RuleRunSpan` at the take and again at the release, and the SQLite store keeps it in the `spans` and `rule_run_spans` tables. The plan, `docs/plans/2026-10-09-2214-issue-342-plan.md`, still states three things the shipped code does differently. Plans are not updated after they ship, the same pattern as `docs/solutions/workflow-issues/release-plans-predate-the-draft-first-release-workflow.md`. A reader who trusts the plan would "fix" correct code or misread the data.

## Guidance

1. **The halt is recorded only when the run chose `failed`.** KTD3 says a span's halt is `stop` when "the run was already stopping when it chose its route". That bullet has no route condition; only its lead-in implies `failed`. The code makes it explicit: the chosen route must be `FailedRoute` (`keepHalt`, `internal/core/statistics.go:221-235`). A stopping run does not always go through `failed`. A rule without actions, and a run of the passed route alone, choose `passed` at the take even while stopping (`internal/crew/fact.go:101-103`). Only an action's end is forced to `failed` by a stop (`finish`, `internal/crew/decide.go:158-163`). The stop did not choose that `passed`, so the span names no halt. Without the route check, such a span reads `routed`, `passed`, `stop`, which counts a Ctrl+C against a run that passed. `TestAStopThatChoosesNoRouteNamesNoHalt` (`internal/core/statistics_test.go:627`) pins this. During the work it failed with `Halt: "stop"` once the route check was removed (session history).

2. **A span with no end has three causes, not two.** The plan's Assumptions and U4 say an open span is a run still running or one a killed crew left. The review found a third: the engine's statistics writer calls `Record` once per record and never retries (`internal/engine/statistics.go:58-63`). A failed write only becomes a `StatisticNotRecorded` warning (`internal/core/statistics.go:56-57`). So when the open landed and the end's write failed, a completed run keeps an open span. `README.md` now names all three causes. A reader of `statistics.db` cannot tell them apart from the row alone. Before treating an open span as "running", check whether a crew process with that `process_id` is still alive.

3. **The store keeps the first end with a conditional update, not a read.** U1 says the store reads `spans.ended_at IS NULL` once, then runs both updates. The simplification pass replaced that with one conditional update (`updateSpanEnd`, `internal/adapter/sqlite/store.go:65`): `UPDATE spans ... WHERE id = ? AND ended_at IS NULL`. The `rule_run_spans` update runs only when it changed a row (`span`, `internal/adapter/sqlite/store.go:161-184`). The invariant is the same: both rows hold the same first end, inside the record's immediate transaction, so two processes writing at once keep one end. Gate the second update on `RowsAffected` of the first. Do not re-read `ended_at` after the first update: by then it is set, so the second row would never get its end. That ordering trap is why U1 first said to read the condition once, before both updates.

## Why This Matters

Each statement is pinned only by a test or a comment, while the plan states the opposite in plain words:

- The halt rule decides failure rates. Wrong one way, every stop that met a passing actionless rule counts as a stop-caused failure. Wrong the other way, stopped runs look like ordinary failures.
- The open-span causes decide whether a reader treats an open span as live work, an abandoned run or a lost record.
- The first-end rule is the store's third decision of its own, beside the restart rule and the outside-move dedupe in `docs/solutions/design-patterns/statistics-store-decides-restart-and-duplicate-label-moves.md`. A new store adapter, or #343's new span kinds, must keep it the same way.

## When to Apply

- Before #343 adds action, route and step spans to the same tables and writer. They inherit the no-retry open end and the first-end rule, and each needs its own answer on when a halt "chose" its end.
- Before changing `keepHalt`, `RouteChosen` handling in `internal/core/steps.go`, or how `internal/crew/fact.go` routes a stopping run.
- Before writing a query or report over `statistics.db` that counts open spans or halts.

## Examples

A rule without actions, `promote triage`, takes issue #1 while crew's stop arrives. The take lands, the run goes through `passed`, and the span's end is:

```go
crew.RuleRunEnd{At: released, Outcome: crew.OutcomeRouted, Route: crew.Some(crew.PassedRoute)}
```

It has no `Halt`, although the run was stopping when it chose its route.

A run takes #42 and its open is written. At the release the disk is full, so the end fails. crew shows `warning: could not record the end of the rule run develop on #42 in the statistics store: disk full` and carries on. `spans` keeps a row with `ended_at` null for a run that finished.

## Related

- `docs/solutions/design-patterns/statistics-store-decides-restart-and-duplicate-label-moves.md`: the store's other two rules, kept in the record's transaction for the same reason.
- `docs/solutions/logic-errors/stale-workspace-claim-drops-another-actions-resume-point.md`: the core's only memory across restarts is the journal's `History`, so nothing replays a lost end.
- #342, #343 (the next span kinds), #315 (the statistics epic).
