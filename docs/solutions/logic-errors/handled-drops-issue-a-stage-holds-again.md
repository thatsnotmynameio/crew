---
title: The live view's Handled section dropped an issue as soon as the next stage took it
date: 2026-10-04
category: logic-errors
module: internal/core, internal/ui/tui
problem_type: logic_error
component: core_reducer
related_components: [tui]
symptoms:
  - "An issue appears in Handled when its stage ends and is gone from it one poll later"
  - "On crew's own workflow (triage, hidden promote triage, development) every entry vanishes within one poll interval"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [live-view, tui, handled, held-again, heldby, on-board, hidden-stage, promote, waiting-card, attention]
---

# The live view's Handled section dropped an issue as soon as the next stage took it

## Problem

Handled showed an issue only until a later stage took it again. On a workflow that hands every issue on, that is one poll, so the boss saw crew's finished work disappear (#109).

## Symptoms

- An issue shows in Handled when its stage ends, then is gone at the next poll.
- On this repository's workflow, `triage` ends in `crew:triage:done`, the hidden `promote triage` takes it at the next poll, then `development` takes it and holds it for hours. No entry survives a poll.
- Issues that end in a label no stage takes (`crew:development:waiting review`, `crew:fix:failed`) stay in Handled. So the drop looks intermittent, not like a rule.

## What Didn't Work

- **Reading it as a bug.** It was a rule: R3 of `docs/plans/2026-10-02-1437-feat-live-view-handled-history-plan.md` and the guide said an issue leaves Handled while a stage holds it again, and a core test asserted it. The first fix run on #109 found that and stopped without a pull request. The boss then moved the issue to development to have the rule reversed. `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md` also states the old rule. Plans are not updated after they ship, so both still read that way.
- **Only dropping the filter in `View`.** The board then drew the issue twice: the held card in the new stage's column, plus a waiting card in the old one. `gone` skips the entries of held issues (`internal/core/gone.go:24`), so the old entry's `Gone` stays false, and the board's `waits` sees a landed move to a stage's label.
- **Keeping "the latest ended stage replaces the entry".** A hidden promote stage ends seconds after taking the issue. Its entry then replaced triage's, and Handled showed `READY · promote triage 4s · now in development` for hours. Triage's outcome, time and cost still vanished one poll after they appeared.
- **Keeping the earlier entry whenever a hidden stage ends well.** That kept an earlier entry that needed attention, too. A failure the boss had sent back through a hidden stage then counted as needing attention again once the hidden stage succeeded, until the next stage took the issue. Code review caught it.

## Solution

- `View` keeps every handled entry and sets `HandledView.HeldBy` to the name of the stage that holds the issue again (`internal/core/model.go:487`).
- `release` replaces the issue's entry as before. The one exception: when the ended stage has `OffBoard` set and both its verdict and the earlier entry ended well, it keeps the earlier entry and marks it `Gone`, because the hidden stage's move took the issue out of that entry's `To` (`internal/core/update.go:547`). Later listings decide `Gone` anew, as for any entry.
- The board's `waits` returns false for an entry with `HeldBy` set (`internal/ui/tui/board.go:73`). The held issue's own card is its only card.
- `needsBoss` is `NeedsAttention() && HeldBy == ""` (`internal/ui/tui/band.go:108`). The tab title, the error progress and the attention-first order use it. A held-again failure shows the last part of its label in the error style instead of `NEEDS ATTENTION`, and its details end in `now in <stage>`.

The core tests are in `internal/core/handled_test.go` (`TestAnIssueTakenAgainKeepsItsEntryMarkedWithTheStageHoldingIt` and the `TestAHiddenStage...` tests). The TUI tests are `TestAnIssueHeldAgainHasOnlyTheCardOfTheStageHoldingIt`, `TestAFailedEntryHeldAgainShowsItsLabelAndSortsByWhenItEnded` and `TestAFailureHeldAgainDoesNotCountAsNeedingAttention`.

## Why This Works

Handled is a history of this run, and the board and Actions already show where a held issue is now. Hiding the history entry while the issue was held made it vanish for exactly as long as crew kept working on it. The core knows who holds each issue and which stages are hidden (`m.stages` holds `crew.Stage`, `OffBoard` included). So it marks the entry rather than leaving the TUI to rebuild that join, and it decides which entry survives a hidden stage. The TUI keeps one rule per screen element: the board skips held entries, and the attention count skips held failures.

## Prevention

- Before turning a "leaves the view while X" rule around, list every consumer of the same list. Here the board's waiting cards, the attention count and the notifications all read `View.Handled`, and each needed its own decision.
- An `on_board: false` stage is still a stage to the core. Any rule keyed on "the latest stage" has to say what a hidden stage's end does, or the hidden stage wins on every workflow that uses promote stages.
- A keep-the-earlier-entry rule must look at the earlier entry, not only at the new verdict. Otherwise it revives a failure the boss already handled.
- Treat R3 of the handled-history plan and the "An issue held again drops out of Handled" line of the dashboard plan as overturned. The guide's "Run it" section and `docs/develop/architecture.mdx` state the current rule.

## Related Issues

- #109, this issue.
- #23 (Handled history) and #33 (the dashboard), which set the overturned rule.
- `docs/plans/2026-10-04-0959-fix-handled-keeps-held-issues-plan.md`, the plan of this fix.
