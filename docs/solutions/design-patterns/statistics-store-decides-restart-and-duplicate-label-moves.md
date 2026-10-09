---
title: The statistics store, not the core, decides restart and duplicate label moves
date: 2026-10-09
category: design-patterns
module: internal/adapter/sqlite, internal/core
problem_type: design_pattern
component: statistics_adapter
severity: medium
applies_when:
  - "Making the core emit IssueSighting or LabelMove records (#424, part 2 of #341)"
  - "Moving the sighting or outside-move rules out of internal/adapter/sqlite/store.go, or into the core"
  - "Adding a statistics store adapter other than sqlite"
  - "Reading the #347, #348 or #351 plans' decision that the core decides what to record"
  - "Changing how the store finds an issue's last label"
tags: [statistics, sqlite, label-moves, issue-sighting, core-purity, multiple-processes, restart, dedupe]
---

# The statistics store, not the core, decides restart and duplicate label moves

## Context

The #347, #348 and #351 plans each settle that "the core decides what to record, the engine writes it" (`docs/plans/2026-10-08-1847-issue-347-plan.md:23`, `docs/plans/2026-10-08-2102-issue-348-plan.md:29`, `docs/plans/2026-10-09-0132-issue-351-plan.md:30`). Plans are not updated after they ship, so they still say so.

#423 (part 1 of #341) added three records: `crew.RepositoryRecord`, `crew.IssueSighting` and `crew.LabelMove` (`internal/crew/statistic.go`). Two decisions about them are made by the SQLite store, not by the core:

1. **The restart rule.** A sighting of an issue the store already holds, whose last label is not the sighting's one state, also inserts a label move made outside crew, at the sighting's time. Without it, a move made while no crew process was running, or between two processes, would never be recorded (`sight`, `internal/adapter/sqlite/store.go:94`).
2. **The outside-move dedupe.** A `LabelMove` with no rule run is inserted only when the issue's last label is not already its `To`, so two crew processes that see the same move record it once. A move with a run is inserted as given (`move`, `internal/adapter/sqlite/store.go:120`).

The reasoning lives in the #423 plan's KTD7 "Conflict call-out", in a plan file the pull request did not commit. Code alone shows what the store does, not why the core does not do it.

## Guidance

- **Keep these two rules in the store, in the record's own transaction.** The core's memory starts empty in each process and holds nothing across processes, so it cannot know an issue's last label after a restart or what another crew process already wrote. Each rule is one conditional insert inside the immediate transaction every record already gets (`_txlock=immediate`, `internal/adapter/sqlite/open.go:62`), so it holds across processes writing `statistics.db` at once.
- **The rejected alternative was reading the store at startup.** It would let the core bridge a restart alone, but it adds a read port and a startup step for one comparison, and it still would not see what another live process writes after startup. Revisit it when a later slice needs other store reads at startup.
- **Everything else stays the core's decision.** The core still decides which sightings and moves exist and builds them; the store only drops a duplicate outside move and fills in the move a restart hid. A new store adapter must implement the same two rules, or part 2's records will differ by store.
- **The last label is the latest move by `id`, not by `seen_at`.** `selectLastLabel` takes the `to_label` of the issue's highest `label_moves.id`, else the issue's `first_label` (`internal/adapter/sqlite/store.go:56-58`). Immediate transactions serialize writers, so `id` order is commit order across processes. `seen_at` comes from each process's own clock and can disagree between them.
- **A null last label records nothing for a sighting, and everything for an outside move.** An issue first seen in two crew labels has a null `first_label`. Until a run's move anchors it, its sightings infer no move, and its outside moves are always written.

## Why This Matters

`docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md` states the default: a decision stays in the pure core, where a table tests it. This is the exception, for a decision that needs memory the core does not have. The core's only memory across restarts is the run journal's `History` (`docs/solutions/logic-errors/stale-workspace-claim-drops-another-actions-resume-point.md`), which holds rule runs, not the labels another process saw.

Someone reading only the shipped plans would expect every record decision in `internal/core` and could "fix" the store by moving the rules there. In the core, a restart loses the move made while crew was down, and two processes each record the same outside move. Neither shows up in a single-process core table test.

## When to Apply

Read this before #424 (part 2 of #341) makes the core emit sightings and moves, before adding another statistics store, and before changing `selectLastLabel` or the `label_moves` order.

Part 2 should also know about three gaps the #423 review left, two residual risks and one missing test:

- **A stale sighting can rewind the label.** Process 1 sights the issue at B; process 2 sights it at C and commits A→C first, while process 1 waits on the busy timeout. Process 1 then inserts C→B, and every later sighting at C inserts B→C. A guard that ignores an outside sighting older than the latest move's `seen_at` would close it.
- **A run's move can be recorded twice.** If another process sights the issue at the run's `To` before the run's move commits, it inserts the outside move, and the run's move is then inserted as given, because moves with a run skip the dedupe. Within one process the engine's ordered writer prevents this.
- **No test races a sighting against an outside `LabelMove`.** `internal/adapter/sqlite/concurrent_test.go` races each path only against itself.

## Examples

Two processes share `statistics.db`; issue 42's last label is `in progress`. A person moves it to `done`, and each process's next listing sees that and records:

```go
crew.LabelMove{Tracker: "github", Issue: id42, From: "in progress", To: "done", Seen: now}
```

The first transaction finds last label `in progress` and inserts the move. The second finds `done`, equal to `To`, and inserts nothing.

crew stops, someone moves issue 42 back to `ready`, and a new process lists it:

```go
crew.IssueSighting{Tracker: "github", Issue: id42, Ref: "#42", Seen: now, State: crew.Some(crew.State("ready"))}
```

The issue row exists, so the insert does nothing, the last label `done` differs from `ready`, and the store inserts the outside move `done` → `ready` at the sighting's time. `internal/adapter/sqlite/issues_test.go` covers both rules, and `concurrent_test.go` covers the dedupe across two stores.
