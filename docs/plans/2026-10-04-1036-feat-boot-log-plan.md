---
title: Boot log while crew starts - Plan
type: feat
date: 2026-10-04
topic: boot-log
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #113
execution: code
---

# Boot log while crew starts - Plan

## Goal Capsule

- **Objective:** while crew starts, the boss can always see which step crew is on, so a slow start no longer looks like a hung process.
- **Means:** a boot log, one line printed to the output as each startup step begins, before the live view or the event lines (KTD1, KTD2, KTD3).
- **Product authority:** the boss, through the brainstorm of #113, whose Product Contract is the issue body.
- **Open blockers:** none.
- **Execution profile:** a step-reporting helper in `internal/port`, one line per step in the three preparers, the engine and `mates.Act`, the printer in `app`, the guide and the architecture page. No config key, no core change, no TUI change.
- **Stop conditions:** stop and report if a boot line cannot be printed before the TUI or the event lines take the output, or if reporting a step needs an import `depguard` forbids.
- **Who ships:** the implementer opens one pull request that closes #113. Merging is the boss's.

---

## Product Contract

Product Contract preservation: unchanged from the body of #113, with one reading recorded under Assumptions (the first failing check ends the checks, so AE3 holds).

### Summary

crew prints a boot log while it loads its config and runs the environment checks: one line as each step starts, such as `checking the gh login`, `making mate alice act`, `creating the label "crew:x"` or `resolving origin's default branch`. It prints the log on every start, with the TUI and with `--plain`, so the last line always names what crew is waiting on.

### Problem Frame

Between `crew` being run and the live view appearing, the terminal stays blank. In that stretch crew runs the environment checks (`internal/app/app.go`, `Run` and `prepare`), bounded by a 10-minute timeout. It makes the configured mates act, which resolves the repository through `gh`, probes git and mints a token per mate through the GitHub API (`internal/mates/act.go`, `Act`). It checks `gh auth status`, finds the boss, reads the repository's labels and creates each missing one (`internal/adapter/github/tracker.go`, `Prepare`). It checks that `claude` is on `PATH` (`internal/adapter/claude/harness.go`). It checks the git checkout and its origin and resolves origin's default branch (`internal/adapter/git/workspace.go`). It reads the run journal (`internal/engine/engine.go`, `prepare`). Each of these but the `PATH` check can wait on `gh`, git, the network or the disk. The TUI starts only after they all pass, and `--plain` prints nothing before its first event either, so the boss cannot tell whether crew is working or stuck.

### Requirements

**What the boot log says**

- R1. While crew loads `.crew/config.yaml` and runs the environment checks, it prints one line as each step starts, naming the step in plain words.
- R2. Every step that can wait on `gh`, git, the GitHub API or the disk gets its line: loading the config, making each configured mate act, checking the gh login, finding the boss, reading the repository's labels, creating each missing label, checking the git checkout and its origin, resolving origin's default branch, and reading the run journal. A step repeated per item, a mate or a missing label, prints a line per item, naming it.
- R3. A step that does not run prints no line: a config with no mates prints no mate line, and a repository with every label prints no "creating the label" line.

**Where it shows**

- R4. The boot log prints on every start, before anything else crew shows: before the live view on a terminal, and before the mates' warnings and the first event line with `--plain` or when the output is not a terminal.
- R5. Once the checks pass, the live view or the event lines take over as they do today, and the live view does not repeat the boot log.

**When a step fails or crew is stopped**

- R6. When a check fails, crew prints its error after the boot log and exits with code 2 as today, so the last boot line names the step that failed.
- R7. A stop signal during the checks still stops crew as today, after the lines already printed.

**Docs**

- R8. `docs/guide/crew.mdx` ("Run it") shows the boot log and says that its last line names what crew is waiting on.

### Key Decisions

