---
title: Asking for issueFieldValues totalCount fails crew's listing on a user-owned repository
date: 2026-10-02
category: integration-issues
module: internal/adapter/github
problem_type: integration_issue
component: tracker_adapter
related_components: [core_reducer]
symptoms:
  - "gh api graphql fails with \"Something went wrong while executing your query\" and a request id, on a repository a user owns"
  - "The same query succeeds on a repository an organization owns"
root_cause: wrong_api
resolution_type: code_fix
severity: medium
retire_when: "GitHub answers issueFieldValues(first: 25) { totalCount } on a user-owned repository; check with a read-only gh api graphql query on any user repository's issues"
tags: [tracker, github, graphql, issue-fields, priority, dispatch, user-owned-repository]
---

# Asking for issueFieldValues totalCount fails crew's listing on a user-owned repository

## Problem

To dispatch issues by GitHub's native priority (#41), the `github` adapter's listing query reads each issue's issue field values. With `totalCount` on that connection, the whole query fails on a repository a user owns, so `List` would fail at every poll and crew would take nothing there. User-owned repositories have no issue fields, which are an organization feature.

## Symptoms

- `issueFieldValues(first: 20) { totalCount }` on the issues of mguilarducci/tantamore, a user-owned repository, returned only `{"errors":[{"message":"Something went wrong while executing your query on ... Please include <request id> when reporting this issue."}]}`, with no `data`.
- The same selection on thatsnotmynameio/crew returned `{"totalCount":0}` for issues #5 and #7, which have no field values.

## What Didn't Work

- **Probing the connection with `totalCount`.** It looked like the cheapest way to ask "does this issue have any field values". On a user-owned repository it is the one selection that breaks the query: `issueFieldValues(first: 20) { nodes { __typename } }` on the same issues returned `{"nodes":[]}` with no error. The schema does not say this; only a live query against a user-owned repository shows it.
- **Reading the option order from `Organization.issueFields`.** That would need a second query per poll, and only works when the owner is an organization. It is not needed: each value carries its field.

## Solution

`issuesQuery` asks for the values' nodes only, never `totalCount` (`internal/adapter/github/tracker.go:57`):

```graphql
issueFieldValues(first: 25) {
  nodes {
    ... on IssueFieldSingleSelectValue {
      optionId
      field { ... on IssueFieldSingleSelect { name options { id } } }
    }
  }
}
```

- `priority` (`internal/adapter/github/tracker.go:187`) takes the value whose field is named `Priority` (ignoring case) and returns the position of its `optionId` among the field's `options` ids, plus one. Anything else gives 0, meaning no priority.
- The rank travels as `crew.Issue.Priority` (`internal/crew/issue.go`), as `Blocked` does. The core's `listed` (`internal/core/scheduler.go`) sorts every stage's candidates together by priority, then later stage, then age.
- `TestListReadsEachIssuesPriorityFromItsIssueField` (`internal/adapter/github/tracker_test.go:318`) fails if the query ever contains `totalCount`.

## Why This Works

- **No `totalCount`, no failure.** On a user-owned repository, GitHub answers `issueFieldValues { nodes }` with an empty list and fails only the count. An empty list decodes to no priority, which is the behavior R6 of #41 asks for there.
- **One query is enough.** A single select value carries its `field`, and the field carries its `options` in order. The live reply for thatsnotmynameio/crew#14 (Priority set to Medium) showed `optionId` `IFSSO_kgDOBGSECQ`, which equals the `id` of the third of four options. So the adapter matches by id, which stays right when an option is renamed.
- **One page holds every value.** GitHub allows at most 25 issue fields per organization ([Managing issue fields in your organization](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/managing-issue-fields-in-your-organization)), and an issue holds one value per field, so `first: 25` returns every value.
- **Other types are ignored.** A value of another field type (date, text, number) does not match the fragment, decodes as an empty object, and gives no priority.

## Prevention

- Before adding any field to a GitHub GraphQL query crew runs every poll, run it once against a user-owned repository as well as an organization's. The two answer issue-field selections differently.
- Keep `totalCount` off `issueFieldValues`. The adapter test pins this; do not loosen it to save a test line.
- The rank is the option's position in `options`. GitHub also exposes `IssueFieldSingleSelectOption.priority`, and the two agreed on 2026-10-02 (Urgent 1 to Low 4). No one has checked that `options` stays in that order after an organization reorders them. If ranks look stale after a reorder, compare the two.
- Like `issueDependenciesSummary`, `issueFieldValues` must exist in the host's schema. On a GitHub Enterprise Server without it, every `List` fails loudly (`ListingFailed`).
- This change replaced the dispatch order in KTD8 of the engine architecture plan (`docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md`), which still reads "later stages first, then the oldest issue first". The order is now priority, then later stage, then oldest, as `docs/plans/2026-10-02-2122-feat-dispatch-by-priority-plan.md` and `docs/develop/architecture.mdx` state.

## Related Issues

- #41, dispatch issues by GitHub priority.
- `docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md`: the same listing query and the same domain-field pattern (`Blocked`).
- #35 (poll pull requests) will need to decide how a pull request ranks.
