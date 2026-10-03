---
title: Poll pull requests the way crew polls issues - Plan
type: feat
date: 2026-10-03
topic: poll-pull-requests
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Poll pull requests the way crew polls issues - Plan

## Goal Capsule

- **Objective:** the boss can hand a pull request to crew with a label, just as they hand it an issue, and crew runs the stage that watches that label on it.
- **Means:** the GitHub tracker's poll also returns the open pull requests that carry a stage's label. crew's core, config and prompts treat each one as it treats an issue.
- **Product authority:** the boss, through the brainstorm of #35. Tying a session's cost to the pull request it worked on is #84, not this work.
- **Open blockers:** none.
- **Execution profile:** one change to the GitHub adapter (KTD1, KTD2), then the docs. The core, the engine, the config and the ports do not change.
- **Stop conditions:** stop and report if `gh issue view` or `gh issue edit` turn out not to accept a pull request's number (KTD3), or if a listing that also returns pull requests would need a change outside the GitHub adapter.
- **Who ships:** the implementer opens one pull request that closes #35. Merging is the boss's.

---

## Product Contract

### Summary

A stage takes every open item that carries its label, whether that item is an issue or a pull request. A pull request goes through a stage exactly as an issue does: the same moves, the same status comment and failure report, and the same prompt fields, filled from the pull request. Only the GitHub adapter knows that an item is a pull request.

### Problem Frame

Some work starts from a pull request, not from an issue: answering review comments, or fixing a failing CI run. Today crew polls only issues (`docs/guide/crew.mdx:8`), and its poll asks GitHub for issues alone, so a pull request is never taken whatever its labels. When a crew session ends, the stop comment from #31 tells the boss that nobody watches the pull request any more. From then on its review comments and CI failures need a person, and the boss cannot hand that work back to crew with a label.

### Key Decisions

- **The label alone decides what a stage takes.** A stage does not declare whether it takes issues or pull requests, and the config gains no field for it. Governs R1, R2. (session-settled: user-directed — chosen over each stage declaring the kind it takes: crew stays agnostic of what the tracker's items are, and the stage's prompt knows what to do with what it gets.)
- **crew adds no guard between the mirrored label and stage labels.** If the boss's workflow makes an issue's mirrored label a stage's label, crew takes the pull request that carries it, and the issue's next move replaces whatever label crew gave that pull request. Getting that right is the boss's config. Governs R1. (session-settled: user-directed — chosen over separate label sets for pull request stages and over dropping the mirror: the clash is the boss's configuration to manage, not crew's.) In this repository today, none of the labels the mirror copies is a stage's label.
- **The workspace does not change.** The session starts on a new `crew/...` branch from the default branch, as for an issue. A prompt that needs the pull request's own branch checks it out itself, for example with `gh pr checkout {{.Issue.Key}}`.
- **The prompt fields stay the four that exist.** `{{.Issue.Ref}}`, `{{.Issue.Key}}`, `{{.Issue.Title}}` and `{{.Issue.URL}}` take the pull request's values. Governs R3.

### Requirements

**What crew takes**

- R1. crew's poll returns the open pull requests that carry any stage's label, by the same rule it applies to issues: opened by the `gh` user crew runs as, in the repository crew runs in.
- R2. A pull request taken by a stage runs that stage's actions and moves through its labels exactly as an issue would: `moves_to` when taken, `on_success` or `on_failure` when the stage ends.
- R3. A prompt rendered for a pull request gets the pull request's reference (`#N`), number, title and URL in the existing fields.
- R4. Issues and pull requests share the slots, queues and the order crew takes waiting items in. A pull request has no Priority and nothing blocks it, so it ranks after every item that has a Priority and is never held back as blocked.

**What crew writes**

- R5. A pull request gets the status comment and, when a stage fails, the failure report, as an issue does.
- R6. When crew moves a pull request, it mirrors no label and posts no stop comment. Those belong to the pull requests that close an issue, and a pull request closes none.
- R7. A pull request's action run resumes after a failure by the same rule as an issue's, keyed by the pull request's number.

**Docs and vocabulary**

- R8. The user guide says that crew polls pull requests as well as issues, and no longer says that crew does not watch pull requests or never takes work from a pull request's labels.
- R9. `CONCEPTS.md` drops the claim, under "Mirrored label", that crew never takes work from a pull request's labels, and its Stage entry says that a stage takes the issues and pull requests that carry its label.

### Acceptance Examples

