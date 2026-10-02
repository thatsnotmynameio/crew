---
title: Status Comment - Plan
type: feat
date: 2026-10-02
topic: status-comment
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Status Comment - Plan

## Goal Capsule

- **Objective:** The boss opens any issue crew handles and sees where it stands in one comment: queued and why it waits, which stage and actions are running and what each session last said, or how it ended. They do not have to open a session log or the live view on the machine running crew.
- **Product authority:** this Product Contract, within `STRATEGY.md` and the engine's architecture plan (`docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md`). It is adapted from pururu-ha's dispatcher status report (`thatsnotmynameio/pururu-ha`, `docs/plans/2026-10-01-1853-feat-dispatcher-status-report-plan.md`). Progress inside a session, such as lfg's own phases, the usage-limit pause, blocked issues and promotion to `ready to merge` are not active scope.
- **Open blockers:** none.

---

## Product Contract

### Summary

Each issue crew takes gets a single status comment, always the same one, edited in place. While the issue is queued, the comment says why it waits. While its stage runs, the comment shows the stage, each action's state and running time, and each session's last sentence. After the stage ends, it shows how the stage ended.

### Problem Frame

Today crew comments on an issue only when it moves to `needs attention`, with the failure report. While sessions run, which is most of an issue's time in crew, the issue shows only its `moves_to` label, such as `in progress`. To know what a session is doing, the boss has to read `.crew/logs/issue-<N>-<action>.log` or watch the live view on the machine running crew. pururu-ha's dispatcher solved the same problem with a status comment. That behavior is listed in `docs/guide/crew.mdx` under "Not built yet from the old dispatcher".

### Key Decisions

