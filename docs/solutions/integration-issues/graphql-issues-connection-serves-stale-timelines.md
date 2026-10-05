---
title: GitHub's issues connection served stale timelines to the blocker backtest
date: 2026-10-05
category: integration-issues
module: .agents/skills/cw-rank-blockers
problem_type: integration_issue
component: tooling
symptoms:
  - "backtest.sh reported 27 links recorded and 17 measured, where GitHub's blockedBy listed 25 current links"
  - "Three links recorded on 2026-10-05 at 20:12 (#135 blocked by #166 and #152, #139 blocked by #135) were missing from the report"
  - "#133 blocked by #134, removed at 20:59, was measured as a current link"
  - "The gate result (14 of 17, 82.4%) looked plausible, so nothing in the output pointed at the read"
root_cause: wrong_api
resolution_type: code_fix
severity: high
retire_when: "repository.issues(first: 100) { nodes { number timelineItems(...) { totalCount } } } returns the same totalCount for every issue as repository.issue(number:) does; compare both for a recently edited issue"
tags: [github, graphql, timeline, issue-history, blocked-by, labels, backtest, jev]
---

# GitHub's issues connection served stale timelines to the blocker backtest

## Problem

`backtest.sh` (#166) replays `rank.sh` against every `blocked_by` link GitHub records, each at the moment it was recorded, so it needs each issue's full history: edits, link events, close and reopen events, label events. Its first version read all of it in one paginated query, nesting `timelineItems` inside `repository.issues`. For some issues that connection returned an old timeline, so the backtest measured the wrong set of links.

## Symptoms

- The first live run reported `27 links recorded: 17 measured, 6 excluded, 4 removed since.` GitHub's `blockedBy` from single-issue queries listed 25 current links.
- The links recorded at 20:12 on 2026-10-05 were absent: #135 blocked by #166, #135 blocked by #152, #139 blocked by #135.
- #133 blocked by #134 was measured as current, although a `BlockedByRemovedEvent` removed it at 20:59:48.
- The run ended on `**Gate passed:** 14 of 17 links found (82.4%)`. A plausible number with no error.

## What Didn't Work

Trusting the report's own counts. They were internally consistent: recorded equals measured plus excluded plus removed. Every number came from the same stale read, so the arithmetic could not expose it. What exposed it was the plan's sanity check (U2): compare the link count with `blockedBy` from `issue(number:)` queries.

## Solution

List the issue numbers through the paginated connection, and read each issue's history through aliased `issue(number:)` lookups, 20 per query (`.agents/skills/cw-rank-blockers/backtest.sh:58-97`):

```graphql
# Numbers only, through the connection.
issues(first: 100, after: $endCursor, states: [OPEN, CLOSED]) { pageInfo { hasNextPage endCursor } nodes { number } }

# History, by number, 20 aliases per query.
repository(owner: $owner, name: $name) {
  i133: issue(number: 133) { ...history }
  i135: issue(number: 135) { ...history }
}
```

`history` is a fragment holding the body, `userContentEdits`, and `timelineItems` filtered to the six event types the backtest reads. The test stub serves the same split: numbers from the paginated call, history only by number. A script that went back to the nested read would therefore measure nothing and fail its tests.

After the fix the report read `30 links recorded: 19 measured, 6 excluded, 5 removed since.`, and the 25 current links matched `blockedBy`. Three runs all gave `14 of 19 links found (73.7%)`.

## Why This Works

Measured on 2026-10-05, the same `timelineItems(first: 100, itemTypes: [BLOCKED_BY_ADDED_EVENT, BLOCKED_BY_REMOVED_EVENT, LABELED_EVENT, UNLABELED_EVENT]) { totalCount }` selection gave:

| Issue | Through `repository.issues(first: 100)` | Through `repository.issue(number:)` |
| --- | --- | --- |
| #133 | 0 | 16 |
| #135 | 0 | 18 |
| #139 | 0 | 2 |

In the session, the nested read with the backtest's full field list, ordered by creation date, returned #133's first 4 items and nothing after 01:44 that day. Aliased `issue(number:)` lookups in one query returned the full counts, the same as one issue per query. The cause on GitHub's side is unknown. Read here as an observed fact: nested `timelineItems` on the issues connection can lag by hours, and `issue(number:)` does not.

In the same session (one-time observations), `blockedBy(first: 20)` nested in `repository.issues(first: 100)` listed #135's three current blockers, and `userContentEdits` there matched the single-issue read for #135 and #166. The staleness was seen on `timelineItems` only. crew's GitHub adapter reads labels and the `issueDependenciesSummary { blockedBy }` count through the connection (`internal/adapter/github/tracker.go:65-66, 97`), never timelines.

A second fact the backtest depends on: GitHub renames a label in place, so old `LabeledEvent` and `UnlabeledEvent` items report the label's current name. Refinement-era label events say `crew:refinement:in progress` even from before the triage rule was renamed (#163); #133's history, for example, holds no triage label at all. That is why the backtest finds the refined side with that one label (`backtest.sh:26`). A search for `crew:triage:in progress` in old events finds nothing.

## Prevention

- Read any issue's history (`timelineItems`) through `issue(number:)`, aliased in batches, never nested in `repository.issues` or `search`. This applies to the hand-correction count the plan defers (KTD10) and to any future backtest.
- Check a history read against an independent source before trusting a number built on it. Here the source was `blockedBy` from single-issue queries. A count that only adds up against itself proves nothing.
- Keep the test stub's shape the same as the production read (numbers from the list, history by number), so a regression to the nested read fails the tests.
- Match label events by the label's current name. Renaming a label rewrites its history.

## Related

- [issueFieldValues totalCount fails listing on user repositories](issue-field-total-count-fails-listing-on-user-repositories.md): another GraphQL selection that behaves differently from what its schema suggests.
- [Blocked issues dispatched by the GitHub tracker](blocked-issues-dispatched-by-github-tracker.md): the same `blocked_by` data, read by crew's tracker adapter.
- Plan: `docs/plans/2026-10-05-1935-feat-refine-shortlist-gate-plan.md` (U2's sanity check).
