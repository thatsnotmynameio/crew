---
title: The mates in the live view - Plan
type: feat
date: 2026-10-04
topic: mates-section
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #114
execution: code
---

# The mates in the live view - Plan

> Superseded by `docs/plans/2026-10-04-1217-feat-mates-section-plan.md`, which carries this Product Contract with its implementation planning.

## Goal Capsule

- **Objective:** while crew runs, the boss sees in the live view, for each mate and for themselves, whether it can act right now, what acts as it, what runs as it now, and what it cost this run, without leaving the terminal.
- **Means:** a Mates section at the top of the live view, plus crew reporting when a mate stops acting mid-run.
- **Product authority:** the boss, through the brainstorm of #114.
- **Open blockers:** none.

---

## Product Contract

### Summary

The live view gets a Mates section right under the header, with two lines per configured mate and a last row for the boss's own `gh` login. The first line says whether the mate can act and what it cost this run. The second says which stages and actions act as it and what runs as it now. When a mate stops acting during the run, its row, the header and Events say so.

### Problem Frame

The live view never names a mate. The Actions section shows the queue an issue holds a slot in, not the mate it acts as. A mate that cannot act at startup gets one warning line under the header, and nothing more.

During the run, two failures stay silent. When GitHub keeps refusing the default mate, crew's own writes go as the boss until restart, and the docs say so: "crew warns only at startup". When a session's or check's mate token fails to renew, crew drops the error, and once the old token expires the action fails with gh auth errors that look like any other failure. To find out which identity did what, the boss reads GitHub.

### Key Decisions

