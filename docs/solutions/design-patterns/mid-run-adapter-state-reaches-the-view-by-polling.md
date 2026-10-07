---
title: What an adapter learns while crew runs reaches the live view by engine polling, through the core
date: 2026-10-04
category: design-patterns
module: internal/engine, internal/core, internal/mates, internal/adapter/github
problem_type: design_pattern
component: core_reducer
related_components:
  - tracker_adapter
  - frontend
severity: medium
applies_when:
  - "Showing in the live view or in --plain something an adapter or internal/mates learns while crew runs, such as a fallback, a failed renewal or a lost permission"
  - "Choosing between pushing state from a goroutine into the engine and having the engine read it"
  - "Adding a per-identity or per-mate total to the live view"
  - "Reading docs/plans/2026-10-03-1824-feat-crew-acts-as-mates-plan.md, whose warnings at startup only no longer hold"
tags: [mates, engine, polling, core-events, live-view, plain, warnings, renewal]
---

# What an adapter learns while crew runs reaches the live view by engine polling, through the core

## Context

Before the mates section of issue #114, a mate's trouble was reported only at startup. The crew-acts-as-mates plan (`docs/plans/2026-10-03-1824-feat-crew-acts-as-mates-plan.md`) left "a warning while crew runs" as considered and not built, and its KTD11 kept warnings out of the engine: `app` handed them straight to the renderers. Two failures then went unnoticed while crew ran:

- the github tracker's writes as the default mate went back to the boss for the rest of the run, silently;
- a mate's token renewal failed, and the renewal loop dropped the error (`_ = a.renew(ctx, s)` in `internal/mates/act.go:435`), so sessions failed later with gh auth errors that looked like any other failure.

That plan is not updated after it shipped, so it still teaches the startup-only design. This document records why the reversal is shaped the way it is (plan: `docs/plans/2026-10-04-1217-feat-mates-section-plan.md`, KTD1 to KTD4).

## Guidance

**Let the producer record state; let the engine read it.** Each source keeps a small piece of state under its own lock, set on the transition, and offers a read-only accessor:

- the github tracker records the warning once, when its writes first go back to the boss (`gh.backToBoss`, `internal/adapter/github/gh.go:96`), worded by what GitHub refused. It quotes nothing gh printed. The tracker exposes it through the optional `port.WriterReporter`;
- `mates.Acting` records each mate's last failed renewal inside `renew` (`internal/mates/act.go:449`), so the loop and the on-demand `Acting.Renew` both report. A success clears it. `cmd/crew` hands `Acting.Failing` to `app` as a plain func, because only `cmd/crew` may import `internal/mates`.

The engine reads both right after the core is built and on every said tick, and steps the core only when the reading changed (`Engine.checkMates`, `internal/engine/engine.go:440`, called at `:256` and `:264`).

**Send the change through the core as events.** The core keeps each mate's problems and emits `MateStopped` or `MateActsAgain` only on a change of state. `--plain` reads the ordered queue, and only `e.step` publishes to it. `refreshSaid`'s `publishLatest` (`internal/engine/engine.go:432`) reaches the TUI alone. The board's read failure stays out of Events on purpose, so `--plain` never prints it. A mate that stops acting must be an event, because `--plain` must print it.

**Credit per-identity totals where the run total is credited.** `actionEnded` adds an action's spend to its identity beside `m.spent` (`internal/core/runs.go`), so the Mates entries always sum to the header. Summing from `View.Handled` would undercount, because a later stage run replaces an issue's entry (see `docs/solutions/logic-errors/handled-drops-issue-a-stage-holds-again.md`).

## Why This Matters

A push design looks simpler and is not:

- **The change can happen before the core exists.** `Tracker.Prepare` creates missing labels through `gh.write` (`internal/adapter/github/tracker.go:474`), so a refused mate falls back to the boss during Prepare, before `core.New`. The renewal loop starts inside `mates.Act` (`internal/mates/act.go:204`), before the engine is even built. A pushed report needs a buffer that outlives both, and a drain before the first step.
- **A push can block after the engine stops.** The engine's inbox has no reader once `Run`'s loop ends. A renewal callback or a write falling back during the final stop would block on a send.
- **Polling coalesces.** A failure that repeats every minute, or a renewal that succeeds every fifty, yields one reading per tick, and the core turns readings into events only on transitions.

The cost is up to 2 seconds of delay, which the live view does not notice.

## When to Apply

- Anything an adapter or `internal/mates` learns in its own goroutine that the boss must see: read it from the engine loop, never call into the engine from the producer.
- Anything `--plain` must print: it has to be a core event, published by `e.step`.
- Any total by identity, stage or queue: credit it where the action ends, never from `View.Handled`.

## Examples

A state that only goes one way is recorded once, under the lock, on the transition, so concurrent writes keep the first reason (`internal/adapter/github/gh.go:96`):

```go
func (g *gh) backToBoss(kind refusalKind, mate string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lost == "" {
		g.lost = lostWarning(kind, mate)
	}
}
```

The engine steps the core only when the reading changed (`internal/engine/engine.go:448`):

```go
if in.WritesLost == e.lastMates.WritesLost && maps.Equal(in.NotRenewed, e.lastMates.NotRenewed) {
	return
}
e.lastMates = in
e.step(ctx, in)
```

A gap the review left open: a renewal failure recorded by the on-demand `Acting.Renew`, after a refused write, is retried by the loop only once the token nears expiry (`internal/mates/act.go:434`). For up to about fifty minutes the warning's "crew tries again every minute" does not hold. Having the loop also renew a mate whose last renewal failed would close it.
