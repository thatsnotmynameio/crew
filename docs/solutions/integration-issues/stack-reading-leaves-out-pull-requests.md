---
title: The GitHub stack reading leaves pull requests out of its result
date: 2026-10-12
category: integration-issues
module: internal/adapter/github
problem_type: integration_issue
component: tracker_adapter
related_components: [core_reducer]
symptoms:
  - "repository.issue(number:) with a pull request's number answers null and a NOT_FOUND error: Could not resolve to an Issue with the number of 449."
  - "gh api graphql exits 1 on that reply but prints the whole JSON body, data included, on stdout"
  - "FindStacks treats a NOT_FOUND alias as an issue GitHub cannot resolve and leaves it out of its result, with no error"
root_cause: wrong_api
resolution_type: documentation_update
severity: medium
tags: [tracker, github, graphql, stacking, stack-finder, not-found, partial-reply, pull-request, alias, gh]
---

# The GitHub stack reading leaves pull requests out of its result

## Problem

#443 (part 3 of #425, stacked pull requests) added `port.StackFinder` and its GitHub implementation, `FindStacks` (`internal/adapter/github/stack.go:220`). It reads each candidate through `repository.issue(number:)`, which cannot resolve a pull request's number. `FindStacks` leaves such an item out of its result without an error. Nothing called `FindStacks` when #443 shipped. The part that wires it into the listing (#447) must not hand it pull requests and then use its result as the list, or every pull-request rule's items vanish.

## Symptoms

Checked live on 2026-10-12 against thatsnotmynameio/crew, with #449 (a merged pull request) and #443 (an issue) in one aliased query:

```json
{"data":{"repository":{"id":"R_kgDOU1r9pg","a0":null,"a1":{"number":443}}},
 "errors":[{"type":"NOT_FOUND","path":["repository","a0"],
            "message":"Could not resolve to an Issue with the number of 449."}]}
```

`gh` printed this body on stdout and exited 1; only the message goes to stderr.

## What Didn't Work

Nothing failed in the code: the review of #443 found this as a constraint on the later parts. The port's doc says only "An issue the tracker cannot resolve is left out" (`internal/port/port.go:434`). That reads like a rare case, a deleted or transferred issue. It does not say that every pull request lands in it.

During U5 the implementation session also learned two facts about the read, which the code relies on but does not explain:

- **gh's exit status does not mean "no data".** `gh api graphql` exits 1 whenever the reply holds any GraphQL error, even when only one alias failed. The rest of the reply is on stdout. `readAliases` parses stdout first and fails only when an error is not `NOT_FOUND` (`internal/adapter/github/stack.go:333`, `graphQLError.fatal` at `:405`). Treating exit 1 as a failed read would fail the whole listing for one missing issue.
- **The `blockedBy(first:)` connection lists closed blockers.** #443, blocked only by the closed #441, gave summary `blockedBy: 0`, `totalBlockedBy: 1` and a `blockedBy` node in state `CLOSED`. `FindStacks` checks both counts and the node's state. `docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md` warns about the counts.

## Solution

Nothing changed in code. The constraints for the part that calls `FindStacks`:

- **Pass only issues.** `Tracker.List` returns pull requests too, with `Kind` set to `crew.KindPullRequest` (`internal/adapter/github/tracker.go:266`). Filter on `Kind() == crew.KindIssue` before calling `FindStacks`, or merge its result back into the listing by `ID`. Never replace the listing with the result.
- **Bound the batch.** Each round puts every value in one `gh api graphql` call as an alias (`a0`, `a1`…): every candidate in round 1, every distinct blocker in round 2, every distinct kept head branch in round 3. Nothing caps it. A large listing can hit the OS argument limit or GitHub's query limits. The stale-timelines backtest batches 20 aliases per query (`docs/solutions/integration-issues/graphql-issues-connection-serves-stale-timelines.md`).
- **Know the silent bounds.** `timelineItems(last: 100)`, `subIssues(first: 100)` and `pullRequests(first: 20)` read no `pageInfo` (`internal/adapter/github/stack.go:38`, `:34`, `:51`). The plan accepts these limits under Assumptions. Past them the reading is incomplete, with no error.

## Why This Works

GitHub's `Repository.issue` resolves only issues: a pull request's number answers NOT_FOUND there. `FindStacks` asks only for issues because a stack reading belongs to a blocked issue, so leaving out an unresolved number is right for the issues it was built for. The risk is in the caller: `List` mixes both kinds in one slice, and the empty result for a pull request looks the same as for a deleted issue.

## Prevention

- In the wiring part, add a test whose listing holds a pull request: it stays in the listing, with no stack reading, after `FindStacks` runs.
- Before trusting a GraphQL read through `gh`, decide what exit 1 means. With aliases it often means "partial": parse stdout and look at each error's `type`.
- The plan's stop condition, whether GitHub cross-references a pull request based on another pull request's branch, was checked live during U5. `docs/plans/2026-10-12-1827-issue-443-plan.md` still calls it unchecked, since plans are not updated after they ship. On cli/cli#14449, the pull requests #14451 to #14456, each based on another pull request's branch, appear as cross-references, and `FindStacks`'s round 2 kept them with their heads and bases. Round 3 on `babakks/refresh-token-a-foundation` found `babakks/refresh-token-b-domain`. Do not repeat that check.

## Related Issues

- #443, the read; #425, stacked pull requests; #447, the part that wires `FindStacks` into the listing.
- `docs/solutions/integration-issues/graphql-issues-connection-serves-stale-timelines.md`: why the rounds read through aliased `issue(number:)` and never through a timeline nested in `repository.issues`.
- `docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md`: `blockedBy` counts open blockers only, `totalBlockedBy` closed ones too.
- `docs/solutions/integration-issues/closing-pull-requests-include-merged-and-foreign-ones.md`: connection arguments are not filters, so each pull request's state and repository are checked.
