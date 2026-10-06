---
title: A subcommand dispatched before start dies by SIGPIPE on a closed stdout
date: 2026-10-06
category: runtime-errors
module: cmd/crew
problem_type: runtime_error
component: infrastructure
symptoms:
  - "crew sessions <id> tasks next with a closed stdout pipe is killed by signal 13 (shell status 141), with nothing on stderr"
  - "The documented exit 1 for a failed write never happens, though the unit test for a failing stdout passes"
root_cause: incomplete_setup
resolution_type: code_fix
severity: medium
tags: [sigpipe, signals, stdout, subcommand, exit-code, cli, subprocess-test]
---

# A subcommand dispatched before start dies by SIGPIPE on a closed stdout

## Problem

`crew sessions <session-id> tasks next|current` (#213) prints one JSON line, and its README section promises exit 1 when it fails while running, which covers a failed write. `run` in `cmd/crew/main.go` dispatches it, like `crew bots`, before crew's flags and so before `start`. Until this fix, `start` was the only place that ignored SIGPIPE, so a reader that closed the pipe early killed the process instead of making the write fail.

## Symptoms

- The built binary run with a closed stdout pipe ends with signal 13 (Python's `subprocess` reports return code -13, a shell 141). Stderr is empty.
- `TestSessionsFailsWhenStdoutFails`, which hands `runSessions` an `io.Writer` that always fails, passes. Every gate was green (tests, lint, 100% changed-line coverage) while the binary broke its contract.

## What Didn't Work

- **A failing `io.Writer` in a unit test.** The write error path ran and exited 1, but that path is exactly what the real binary never reaches. Go turns a broken pipe into a fatal SIGPIPE only for writes to file descriptors 1 or 2 (`go doc os/signal`, "When a Go program writes to a broken pipe"). A fake writer has no file descriptor at all.
- **Swapping `os.Stdout` in-process.** `runCaptured` in `cmd/crew/bots_test.go` replaces `os.Stdout` with a file. That file's descriptor is not 1, so even a real broken pipe swapped in there fails with `EPIPE` and never raises the fatal signal. An in-process test cannot reproduce the bug.
- **Spotting it during planning without testing for it (session history).** The planning flow analysis noted that the runtime kills the process on a broken stdout, but still chose "unit test with a failing io.Writer to cover it". The plan's feasibility review then flagged "Closed stdout pipe kills crew sessions by SIGPIPE, not exit 1" at P3, confidence 50, and it was left as an FYI. The plan's Scope Boundaries ruled out signal handling in `crew sessions` with SIGINT in mind only. The adversarial code review found it again and reproduced it on the built binary.

## Solution

Ignore SIGPIPE in the `sessions` branch of `run` (`cmd/crew/main.go:73-78`), as `start` does (`cmd/crew/main.go:124-131`). A broken stdout then makes `enc.Encode` return `EPIPE`, and `runSessions` prints `crew: write /dev/stdout: broken pipe` and exits 1:

```go
if len(args) > 0 && args[0] == "sessions" {
	// A closed stdout then fails the write, which exits 1, instead of
	// killing crew by SIGPIPE.
	signal.Ignore(syscall.SIGPIPE)
	return runSessions(args[1:], stdout, stderr, captain.Dumb{})
}
```

The test re-runs the test binary as crew in a child process whose fd 1 really is a pipe with its read end closed (`TestSessionsExitsOneWhenStdoutIsClosed` in `cmd/crew/sessions_test.go`):

```go
if os.Getenv("CREW_TEST_CLOSED_STDOUT") == "1" {
	os.Exit(run([]string{"sessions", sessionID, "tasks", "next"}))
}
r, w, _ := os.Pipe()
_ = r.Close()
//nolint:gosec // G702: os.Args[0] is this test binary, re-run as crew with a closed stdout
cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSessionsExitsOneWhenStdoutIsClosed$")
cmd.Env = append(os.Environ(), "CREW_TEST_CLOSED_STDOUT=1")
cmd.Stdout, cmd.Stderr = w, &stderr
// want: *exec.ExitError with ExitCode() == 1, and "broken pipe" on stderr
```

With the `signal.Ignore` line removed, the test fails with `signal: broken pipe`. With it in place, the test passes. gosec's G702 on the `os.Args[0]` re-exec is suppressed with a `//nolint:gosec` line and its reason, as `internal/proc/proc_test.go` does for its own re-exec.

## Why This Works

When SIGPIPE is ignored, the kernel still refuses the write, but the Go runtime no longer exits on it. The write returns `syscall.EPIPE`, and the command's ordinary error branch handles it. The fault was not in `runSessions`. Process-wide setup that every path to output needs lived in `start`, and the subcommands are dispatched before `start` runs.

## Prevention

- Any subcommand `run` dispatches before `start` (`crew bots` and `crew sessions` today) skips `start`'s signal setup. It must decide its own SIGPIPE handling if it writes to stdout and documents an exit code for a failed write. `crew bots create` sets up only SIGINT, SIGTERM and SIGHUP (`cmd/crew/bots.go`). Whether a closed stdout matters there was not judged here.
- Test stdout-failure behaviour with a child process whose fd 1 is a real closed pipe, not with a fake writer or a swapped `os.Stdout`. A unit test with a fake writer covers only the error branch, not whether the process lives long enough to reach it.
- When a plan review flags a gap between a documented exit code and the runtime's default signal behaviour, carry it into the plan's KTDs or tests even at low confidence. Reproducing it with the built binary takes about a minute (`os.pipe()`, close the read end, run the binary with the write end as stdout).

## Related Issues

- #213: the captain and `crew sessions`, where this was found and fixed.
