---
title: Rule sequences read their full plan more narrowly than the plan's text
date: 2026-10-07
last_updated: 2026-10-08
category: design-patterns
module: internal/crew, internal/core, internal/engine, .crew/config.yaml
problem_type: design_pattern
component: core_reducer
related_components:
  - config_loader
  - documentation
severity: medium
applies_when:
  - "Planning or building more of #220 from docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md"
  - "Changing what CREW_ACTION holds for a shell action, or what a resumed run's shell actions inherit"
  - "Changing when a route counts as finished, where a resumed run restarts, or what time-up does to a run"
  - "Reading AE19, R22 or R52 of the full rule-sequences plan, or its U12 step 1"
  - "Reading the full plan's KTD21, or changing when a waiting session's open question closes or where crew reads its answers"
  - "Reading KTD4 of the plan for #310 (a resume after a question action), or changing askedAfter, sessionBefore or passedAfter"
  - "Changing who gets a rule's question's answers, or when a rule's question closes"
tags: [rule-sequences, routes, resume, crew-action, time-up, plan-drift, jev-judge, waiting, answers, open-question, rule-question, answered-rule]
---

# Rule sequences read their full plan more narrowly than the plan's text

## Context

The format switch of #220 shipped as #254 from a part plan, `docs/plans/2026-10-07-1020-feat-rule-sequences-format-switch-plan.md`. That part plan narrows the full plan, `docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md`. Plans are not updated after they ship, so the full plan still states things the code no longer does: four that part 2 changed, and two more that part 3 changed. The later parts of #220, #255 (waiting and answers) and #256 (functions), are refined from the full plan, so a planner who reads only the full plan would bring the old readings back. Each departure is recorded as a KTD-S decision and an assumption in the part plan; this note collects them where the next planner looks.

Part 3, #255, builds waiting and answers from its own part plan, `docs/plans/2026-10-07-1724-feat-session-waits-for-answers-plan.md`. It departs from the full plan's KTD21 twice, as KTD-W6 and KTD-W7 there; corrections 5 and 6 below record them.

## Guidance

Read the full plan through these corrections.

