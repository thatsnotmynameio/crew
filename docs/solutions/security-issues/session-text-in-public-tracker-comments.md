---
title: Session text in a public tracker comment leaks what the session printed
date: 2026-10-02
category: security-issues
module: internal/adapter/github
problem_type: security_issue
component: tracker_adapter
symptoms:
  - "The failure comment on a public issue quoted the session's last message, including commands it ran and their output"
  - "A secret the session echoed in its last message would be posted with it"
root_cause: missing_validation
resolution_type: code_fix
severity: high
tags: [tracker, github, failure-comment, status-comment, session-output, secrets, public-comment]
---

# Session text in a public tracker comment leaks what the session printed

## Problem

crew's GitHub failure comment put each failed action's `Reason` in a fenced code block. The reason is the session's or a tool's last words, so commands, their output and any secret the session echoed were posted on a public issue (#44; fix opened in #46, unmerged as of this writing).

## Symptoms

- The failure comment's code block held whatever the session said last, such as `ran go test ./... and got: FAIL token=...`.
- Only the repository root and the home directory were hidden: the engine writes them as `.` and `~` (`internal/engine/paths.go:39-40`).

## What Didn't Work

- **Fencing the text.** The fence, sized longer than any backtick run in the text, stops it from rendering, linking or @-mentioning anyone. It does nothing about what the text says.
- **Scrubbing paths.** At the time, `scrub` replaced two directory prefixes only. It now also redacts GitHub tokens and PEM private keys (`internal/engine/paths.go`), but hostnames, file contents, command output and any other secret still pass through.
- Both were accepted on purpose when the comments were planned. The architecture plan settled that the failure comment carries the session's last message (`docs/plans/2026-10-01-2202-feat-crew-engine-architecture-plan.md:55`). The status-comment plan named the risk and accepted it: the fence "does not remove secrets a session prints, as is already true of the failure report" (`docs/plans/2026-10-02-0152-feat-status-comment-plan.md:178`). Those plans still read that way.

## Solution

The failure comment carries no session text. It names each failed action and points to its log by its repository-relative path; an action that failed before it had a log (its worktree could not be created) gets a line saying so.

```text
crew: 1 action failed on #42.

**`development`** failed. Its log is `.crew/logs/issue-42-development.log`.
```

`renderReport` (`internal/adapter/github/report.go`) no longer reads the reason. Since #240 it cannot: `crew.ActionFailure` has no reason field, and an outcome's reason is a `crew.SessionText` that neither the failure report nor a pull request's `RuleEnd` carries, so the compiler keeps it out. The reason still reaches crew's own output, where `ui/lines` prints it with the action's end, and, for a session that ran, the output in its log on the boss's machine. `TestReportFailurePointsToEachLogWithoutTheSessionsWords` asserts the exact body and that no reason string appears in it.

## Why This Works

The comment is public and the log is local. Pointing to the log gives the boss the same detail without publishing it, and a path crew builds itself (`.crew/logs/<workspace>.log`) holds nothing a session wrote.

## Prevention

- Treat harness-derived text as untrusted and possibly secret: `crew.Outcome.Reason` (a `crew.SessionText`) and the status's `Said` (a `crew.Said`). A public tracker comment points to the log instead of quoting it. This holds for every tracker adapter, not only `github`.
- A fence or a scrub is a rendering control, not a disclosure control. Do not cite either as making session text safe to post.
- Still open: the status comment's running line (`internal/adapter/github/status.go:215-216`, "It last said:") posts up to the last 200 characters of what the session said, with the same fence and scrub. GitHub keeps every edit of a comment in its history, so editing it later does not remove what was shown. #44 covered only the failure comment.
- When planning new tracker output, check the plans above: they record the old acceptance and would lead a reader to copy it.

## Related Issues

- #44, the report; #46, the fix (open as of this writing).
