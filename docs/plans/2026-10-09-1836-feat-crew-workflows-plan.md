---
title: Run some of this repository's crew scripts as manual GitHub Actions workflows - Plan
type: feat
date: 2026-10-09
topic: crew-workflows
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
---

# Run some of this repository's crew scripts as manual GitHub Actions workflows - Plan

Brainstorm preview for #420. The brainstorm is not finished: the open questions below still need answers before planning.

## Goal Capsule

**Objective:** add five GitHub Actions workflows, named `crew-*`, that each do what one of this repository's local crew scripts in `.crew/scripts/` does, run on GitHub's runners and are started only by hand.

**Not in scope:** changing how crew runs today. `.crew/config.local.yaml` keeps running the local scripts; nothing dispatches the workflows yet.

## Product Contract

### Summary

Five of the eleven scripts behind the shell actions in `.crew/config.local.yaml` become `workflow_dispatch` workflows: `crew-merge-gate`, `crew-publish-split`, `crew-clean-split` (new), `crew-plan-to-body` and `crew-plan-from-issue`. What a script reads from the local worktree or the session today, a workflow reads from the issue's branch or from its inputs. The workflows read and write only that branch, never `main`.

### Problem Frame

The scripts run on the machine that runs crew and need `gh` logged in as a bot and `jq` there. #420 asks which of them could run on GitHub instead. Six of them depend on files only the local worktree or the session has, and others do no more than one `gh` command. This plan keeps only the scripts whose logic or effect is worth a workflow, as a first step that changes nothing in crew's flow.

### Requirements

- R1. Each workflow's name starts with `crew-`.
- R2. Each workflow starts only through `workflow_dispatch`: no push, pull request, label, comment or schedule trigger.
- R3. Adding the workflows changes nothing in how crew runs: `.crew/config.local.yaml` and `.crew/scripts/` stay as they are.
- R4. A workflow reads and writes only the branch it is given, and refuses `main`.
- R5. `crew-merge-gate` does what `merge-gate` does: checks that the pull request is open, mergeable and clean, and that its last word is crew-developer's comment ending with `<!-- crew: stopped watching -->`, and comments on the pull request when either does not hold. It takes the pull request's number as an input.
- R6. `crew-publish-split` does what `publish-split` does, reading the split's parts from `docs/splitting/<kind>/issue-N/` on the branch, and taking the split's outcome (`split`, `not split`, ...) as an input in place of the session's last message.
- R7. `crew-clean-split` removes `docs/splitting/<kind>/issue-N/` from the branch, commits and pushes.
- R8. `crew-plan-to-body` does what `plan-to-body` does, reading the plan committed on the branch at a path given as an input, in place of `.git/planning/plan-path`.
- R9. `crew-plan-from-issue` does what `plan-from-issue` does, writing the plan from the issue's body to `docs/plans/` and committing it on the branch. It reports the branch's base in the job's summary, as a runner has no `.git/development`.
- R10. `/docs/splitting/` comes out of `.gitignore`, so the split's parts can be committed on a branch.
- R11. Each workflow ends with the script's verdict and reason line visible in the run.

### Key Decisions

- **Only five scripts become workflows.** A manual workflow pays only when it does more than one or two `gh` commands a person would type. (session-settled: user-approved — chosen over turning every script into a workflow: the six cut ones are one `gh` command or have no effect on a runner.) Cut:
  - `pr-of-issue`: `gh issue view N --json closedByPullRequestsReferences` gives the pull requests that close an issue; the script exists to write `.git/pull-request` for the actions after it, which a runner does not share.
  - `pr-merged`: `gh pr view N --json state`.
  - `pr-checkout`: a checkout on a runner leaves nothing behind.
  - `pr-closes-issue`: the `Closes #N` link it checks is the one `closedByPullRequestsReferences` exposes.
  - `pr-mergeable`: `gh pr view N --json state,mergeable,mergeStateStatus`, asked again while `UNKNOWN`.
  - `plan-units`: a `sed | grep -c` over the issue's body.
- **`session-finished` stays local for now.** (session-settled: user-directed — chosen over committing the session's prompt and last message on the branch or passing them as inputs: the repository is public, and the script is written never to echo that text.)
- **Local files move to the branch.** What a script reads from git-ignored or uncommitted files, a workflow reads from the issue's branch, and a cleanup workflow removes the split's files from it. (session-settled: user-directed — chosen over leaving the worktree-dependent scripts out.)
- **The workflows write as `github-actions[bot]` through `GITHUB_TOKEN`, for now.** (session-settled: user-directed — chosen over a GitHub App token for one of crew's bots, with the key as a repository secret: no new secret for now.) Known consequences, accepted while nothing dispatches the workflows:
  - Sub-issues `crew-publish-split` creates are opened by `github-actions[bot]`, which is not in `$CREW_CODE_OWNERS` or `$CREW_BOTS`, so crew does not take them and the relationship rule leaves them out.
  - A push through `GITHUB_TOKEN` starts no workflow, so a commit `crew-clean-split` or `crew-plan-from-issue` pushes on a pull request's branch has none of the checks the `main` ruleset requires.
  - `merge-gate` already skips `github-actions` when it looks for the last word, so `crew-merge-gate`'s own comment does not change its result.
- **A trivial check that must leave the machine later belongs in a crew function, not a workflow.** Dispatching a workflow adds a runner's start to a check that takes under a second locally; `internal/function/` is where such a check would go. This answers #420's scope question for the cut scripts, and is not work for this plan.

### Scope Boundaries

- No change to `.crew/config.local.yaml`, `.crew/scripts/` or `.crew/config.yaml`.
- No wrapper that dispatches a workflow from a shell action and waits for it (#420's dispatch, `gh run watch`, artifact flow).
- No GitHub App token or new repository secret.
- No `session-finished` workflow.

### Outstanding Questions

- Where the shell logic lives: the scripts are git-ignored (`*.local*`), so a workflow either copies a script's logic or runs a committed copy that the local config could later point at. Two copies drift.
- How a verdict other than pass or fail (exit code 3, such as `plan-from-issue`'s `unplanned`) shows in a run that only succeeds or fails.
- Whether removing `/docs/splitting/` from `.gitignore` lets a crew session commit a split's files by accident, for example a later session in the same worktree staging everything.
- Which branch `crew-publish-split` and `crew-clean-split` take when the split ran in a crew worktree whose branch is never pushed.

### Sources / Research

- #420 and its first pass over the eleven scripts.
- `.crew/config.local.yaml` and `.crew/scripts/*.local.sh` (local, not committed).
- `.github/workflows/release.yml`, the repository's `workflow_dispatch` precedent, and `docs/plans/2026-10-08-1217-feat-manual-release-workflow-plan.md`.
- The `main` ruleset: nine required checks and `required_review_thread_resolution: true`.
