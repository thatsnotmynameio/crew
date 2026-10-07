---
title: A NUL byte in text passed to gh freezes the issue's status comment
date: 2026-10-02
category: runtime-errors
module: internal/engine, internal/adapter/github
problem_type: runtime_error
component: tracker_adapter
symptoms:
  - "Every status write for one issue fails with fork/exec ...: invalid argument, at every poll, until crew restarts"
  - "The issue's status comment keeps the stage's running entry, and later stages of that issue never appear in it"
  - "A bare carriage return in a failed check's reason ends the Markdown code span early, so the rest of the line renders as Markdown"
root_cause: missing_validation
resolution_type: code_fix
severity: medium
tags: [tracker, github, status-comment, gh, exec, nul, control-characters, check]
---

# A NUL byte in text passed to gh freezes the issue's status comment

## Problem

A failed check's last line becomes the action's reason, and the reason goes into the GitHub status comment, whose whole body crew passes to `gh` as one command-line argument. A NUL byte in that line, as `find -print0` or `git status -z` print, made every write of that comment fail before `gh` started. crew retried the write at every poll forever, and the issue's later stage runs never reached the comment. Review of the #14 work caught it before it merged.

## Symptoms

- The status write fails with `fork/exec /usr/bin/gh: invalid argument`, with nothing on gh's stderr.
- crew's output reports the status failure once (failures in a row are reported once), then stays quiet while it retries.
- The comment's latest entry stays as the stage was before it ended; a later stage, such as `fix` after `development`, never gets its entry.

## What Didn't Work

- **Trimming and UTF-8 repair.** The check's last line was `strings.TrimSpace(strings.ToValidUTF8(...))`. NUL is valid UTF-8 and not white space, so it survived both.
- **The code span.** `codeSpan` (`internal/adapter/github/report.go`) sizes its backtick delimiter so the text cannot close it, which is a rendering control. It removes no characters, so a bare `\r`, which CommonMark reads as a line ending, still ended the paragraph inside the span.
- **Path scrubbing and `lastWords`.** They shorten paths and cut length. Neither touches control characters.

## Solution

`lastLine` in `internal/engine/exec.go`, which keeps a check's last non-empty line, now ends a line at `\r` as well as `\n`, and drops every control character except tab, plus DEL, before the line can become a reason:

```go
// end ends the line being written, keeping it when it is not blank. Control
// characters other than tab are dropped: the line becomes a reason that goes
// into a gh argument, which cannot hold a NUL, and into a comment.
func (l *lastLine) end() {
	line := strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(l.cur), ""))
	...
```

`TestACheckReasonCarriesNoControlBytes` (`internal/engine/check_test.go`) drives a fake check that prints a NUL, a progress line ending in `\r`, and ANSI escapes, and asserts the exact reason.

## Why This Works

Three behaviors chain into the freeze, and each one is reasonable alone:

1. **The body is an exec argument.** `writeStatus` and `createStatus` call `gh api ... -f body=<body>` (`internal/adapter/github/status.go`). An `execve` argument is a C string, so Go's `os/exec` refuses one that holds a NUL, and `gh` never starts.
2. **`classify` calls it transient.** `classify` (`internal/adapter/github/status.go`) maps only HTTP statuses it finds in gh's stderr to a lasting error: 403 that is not a rate limit, and 404 or 410 on the issue itself. A start failure has no stderr, so it falls through as transient.
3. **A transient failure of an ended status holds the queue.** The core retries an ended status that failed transiently at every tick (`sl.owed`, `internal/core/status.go`), and `pump` sends nothing else for that issue while one is owed. That is what makes an earlier stage run's ended entry land before the next run's. The newer ended status of the same run carries the same reason, so it fails the same way.

Removing the bytes at the source breaks the chain at its first link. Every later layer then gets text that `exec` accepts and Markdown cannot reinterpret.

## Prevention

- **Text that crew did not write never reaches a `gh` argument with control characters in it.** A check's last line drops them in `lastLine` (fixed here). Since #240, every text crew shows from a session or a check has its own type in `internal/crew` (`SessionText` for an outcome's reason, `Said` for the status's "It last said:" line, `CheckReason` for a check's reason), and only a constructor that strips control characters and escape sequences can build one. The engine builds them after its scrub, so no running session's words reach the status body with a NUL either.
- **When adding a field to the status body or a failure comment,** strip control characters where the text enters crew (the engine or the adapter that produced it), not in the renderer, so every tracker gets clean text.
- **A new failure mode in a `gh` call is transient by default.** Before relying on a retry, check whether `classify` can tell the failure apart; a failure that recurs on every try needs to be prevented, since retrying it changes nothing.
- **Test with hostile bytes.** A test of any path from external output to a comment should include `\x00`, a bare `\r`, and an escape sequence, and assert the exact text that reaches the tracker.

## Related Issues

- #14, where the check and the per-run status entries were added; review of that work found this.
- `docs/solutions/security-issues/session-text-in-public-tracker-comments.md`: the rule that a public comment carries no session words. That rule is about what the text says; this one is about which bytes it may hold.