1. **`CREW_ACTION` names the run's latest session, not the shell action.** The full plan's U12 step 1 says a shell action's `CREW_ACTION` names the shell action (`docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md:801`). The code passes the latest session's name instead (`internal/adapter/shell/shell.go:76`), and it is empty before any session in a fresh run (KTD-S11, KTD-S12). The reason is this repository's Jev judge: `session-finished` in `.crew/config.yaml` sends `$CREW_ACTION` to Jev as the action under judgment, and its thresholds were measured with the session's name, which `tools/crew_config_test.sh` pins (`CREW_ACTION=lfg`, `tools/crew_config_test.sh:256`). Naming the shell action would hand Jev `session-finished` and shift every judgment without any test noticing the thresholds no longer apply.
2. **A route is finished when its final move or close landed, or was dropped because the item moved meanwhile.** AE19 says a run that chose `passed` "and whose move was given up" runs the `passed` route alone next time. The code reads "given up" as the outbox giving up after its final try, or a crash before the step settled; a move dropped because the item moved meanwhile counts as finished (`passedAfter`, `internal/crew/history.go:128`). The broad reading breaks this repository's refinement rule: its prompt strips every `crew:` label from a split parent on purpose (`.crew/config.yaml`, step 7 of the refine prompt), so crew's own move is dropped as moved meanwhile. Under AE19's literal text, a split parent that later returned to `crew:refinement:ready` would go straight to `done` with no session.
3. **A resume steps back to the session before a judge only when the judge ran and returned its own verdict.** R22 says a run that ended at a shell action after a session restarts at that session. The code applies that only when the shell action ran its script to the end and was not stopped (`judgedItself`, called from `restartPoint` in `internal/crew/history.go:334`). A judge that never started, because crew stopped or ran out of time between the session and the judge, or that a stop cut short, resumes at itself (KTD-S7). Otherwise every stop that lands between a finished session and its judge would rerun the whole session, although the judge can still read that session's kept last message.
4. **Time-up ends a run through `failed` only when it cuts the sequence short.** R52 says that when the run time is up, "the rule ends through `failed`". The code applies time-up between actions: the running action finishes, and when its verdict chose a route of its own, or it was the last action and went next, the run keeps that route: `finish` in `internal/crew/decide.go` forces `failed` only for a stop. Only an action that time-up kept from starting ends the run through `failed`, with the time-up cause (`halted`, same file).
5. **A run keeps a list of open questions per action, not one.** KTD21 kept one open question per session action, set at each session's start and closed by a later session that ended with any verdict but a stop. That loses answers twice. A resumed session cut short before it asks again, stopped or failed by its harness, becomes the question; crew then finds no comment carrying its marker, and the next session never sees the answers to the question actually asked. And a resumed session that waits again without asking anew closes the original question, so an answer posted later is lost too; code review found this second case after the first fix. The code keeps every session at the action that may have asked, each with its run and login (`internal/crew/question.go`). A session that ended well with a verdict other than `waiting` closes every question at its action; one that ended with `waiting` keeps them all; one that failed, was stopped or never started closes none (`endedOn` in `internal/crew/question.go`, called from `ActionEnded.apply` in `internal/crew/apply.go`). `crew.Answers` takes the latest marked question among them (`internal/crew/answer.go`), and the waiting paragraph's read command prints the earlier questions too.
6. **crew reads the answers as session plumbing, not as a phase of the run.** KTD21 had the run emit an `AnswersAsked` event and receive an `AnswersRead` fact. The code has the core send a `ReadAnswers` command before `StartSession` and take an `AnswersRead` input, keeping the answers on the held run in memory only (`internal/core/answers.go`). No answer text reaches a run event, so none reaches the journal; a crash before the session starts reads the comments again at the next take. The domain only carries what must survive restarts: the open questions, on `ActionSessionStarted` (the session's login and whether it may ask) and `RunTaken`.
7. **A rule's question narrows the resume rules three ways (#311).** Part 1 of #308 (#310, plan `docs/plans/2026-10-07-2037-feat-rule-asks-question-plan.md`) says in its KTD4 that a run after a question action that ended `asked` starts after it. Part 2 (#311, plan `docs/plans/2026-10-07-2352-feat-return-answered-question-plan.md`, KTD4 and KTD9) made crew return an answered item on its own, which turned three gaps into loops or wrong returns:
   - A resume steps past a question action only when the question was posted: `askedAfter` checks `questionLanded`, the route's question step settled as landed (`internal/crew/history.go:172`, `:195`). A question whose post the tracker gave up, or that crew crashed before settling, is asked again at the question action instead of being skipped with no answer to come.
   - Correction 3's step back from a judge to the session before it never crosses a question action: `sessionBefore` resets at a `QuestionSpec` (`internal/crew/history.go:388`). Without it, a judge that failed after an answered question reran the earlier session, which asked the same question again, and the automatic return made that a loop needing only the answerer's move each round.
   - Correction 2's "the passed route alone after a final move given up" does not apply to a rule that returns the item, crew's built-in `answered` rule: `passedAfter` starts it fresh (`internal/crew/history.go:137`, `Rule.returns` at `:146`). Its move's label comes from its check of the comments, recorded only in that run's `RouteChosen`, so a passed route alone would plan a move to no label; checking again is a read and changes nothing.

   A rule's question is a `crew.Question` with an `ID` and no action (`internal/crew/question.go`). Unlike correction 5's session questions, it is tied to no action: the first session a later run starts gets it, whatever action that run starts at (`Questions` and `firstSession`, `internal/crew/question.go:27`, `:50`). That session closes it when it ends well with a verdict other than `waiting` (`endedOn`, `:82`), and it survives a finished `passed` route, which drops every session question (`History.Questions`, `:98`).

## Why This Matters

Each correction keeps a promise the full plan's own Key Decisions make: a judge that judges what the session left, a split that ends the way its prompt says, a resume that does not redo finished work, and answers that reach the next session whatever cut the last one short, without ever landing in the journal. The literal readings break those promises in ways no unit test of the new code catches, because each of those tests pins the corrected behaviour. A planner of #255 or #256 who copies the full plan's unit text would write tests for the literal reading and "fix" the code back.

The part plan also keeps three readings the boss has not confirmed, listed in its Assumptions: corrections 1 to 3 above. If the boss overturns one, update this note and the part plan's assumption together.

## When to Apply

- Before planning further work from the full plan: its units cite R22, R23, U12 and U18 to U20 in their original wording. #256 shipped as #305; #255 is built from its own part plan.
- Before changing when an open question closes (`endedOn`), how `crew.Answers` picks the question, or where the answers are read: the tests in `internal/crew/question_test.go` and `internal/core/answers_test.go` pin the corrected rule, and the full plan's KTD21 text would undo it.
- Before touching `restartPoint`, `passedAfter`, the shell adapter's environment, or `halted` and `finish` in `internal/crew/decide.go`.
- When a judge's measured thresholds depend on what crew passes it: re-measure before changing the input.
- Before reading the #310 plan's KTD4 or "Deferred to Follow-Up Work" as current: correction 7 narrows its resume after a question action, and #311 built the return it deferred. The tests in `internal/crew/rulequestion_test.go` and `internal/crew/answered_test.go` pin the narrowed rules.
- Before tying a rule's question to an action, or closing it at a finished `passed` route as session questions are: the answer would reach no session after a fresh start, a judge's step back or a question asked on `passed`.

## Examples

A plan for #256 that reads U12 step 1 and makes a function's or shell action's `CREW_ACTION` its own name would change the judge's input from:

```text
action: lfg
```

to:

```text
action: session-finished
```

and `tools/crew_config_test.sh` would need its expected state changed too. That edit is the warning sign: the thresholds in `session-finished` were never measured with that input.

A planner reading AE19 for #255's waiting route should keep the narrow reading: a `waiting` route's final move that lands, or that someone else's move drops, finishes the route, and only an outbox give-up or a crash leaves it for the next run.