- AE1. **Covers R1, R2, R3.** **Given** a stage with label `crew:fix review` whose prompt says `gh pr checkout {{.Issue.Key}}`, **when** the boss adds `crew:fix review` to their open pull request #90, **then** at the next poll crew moves #90 to the stage's `moves_to` label and runs the action with `#90`, `90`, #90's title and #90's URL in the prompt.
- AE2. **Covers R1.** **Given** an open pull request with a stage's label, opened by someone other than the `gh` user crew runs as, **then** crew does not take it, as it would not take such an issue.
- AE3. **Covers R6.** **Given** crew moves pull request #90 at the end of its stage, **then** #90 gets its new label and an updated status comment, and no other pull request gets a label or a stop comment because of that move.
- AE4. **Covers R1.** **Given** issue #42 is closed by pull request #90, and a stage watches the label the mirror copies from #42, **when** crew moves #42, **then** #90 gets that label and crew takes #90 at its next poll.

### Scope Boundaries

- No new prompt fields for pull requests, such as the head branch, the base branch or the issues it closes.
- crew does not check out the pull request's branch for the session (see the workspace decision).
- No check in the config against a label being both mirrored and watched by a stage.
- The pull request recorded with an action run (#63) is still found by the action's own `crew/...` branch. A session that pushes to the pull request's own branch is recorded with no pull request. #84 changes that.

<!-- ce-section: work-relationships -->
### How This Work Fits Together

This plan lets a stage take a pull request. The rest is the current understanding, not a committed roadmap.

- #84, the cost of a pull request across every session that worked on it. **Depends on** this plan, because sessions that push to a pull request's own branch only appear once a stage can take a pull request. **Still to decide** how crew learns which pull request a session worked on.

### Sources

- `internal/adapter/github/tracker.go:49-69`: the poll's GraphQL query reads `repository.issues`, which never returns pull requests. It also reads Priority and blocking from issue-only fields.
- `internal/adapter/github/comment.go`, `status.go`: comments go through the issues comments API, which serves pull requests too.
- `internal/adapter/github/pullrequest.go`: the mirror finds an issue's closing pull requests with `repository.issue(number)`, which does not resolve a pull request's number (R6).
- `internal/adapter/github/branchpr.go:37-38`: an action run's pull request is looked up by the action's branch.
- `internal/crew/workflow.go:67-85`: the four fields a prompt can reach.
- `internal/adapter/git/workspace.go:93`: every workspace is a new branch from the default branch.
- `docs/guide/crew.mdx:8`, `:10`, `:377`, `:380`, and `CONCEPTS.md` "Mirrored label": the text that R8 and R9 change.
- `docs/plans/2026-10-02-2014-feat-pull-request-state-plan.md`: the mirrored label and the stop comment (#31).

Product Contract unchanged.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **One GraphQL query lists both kinds.** `issuesQuery` gains a `pullRequests(first: 100, states: OPEN, labels: $labels, orderBy: {field: CREATED_AT, direction: ASC})` connection next to `issues`, under the same `repository`, so a poll is still one `gh api graphql` call. Each pull request node reads `number`, `title`, `url`, `createdAt`, `author { login }` and `labels(first: 100)`, and nothing about Priority or blocking, so a pull request decodes with `Priority` 0 and `Blocked` false (R3, R4). Checked live on 2026-10-03: the combined query runs on this repository with the same `$labels` variable for both connections.
- KTD2. **The author rule is applied in the adapter.** `Repository.pullRequests` has no `createdBy` filter, unlike `issues(filterBy:)`, so `List` drops every pull request whose `author.login` is not the login `gh` runs as (R1, AE2). A pull request whose author is gone decodes with no login and is dropped. Rejected: the search API (`is:pr author:<login> label:...`), which filters by author on the server but is eventually consistent, so a label the boss just added might not show at the next poll.
- KTD3. **`Move` stays as it is.** `gh issue view <n> --json state,labels` answers for a pull request number (checked live: #85 returned `"state":"MERGED"` and its labels), and `gh issue edit` updates a pull request through the same lookup, so the existing swap moves a pull request with no new branch (R2). A merged or closed pull request is not `OPEN`, so `Move` returns `port.ErrMovedMeanwhile` as it does for a closed issue. The implementer confirms in the scripted tests that the arguments are unchanged for a pull request, and must not switch a pull request's move to `gh pr edit`: `Move` does not know the kind of the item, and no kind travels in `crew.Issue`.
- KTD4. **`ReportPullRequests` asks for `issueOrPullRequest`.** `pullRequestsQuery` reads `repository.issueOrPullRequest(number:)` with an `... on Issue` fragment holding today's fields. When the number is a pull request, the fragment is empty, the reply has no closing pull requests, and the report writes nothing and returns nil (R6, AE3). Without this, `repository.issue(number:)` fails with "Could not resolve to an Issue" for a pull request number, which `pullRequests` turns into `port.ErrMovedMeanwhile` on every move of a pull request. A number that is neither now fails with "Could not resolve to an issue or pull request" (checked live with #99999), so the stderr match becomes case-insensitive on "could not resolve to an issue", which covers both messages and keeps a deleted issue as `port.ErrMovedMeanwhile`.
- KTD5. **The listing keeps the oldest first across both kinds.** `List` appends the pull requests after the issues and sorts the whole slice by `Created`, oldest first, with the key as a tie-break. The core orders the candidates itself (priority, later stage, oldest), so this order only makes `List`'s reply deterministic for its tests and logs.
- KTD6. **The adapter keeps no record of which items are pull requests.** Every write either works on both kinds through the same call (`Move`, the comments API in `postComment` and `status.go`) or asks GitHub (KTD4). A cache filled by `List` would be empty after a restart, when the core retries owed calls before its first listing.

### Assumptions

- In this repository no label the mirror copies is a stage's `label` (the Product Contract's mirror decision), so this change makes crew take no pull request here until the boss labels one.
- A pull request crew takes gets a worktree branch named like an issue's, `crew/issue-<number>-<action>`, because the workspace does not change. The name is not shown to anyone but the prompt and the check (`CREW_BRANCH`).
- The 100-item window applies to each kind on its own: at most 100 issues and 100 pull requests per poll. Pull requests other people opened count against the window before KTD2 drops them, so a repository with more than 100 open labeled pull requests from others can hide the boss's. Recorded in the Risks.

### Risks

| Risk | Mitigation |
| --- | --- |
| `gh issue edit` stops accepting pull request numbers in a later `gh` release, so moving a pull request fails. | The scripted tests pin the arguments; a real failure is loud, since `Move` returns gh's error and the move is retried and reported, not lost. |
| More than 100 open pull requests from other people carry crew's labels, filling the window before the author filter (KTD2). | Accepted: crew's labels are the boss's own. The guide says the listing keeps the oldest 100 of each kind. |
| A pull request is both a taken item and the target of an issue's mirror, and the issue's move overwrites the label crew gave it. | The boss's configuration, per the Product Contract's mirror decision; the guide says so (U2). |

---

## Implementation Units

### U1. The GitHub adapter lists pull requests and leaves them out of the mirror

- **Goal:** `List` returns the boss's open pull requests that carry a stage's label, as items like issues, and a pull request's moves write nothing to other pull requests.
- **Requirements:** R1, R2, R3, R4, R5, R6, R7; KTD1 to KTD6.
- **Dependencies:** none.
- **Files:** `internal/adapter/github/tracker.go`, `internal/adapter/github/tracker_test.go`, `internal/adapter/github/pullrequest.go`, `internal/adapter/github/pullrequest_test.go`.
- **Approach:**
  1. Add the `pullRequests` connection to `issuesQuery` and to `issuesReply` (KTD1), keeping the rule from `docs/solutions/integration-issues/issue-field-total-count-fails-listing-on-user-repositories.md`: no `totalCount` anywhere in the query.
  2. In `List`, build a `crew.Issue` from each pull request node whose author's login is the viewer's (KTD2), with the same key, ref and state mapping as an issue, then sort the merged slice (KTD5). Share the label-to-states loop between the two kinds rather than copying it.
  3. Switch `pullRequestsQuery` and its reply type to `issueOrPullRequest` with an `... on Issue` fragment, and widen the "could not resolve" match (KTD4).
  4. Update the package comment, `List`'s and `ReportPullRequests`' doc comments, and `issuesQuery`'s comment to say what they now do with pull requests. `Move` gets one sentence saying it moves a pull request the same way (KTD3).
- **Patterns to follow:** `TestListSendsOneQueryFilteredByLoginAndLabels` and `TestListMarksAnIssueBlockedOnlyWhileAnOpenIssueBlocksIt` for scripted `gh` replies; `TestAnIssueWithoutAPullRequestGetsOnlyTheQuery` for a report that makes only the query.
- **Test scenarios:**
  - Covers AE1. A reply with an open pull request #90 by the viewer carrying a stage's label lists an item with key `90`, ref `#90`, its title, URL, creation time and state, and with priority 0 and not blocked.
  - Covers AE2. A pull request whose author is another login, and one whose author is null, are not listed.
  - A reply with issues and pull requests lists them together, oldest first, and a pull request with two stage labels lists both states, as an issue does (the core then skips it).
  - The query sent by `List` holds both connections, the labels and login arguments, and no `totalCount`; it is still one `gh` call per `List`.
  - Covers AE1. `Move` of key `90` from a reply where #90 is an open pull request runs the same `gh issue view` and `gh issue edit` arguments as for an issue, and a merged #90 (`state` `MERGED`) returns `port.ErrMovedMeanwhile` with no edit.
  - Covers AE3. `ReportPullRequests` for key `90`, whose `issueOrPullRequest` reply is a pull request (an empty object), makes only the query, posts no comment, edits no label and returns nil, with or without `End`.
  - Covers AE4. An issue whose closing pull request #90 is mirrored a stage's label: the existing mirror tests keep passing on the `issueOrPullRequest` reply shape, and a later `List` reply carrying #90 with that label lists #90.
  - The query failing with "Could not resolve to an issue or pull request with the number of 99999" is `port.ErrMovedMeanwhile`; the old "Could not resolve to an Issue" message still is; any other failure stays transient (extend `TestTheQuerysErrorsAreClassified`).
- **Verification:** the adapter's tests pass under `-race`, and the changed lines of both files are covered by the tests above.

### U2. The guide, the README and CONCEPTS.md say crew polls pull requests

- **Goal:** every page that says crew polls only issues, or never takes work from a pull request's labels, says what crew now does.
- **Requirements:** R8, R9.
- **Dependencies:** U1.
- **Files:** `docs/guide/crew.mdx`, `README.md`, `CONCEPTS.md`, `docs/develop/architecture.mdx`.
- **Approach:**
  1. `docs/guide/crew.mdx`: the intro (line 8) and the scope paragraph (line 10) say crew polls the issues and pull requests you labeled. The poll's step 1 says it lists the open issues and pull requests you opened, and that a pull request has no priority and is never blocked (R4). The prompt fields table says the fields hold the pull request's values for a pull request. Its example of working on the pull request's own branch, since the worktree branches from the default branch, is `gh pr checkout {{.Issue.Key}} --detach`, then pushing with `git push origin HEAD:<branch>`, the branch read from `gh pr view {{.Issue.Key}} --json headRefName`. The guide says why: git checks out a branch in one worktree only, and a pull request crew opened keeps its branch checked out in crew's worktree, which crew never removes, so a plain `gh pr checkout` fails there.
  2. "The pull requests" section drops "does not watch them" and "never takes work or a state from a pull request's labels". It says that a stage's label on a pull request makes crew take it, and that a pull request crew takes gets a status comment and failure report like an issue. It says that an issue's move replaces the label crew gave a pull request that closes it (the mirror decision). It also says that a pull request still carrying the mirrored crew label is skipped as carrying two crew labels (the poll's step 2), so the boss replaces that label with the stage's label instead of adding it.
  3. `README.md` line 3: the issues and pull requests you labeled.
  4. `CONCEPTS.md`: the Stage entry says a stage takes the issues and pull requests that carry its label. The Mirrored label entry drops "crew never takes work or a state from a pull request's labels" and keeps that crew replaces a hand-set crew label at the issue's next move.
  5. `docs/develop/architecture.mdx`: the `Tracker` port line says `List` returns the open items in some states, and that the GitHub adapter's items are issues and pull requests.
- **Patterns to follow:** the guide's existing voice (short declarative sentences, "you" for the boss); MDX rule: keep `{{` in backticks.
- **Test expectation:** none -- documentation only; `pnpm docs:check` covers links and MDX.
- **Verification:** no page under `docs/`, nor `README.md` or `CONCEPTS.md`, still says crew polls only issues or never takes work from a pull request's labels, and `pnpm docs:check` passes.

---

## Verification Contract

| Gate | Command | Applies to |
| --- | --- | --- |
| Tests | `go test -race ./...` | U1 |
| Format | `gofmt -l cmd internal tools` prints nothing | U1 |
| Vet | `go vet ./...` | U1 |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | U1 |
| Coverage | the coverage profile, then `go-test-coverage` (total >= 90%) and `tools/diffcover` on the branch diff (changed lines >= 90%), as AGENTS.md lists | U1 |
| Docs | `pnpm docs:check` | U2 |

---

## Definition of Done

- AE1 to AE4 each have an adapter test named in U1, and they pass.
- `internal/core`, `internal/engine`, `internal/config`, `internal/port` and `internal/crew` have no diff.
- The docs of R8 and R9 describe the shipped behaviour, and no page keeps the old claims.
- All gates in the Verification Contract pass.
- No dead code or abandoned attempt is left in the diff.