- **One comment, edited in place, not a new comment per change.** Governs R1. (session-settled: user-directed — chosen over a new comment at each status change: the issue keeps one place to look instead of a growing history; carried from pururu-ha's plan at the boss's request)
- **The failure report stays a separate comment.** Editing a comment notifies no one, so the comments that should notify keep being posted. Governs R2. (session-settled: user-directed — chosen over folding it into the status comment, which would stop its notification; carried from pururu-ha's plan)
- **The comment starts in the queue, not when the issue is taken.** Governs R3, R4. (session-settled: user-directed — chosen over commenting only once sessions run; carried from pururu-ha's plan)
- **crew reads what the session already writes; the session is not asked to report.** This keeps working when a session hangs or dies, which is when the status matters most. Governs R9. (session-settled: user-approved — chosen over telling the session in its prompt to edit the comment; carried from pururu-ha's plan)
- **A comment left behind when the boss removes the label stays as it was.** See Scope Boundaries. (session-settled: user-approved — chosen over an extra search each poll to mark such issues "not queued"; carried from pururu-ha's plan)
- **The comment shows crew's stages and actions, not lfg's phases.** crew's workflow and prompts are configurable, and lfg is one prompt among others. A checklist of lfg's skills is the "per-stage progress" item, left for later. Governs R7, R8. (session-settled: user-approved — chosen over pururu-ha's checklist of lfg's phases, which ties the comment to one prompt and one harness's log format)
- **The status comment is an optional tracker capability.** Following the engine's R12, crew works as it does today with a tracker that lacks it. Governs R13.
- **English text, matching crew's other comments and labels.** Governs R12.

### Requirements

**One comment per issue**

- R1. crew keeps exactly one status comment per issue. It is created the first time crew reports on the issue, then edited in place across stages, crew restarts and re-queues after `needs attention`.
- R2. The failure report crew posts on a move to `needs attention` is still posted as a new comment, unchanged.
- R3. Only queuing or taking an issue creates its status comment. Every later report edits an existing one, so an issue crew handled before this feature never gets one.

**In the queue**

- R4. An issue that carries a stage's `label` and is listed but not taken in a poll gets the comment saying it is queued for that stage, waiting for a free slot (`config.max_parallel_issues`).
- R5. Outside running sessions, the comment is edited only when its text changes.

**While a stage runs**

- R6. At every poll while any of the issue's actions runs, the comment is updated with the time it was updated.
- R7. The comment names the stage and lists each of its actions as running, succeeded or failed, with each running action's elapsed time.
- R8. Each running action shows the last sentence its session narrated, once it has one.
- R9. That sentence comes from the session's own output; the session is not asked to report anything. With a harness that cannot provide it, the comment shows the rest without it.
- R10. The sentence goes through the same cleaning as the failure report: the repository's path shows as `.` and the home directory as `~`, and it sits in a code block, so nothing in it renders or mentions anyone.

**After a stage**

- R11. When the stage ends, the comment shows each action's final state and where the issue went: the next state on success, or `needs attention` on failure, including when crew stopped the sessions. When a later stage takes the issue from that state, the same comment shows the new stage.

**Text and trackers**

- R12. The comment is written in English.
- R13. A tracker adapter without status comments leaves crew working as it does today, with no status reported and no error. `github` provides them.

### Acceptance Examples

- AE1. **Covers R1, R3, R4, R5.** **Given** `max_parallel_issues: 2`, two issues running and #74 newly labelled `ready`, **when** a poll lists #74, **then** crew creates #74's comment saying it is queued for `implement`, waiting for a free slot. The next poll, with nothing changed, does not edit it.
- AE2. **Covers R6, R7, R8.** **Given** #74 was taken by `implement`, whose action `lfg` has run for 42 minutes and last wrote "U1 committed: 168 tests pass. Starting U2.", **when** a poll runs, **then** the comment shows `implement`, `lfg` running for 42 minutes with that sentence, and the update time.
- AE3. **Covers R7, R11.** **Given** a stage with two actions, `development` and `acceptance`, where `acceptance` has succeeded and `development` still runs, **when** a poll runs, **then** the comment shows `acceptance` succeeded and `development` running. When `development` fails, the comment shows both final states and that #74 moved to `needs attention`, and the failure report is posted as a separate comment (R2).
- AE4. **Covers R2, R11.** **Given** the boss stops crew with Ctrl-C while #74's `lfg` runs, **when** crew stops the session and moves #74 to `needs attention`, **then** the comment shows `lfg` failed and the move, and the failure report is posted as today.
- AE5. **Covers R1.** **Given** crew restarts after #74 moved to `in review`, **when** a later stage takes #74, **then** crew edits #74's existing comment, and no second status comment appears.
- AE6. **Covers R10.** **Given** a session's last sentence names `/Users/boss/Projects/crew/internal/core/update.go` and contains `@someone`, **when** the comment shows it, **then** the path reads `./internal/core/update.go`, and the sentence sits in a code block that mentions no one.
- AE7. **Covers R3.** **Given** #60 was already `in review` before this feature, with no status comment, **when** polls run, **then** crew never creates one for it.

### Scope Boundaries

- An issue whose label the boss removes while it is queued keeps its last status comment. crew does not look for it again.
- An issue skipped for carrying two crew labels gets no status comment; crew reports it as it does today.
- Edits notify no one. Notification stays with the failure report (R2).
- No progress inside a session, such as lfg's phases or "unit 2 of 5". The session's last sentence carries whatever it says. This is the "per-stage progress" item.
- The usage-limit pause, blocked issues and promotion to `ready to merge` are not built in crew, so the comment has no wording for them yet.
- Nothing moves to the issue's body or to the pull request.
- Deleting or collapsing duplicate status comments is not built. A duplicate exists only when a create succeeded but its reply was lost.

### Dependencies / Assumptions

- The repository is public. The session's last sentence is posted as written, after the cleaning in R10.
- Only issues the boss opened are listed (`docs/guide/crew.mdx`, "What crew does on each poll"), so only they get a status comment.

### Outstanding Questions

**Deferred to Planning**

- How the comment is found again after a restart. pururu-ha uses a hidden marker on the comment's last line, written only by the `gh` login, with the newest winning.
- How a running session's last sentence reaches the core: an optional harness capability, or the engine reading the session's log.
- Whether a failed status write is retried like an owed move, or only recomputed at the next poll. A running or queued status is recomputed every poll anyway.

### Sources

- `thatsnotmynameio/pururu-ha`, `docs/plans/2026-10-01-1853-feat-dispatcher-status-report-plan.md`: the source plan, its KTD1 (finding the comment by a hidden marker) and KTD6 ("the text changes" ignores the update time).
- `docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md`: R12 (optional capabilities) and its table of behaviors to port ("Status comment edited in place: optional tracker capability, fed by domain events").
- `docs/develop/architecture.mdx`: the core's events (`IssueTaken`, `ActionStarted`, `ActionEnded`, `IssueMoved`, `FailureReported`, `PollDone`) and the `Tracker` port.
- `docs/guide/crew.mdx`: the failure report, path cleaning, "Stop it" and "Limits".
