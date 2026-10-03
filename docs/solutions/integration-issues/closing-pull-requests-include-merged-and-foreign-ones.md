---
title: GitHub lists merged and foreign pull requests as closing an issue
date: 2026-10-02
category: integration-issues
module: internal/adapter/github
problem_type: integration_issue
component: tracker_adapter
related_components: [core_reducer]
symptoms:
  - "closedByPullRequestsReferences with includeClosedPrs false still lists merged pull requests (#44 lists merged #46 and #53)"
  - "A pull request in another repository that says Closes owner/repo#N is listed with only its number, which gh pr edit and the comments API resolve in crew's own repository"
  - "Older plans say crew writes comments with gh issue comment, whose errors carry no HTTP status to classify"
root_cause: wrong_api
resolution_type: code_fix
severity: medium
tags: [tracker, github, graphql, pull-request, closing-reference, comment, rest, classify]
---

# GitHub lists merged and foreign pull requests as closing an issue

## Problem

For #31, crew had to write to the pull requests that belong to an issue: mirror the issue's crew label onto them and post a stop comment when a stage ends. GitHub's list of pull requests that close an issue contains more than the open ones in the issue's repository. Trusting it as is would label and comment merged pull requests, or a pull request or issue in crew's repository that only shares a number with one elsewhere. These problems were caught in planning and review, before the code shipped.

## Symptoms

- `closedByPullRequestsReferences(first: 100)`, with the default `includeClosedPrs: false`, returned the merged #46 and #53 for #44 (checked live on 2026-10-02). The argument's name suggests merged ones would be left out.
- A pull request in another repository whose body says `Closes owner/repo#N` is a closing reference too. The plan's first version kept only `state == OPEN`, and its document review noticed that the node's number is then used with `gh pr edit <number>` and `POST repos/{owner}/{repo}/issues/<number>/comments`. Both resolve the number in crew's repository.
- `docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md:299` still says crew's writes use `gh issue edit` / `gh issue comment`. `ReportFailure` did use `gh issue comment`, and all its errors were transient.

## What Didn't Work

- **Trusting `includeClosedPrs`.** Its default leaves closed pull requests out, but a merged one still comes back. Only the node's `state` tells them apart.
- **Filtering on state alone.** It keeps an open pull request from another repository, and its bare number then names a different pull request or issue here.
- **Commenting with `gh issue comment`.** It is the command the architecture plan named, and the failure report used it. Its error carries no HTTP status, so a deleted issue or a locked conversation looked transient and was retried. A second, pull-request-only comment function was out too: the user settled on one commenting implementation for issues and pull requests (#31's Key Decisions).

## Solution

- The query (`pullRequestsQuery`, `internal/adapter/github/pullrequest.go:20`) asks for the issue's `repository { nameWithOwner }` and each node's `state` and `repository { nameWithOwner }`. `pullRequests` keeps a node only when `n.State == "OPEN" && n.Repository.NameWithOwner == issue.Repository.NameWithOwner` (`internal/adapter/github/pullrequest.go:142`). An issue GitHub cannot resolve (`Could not resolve to an Issue` on stderr) is `port.ErrMovedMeanwhile` (`internal/adapter/github/pullrequest.go:105`).
- Every new comment goes through one helper, `postComment` (`internal/adapter/github/comment.go:15`): `gh api --method POST repos/{owner}/{repo}/issues/<number>/comments -f body=... --jq .id`, with errors classified by `classify(err, out, true)` (`internal/adapter/github/comment.go:19`). The issues comments endpoint serves pull requests as well, so the failure report (`internal/adapter/github/tracker.go:251`), the status comment's creation (`internal/adapter/github/status.go:123`) and the stop comment (`internal/adapter/github/pullrequest.go:183`) all post through it.
- Tests: `TestOnlyTheOpenPullRequestsOfTheIssuesRepositoryAreWritten` lists a merged #48 and a closed #47 in the issue's repository and an open #51 in `other/r` next to the open #50, and only #50 is written (`internal/adapter/github/pullrequest_test.go`). `TestReportFailureErrorsAreClassifiedFromTheHTTPStatus` covers 404, a locked 403, a rate-limit 403 and no status (`internal/adapter/github/tracker_test.go`).

## Why This Works

A closing reference is a link GitHub keeps whatever the pull request's state or repository, while the label edit and the comment work on a number in one repository. Checking the state and the repository on every node is the only way to know the number names the right pull request. The REST comments endpoint prints `HTTP <code>` on failure, which `classify` turns into crew's error classes. So a write that cannot work is dropped and reported, not retried until crew stops.

## Prevention

- Before using a GraphQL connection argument as a filter, check it against live data that has the case it should exclude, as the blocked-by count needed (`docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md`).
- A node's bare `number` belongs to its own repository. Before passing it to a command scoped to crew's repository, check `repository.nameWithOwner`.
- Post new comments, on issues or pull requests, through `postComment` only. A new `gh issue comment` or `gh pr comment` call loses the HTTP status and makes every failure transient. Text crew did not write still follows `docs/solutions/runtime-errors/nul-in-gh-argument-freezes-status-comment.md` and `docs/solutions/security-issues/session-text-in-public-tracker-comments.md`.
- Do not follow the architecture plan's "`gh issue comment` for writes". The REST helper is the shipped design.

## Related Issues

- #31, the work this comes from; its plan is `docs/plans/2026-10-02-2014-feat-pull-request-state-plan.md` (KTD4, KTD6).
- #35 (poll pull requests the way crew polls issues) will read the same pull requests and must keep the same filters.
- `docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md`: another GitHub field whose name says less than its data.
