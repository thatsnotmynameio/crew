---
title: A board the boss configures - Plan
type: feat
date: 2026-10-04
topic: configurable-board
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #110
execution: code
---

# A board the boss configures - Plan

## Goal Capsule

- **Objective:** while crew runs, the boss sees on the live view's board the issues they care about, such as parked ideas, open bugs and finished work, grouped into columns they chose.
- **Means:** a new top-level `board` key in `.crew/config.yaml` that lists the columns and the labels of each.
- **Product authority:** the boss, through the brainstorm of #110.
- **Open blockers:** none.

---

## Product Contract

### Summary

`.crew/config.yaml` gets a top-level `board` key: a list of columns, each with a name and one or more GitHub labels. When it is set, the live view's board shows every open issue that carries a column's labels, read from GitHub at each poll, and the stages no longer shape it. When it is not set, the board stays as it is today.

### Problem Frame

The board's columns come from the workflow's stages, and it shows only the issues crew has in play (`docs/guide/crew.mdx`, "Run it"). The boss cannot choose what it shows. Parked ideas waiting in `crew:brainstorm:ready`, open bugs, and finished work waiting for review never reach it. To see them, the boss leaves the terminal for GitHub.

### Key Decisions

- **A configured board lists every open issue with a column's labels.** (session-settled: user-directed — chosen over only the issues crew has in play, the decision of #33, and over the issues crew touched this run: the boss wants columns such as ideas and bugs that crew never takes.) Governs R6.
- **A column names any GitHub label.** (session-settled: user-directed — chosen over only the labels the config already names, which crew could check at load: the boss wants columns such as `bug`.) Governs R3.
- **With `board` set, the board ignores the stages.** (session-settled: user-directed — chosen over keeping `on_board` as a filter on the configured board, over refusing a config with both, and over letting a hidden stage still hide its cards.) Governs R5.
- **`on_board: false` still mutes notifications.** Without it, the clerk's promote stages would notify every few minutes. (session-settled: user-approved — proposed with every stage notifying, and notifications following the board, as alternatives.) Governs R14.
- **An issue shows in every column whose labels it carries.** (session-settled: user-approved — chosen over only the first matching column: a column lists every open issue with one of its labels.) Governs R7.
- **Issues only, no pull requests.** (session-settled: user-directed — chosen over issues and pull requests together, and over a per-column choice like a stage's `takes`.) Governs R6.
- **The board refreshes at each poll.** (session-settled: user-approved — chosen over a refresh interval of its own: no extra GitHub calls between polls.) Governs R9.
- **Cards look as they do today.** (session-settled: user-approved — chosen over reference and title only, and over also showing the issue's labels.) Governs R10, R11.
- **The key is a top-level `board` list.** (session-settled: user-approved — chosen over nesting it as `config.board`.) Governs R1.
- **The board lists the same authors crew lists.** (session-settled: user-approved — proposed after the check found that crew lists only issues opened by the boss and the mates; the boss accepted.) Governs R8.
- **Without `board`, the board is today's.** The issue asked for it; the scoping synthesis named the consequence that `board` changes what a card means, and the boss accepted. (session-settled: user-approved — chosen over making every board label-based.) Governs R4.

### Requirements

**Config**

- R1. `.crew/config.yaml` takes a top-level `board` key: a list of columns, drawn left to right in list order, each with a `name` and `labels`, a list of one or more labels.
- R2. crew refuses to start, as for any config error, when `board` is an empty list, a column has no name or no label, or two columns share a name, and the error names the column.
- R3. A column's labels may be any GitHub label, crew's own or not. crew does not check at load that a label exists on GitHub, so a label no issue carries shows an empty column.
- R4. When the config has no `board`, the board is unchanged: one column per stage, except the stages with `on_board: false`, holding the issues crew has in play.

**What a configured board shows**

- R5. With `board` set, the board's columns are exactly the configured ones. Stages and `on_board` neither add, hide nor order columns.
- R6. A column holds a card for each open issue that carries at least one of its labels. Pull requests never get a card, including those crew gave an issue's label.
- R7. An issue that carries the labels of several columns has a card in each. An issue that carries no column's label has no card, even while crew holds it; its work still shows in Actions.
- R8. The board shows only issues opened by the boss or a mate, the same issues crew takes.
- R9. The board's issues are read from GitHub every `poll_interval_seconds`, including when crew is too busy to take new issues. A move crew makes shows on the board at once; a label changed on GitHub, or an issue closed there, shows after the next read.

**Cards**

- R10. A card shows the issue's reference and title, and crew's claim (`⠋ running`, `◌ taking`, `⠋ judging`, `! owed`, `■ stopping`) while crew holds the issue.
- R11. When an issue's labels move it to another column, its card slides there, as cards slide today.
- R12. A configured board has no waiting card: a card sits wherever the issue's labels put it, and never stays in a column marked `→` and a label.
- R13. A column that does not fit its cards ends in `+N more`, and columns that do not fit the window drop when empty, then scroll sideways, as today.

**Unchanged and documented**

- R14. A stage with `on_board: false` sends no desktop notification, whether or not `board` is set.
- R15. `--plain`, Actions, Queues, Handled and Events are unchanged.
- R16. `docs/guide/crew.mdx` documents the `board` key in its keys and describes the configured board in "Run it", next to today's board.

### Acceptance Examples

- AE1. **Covers R1, R6, R7, R10.** Given a board of `ideas` (`crew:brainstorm:ready`), `bugs` (`bug`) and `done` (`crew:brainstorm:done`, `crew:triage:done`), and #20 open with `bug` and `crew:fix:in progress` while fix runs on it, #20 has one card, in `bugs`, marked `⠋ running`. No column names `crew:fix:in progress`, so #20 has no other card.
- AE2. **Covers R7.** Given the same board, #21 open with both `bug` and `crew:brainstorm:ready` has a card in `ideas` and one in `bugs`.
- AE3. **Covers R9, R11.** Given a board of `triage` (`crew:triage:ready`, `crew:triage:in progress`) and `review` (`crew:development:waiting review`), and #12 in development, when development ends and moves #12 to `crew:development:waiting review`, a card for #12 slides into `review` at once, without waiting for the next poll.
- AE4. **Covers R9.** Given every slot is busy, so crew takes no new issue at this poll, when someone labels #30 `bug` on GitHub, #30 gets a card in `bugs` after the next poll.
- AE5. **Covers R2.** Given a column `bugs` with `labels: []`, crew refuses to start and names `bugs`.
- AE6. **Covers R3.** Given a column whose only label is the typo `bgu`, crew starts and the column is empty.
- AE7. **Covers R8.** Given #40 labeled `bug`, opened by someone who is neither the boss nor a mate, #40 has no card.
- AE8. **Covers R4.** Given this repository's config with no `board`, the board is the one of #33: triage, development and fix columns, holding only the issues crew has in play.
- AE9. **Covers R14.** Given `board` set and the terminal not focused, when promote triage, which has `on_board: false`, ends on #12, crew sends no notification; when triage ends on #12, it sends one.

### Scope Boundaries

- Pull requests on the board.
- Issues opened by anyone other than the boss and the mates.
- Closed issues: a `done` column shows open issues with a done label, not closed ones.
- A refresh interval of the board's own.
- Checking at load that a column's labels exist on GitHub.
- Hiding, ordering or filtering a configured board by stage.
- Any change to `--plain`, Actions, Queues, Handled or Events.

### Outstanding Questions

**Deferred to Planning**

- Whether a column's label matches an issue's label regardless of case, as GitHub treats label names.
- How the tracker lists open issues by any label: `port.Tracker.List` returns only crew states, and the GitHub adapter asks only for the stages' labels.
- How the board's read joins the poll when the core skips its listing because every slot is busy, as R9 requires.
- How many issues a column can read: the adapter lists the first 100 issues per author.
- How a card slides when its issue has cards in several columns.
- The order of the cards in a column, which also decides which cards `+N more` hides. It must stay stable from one read to the next.
- The Workflow section's title and summary on a configured board.

### Sources / Research

- Board: `internal/ui/tui/board.go` (`cards`, `waits`, `layout`, `boardRows` with `+N more`).
- Notifications muted by `on_board`: `internal/ui/tui/outside.go:51`.
- `on_board` parsed at `internal/config/validate.go:24` and `:122`, into `crew.Stage.OffBoard` (`internal/crew/workflow.go:36`).
- Top-level keys and strict decoding: `internal/config/config.go:77-84`, `internal/config/decode.go:80-86`.
- Tracker: `port.Tracker.List` (`internal/port/port.go:47-54`) carries only crew states. The GitHub adapter's query filters by label and author, the first 100 per author (`internal/adapter/github/tracker.go:85-95`, `:205-211`, `:434`).
- Poll: `internal/engine/engine.go:198` (ticker), `internal/core/update.go:93-96` (listing skipped when every slot is busy), `internal/core/update.go:112-121` (only the stages' labels are listed).
- Pull requests get their issue's crew label: `internal/adapter/github/pullrequest.go:57`, `:165`; `docs/guide/crew.mdx:10`.
- Board docs: `docs/guide/crew.mdx:144` (`on_board`), `:591` (the board), `:613` (notifications).
- The board's design and the in-play decision this plan reverses: `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md`.
