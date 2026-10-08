---
title: diffcover passes silently when git writes mnemonic diff prefixes
date: 2026-10-08
category: logic-errors
module: tools/diffcover
problem_type: logic_error
component: tooling
symptoms:
  - "diffcover prints `diffcover: no coverable lines changed` and exits 0 for a change that adds Go code"
  - "The diff fed to it has `--- c/path` and `+++ w/path` headers instead of `a/` and `b/`"
root_cause: config_error
resolution_type: workflow_improvement
severity: medium
retire_when: "tools/diffcover/main.go stops assuming the `b/` prefix (for example, it strips any one-letter prefix or fails on an unknown one); check parseDiff's `+++ ` branch"
tags: [diffcover, coverage, git-diff, mnemonicprefix, diff-prefix, false-pass]
---

# diffcover passes silently when git writes mnemonic diff prefixes

## Problem

`tools/diffcover`, the changed-lines coverage gate, takes the changed files' names from the diff's `+++ b/path` headers. When git writes other prefixes, every file name comes out wrong, no changed line matches the coverage profile, and the gate passes with "no coverable lines changed". Nothing about the output says the check did not run.

## Symptoms

- `diffcover: no coverable lines changed`, exit 0, for a change that adds Go code (during #347's development, a change that added `registry.Statistics` and a fake store).
- The diff's headers read `--- c/internal/registry/registry.go` and `+++ w/internal/registry/registry.go`.

## What Didn't Work

- Looking for an exclusion first: the session checked `tools/diffcover/main.go` and `.testcoverage.yml` for a rule that skips fakes or tests before looking at the diff itself. Neither excludes the files; the diff's headers were the cause.

## Solution

Pass the standard prefixes explicitly whenever the diff is run on a machine whose git config may change them:

```sh
# Before committing: compare origin/main with the working tree.
# git add -N makes new, untracked files show up in the diff.
git add -N <new files>
git diff -U0 --src-prefix=a/ --dst-prefix=b/ origin/main | go run ./tools/diffcover -profile coverage.out
```

With the prefixes set, the same change reported `65 of 65 changed coverable lines covered (100.0%, minimum 90.0%)`.

The command AGENTS.md and CI run, `git diff -U0 origin/main...HEAD`, compares two commits. On this machine it still wrote `a/` and `b/` headers with `diff.mnemonicPrefix=true` set, and diffcover reported the same 65 of 65 lines. The false pass appears when the diff's new side is the working tree or the index, which is the form a session reaches for to check uncommitted work.

## Why This Works

`parseDiff` takes a file name from the `+++ ` line that follows a `--- ` line and strips only `b/` (`tools/diffcover/main.go:268-272`):

```go
file = strings.TrimPrefix(strings.TrimPrefix(text, "+++ "), "b/")
```

With git's `diff.mnemonicPrefix=true` (set in this machine's `~/.config/git/config`), a diff against the working tree names its sides `c/` (commit) and `w/` (working tree), or `i/` for the index. diffcover then keys the changed lines as `w/internal/...`, which no profile entry matches. `report` treats zero coverable lines as a pass (`tools/diffcover/main.go:98-101`), the case meant for a change to docs or tests only. `--src-prefix=a/ --dst-prefix=b/` overrides the config for one command, so the headers are the ones diffcover expects.

CI is not affected: it runs the three-dot form on a runner with git's default config (`.github/workflows/ci.yml`, step "changed lines coverage").

## Prevention

- Treat `diffcover: no coverable lines changed` on a change that touches non-test Go code as a sign that the diff's headers are wrong, not as a pass. Look at the diff's `+++ ` lines.
- When running the gate locally on uncommitted work, pass `--src-prefix=a/ --dst-prefix=b/`.
- A lasting fix belongs in diffcover: strip any single-letter prefix, or fail when a header names a `.go` file outside the module. This was not done in #347, which only ran the gate.

## Related Issues

- #347, the development run that hit it.
- `docs/solutions/tooling-decisions/codacy-lizard-and-golangci-lint-limits.md`, another local quality gate that must agree with CI.
