---
title: Rule sequences read four requirements of their full plan more narrowly than the plan's text
date: 2026-10-07
category: design-patterns
module: internal/crew, internal/engine, .crew/config.yaml
problem_type: design_pattern
component: core_reducer
related_components:
  - config_loader
  - documentation
severity: medium
applies_when:
  - "Planning or building #255 (waiting and answers) or #256 (functions) from docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md"
  - "Changing what CREW_ACTION holds for a shell action, or what a resumed run's shell actions inherit"
  - "Changing when a route counts as finished, where a resumed run restarts, or what time-up does to a run"
  - "Reading AE19, R22 or R52 of the full rule-sequences plan, or its U12 step 1"
tags: [rule-sequences, routes, resume, crew-action, time-up, plan-drift, jev-judge]
---

# Rule sequences read four requirements of their full plan more narrowly than the plan's text

## Context

The format switch of #220 shipped as #254 from a part plan, `docs/plans/2026-10-07-1020-feat-rule-sequences-format-switch-plan.md`. That part plan narrows the full plan, `docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md`. Plans are not updated after they ship, so the full plan still states four things the code no longer does. The later parts of #220, #255 (waiting and answers) and #256 (functions), are refined from the full plan, so a planner who reads only the full plan would bring the old readings back. Each departure is recorded as a KTD-S decision and an assumption in the part plan; this note collects them where the next planner looks.

## Guidance

Read the full plan through these four corrections.

1. **`CREW_ACTION` names the run's latest session, not the shell action.** The full plan's U12 step 1 says a shell action's `CREW_ACTION` names the shell action (`docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md:801`). The code passes the latest session's name instead (`internal/adapter/shell/shell.go:73`), and it is empty before any session in a fresh run (KTD-S11, KTD-S12). The reason is this repository's Jev judge: `session-finished` in `.crew/config.yaml` sends `$CREW_ACTION` to Jev as the action under judgment, and its thresholds were measured with the session's name, which `internal/config/judge_test.go` pins (`CREW_ACTION=lfg`). Naming the shell action would hand Jev `session-finished` and shift every judgment without any test noticing the thresholds no longer apply.
2. **A route is finished when its final move or close landed, or was dropped because the item moved meanwhile.** AE19 says a run that chose `passed` "and whose move was given up" runs the `passed` route alone next time. The code reads "given up" as the outbox giving up after its final try, or a crash before the step settled; a move dropped because the item moved meanwhile counts as finished (`passedAfter`, `internal/crew/history.go:127`). The broad reading breaks this repository's refinement rule: its prompt strips every `crew:` label from a split parent on purpose (`.crew/config.yaml`, step 7 of the refine prompt), so crew's own move is dropped as moved meanwhile. Under AE19's literal text, a split parent that later returned to `crew:refinement:ready` would go straight to `done` with no session.
3. **A resume steps back to the session before a judge only when the judge ran and returned its own verdict.** R22 says a run that ended at a shell action after a session restarts at that session. The code applies that only when the shell action ran its script to the end and was not stopped (`judgedItself`, called from `restartPoint` in `internal/crew/history.go:244`). A judge that never started, because crew stopped or ran out of time between the session and the judge, or that a stop cut short, resumes at itself (KTD-S7). Otherwise every stop that lands between a finished session and its judge would rerun the whole session, although the judge can still read that session's kept last message.
4. **Time-up ends a run through `failed` only when it cuts the sequence short.** R52 says that when the run time is up, "the rule ends through `failed`". The code applies time-up between actions: the running action finishes, and when its verdict chose a route of its own, or it was the last action and went next, the run keeps that route: `finish` in `internal/crew/decide.go` forces `failed` only for a stop. Only an action that time-up kept from starting ends the run through `failed`, with the time-up cause (`halted`, same file).

## Why This Matters

Each correction keeps a promise the full plan's own Key Decisions make: a judge that judges what the session left, a split that ends the way its prompt says, and a resume that does not redo finished work. The literal readings break those promises in ways no unit test of the new code catches, because each of those tests pins the corrected behaviour. A planner of #255 or #256 who copies the full plan's unit text would write tests for the literal reading and "fix" the code back.

The part plan also keeps three readings the boss has not confirmed, listed in its Assumptions: corrections 1 to 3 above. If the boss overturns one, update this note and the part plan's assumption together.

## When to Apply

- Before refining or planning #255 or #256: their units cite R22, R23 and the full plan's U12 and U18 to U20.
- Before touching `restartPoint`, `passedAfter`, the shell adapter's environment, or `halted` and `finish` in `internal/crew/decide.go`.
- When a judge's measured thresholds depend on what crew passes it: re-measure before changing the input.

## Examples

A plan for #256 that reads U12 step 1 and makes a function's or shell action's `CREW_ACTION` its own name would change the judge's input from:

```text
action: lfg
```

to:

```text
action: session-finished
```

and `internal/config/judge_test.go` would need its expected state changed too. That edit is the warning sign: the thresholds in `session-finished` were never measured with that input.

A planner reading AE19 for #255's waiting route should keep the narrow reading: a `waiting` route's final move that lands, or that someone else's move drops, finishes the route, and only an outbox give-up or a crash leaves it for the next run.
