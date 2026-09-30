---
name: crew
last_updated: 2026-09-30
---

# crew Strategy

## Purpose

Once I've conceived an idea, taking it to production alone with Claude Code is slower and lower-quality than it should be.

## Positioning

I'm the boss, not the reviewer: crew is a team of role-based agents, each in its own Claude Code session with its own responsibility, that pull ready work from a shared backlog, talk to each other, and check each other's work — no change merges without approval from a role other than its author. It's assembled from existing plugins and agents first, building only the roles nothing fills, so my time goes to strategy and direction while the team delivers speed with quality.

## Users

**Primary:** Me, the boss of my own crew - I work with a strategist / product-manager agent to set direction and approve backlog prioritization; after that the workflow is live — the free agent whose role fits a ready backlog item picks it up and gets it done — and I step back in only to approve releases.

**Secondary:** Other solo builders who want to run their own crew - crew stays configurable and documented enough for them to adopt, but when their needs conflict with mine, mine win.

## Boundaries

- No autonomous releases: every release goes through me, whatever its size.

_Resist a change when:_ it would let work reach production — or merge — without a check by someone other than its author.

## Key metrics

- **Lead time** - backlog approval → released to production; GitHub Issues/Projects timestamps.
- **Time per stage** - how long items sit in each status, exposing the bottleneck; GitHub Projects status history.
- **Release rejection rate** - share of releases I send back; my release approvals.
- **Escaped defects** - bugs found after release, including those reported by customer agents; GitHub Issues labeled `bug`.
- **Boss interventions** - times I had to step in outside my two checkpoints; tracked by hand to start.

## Tracks

### Coordination

How sessions talk to each other and pull ready work live.

_Why it serves the approach:_ a parallel team only works if nobody — me included — has to hand out tasks.

### Backlog & tracking

GitHub Issues/Projects across repos: epics broken into stories, and statuses that drive the pull.

_Why it serves the approach:_ the board is the crew's shared picture of the work, and where the metrics come from.

### Roles

Which roles exist, what each one owns, and which existing plugin or agent (or a custom one, when nothing fits) fills it.

_Why it serves the approach:_ specialization is where the quality comes from.

### Customer feedback loop

Agents in consuming repos (e.g. my private Home Assistant repo) report back to the product team.

_Why it serves the approach:_ real usage catches problems that I won't be around to catch.
