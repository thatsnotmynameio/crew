---
title: The live view's Handled section dropped an issue as soon as the next stage took it
date: 2026-10-04
last_updated: 2026-10-05
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
tags: [live-view, tui, handled, held-again, heldby, rule-without-actions, notify, promote, attention]
---

# The live view's Handled section dropped an issue as soon as the next stage took it

Stages are called rules since the new config keys of #134, which this update ships with. This learning uses the words of the time where it tells what happened, and the current ones in Solution and Prevention.

## Problem

Handled showed an issue only until a later stage took it again. On a workflow that hands every issue on, that is one poll, so the boss saw crew's finished work disappear (#109).

## Symptoms

- An issue shows in Handled when its stage ends, then is gone at the next poll.
- On this repository's workflow, `triage` ends in `crew:triage:done`, the hidden `promote triage` takes it at the next poll, then `development` takes it and holds it for hours. No entry survives a poll.
- Issues that end in a label no stage takes (`crew:development:waiting review`, `crew:fix:failed`) stay in Handled. So the drop looks intermittent, not like a rule.

## What Didn't Work

- **Reading it as a bug.** It was a rule: R3 of `docs/plans/2026-10-02-1437-feat-live-view-handled-history-plan.md` and the guide said an issue leaves Handled while a stage holds it again, and a core test asserted it. The first fix run on #109 found that and stopped without a pull request. The boss then moved the issue to development to have the rule reversed. `docs/plans/2026-10-03-2217-feat-live-view-dashboard-plan.md` also states the old rule. Plans are not updated after they ship, so both still read that way.
- **Only dropping the filter in `View`.** The board then drew the issue twice: the held card in the new stage's column, plus a waiting card in the old one, because the old entry's `Gone` stayed false. (#134 removed waiting cards with the stage board, so this trap is gone, but the same double-count returns if a view ever builds cards from Handled again.)
- **Keeping "the latest ended stage replaces the entry".** A hidden promote stage ends seconds after taking the issue. Its entry then replaced triage's, and Handled showed `READY · promote triage 4s · now in development` for hours. Triage's outcome, time and cost still vanished one poll after they appeared.
- **Keeping the earlier entry whenever a hidden stage ends well.** That kept an earlier entry that needed attention, too. A failure the boss had sent back through a hidden stage then counted as needing attention again once the hidden stage succeeded, until the next stage took the issue. Code review caught it.

## Solution

- `View` keeps every handled entry and sets `HandledView.HeldBy` to the name of the rule that holds the issue again (`View` in `internal/core/view.go`).
- `release` replaces the issue's entry. The one exception: when the ended rule has no actions and both its verdict and the earlier entry ended well, it keeps the earlier entry and marks it `Gone`, because the rule's move took the issue out of that entry's `To` (`handle` in `internal/core/handled.go`, which `release` calls). Later listings decide `Gone` anew, as for any entry. Without an earlier good entry, the rule without actions leaves its own entry.
- `needsAttention` is `NeedsAttention() && HeldBy == ""` (`internal/ui/tui/band.go:108`). The tab title, the error progress and the attention-first order use it. A held-again failure shows the last part of its label in the error style instead of `NEEDS ATTENTION`, and its details end in `now in <rule>`.
- Desktop notifications come from new Handled entries, muted per rule by its `notify` key, looked up among the rules by name (`internal/ui/tui/outside.go:63`). A rule without actions has `notify` off by default, so the earlier entry it keeps sends nothing, and an entry it adds sends nothing either unless the config turns `notify` on.

Between #109 and #134 the exception keyed on a hidden stage (`on_board: false`, `crew.Stage.OffBoard`), the board's waiting cards skipped held entries, and `on_board: false` also muted notifications. #134 replaced the hidden promote stages with rules without actions, so the exception now keys on `len(rule.Actions) == 0`. `on_board` became the `board` and `notify` keys, and the board lost its waiting cards.

The core tests are in `internal/core/handled_test.go` (`TestAnIssueTakenAgainKeepsItsEntryMarkedWithTheRuleHoldingIt`, `TestARuleWithActionsThatSucceedsReplacesAnEarlierEntryThatEndedWell`) and `internal/core/actionless_test.go` (`TestAE2ARuleWithoutActionsMovesTheLabelWithoutASessionAndKeepsTriagesEntry`, `TestARuleWithoutActionsAndNoEarlierEntryLeavesItsOwn`, `TestARuleWithoutActionsReplacesAnEarlierFailedEntry`). The TUI tests are `TestAnEntryHeldAgainSaysWhichRuleHoldsIt`, `TestAFailedEntryHeldAgainShowsItsLabelAndSortsByWhenItEnded` and `TestAFailureHeldAgainDoesNotCountAsNeedingAttention`.

## Why This Works

Handled is a history of this run, and the board and Actions already show where a held issue is now. Hiding the history entry while the issue was held made it vanish for exactly as long as crew kept working on it. The core knows who holds each issue and which rules have no actions (`m.rules` holds `crew.Rule`, its actions included). So it marks the entry rather than leaving the TUI to rebuild that join, and it decides which entry survives a rule that only moves a label. The TUI keeps one rule per screen element: the attention count skips held failures, and notifications follow each rule's `notify`.

## Prevention

- Before turning a "leaves the view while X" rule around, list every consumer of the same list. Here the board, the attention count and the notifications all read `View.Handled`, and each needed its own decision.
- A rule without actions is still a rule to the core: it takes, moves, judges and releases. Any rule keyed on "the latest rule" has to say what such a rule's end does, or it wins on every workflow that chains labels through rules without actions.
- A keep-the-earlier-entry rule must look at the earlier entry, not only at the new verdict. Otherwise it revives a failure the code owner already handled.
- When a config key that drives one of these decisions changes (as `on_board` did in #134), grep the core and the TUI for every reader of the old field and give each its own replacement. `on_board` drove three things at once: the board's columns, the keep-the-earlier-entry exception and the notification muting.
- Treat R3 of the handled-history plan and the "An issue held again drops out of Handled" line of the dashboard plan as overturned, and the hidden-stage mechanism of `docs/plans/2026-10-04-0956-feat-configurable-board-plan.md` as replaced by `docs/plans/2026-10-04-2304-feat-config-keys-plan.md` (KTD6). The guide's "Run it" section and `docs/develop/architecture.mdx` state the current rule.

## Related Issues

- #109, this issue.
- #23 (Handled history) and #33 (the dashboard), which set the overturned rule.
- #110 (the configurable board), which introduced `on_board` and the waiting cards.
- #134 (the new config keys), which replaced hidden stages with rules without actions and `on_board` with `notify`.
- `docs/plans/2026-10-04-0959-fix-handled-keeps-held-issues-plan.md`, the plan of this fix.
