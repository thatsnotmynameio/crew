---
title: A moved repository no longer lists its worktrees at their new path
date: 2026-10-02
category: integration-issues
module: internal/adapter/git
problem_type: integration_issue
component: workspace_adapter
symptoms:
  - "Resuming a failed action after the repository moved fails with \"the folder ... is not one of this repository's git worktrees\""
  - "`git worktree list --porcelain` names the worktree under the repository's old path and marks it `prunable gitdir file points to non-existent location`"
root_cause: wrong_api
resolution_type: code_fix
severity: medium
tags: [git, worktree, git-worktree-repair, prunable, resume, workspace, reopen]
---

# A moved repository no longer lists its worktrees at their new path

## Problem

crew resumes a failed action in that run's worktree, `.crew/worktrees/<name>`, which it finds again by rebuilding the path from the repository root and looking it up in `git worktree list --porcelain` (`Reopen`, `internal/adapter/git/workspace.go`). Once the boss moves the repository, git no longer lists the worktree at that path. The first version of `Reopen` then told the boss to remove the folder, and that folder holds the failed run's uncommitted work, which is what resuming exists to keep (#15).

## Symptoms

- The relabeled action fails before its session starts, and crew's output gives the reason from `Reopen` naming the worktree folder.
- From the moved repository, `git worktree list --porcelain` prints the worktree as `worktree <old root>/.crew/worktrees/<name>`, followed by `prunable gitdir file points to non-existent location`. The new path is missing although the folder exists there.

## What Didn't Work

- **Rebuilding the path from the root.** The plan assumed that because the path is recomputed from wherever the repository now is, "a moved repository still finds it" (`docs/plans/2026-10-02-1814-feat-resume-failed-action-plan.md`, U4 Approach). That plan still reads that way. The folder is found, but git's record of the worktree is not.
- **Treating an unlisted folder as "remove it".** `Reopen` sorts a worktree into three states: listed, gone (missing, or listed as prunable), and present but unlisted. The last one is an error rather than "gone" so work is never silently abandoned for a fresh worktree. But its message said to remove the folder, which after a move is the one action that loses the work.

## Solution

The unlisted-folder error now offers the repair before the removal (`internal/adapter/git/workspace.go:137-139`):

```text
the folder <dir> is not one of this repository's git worktrees: if the repository moved since crew created it, run `git worktree repair <dir>` to keep its work; otherwise save any work in it, then remove it so crew can create a new one
```

From the moved main checkout, `git worktree repair <new path>` rewrites both links. The worktree is listed at its new path again, on its branch, with its uncommitted files intact (checked by hand with git 2.55.0). The next relabel then resumes in it. `TestReopenAfterTheRepositoryMovedSaysToRepairTheWorktree` (`internal/adapter/git/workspace_test.go`) moves a repository between `Create` and `Reopen`. It checks that the error is not `port.ErrWorkspaceGone`, that it names `git worktree repair`, and that the uncommitted file is still there.

## Why This Works

A linked worktree is two links holding absolute paths: the main repository's `.git/worktrees/<name>/gitdir` points at the worktree's `.git` file, and that file points back. Moving the main checkout carries the worktree folder along, but both links still name the old location. So git reports the entry under the old path and marks it prunable because that path no longer exists. Matching paths in `Reopen` cannot fix this: the record itself is stale. `git worktree repair` exists to rewrite those links, and running it is the boss's decision, because crew's workspace adapter only reads.

## Prevention

- Do not assume a worktree's path in `git worktree list` follows the repository. Recomputing a path from the root finds the folder, not git's registration of it.
- When code finds an existing folder that git does not list, its message must not lead with removal: that folder may hold the only copy of uncommitted work. Offer `git worktree repair` first.
- Repair before anything prunes. `git worktree prune`, which `git gc` can run, deletes the stale entry altogether. The folder still reads as unlisted, but `git worktree repair` then fails with "unable to locate repository" (checked with git 2.55.0). The files and the branch survive, so copy the work out, or add a worktree on the same branch and move the files in. That is why the error says to save any work before removing the folder.
- A test that moves the repository between creating and reopening a worktree catches this. Path-matching tests, symlinked roots included, do not.

## Related Issues

- #15, resuming an action that stopped partway, which added `Reopen`.