- **The section answers health, mapping, current work and cost equally.** (session-settled: user-directed — chosen over a section about only one of whether each mate can act, what it is doing, or who acts as whom.) Governs R4, R5, R6, R7.
- **The section follows the mates' live state.** (session-settled: user-approved — chosen over showing only what crew knew at startup: a row saying "acting" while crew's writes go as the boss would mislead.) Governs R8, R9, R10.
- **A session token that fails to renew counts as a mate that stopped acting.** (session-settled: user-approved — proposed after the check found the renewal error is dropped; without it, the row says "acting" while its sessions fail.) Governs R9.
- **The section shows a short state; the header keeps the full reason and fix.** (session-settled: user-directed — chosen over moving the warnings into the section and over showing them in both places in full.) Governs R4, R11.
- **A row shows what runs now plus this run's totals.** (session-settled: user-directed — chosen over only what runs now, and over now plus a count without cost.) Governs R6, R7.
- **The boss gets a row, and the section shows even without mates.** (session-settled: user-directed — chosen over mates-only rows with no section when no mate is configured, and over a boss row only when work acts as the boss.) Governs R2, R3.
- **The section sits at the top, under the header.** (session-settled: user-directed — chosen over a full-width section after Actions and a column in the band beside Queues and Handled.) Governs R1.
- **Two lines per mate.** (session-settled: user-directed — chosen over one truncated line per mate.) Governs R4, R5.
- **Mates gives way last, just before the view is cut.** (session-settled: user-directed — chosen over dropping the second lines first, and over dropping them with Actions' session lines.) Governs R13.
- **An action's totals go to the identity it acted as.** An action that ran as the boss because its mate could not act counts on the boss's row, so each row's cost matches what GitHub shows under that identity. Governs R7.

### Requirements

**The section**

- R1. The live view shows a Mates section directly under the header and its warnings, above Workflow. Its title rule ends in a summary that counts the mates that act and those that do not, such as `2 acting · 1 cannot act`.
- R2. The section has one entry per mate the config names, the default mate first, then the others in the order the config first names them, and a last entry for the boss, labelled `you` with the `gh` login crew acts as when it acts as the boss.
- R3. A config that names no mate still shows the section, with only the `you` entry.

**One entry**

- R4. An entry's first line shows the name, a short state (`acting`, or `cannot act` with a short reason such as `no key`), and this run's totals per R7. The `you` entry has no state.
- R5. An entry's second line lists the stages and actions that act as it, as `stage/action`, marks the default mate's line with crew's own writes, and ends with the actions running as it now, each with its issue reference. When the line does not fit the window, the list of stages and actions is cut with `…` first, so the actions running now stay visible.
- R6. An action counts as running on an entry from when its session starts until it ends, its check included.
- R7. The totals count the actions that ended this run as that identity: how many, their cost and their tokens. Cost and tokens follow the header and Handled: a cost some session did not report is marked `(partial)`, and none at all reads `cost not reported`. The totals start at zero each time crew starts.
- R8. A mate that cannot act at startup shows `cannot act` and its short reason. Its second line says its stages and actions act as `you`, and those actions run and count on the `you` entry.

**Live state**

- R9. A mate stops acting during the run when crew's writes as the default mate fall back to the boss, or when a token of a session or check acting as the mate fails to renew. Its entry's state changes then, with a short reason, such as `writes as you` or `token not renewed`.
- R10. A mate whose token renews after a failure acts again: its entry's state returns to `acting`, and its header warning goes. A writes fallback lasts until crew restarts, as today.
- R11. When a mate stops acting during the run, the header gains a warning with the full reason and the fix, in the words of today's startup warnings. Events gains a line each time a mate stops acting, saying which mate and why, and each time it acts again. `--plain` prints those lines like any event.
- R12. A mate's startup warning stays under the header for the whole run, as today, next to its entry's short state.

**Fit and the rest**

- R13. When rows run out, the existing order holds (Events, then Handled, then Actions' session lines, then board cards), then each Mates entry drops its second line, and only then is the view cut.
- R14. No key of the live view acts on a mate, and the view adds no way to create or fix one: `crew mates create` stays the way.
- R15. `--plain` keeps its startup warning lines, and Actions, Queues, Handled, the board and Events are otherwise unchanged.
- R16. `docs/guide/crew.mdx` documents the section in "Run it", and "When a mate cannot act" no longer says crew warns only at startup and describes the mid-run warnings.

### Acceptance Examples

- AE1. **Covers R2, R4, R5, R7.** Given the default mate `clerk` for triage and `developer` for `implement/development`, when development is running on #1 and three triage actions have ended, `clerk`'s first line shows `acting` and the three actions with their cost and tokens; its second line shows crew's writes and `triage/triage`. `developer`'s second line ends with `#1 development/lfg` as running now.
- AE2. **Covers R3.** Given a config with no `mate`, the section has only the `you` entry, and every action that ends counts on it.
- AE3. **Covers R8, R12.** Given `reviewer` has no key on this machine, its first line shows `cannot act: no key`, its second line says `review/review` acts as `you`, the header keeps the full warning with `crew mates create reviewer`, and a review action that ends counts on `you`.
- AE4. **Covers R9, R11.** Given GitHub keeps refusing the default mate `clerk` mid-run, so crew's writes fall back to the boss, `clerk`'s state changes to `writes as you`, the header gains the full warning, and Events gains a line, which `--plain` prints too.
- AE5. **Covers R9, R10, R11.** Given a renewal of `developer`'s session token fails, `developer`'s state changes to `token not renewed`, the header gains a warning and Events gains a line; when the next renewal succeeds, the state returns to `acting`, that warning goes, and Events gains a line saying `developer` acts again.
- AE6. **Covers R13.** Given a window too short for every section, after Events, Handled, Actions' session lines and board cards have shrunk, each Mates entry drops its second line before the view is cut.

### Scope Boundaries

- Acting on a mate from the live view, or creating or fixing one there.
- Totals that survive a restart.
- Showing a mate on Actions' rows or on board cards.
- Retrying a writes fallback to the mate during the run: it lasts until restart, as today.
- A cost for a running action: it shows once the action ends, as in Handled.

### Dependencies / Assumptions

- crew knows each action's mate from the config, and whether each mate acts at startup, but neither reaches the live view today.
- crew today emits nothing when its writes fall back to the boss or a session token fails to renew. R9 to R11 need crew to report both.

### Outstanding Questions

**Deferred to Planning**

- Where crew learns the `gh` login it acts as, for the `you` entry: today the view receives neither it nor the boss's logins.
- How crew's writes fall back for a 404 or 410 only after the boss's retry succeeds, and whether that case reads as `writes as you` too.
- How a token that is still valid but failed to renew differs from one that expired, and whether the state changes at the failure or at expiry.
- The exact short reasons, and the section's summary when no mate is configured.
- The order of the stages and actions on a second line, and what it shows for a mate no action names (the default mate with only crew's writes).

### Sources / Research

- Warnings built once at startup: `internal/mates/act.go:232`, `:241`, `:307`, `:311`; passed to the renderers at `internal/app/app.go:150`, `:308`, `:315`; drawn under the header at `internal/ui/tui/layout.go:96-98`; printed first by `--plain` at `internal/ui/lines/lines.go:32-34`.
- Writes fallback to the boss, silent: `internal/adapter/github/gh.go:83-131`.
- Token renewal error dropped: `internal/mates/act.go:365-384` (`_ = a.renew(ctx, s)` at `:380`).
- Actions shows the queue: `internal/ui/tui/actions.go:78`, `:90`.
- What the view receives: `internal/engine/stream.go:19-44`; `ActionView` carries no mate (`internal/core/model.go:440-455`), though core holds it (`internal/core/model.go:112`).
- Per-action spend on Handled entries: `internal/core/model.go:379-386`; run total `internal/core/model.go:326-346`.
- Each action's mate resolved at parse: `internal/crew/workflow.go:72-75`, `internal/config/validate.go:147-192`.
- Boss logins: `port.BossFinder` (`internal/port/port.go:217-223`), stored at `internal/engine/engine.go:296-298`.
- Shrink order: `internal/ui/tui/layout.go:48-89`.
- Docs to update: `docs/guide/crew.mdx` "When a mate cannot act" and "Run it".