- **A boot log of plain lines, not a loading screen inside the TUI.** The blank stretch comes before the TUI exists, and lines are enough. (session-settled: user-directed — chosen over a loading state inside the TUI, the issue's original proposal: the boss saw a blank terminal, not an empty live view, and asked for boot logs.) Governs R1, R4.
- **One line per step, as it starts.** (session-settled: user-directed — chosen over a line per step with its result and duration, and over a single line for all the checks: the last line names the step crew is waiting on.) Governs R1, R2.
- **The boot log prints with the TUI and with `--plain` alike.** The blank stretch is the same in both. (session-settled: user-approved — chosen over a boot log in one mode only: named in the brainstorm's scoping synthesis, and the boss confirmed it.) Governs R4.

### Acceptance Examples

- AE1. **Covers R1, R2, R4.** **Given** a config that names mates `alice` and `bob`, and a repository missing two of the workflow's labels, **when** the boss runs `crew` on a terminal, **then** a line appears as each step starts, one per mate and one per missing label, and the live view appears after the last.
- AE2. **Covers R3.** **Given** a config with no mates and a repository that has every label, **when** crew starts, **then** the boot log has no mate line and no "creating the label" line.
- AE3. **Covers R6.** **Given** `gh` is not logged in, **when** crew starts, **then** the boot log ends with the gh login line, the error follows it, and crew exits with code 2.
- AE4. **Covers R4.** **Given** `crew --plain`, **when** crew starts, **then** the boot log prints first, then any mate warnings, then the event lines.

### Assumptions

- **The first failing check ends the checks.** Today the engine runs every port's `Preparer` and reads the journal even after one fails, then joins the errors. With a boot log that would print the harness, workspace and journal lines after a failed gh login, and AE3 could not hold. The engine therefore stops at the first port whose `Preparer` fails (KTD5). A boss with two broken tools sees one error per start instead of both at once. The checks, their order and their messages stay as they are.
- **Two short mate preamble lines.** Before any mate acts, `mates.Act` resolves the repository through `gh` and probes git once for all mates. Each gets its own line (`resolving the repository for the mates`, `checking git for the mates`) ahead of the per-mate lines, since either can wait (R2).
- **The `claude` PATH check gets a line too.** It cannot wait, but a line keeps every check visible and costs nothing, and it keeps the last line right when it fails (R6).

### Scope Boundaries

- The empty board between the live view starting and the first poll returning: the boss did not see it as a problem, so it stays as it is.
- Durations, spinners or a progress bar on the boot lines. Each line carries the event lines' clock time, which is not a duration (KTD3).
- Writing the boot log to `.crew/logs/` or anywhere but the terminal output.
- Changing the checks themselves, their order, their timeout or their error messages. Stopping at the first failure (KTD5) skips the later checks and changes none of them.
- A boot line for `crew mates create`: it is a separate flow with its own output.

### Sources

- `internal/app/app.go`: `Run`, which builds, prepares, and only then starts the renderer.
- `internal/engine/engine.go`: `prepare`, which runs each port's `Preparer` and reads the journal.
- `internal/ui/lines/lines.go`: the event lines, which print the mates' warnings first, as `HH:MM:SS crew: <text>`.
- `docs/guide/crew.mdx`: "Run it", "When crew refuses to start" and "Exit codes".

---

## Planning Contract

### Key Technical Decisions

- KTD1. **Steps reach crew's code through the context, with a helper in `internal/port`.** `port` gains a way to attach a step reporter to a context and a function that reports a step on a context, doing nothing when none is attached. The app attaches its printer to the context the checks run on. The preparers and the engine report on the context they already receive, so the `Preparer` interface, the fakes' signatures and the type-assertion design stay as they are, and a step nested deep in an adapter, such as each label creation, reports without threading a parameter through. Adapters and the engine already import `port`, so `depguard` allows it. A context value is the right carrier here because the reporter is scoped to one call tree, the checks, and a call outside it, such as `Workspace.Create` resolving the default branch later, must stay silent. Rejected: a step callback parameter on `Preparer.Prepare`, which touches every adapter and fake for a display concern. Also rejected: an optional interface that hands adapters a callback, whose state would outlive the checks. Governs R1, R2.
- KTD2. **`mates.Act` takes a step callback in `ActOptions`.** `mates` may import only the standard library and `proc`, so it cannot use the `port` helper. A nil callback reports nothing. `cmd/crew`'s `actingMates` already receives the checks' context, and bridges the callback to the `port` helper on that context. Governs R2, R3.
- KTD3. **A boot line is an event line: standard output, `HH:MM:SS crew: <text>`, written through the `lines` package's format.** `lines` exposes its one-line writer (or a small boot-line function) so the format has one owner. R4 orders the boot log before the warnings and event lines, which go to standard output, and one stream keeps that order when the output is redirected. Standard error stays for errors, so a failure still stands out. The time stamp shows how long the current step has been running without adding durations. Rejected: standard error, which splits the boot log from the event lines in a redirected `--plain` run. Governs R4, R5.
- KTD4. **`app` owns the printer.** `Run` prints the config line itself before `build`, then attaches its printer to the checks' context in `prepare`, so every later line comes from KTD1 and KTD2. The printer writes to `Options.Stdout` with the local clock. Steps arrive one at a time from the goroutine running the checks, so the printer needs no lock. A failed write is dropped, as `errorf` drops one: a broken output fails the renderer's first write, which already stops crew with 1. The renderer gets nothing from the boot log, so the live view does not repeat it (R5). Governs R1, R4, R5, R7.
- KTD5. **The engine's `prepare` stops at the first port whose `Preparer` fails.** It returns that port's error, named as today, without running the later ports, asking for the boss or reading the journal. This is the only way the last boot line names the failed step (R6, Assumptions). `Run` already returns before polling on a prepare error, so nothing reads the unbuilt core. Governs R6.
- KTD6. **The boot lines, each lowercase without a final period, as the event lines are:**

  | Step | Line | Reported by |
  | --- | --- | --- |
  | Config | `loading .crew/config.yaml` | `app.Run` |
  | Mates' repository | `resolving the repository for the mates` | `mates.Act` |
  | Mates' git | `checking git for the mates` | `mates.Act` |
  | Each mate | `making mate <name> act` | `mates.Act`, before each mate resolves |
  | gh login | `checking the gh login` | github tracker `Prepare` |
  | Boss | `finding the boss` | github tracker `Prepare` |
  | Labels | `reading the repository's labels` | github tracker `Prepare` |
  | Each missing label | `creating the label "<name>"` | github tracker `Prepare`, quoted with `%q` as its error is |
  | claude | `looking for claude on PATH` | claude harness `Prepare` |
  | Checkout | `checking the git checkout and its origin` | git workspace `Prepare` |
  | Default branch | `resolving origin's default branch` | git workspace `Prepare` |
  | Journal | `reading the run journal` | engine `prepare` |

  Governs R1, R2.

### Sequencing

U1 adds the helper and the printer format. U2, U3 and U4 report steps and can land in any order after U1. U5 wires the printer and proves the whole log. U6 documents it.

---

## Implementation Units

### U1. The step helper and the boot line format

- **Goal:** a context-carried step reporter in `port` (KTD1) and an exported boot-line writer in `lines` (KTD3).
- **Requirements:** R1, R4.
- **Dependencies:** none.
- **Files:**
  - `internal/port/port.go` (or a new `internal/port/step.go`)
  - `internal/port/port_test.go` (or `internal/port/step_test.go`)
  - `internal/ui/lines/lines.go`
  - `internal/ui/lines/lines_test.go`
- **Approach:**
  1. Add the attach and report functions with an unexported context key. Document that reporting on a context without a reporter does nothing.
  2. In `lines`, expose the existing `line` format for one boot line, without the `warning:` prefix. Keep `Run`'s behaviour unchanged.
- **Patterns to follow:** the package comment of `port` on optional capabilities; `lines.line` for the format.
- **Test scenarios:**
  - Reporting on a context with a reporter calls it with the text, once per call, in order.
  - Reporting on a plain context, and on a context derived from one with a reporter, behaves as expected: nothing for the first, the reporter for the second.
  - The boot-line writer prints `HH:MM:SS crew: <text>` in the given location.
- **Verification:** `go test -race ./internal/port ./internal/ui/lines` passes.

### U2. Steps in the adapters' preparers

- **Goal:** the github tracker, the claude harness and the git workspace report each step of their `Prepare` (KTD6).
- **Requirements:** R2, R3, R6.
- **Dependencies:** U1.
- **Files:**
  - `internal/adapter/github/tracker.go`
  - `internal/adapter/github/tracker_test.go` (or the existing prepare test file in that package)
  - `internal/adapter/claude/harness.go`
  - `internal/adapter/claude/harness_test.go`
  - `internal/adapter/git/workspace.go`
  - `internal/adapter/git/workspace_test.go`
- **Approach:**
  1. In the tracker's `Prepare`, report each step just before its call: the gh login, the boss, the labels, and each missing label inside the loop, after the case-insensitive check (R3).
  2. In the harness's `Prepare`, report the `claude` lookup.
  3. In the workspace's `Prepare`, report the checkout check before `rev-parse` and the default branch before acquiring the lock. Never report in `resolveDefault`, which `Create` also calls.
- **Patterns to follow:** the scripted `gh` and `git` runners in each package's tests. A test attaches a recording reporter to its context.
- **Test scenarios:**
  - Covers AE1. The tracker's `Prepare`, with a repository that lacks two of the states' labels, reports the gh login, the boss, the labels, then one `creating the label` line per missing label, naming each.
  - Covers AE2. With every label present, ignoring case, no `creating the label` line is reported.
  - Covers AE3. With `gh auth status` failing, the only reported step is `checking the gh login`.
  - The harness reports `looking for claude on PATH`.
  - The workspace reports its two steps in order. With no origin remote, it reports only the first.
  - `Workspace.Create` on a context with a reporter reports nothing.
- **Verification:** the three adapter packages' tests pass under `-race`, and `golangci-lint` reports no `depguard` finding.

### U3. Steps in `mates.Act`

- **Goal:** `mates.Act` reports its two preamble steps and one step per configured mate (KTD2, KTD6), and `cmd/crew` bridges them to the checks' context.
- **Requirements:** R2, R3.
- **Dependencies:** U1 (for the bridge only).
- **Files:**
  - `internal/mates/act.go`
  - `internal/mates/act_test.go`
  - `cmd/crew/act.go`
  - `cmd/crew/act_test.go`
- **Approach:**
  1. Add a step callback field to `ActOptions`, nil meaning silent.
  2. Report the preamble before `ResolveRepo` and before `probeGit`, and `making mate <name> act` before each `resolve`. Names are already validated, so a line names a real mate. A mate that ends up a warning still had its line: the step started.
  3. In `actingMates`, pass a callback that reports on the context it received.
- **Patterns to follow:** the existing `Act` tests and their scripted runner and store.
- **Test scenarios:**
  - Covers AE1. With names `alice` and `bob`, the callback receives the two preamble lines, then `making mate alice act`, then `making mate bob act`.
  - With no names, the callback is never called (R3).
  - With a nil callback, `Act` works as today.
  - With `ResolveRepo` failing, the last step received is `resolving the repository for the mates`.
  - In `cmd/crew`, `actingMates` called on a context with a recording reporter forwards the steps, if the existing tests can reach `Act` there; otherwise the bridge is covered by U5's app test through `Options.Mates`.
- **Verification:** `go test -race ./internal/mates ./cmd/crew` passes.

### U4. The journal step, and the engine stops at the first failing port

- **Goal:** the engine reports the journal step and stops preparing at the first port that fails (KTD5, KTD6).
- **Requirements:** R2, R6.
- **Dependencies:** U1.
- **Files:**
  - `internal/engine/engine.go`
  - `internal/engine/prepare_test.go`
  - `internal/engine/journal_test.go`
  - `internal/fake/tracker.go` (if a fake needs to report a step)
  - `internal/fake/harness.go` (same)
- **Approach:**
  1. In `prepare`, return on the first port error, wrapped as today. Ask for the boss and read the journal only when every port prepared.
  2. Report `reading the run journal` before `readJournal`.
  3. Rewrite the comments of `Prepare` and `prepare` that say the errors are joined.
  4. If the app test in U5 needs the fakes' preparers to report a step, let `fake.Preparation` report a configurable text through the `port` helper. Keep its zero value silent.
- **Patterns to follow:** `prepare_test.go`'s fakes and the `slowPreparer` in `engine_test.go`.
- **Test scenarios:**
  - A tracker whose `Preparer` fails: `Prepare` returns an error naming the tracker, the harness's `Preparer` never ran, and the journal step was not reported.
  - All ports succeed: the last reported step is `reading the run journal`, and the core is built as today.
  - A journal that cannot be read still fails `Prepare` with its own error, after the journal step.
- **Verification:** `go test -race ./internal/engine` passes, and no engine comment still says the preparers' errors are joined.

### U5. The app prints the boot log

- **Goal:** `app.Run` prints the config line, attaches the printer to the checks' context, and the whole log shows before the renderer in both modes (KTD4).
- **Requirements:** R1, R3, R4, R5, R6, R7.
- **Dependencies:** U1, U4. U2 and U3 complete the real log but are not needed by these tests.
- **Files:**
  - `internal/app/app.go`
  - `internal/app/app_startup_test.go`
  - `internal/app/app_mates_test.go`
  - `internal/app/app_stop_test.go`
  - `internal/app/app_test.go` (where a test asserts the first lines of standard output)
- **Approach:**
  1. Print `loading .crew/config.yaml` before `build`.
  2. In `Run`, attach the printer to the context handed to `prepare`, so `b.mates` and `eng.Prepare` both see it.
  3. Update the startup tests that assert standard output is empty on a config error: it now holds the config line and nothing else.
  4. Update the comments of `Run` and `Options.Stdout` to mention the boot log.
- **Patterns to follow:** `options(t, ...)` and `listCounter` in the app tests; `synctest` where a test waits on a signal.
- **Test scenarios:**
  - Covers AE4. With `Plain`, a config naming mates and an `Options.Mates` that reports a step and returns a warning, standard output starts with the config line, then the mate's step, then the fake preparers' steps and the journal line, then the warning line, then the event lines.
  - Covers AE2. With no mates, no `Options.Mates` call happens and no mate line appears.
  - Covers AE3. With the fake tracker's `Preparer` failing, standard output ends with the tracker's step, standard error holds the error, and the exit code is 2.
  - A config error prints only `loading .crew/config.yaml` to standard output, the error to standard error, and exits with 2.
  - A signal during a slow `Preparer` (as in `app_stop_test.go`): the lines printed so far stay, crew exits with 2 as today, and no line is printed after the stop.
  - With `Terminal` set and without `Plain`, if the app tests can run the TUI against a buffer, the boot lines come before the TUI's first frame. Otherwise this ordering is by construction: the printer runs before the renderer is chosen.
- **Verification:** `go test -race ./internal/app` passes.

### U6. Document the boot log

- **Goal:** the guide shows the boot log and the architecture page records the step helper (R8).
- **Requirements:** R8.
- **Dependencies:** U1 through U5.
- **Files:**
  - `docs/guide/crew.mdx`
  - `docs/develop/architecture.mdx`
- **Approach:**
  1. In "Run it", before the live view, show a short boot log in a `text` block (a config line, a mate line, the gh steps, the git steps, the journal line), and say it prints on every start, in both modes, and that its last line names what crew is waiting on.
  2. In "When crew refuses to start", say the error follows the boot log, whose last line names the check that failed, and that crew stops at the first failing check.
  3. In `architecture.mdx`, steps 4 and 5 of the startup sequence and the `Preparer` bullet: the app prints the boot log on the checks' context, adapters report steps through the `port` helper, `mates.Act` through its callback, and the engine stops at the first failing port.
- **Test expectation:** none -- documentation only; `pnpm docs:check` covers links and MDX.
- **Verification:** `pnpm docs:check` passes, and no page still says the startup checks report every failure at once.

---

## Verification Contract

| Gate | Command |
| --- | --- |
| Format | `gofmt -l cmd internal tools` prints nothing |
| Vet | `go vet ./...` |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` |
| Tests | `go test -race ./...` |
| Coverage | `go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...`, then `go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml` and `git diff -U0 origin/main...HEAD \| go run ./tools/diffcover -profile coverage.out` |
| Docs | `pnpm docs:check` |

A manual smoke run, `go build ./cmd/crew` then `./crew --plain` in a scratch repository, should show the boot log before the first event line, ending with the step crew waits on.

---

## Definition of Done

- R1 through R8 hold, each proven by a U1 to U5 test scenario or by U6's docs check, and AE1 to AE4 each have a covering test.
- No comment or page still says the startup checks join every port's error.
- Every gate in the Verification Contract passes.
- No dead or experimental code from abandoned approaches remains in the diff.
