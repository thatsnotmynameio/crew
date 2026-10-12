---
title: "A session crew stopped records no tokens, whatever the #431 and #432 plans say"
date: 2026-10-12
category: design-patterns
module: internal/adapter/claude, internal/adapter/codex, internal/port, internal/crew, internal/core
problem_type: design_pattern
component: statistics_adapter
related_components: [harness_adapter, core_reducer]
severity: medium
applies_when:
  - "Reading docs/plans/2026-10-12-1251-issue-431-plan.md KTD7, docs/plans/2026-10-12-1327-issue-432-plan.md KTD7 or its test scenarios, or docs/plans/2026-10-12-1514-issue-434-plan.md KTD7"
  - "Summing a session's tokens from statistics.db, or reading an action span whose outcome is stopped"
  - "Changing how a harness adapter reads its usage when crew stops a session, or port.UsageReporter's contract"
  - "Writing README or CONCEPTS text about what a stopped session reports"
tags: [statistics, spans, action-span, session-tokens, usage, stopped, usage-reporter, stale-plan]
---

# A session crew stopped records no tokens, whatever the #431 and #432 plans say

## Context

#431 to #434 record each action of a rule run as a span, and a session's span holds the tokens its harness reported, by model, with no record when it reported none (R14: not reported, never zero). KTD7 of the shipped plans for #431 (`docs/plans/2026-10-12-1251-issue-431-plan.md:114`) and #432 (`docs/plans/2026-10-12-1327-issue-432-plan.md:116`) says: "A stopped session is not always one with no tokens: a Claude Code session stopped after an earlier result reports what it had." #432's test scenarios add "A stopped session whose harness reported partial usage records that usage" (line 183), and #434's plan repeats KTD7 (`docs/plans/2026-10-12-1514-issue-434-plan.md:108`). Plans are not updated after they ship, so these sentences stay.

The claim is false. #434's docs unit copied it into the README's `## Statistics`. Claude's own review pass checked the docs against `internal/core/statistics.go` and the statistics model and found nothing, because the core passes on whatever usage the harness gives. Only the cross-model pass (Codex) traced the sentence down to the harness adapters and found it wrong (session history). The review fix rewrote the README sentence; the plans keep it.

## Guidance

1. **A session crew stopped reports no usage, in both harnesses.** Claude Code's `reap` (`internal/adapter/claude/harness.go:167-180`) reads usage only when crew did not stop the session and no signal ended it. Once `Stop` has set `s.stopped` (line 156), it records the stopped end and leaves usage empty, whatever results claude printed earlier. Codex's `recorder.usage(stopped)` (`internal/adapter/codex/events.go:162-164`) returns an empty `crew.Usage` when stopped. `port.UsageReporter` (`internal/port/port.go:395-399`) states the contract: a session that was stopped, killed or crashed reports nothing.

2. **An action span can still be `stopped` and hold tokens, by a race, not by an earlier result.** Once crew's stop reaches a run, `judgeSession` (`internal/crew/verdict.go:175-178`) gives `CauseStopped` whatever the harness said, so the span's outcome is `stopped`. At the same moment the run asks the session to stop (`ActionSessionStopAsked`, `internal/crew/fact.go:135`, and `internal/crew/fact_action.go:72` for a session that starts after the stop). Each harness's `Stop` returns at once when the session is already reaped (`internal/adapter/claude/harness.go:151-154`, `internal/adapter/codex/harness.go:150-154`), so a session that ended on its own and was reaped before the stop landed was never stopped by its harness, and it kept the usage it reported. That is the only kind of `stopped` session with token records. A stop that lands after the process exited but before `reap` closed `done` still sets `stopped`, and that session's usage is dropped.

3. **Read a `stopped` span by its tokens.** With no `session_tokens` rows, crew stopped the session and its tokens are not reported: count it as missing from the sum, never as zero. With rows, the session finished on its own as the stop arrived, and the rows are its whole usage. A sum over `stopped` spans therefore undercounts by every session crew actually cut off.

4. **Check a plan's claim about a harness against the adapter before it reaches user-facing text.** The core only copies `ActionEnded.Usage` into `SessionWork` (`internal/core/statistics.go:300`). Whether there is any usage is decided in `internal/adapter/<harness>/`, so a docs check that stops at the core cannot catch a wrong claim about it.

## Why This Matters

- Token sums are what the statistics epic (#315) is for. A reader who trusts KTD7 expects a stopped Claude Code session to carry its usage up to the stop, and reads its missing rows as a bug, or "fixes" `reap` to read usage after a stop. That would break `port.UsageReporter`'s contract and make Claude Code and Codex disagree.
- A test written from #432's scenario, "a stopped session records partial usage", cannot pass against the real adapters. Only a fake harness can make it pass, which would pin behaviour no harness has.
- AE6 in #432's plan, a stopped session with no token records, is the normal case, not one of two.

## When to Apply

- Before writing a report or query over `statistics.db` that sums tokens per issue, rule, agent or queue.
- Before changing `reap`, `Stop` or `Usage` in `internal/adapter/claude/harness.go`, `recorder.usage` in `internal/adapter/codex/events.go`, or adding a harness adapter that implements `port.UsageReporter`.
- Before a plan or doc says what a stopped session records.

## Examples

Crew's stop arrives while `develop` runs a Claude Code session on #42 that has already printed one result. The run asks the session to stop, `reap` sees `s.stopped` set, and the action span is:

```go
crew.ActionSpan{ /* ... */ Outcome: crew.WorkStopped,
	Work: crew.SessionWork{Started: crew.Some(started), Tokens: nil}}
```

It has no token records: not reported.

The same stop arrives a moment after the session wrote its final result and exited. The session was never stopped, so `reap` read its usage. The run's `judgeSession` still gives `CauseStopped`, so the span is `stopped` with token records for each model the session used.

## Related

- `docs/solutions/design-patterns/rule-run-spans-differ-from-the-342-plan.md`: the same gap between a shipped statistics plan and the code, for rule run spans. Its "When to Apply" asks each new span kind when a halt chose its end; this doc answers it for a session's tokens.
- `docs/solutions/workflow-issues/release-plans-predate-the-draft-first-release-workflow.md`: why shipped plans keep stating what the code no longer does.
- #431, #432, #434 (the action spans and their docs), #343 (their parent), #315 (the statistics epic).
