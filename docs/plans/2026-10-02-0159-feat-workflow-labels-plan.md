---
title: Workflow Labels - Plan
type: feat
date: 2026-10-02
topic: workflow-labels
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Workflow Labels - Plan

## Goal Capsule

- **Objective:** The boss reads `.crew/config.yaml` and sees the exact labels each issue moves through, with no second table that maps names to labels.
- **Means:** the workflow writes GitHub label text directly. `tracker.labels` and crew's fixed set of eight states go away, and each stage names its own failure label.
- **Product authority:** this Product Contract, within `STRATEGY.md`. Behavior for the reserved states (`paused`, `ready_to_merge`, `done`) is not active scope.
- **Open blockers:** none.

---

## Product Contract

### Summary

A stage's `label`, `moves_to`, `on_success` and a new required `on_failure` hold the label text as it appears on GitHub, such as `moves_to: in progress`. `tracker.labels` and the eight fixed states are removed. crew's labels are exactly the ones the workflow names.

### Problem Frame

Today the workflow names crew states (`in_progress`), and `tracker.labels` maps each of the eight states to a GitHub label (`in progress`). To know which label an issue gets, the boss has to read two places. Three of the eight states (`paused`, `ready_to_merge`, `done`) have no behavior: they only exist so the mapping is complete. Failure is the one destination outside the workflow, hardcoded to `needs_attention`.

### Key Decisions

- **The workflow writes label text, and crew has no fixed states.** The indirection added nothing the boss needed. A future tracker would have the workflow name its own statuses the same way. Governs R1, R3, R4. (session-settled: user-directed — chosen over keeping `tracker.labels` with the eight state keys: the mapping is not needed)
- **Each stage declares `on_failure`, and the key is required.** Every label crew touches is then written in the workflow, at the cost of one line per stage. Governs R2, R8. (session-settled: user-directed — chosen over a fixed `needs attention` label and over an optional `on_failure` defaulting to `needs attention`: no label should be hidden from the workflow)
- **crew's labels are the labels the workflow names.** A label no stage names is the boss's, not crew's. Governs R7. (session-settled: user-approved — consequence shown: an issue with `paused` and `ready` is now taken instead of skipped)
- **`on_failure` follows the rules of `on_success`.** It may be another stage's `label`, which allows a stage that picks up failures and also an automatic retry loop. Governs R6. (session-settled: user-approved — chosen over forbidding a stage's label as a failure destination)
- **Output shows label text.** Event lines and the live view show what the boss sees on GitHub (`ready -> in progress`). Governs R9. (session-settled: user-approved)

### Requirements

**Workflow**

- R1. Each stage's `label`, `moves_to`, `on_success` and `on_failure` hold label text exactly as it is written on the tracker, such as `in progress`.
- R2. `on_failure` is required on every stage: it is the label an issue moves to when any of the stage's actions failed, with crew's failure comment as today.
- R3. `tracker.labels` is no longer a key. A config that has it is refused like any unknown key, naming the key and its line, and crew exits with `2`.
- R4. Any non-empty label text is accepted. There is no list of valid states to match.

**Validation**

- R5. Label texts that differ only in case are the same label in every workflow check, because GitHub does not tell them apart. The current rules stay: two stages cannot take the same label, `moves_to` cannot be any stage's `label`, and `on_success` cannot be the stage's own `label`.
- R6. `on_failure` cannot be the stage's own `label`, and it may be any other stage's `label`.

**crew's labels on GitHub**

- R7. crew's labels are the labels any stage names in its four keys, and no others. An issue carrying two of them is skipped and reported. A move removes the other crew labels and leaves every other label alone.
- R8. At startup crew creates the workflow's labels that the repository lacks, each `on_failure` label included. It creates no label the workflow does not name, so `needs attention` is no longer created unless a stage names it.

**Output**

- R9. Event lines and the live view show labels as written in the workflow, such as `implement took #42 "Add login form" (ready -> in progress)`.

**Docs and crew's own config**

- R10. `docs/guide/crew.mdx`, `docs/develop/architecture.mdx` and this repository's `.crew/config.yaml` follow the new shape in the same pull request. The guide's "Limits" no longer says the `paused` state is reserved.

### Acceptance Examples

- AE1. **Covers R1, R2, R7.** **Given** a stage with `label: ready`, `moves_to: in progress`, `on_success: in review` and `on_failure: needs attention`, **when** issue #42 is labeled `ready`, **then** crew takes it and #42 carries `in progress` and none of the other three. If an action fails, #42 moves to `needs attention` and gets crew's failure comment.
- AE2. **Covers R2.** **Given** a stage without `on_failure`, **when** crew starts, **then** it exits with `2` and the message names the stage's `on_failure` key and the stage's line.
- AE3. **Covers R3.** **Given** a config that still has `tracker.labels`, **when** crew starts, **then** it exits with `2` and the message names `tracker.labels` and its line.
- AE4. **Covers R5.** **Given** two stages with `label: Ready` and `label: ready`, **when** crew starts, **then** it exits with `2` because both stages take the same label.
- AE5. **Covers R6.** **Given** stage `review` with `label: ready to review` and `on_failure: ready to review`, **when** crew starts, **then** it exits with `2`. **Given** instead `on_failure: ready`, the label of stage `implement`, **then** crew starts, and a failed review goes back to `implement`.
- AE6. **Covers R7.** **Given** no stage names `paused` and issue #7 carries `paused` and `ready`, **when** crew polls, **then** it takes #7, and after the move #7 carries `paused` and `in progress`.
- AE7. **Covers R8.** **Given** a repository with only `ready`, and a workflow naming `ready`, `in progress`, `in review` and `needs attention`, **when** crew starts, **then** it creates `in progress`, `in review` and `needs attention`, and nothing else.

### Scope Boundaries

- Behavior for `paused` (usage-limit pause), `ready_to_merge` and `done`. Each comes back as a label the workflow names when its feature is built.
- Migrating old configs. An old config fails at startup (R2, R3), and the boss rewrites it by hand.
- Per-stage label colors or descriptions on GitHub.

### Outstanding Questions

**Deferred to Planning**

- What the domain calls a workflow label once it is free text, and whether the tracker port keeps the word "state".
- How a move from a label to the same label behaves, such as `on_failure` equal to the stage's `moves_to`, and whether the workflow checks refuse it.

### Sources

- `internal/crew/state.go`: the eight states, `States()` and `Valid()`.
- `internal/adapter/github/config.go`: `tracker.labels`, its defaults and the case-insensitive duplicate check.
- `internal/config/validate.go`: `checkGraph` and `state`, the workflow rules R5 and R6 extend.
- `internal/core/update.go` (the failed-stage move) and `internal/engine/engine.go` (`workflowStates`): the two places `needs_attention` is fixed today.
- `internal/fake/tracker.go`: accepts and ignores a `labels` section.
- `docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md`: the decision "crew's states are the engine's vocabulary" and R15, which this plan replaces.
