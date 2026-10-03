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
