---
title: Run Time Limit - Plan
type: feat
date: 2026-10-02
topic: run-time-limit
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Run Time Limit - Plan

## Goal Capsule

- **Objective:** The boss can start crew for a set amount of time, such as overnight, and it winds down by itself when that time is up, without losing work in progress.
- **Product authority:** this Product Contract, within `STRATEGY.md`. Stopping on idleness, at a clock time, or through a CLI flag is not active scope.
- **Open blockers:** none.

---

## Product Contract

### Summary

A new optional key in `config` limits how long crew runs. Without it, crew polls until it is stopped, as it does today. When the time is up, crew takes no new issues, lets the running sessions finish, and exits with `0`.

### Key Decisions

- **The limit counts total run time.** Governs R2. (session-settled: user-directed — chosen over a limit on idle time and over a clock end time)
- **When the time is up, running sessions are left to finish.** No issue lands in `needs attention` because of the limit, even if crew then runs well past it. Governs R4, R5. (session-settled: user-directed — chosen over stopping like Ctrl-C, which kills sessions after 10 seconds and moves their issues to `needs attention`)
- **The value is in seconds.** It matches `config.poll_interval_seconds`, so 8 hours is `28800`. Governs R3.
- **The clock starts at the first poll, not at launch.** The startup checks can take up to 10 minutes and should not use up the run time. Governs R2.

### Requirements

**Configuration**

- R1. The limit is optional, and without it crew runs until it is stopped, exactly as today.
- R2. When the limit is set, crew stops taking new issues once that many seconds have passed since its first poll.
- R3. A limit that is not a positive whole number of seconds is a config error. crew refuses to start and exits with `2`, naming the key and its line, like every other config error.

**Winding down**

- R4. After the limit, sessions already running go on until they end, and their issues are judged as usual.
- R5. Moves and comments still owed get their usual last try, and crew then exits with `0`.
- R6. crew says that it is winding down because the run time is up, both in the live view and in `--plain` output, so this end is not mistaken for a crash or a manual stop.
- R7. Ctrl-C, SIGTERM or SIGHUP while crew winds down stops it the usual way, and a second one forces the exit.

**Docs**

- R8. `docs/guide/crew.mdx` lists the key in its config table and describes the wind-down next to "Stop it". The "No time limit on a session" limit stays, since this limit applies to crew and not to a single session.

### Acceptance Examples

- AE1. **Covers R1.** **Given** a config without the limit, **when** crew runs for days, **then** it keeps polling until someone stops it.
- AE2. **Covers R2, R5.** **Given** a limit of `3600` and no running session when the hour is up, **when** the hour passes, **then** crew takes no more issues and exits with `0` once owed calls have had their last try.
- AE3. **Covers R2, R4, R6.** **Given** a limit of `3600` and issue #42's session still running when the hour is up, **when** the hour passes, **then** crew says it is winding down and takes no new issue, even if one is `ready`. #42's session runs to its end, and #42 moves to its `on_success` state (or to `needs attention` if the action failed), as it would without the limit.
- AE4. **Covers R7.** **Given** crew winding down with #42's session running, **when** the boss presses Ctrl-C, **then** #42's session gets 10 seconds to stop, #42 moves to `needs attention`, and crew stops as it does on any Ctrl-C.
- AE5. **Covers R3.** **Given** a limit of `0`, `-5` or `"8h"`, **when** crew starts, **then** it exits with `2` before its first poll, and the message names the key and its line.

### Scope Boundaries

- Stopping after a stretch with no ready issue and no running session.
- Stopping at a clock time, such as 07:00.
- A CLI flag that sets or overrides the limit.
- A limit on a single session's run time.
- Durations written as `8h` or `90m`.

### Outstanding Questions

**Deferred to Planning**

- The key's name, under `config` next to `poll_interval_seconds`.
- Whether crew keeps polling while it winds down so it can retry owed moves, or only retries them at the end. Either way, it takes no new issue (R2).

### Sources

- `internal/engine/engine.go` (`Engine.Run`): the poll loop and the stop sequence that the wind-down sits next to.
- `internal/config/config.go`: `poll_interval_seconds`, its default and its validation, the pattern for the new key.
- `docs/guide/crew.mdx`: the config table, "Stop it", "Exit codes" and "Limits".
