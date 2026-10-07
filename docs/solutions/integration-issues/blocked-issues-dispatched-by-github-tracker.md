---
title: crew took issues that an open GitHub issue blocks
date: 2026-10-02
category: integration-issues
module: internal/adapter/github
problem_type: integration_issue
component: tracker_adapter
related_components: [core_reducer]
symptoms:
  - "An issue recorded as blocked by an open issue was moved to its stage's moves_to label and its actions started"
  - "Issues in crew:in progress whose blockers were still open"
root_cause: missing_validation
resolution_type: code_fix
severity: medium
tags: [tracker, github, graphql, issue-dependencies, blocked-by, dispatch, picking, status-comment]
---

# crew took issues that an open GitHub issue blocks

## Problem

A stage took every open issue carrying its label, whether or not another open issue blocked it through GitHub's issue dependencies ("blocked by"). Work that had to wait for another issue started anyway (#50; the fix is in the pull request that closes it).

## Symptoms

- When #50 was filed, #14 and #15 sat in `crew:in progress` while their blockers (#43, #14) were open, and #40 was in `crew:ready for development` with #43 open, so the next poll would take it.
- The guide listed "skipping issues blocked by others" as not built yet.

## What Didn't Work

Nothing was tried and dropped. Two facts had to be checked before the fix, and both are easy to get wrong:

- **Which count to read.** GitHub's `IssueDependenciesSummary` has `blockedBy` and `totalBlockedBy`. The schema describes `blockedBy` only as "Count of issues this issue is blocked by", and `totalBlockedBy` as "(open and closed)". Live data settles it: #31, blocked only by the closed #44, reports `blockedBy: 0` and `totalBlockedBy: 1`. Reading `totalBlockedBy`, or counting the `blockedBy(first: N)` connection's nodes without checking their state, would keep an issue blocked forever after its blocker closed.
- **Where the capability goes.** The architecture plan's deferred table says "Blocked-by skipping | Optional tracker capability, consulted when picking issues" (`docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md:167`). That reads like a separate optional port interface, as `port.StatusReporter` is. The fix does not do that, and the plan still reads that way.

## Solution

The fix has three parts:

- The GitHub `List` query asks for one more field per issue, `issueDependenciesSummary { blockedBy }` (`internal/adapter/github/tracker.go:49`), and sets `Blocked: n.Dependencies.BlockedBy > 0` (`internal/adapter/github/tracker.go:136`).
- `crew.Issue` carries `Blocked` (`IssueData.Blocked`, read through `Issue.Blocked`, in `internal/crew/issue.go`). A tracker that knows no dependencies leaves it false.
- `core.listed` leaves blocked issues out of each stage's candidates (`waiting` in `internal/core/scheduler.go`). A blocked issue is neither taken nor reported as queued, and a later poll takes it once nothing blocks it.

The tests are `TestBlockedIssueIsNotTakenUntilNothingBlocksIt` (core) and `TestListMarksAnIssueBlockedOnlyWhileAnOpenIssueBlocksIt` (github adapter).

## Why This Works

The blocker count arrives with the listing crew already makes every poll. The decision stays in the pure core, where it can be tested with a table. A separate optional interface would need its own core command, engine goroutine and result input, plus one more GitHub call per poll, to carry one boolean per issue that the existing call can return. A field that defaults to false keeps a tracker without dependencies working unchanged, which is all the "optional" in the plan needs.

## Prevention

- When a GitHub summary field has an open-only count and a total, check which one you read against an issue whose blocker is closed. The schema text alone is ambiguous.
- An issue that was queued at one poll and is blocked at the next keeps its "queued" status comment, because a queued status is sent only when it changes (`docs/develop/architecture.mdx:127`). The guide says so. A change that wants a "blocked" status needs a new status kind, not a tweak to `listed`.
- A blocked issue is skipped silently: no event, unlike the two-label skip (`IssueSkipped`). If the boss asks why a labeled issue sits idle, look at its relationships first.
- `issuesQuery` now needs `Issue.issueDependenciesSummary`. On a GitHub host whose schema lacks it, such as an older GitHub Enterprise Server, every `List` fails and crew takes nothing. The failure is loud (`ListingFailed` every poll), not silent.
- Blocked issues stay in the listing's window of the 100 oldest issues, so many old blocked issues with stage labels can hide newer unblocked ones.
- Do not copy the plan's "optional tracker capability" row for blocked-by as a new port interface. The domain field is the shipped design.

## Related Issues

- #50, the report.
- #41 (dispatch by GitHub priority) changes the same `List` query and `core.listed` ordering.
- `docs/solutions/security-issues/session-text-in-public-tracker-comments.md`: same adapter, unrelated problem.
