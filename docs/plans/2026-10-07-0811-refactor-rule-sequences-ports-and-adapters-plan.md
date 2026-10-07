---
title: Prepare ports and adapters for rule sequences - Plan
type: refactor
date: 2026-10-07
topic: rule-sequences-ports-and-adapters
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md
execution: code
---

# Prepare ports and adapters for rule sequences - Plan

## Goal Capsule

- **Objective:** crew behaves for its users exactly as it does today, while the format switch of #220 (rules as a sequence of actions with routes) gets a smaller pull request, because the outside capabilities and names it needs are already on `main`, tested and unused.
- **Means:** the full plan's Implementation Units U1, U2, U4 and U5, built in that order, with the decisions below where the full plan left them open (KTD-P1 to KTD-P7).
- **Product authority:** the full plan, `docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md`, and issue #253. The full plan's Product Contract and KTDs win; this plan only narrows them to part 1 and settles what the full plan deferred.
- **Stop conditions:** stop and report when a unit cannot keep every gate green without a user-visible change, or when a settled KTD of the full plan proves unworkable here.
- **Execution profile:** one branch, one pull request that closes #253. Units land in U-ID order (U1, U2, U4, U5), as #253 directs; each keeps `go test -race ./...` green.

---

## Product Contract

Product Contract preservation: narrowed, no scope change. This part builds the full plan's U1, U2, U4 and U5 only. Its R-IDs are the full plan's, cited as written there; none lands whole here.

### Summary

Adds, unused by the rules: a shell script's exit status (U1), the tracker's comment, close and comment listing (U2), and a private verdict file in both harnesses (U4). Renames the rule run's ending vocabulary so "verdict" is free for an action's result (U5). Nothing a user sees changes.

### Problem Frame

The format switch of #220 is the part that cannot be split further. Everything it needs from ports and adapters, and the rename that frees the word "verdict", can land first without changing behaviour. Landing them first shrinks the switch's diff and lets each capability be reviewed on its own, with its tests.

### Requirements

- R8 (partial). A shell script's exit status reaches crew as a number, so a later shell action can map codes to verdicts. Checks keep passing on 0 and failing otherwise.
- R9 (partial). A session can be handed a private file to write its verdict to. Nothing reads it yet.
- R14, R15 (partial). The tracker can post a comment and close an issue, the effects a route's `comment` and `close` steps will use.
- R39 (partial). The tracker lists an issue's comments with each author's login and whether the author is an App.
- R51 (partial). Closing an issue removes crew's labels from it and from its open pull requests. The stop comment on them belongs to U14.
- N1. No user-visible change: no config key, message, `--plain` line, TUI frame, comment text or journal line changes.

### Scope Boundaries

- The full plan's U3 (function port) and U6 onward are not built here.
- No core, engine or app code calls `Commenter`, `Closer`, `CommentLister`, the exit status beyond today's pass and fail, or the verdict file.
- `ClaimJudging` and its "judging" text on the TUI card stay: KTD1 does not list them, and the text is user-visible.
- The TypeSafe judge (`internal/config/judge_test.go`, CONCEPTS.md's "Judge") is a different concept and is not renamed.
- The README, CONCEPTS.md and AGENTS.md wording about how a run "is judged" stays until the format switch (U15) rewrites it; AGENTS.md's port list is updated where U1 renames `Checker`.
- Considered and not built: a guard against a session running at the same time as another writing into the other's verdict directory. Mode 0700 stops other users only; KTD8 claims only that an earlier session cannot plant a verdict for a later one. Evidence that parallel sessions in one crew tamper with each other would change the call.
- Considered and not built: teaching the fake GitHub to page comment listings. `gh api --paginate` joins pages before crew reads them; the adapter's own test feeds it two concatenated pages, and the fake's one-array answer stays pinned by its test.

### Sources / Research

- `docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md`: the full plan; its U1, U2, U4, U5, KTD1, KTD7, KTD8 and KTD10 govern this part.
- `docs/solutions/runtime-errors/nul-in-gh-argument-freezes-status-comment.md`: strip control characters where text enters a `gh` argument.
- `docs/solutions/security-issues/session-environment-values-in-codex-command-line.md`: values reach a Codex session through its environment only.
- `docs/solutions/integration-issues/closing-pull-requests-include-merged-and-foreign-ones.md`: only open pull requests in the same repository are the issue's.
- `docs/solutions/security-issues/session-text-in-public-tracker-comments.md`: what crew posts carries no session text.

---

## Planning Contract

### Key Technical Decisions

- KTD-P1. **The run-level names become an "ending" family.** Per the full plan's KTD1:
  - `JudgingPhase` becomes `EndingPhase`, with its fields `Verdict` → `Ending` and `Judged` → `Ended`.
  - `Verdict` (crew) becomes `RunEnding`, `SettledVerdict` becomes `SettledEnding`, `VerdictMove` becomes `EndingMove`, `VerdictLanded` becomes `EndingLanded`, `VerdictGivenUp` becomes `EndingGivenUp`.
  - Events: `RunJudged` becomes `RunEnded`, `VerdictMoved` becomes `EndingMoved`, `VerdictDropped` becomes `EndingDropped`. The fact `VerdictSettled` becomes `EndingMoveSettled`, so no fact shares an event's name.
  - The core's `purposeVerdict` becomes `purposeEnding`. `RuleRunID.VerdictReport` and `RuleRun.VerdictReport` become `EndingReport`.
  - `port.Verdict` becomes `port.SessionEnd`, as KTD1 names it.
  - Private helpers and test helpers named after the run's verdict or its judging follow (`judge`, `judged`, `judging`, `verdict`, `reportVerdict`, `judgedFailed`). `internal/crew/verdict_test.go` becomes `ending_test.go`, so U6's `verdict.go` gets a clean name.
  - Error texts that name the old words, such as `refused("a verdict")`, change only where no test, golden file or acceptance snapshot reads them. Comments that describe the run's ending follow the new words.
  - (session-settled: user-approved — chosen over a new word for the action's result: the config already calls it a verdict.)
- KTD-P2. **The journal's wire names are pinned before the rename.** `run_judged`, `verdict_moved`, `verdict_dropped` and the `failures` key stay exactly as written, and a jsonl test spells them out as literals, because today nothing would catch a find-and-replace that changed them.
- KTD-P3. **`port.Checker` becomes `port.Shell` with a result.** `Shell.Run(ctx, Script) (ShellResult, error)` replaces `Check(ctx, Check) error`; `ErrCheckFailed` goes. The result carries the exit status. The error is only for a script that could not start or a context that ended, as today. A script killed by a signal crew did not send reports status -1, which the engine maps to failed like any non-zero status. The engine's `check` keeps its reasons and their order: a ran-and-failed status is judged before the context cases. The exact type and field names are the implementer's within this shape.
- KTD-P4. **The tracker's three capabilities are optional interfaces in `internal/port`.**
  - `Commenter.Comment(ctx, issue, body)`.
  - `Closer.Close(ctx, issue, from)`.
  - `CommentLister.Comments(ctx, issue)` returning `[]crew.Comment`, a value with the author's login, whether the author is an App, the body and the time.
  - Their errors are classified as `Move`'s are (`ErrMovedMeanwhile`, `ErrRefused`, anything else transient).
  - The comment type lives in `crew`, so the domain's answer filter (U19) reads it without importing `port`.
- KTD-P5. **A comment body is stripped of controls but keeps its lines.** `crew.StripControls` turns `\n` into a space, which would flatten a Markdown body. A sibling in `internal/crew/text.go` keeps `\n` and tabs and drops a `\r` (so `\r\n` becomes `\n`), and removes escape sequences and every other control as `StripControls` does. `Comment` applies it before `postComment`.
- KTD-P6. **`Close` is safe to retry whatever step failed.** It reads the item's state and labels first.
  1. A merged pull request is refused (`ErrRefused`), checked from that read, so GitHub's answer to closing it never reaches `classify`, which would call a 422 transient. A read that fails with gh's "Could not resolve to an issue or pull request" is `ErrMovedMeanwhile`, as `pullRequests()` maps it.
  2. An open item not in `from` is `ErrMovedMeanwhile`.
  3. A closed item that carries crew labels, none of them `from`, is `ErrMovedMeanwhile`. Any other closed item (carrying `from`, or no crew label) goes on, with no new close call.
  4. An open item in `from` is closed through the REST API (`PATCH repos/{owner}/{repo}/issues/N` with `state=closed`), written as the bot like `postComment`.
  5. crew's labels are removed from its open pull requests in the same repository, then from the item. Each edit only removes, and is skipped when there is nothing to remove.

  This refines KTD10's "succeeds when the issue is already closed with no crew label": a retry after a failed pull-request edit still reaches the pull requests, because the item's labels go last.
- KTD-P7. **The fake tracker keeps the capabilities off `*fake.Tracker`.** They go on a wrapper type composed like `ReportingTracker`, so a later test can build a tracker without them (U13's startup refusal). The test helper `Tracker.Close(key)` is renamed `CloseIssue(key)` so it cannot be confused with `Closer.Close`.

### Assumptions

- GitHub's REST issue PATCH with `state=closed` closes an issue as completed, and `gh issue view --json state` reports a merged pull request as `MERGED`, as the adapter already relies on for `Move`.
- Claude Code's `--add-dir` takes one or more paths, so the adapter puts it before another flag or uses its `=` form, never right before `--`.

### Deferred to Implementation

- The exact type and field names of the shell result and the comment value, within KTD-P3 and KTD-P4.
- Whether `Comments` reads `created_at` as the time or leaves it zero when GitHub omits it.
- U4's execution note (a real Claude Code and Codex session writing `CREW_VERDICT_FILE`) belongs before U12 relies on the file, not to this part.

---

## Implementation Units

### U1. Shell port with the exit status

- **Goal:** a shell script's exit status reaches crew instead of being folded into an error.
- **Requirements:** R8; full plan KTD7; KTD-P3.
- **Dependencies:** none.
- **Files:** `internal/port/port.go`, `internal/adapter/shell/check.go` (renamed `shell.go`), `internal/adapter/shell/check_test.go` (renamed `shell_test.go`), `internal/fake/checker.go` (renamed `shell.go`), `internal/fake/fake_test.go`, `internal/engine/{engine,exec}.go`, `internal/engine/check_test.go`, `internal/app/app.go`, `internal/app/{app,app_bots}_test.go`, `cmd/crew/main.go`, `AGENTS.md`.
- **Approach:**
  1. Replace `Checker`, `Check` and `ErrCheckFailed` with `Shell`, its script input and a result carrying the status (KTD-P3). The input keeps every field and the environment-and-files contract of today's `Check`.
  2. The adapter reads the status from `*exec.ExitError` through `proc`'s `Wait`. A script that cannot start returns an error. Timeout and stop behave as today.
  3. The fake's `CheckScript.Exit` becomes the reported status; `StartErr`, `Block` and `Delay` behave as today.
  4. The engine's `check` maps status 0 to passed and any other to failed, with today's reasons. `Config.Checker` and `Options.Checker` become `Shell`.
  5. AGENTS.md's port list and test notes name `Shell` and the scripted shell.
- **Patterns to follow:** `Checker.Check` in `internal/adapter/shell/check.go`; `saying` and `lastLine` in `internal/engine/exec.go`.
- **Test scenarios:**
  - A script that exits 3 reports status 3 and no error; one that exits 0 reports 0.
  - A script in a directory that does not exist reports a start error, not a status.
  - A script that kills itself with a signal reports a non-zero status.
  - A script whose context ends is killed with what it started, and the error wraps the context's error.
  - A check whose script exits 3 still fails its action with today's reason, and one whose script exits 0 passes with today's reason.
  - The fake shell scripted with `Exit: 2` reports status 2 and records the run's identity.
- **Verification:** `go test -race ./internal/adapter/shell ./internal/engine ./internal/fake ./internal/app` passes and no check test changed its expected reason.

### U2. Tracker capabilities: comment, close, comment listing

- **Goal:** the tracker can post a comment, close an issue and list an issue's comments with their authors' kind.
- **Requirements:** R14, R15, R39, R51; full plan KTD7, KTD10; KTD-P4 to KTD-P7.
- **Dependencies:** none.
- **Files:** `internal/port/port.go`, `internal/crew/text.go`, `internal/crew/text_test.go`, a new comment type in `internal/crew` with its test, `internal/adapter/github/{comment,close,comments}.go`, `internal/adapter/github/{comment,close,comments}_test.go`, `internal/adapter/github/tracker.go` (compile guards, a remove-only label edit), `internal/fake/tracker.go` (the `CloseIssue` rename), `internal/fake/tracker_test.go`, `internal/fake/routing.go` and `internal/fake/routing_test.go` (the capability wrapper, kept out of `tracker.go`, which is near Codacy's file limit), `acceptance/fakegithub/{rest,issues}.go`, `acceptance/fakegithub/rest_test.go`, `acceptance/README.md`.
- **Approach:**
  1. Add `Commenter`, `Closer` and `CommentLister` (KTD-P4), and list them in the port package's doc.
  2. Add the line-keeping strip (KTD-P5). `Comment` posts the stripped body through `postComment`.
  3. `Close` follows KTD-P6. The remove-only edit sits beside `editLabels`, with the same `missingLabel` handling, and pull requests come from `pullRequests()`.
  4. `Comments` reads `gh api --method GET --paginate repos/{owner}/{repo}/issues/N/comments?per_page=100` as you, decoding the joined page arrays as `findStatus` does, with `user.type == "Bot"` as an App, oldest first.
  5. Errors are classified like `Move`'s and `postComment`'s.
  6. The fake tracker gets a wrapper type with the three methods (KTD-P7), in its own file: it records comments and closes, closes the issue and strips its labels, and serves a scripted comment listing. It can be scripted to fail as the other fake calls can.
  7. The fake GitHub learns `PATCH repos/:owner/:repo/issues/:number` with `state` (closing an item), and acceptance/README.md's gh subset lists it. Comments there already carry `user.type`.
- **Patterns to follow:** `postComment`, `Move`, `swap`, `editLabels` and `findStatus` in `internal/adapter/github`; the scripted `gh` runner (`reply`, `fakeGh`, `wantClassified`) and the two-page fixture in `status_test.go`; `ReportingTracker` and `StatusBoard` in `internal/fake/tracker.go`; the route table in `acceptance/fakegithub/rest.go`.
- **Test scenarios:**
  - `Comment` posts the exact body as the writer bot.
  - A body holding `\x00`, a bare `\r`, `\r\n` and an ANSI escape is posted with them stripped and its newlines kept.
  - The line-keeping strip keeps tabs and `\n`, turns `\r\n` into `\n`, and changes nothing when applied twice.
  - `Close` on an open issue in `from` closes it, then removes crew's labels from its open pull requests and from it, leaving labels that are not crew's.
  - `Close` on an issue already closed with no crew label succeeds without a close call, and still strips a crew label left on an open pull request.
  - `Close` on an open issue no longer in `from` returns `ErrMovedMeanwhile`.
  - `Close` on a closed issue carrying another crew label but not `from` returns `ErrMovedMeanwhile`.
  - `Close` on an issue closed while carrying `from` and another crew label strips both, with no close call.
  - `Close` on an issue gh cannot resolve returns `ErrMovedMeanwhile`.
  - `Close` on a merged pull request returns `ErrRefused` without a close call.
  - The listing returns comments across two pages, oldest first, with an App's comment flagged and a user's not.
  - A transient failure of each of the three is classified as one the outbox owes, and a 404 on the comment calls as `ErrMovedMeanwhile`.
  - The fake tracker's wrapper records a comment and a close, and `List` no longer returns the closed issue.
  - The fake GitHub closes an issue on `PATCH` with `state=closed` and refuses an unknown field.
- **Verification:** the adapter, fake and fake GitHub tests pass. `go -C acceptance vet ./...` and golangci-lint in `acceptance/` pass. No core, engine or app code uses the capabilities.

### U4. Verdict file plumbing in the harnesses

- **Goal:** a session can be given a private verdict file it is allowed to write.
- **Requirements:** R9; full plan KTD8.
- **Dependencies:** none.
- **Files:** `internal/port/port.go`, `internal/adapter/claude/command.go`, `internal/adapter/claude/command_test.go`, `internal/adapter/codex/command.go`, `internal/adapter/codex/command_test.go`, `acceptance/fakeclaude/script.go`, `acceptance/fakeclaude/script_test.go`, `acceptance/README.md`.
- **Approach:**
  1. `port.Run` gains the verdict file and its directory, both optional, documented as owned by crew and outside the worktree.
  2. When set, both adapters put `CREW_VERDICT_FILE` in the session's environment, never in its arguments, and add the directory with `--add-dir`. In Claude Code's arguments `--add-dir` sits before another flag, never right before `--`. Codex's `--add-dir` joins its existing de-duplicated list.
  3. Fake claude accepts `--add-dir <dir>`, and acceptance/README.md says a script can write `CREW_VERDICT_FILE`.
- **Patterns to follow:** `docs/solutions/security-issues/session-environment-values-in-codex-command-line.md`; the existing `--add-dir` loop in `internal/adapter/codex/command.go`.
- **Test scenarios:**
  - The claude command's environment holds `CREW_VERDICT_FILE` with the file's path, and its arguments add the directory with `--add-dir` before the prompt's `--`.
  - The codex command's environment holds `CREW_VERDICT_FILE`; its arguments add the directory with `--add-dir`, carry no `CREW_VERDICT_FILE` value, and never name `.crew/logs`.
  - Without a verdict file, both commands are exactly today's (the claude test that pins the whole environment stays unchanged).
  - Fake claude accepts `--add-dir` with a value and still refuses an unknown flag.
- **Verification:** the adapter and fake claude tests pass; the acceptance module's vet and lint pass.

### U5. Rename the rule run's ending vocabulary

- **Goal:** the word "verdict" is free for an action's result, with no behaviour or journal change.
- **Requirements:** N1; full plan KTD1; KTD-P1, KTD-P2.
- **Dependencies:** none.
- **Files:** `internal/crew/{run,event,fact,apply,decide,report,identity,pullrequest}.go` and their tests, `internal/crew/verdict_test.go` (renamed `ending_test.go`), `internal/core/{runs,outbox,handled,gone,view,pullrequest}.go` and their tests, `internal/port/port.go`, `internal/engine/exec.go` and its tests, `internal/adapter/claude/{harness,stream}.go`, `internal/adapter/codex/{harness,events}.go`, `internal/adapter/jsonl/{encode,decode,line}.go`, `internal/adapter/jsonl/*_test.go`, `internal/fake/harness.go`, `internal/app/*_test.go`, `internal/ui/lines/lines.go`, `internal/ui/tui` tests.
- **Approach:**
  1. Add the jsonl test that pins the wire names as literals (KTD-P2), and see it pass before renaming.
  2. Rename per KTD-P1 with gopls-style renames, package by package, keeping each package building.
  3. Grep afterwards for `Judg`, `judg` and `Verdict` in non-test and test Go code; each hit left is the TypeSafe judge, `ClaimJudging`, a jsonl wire name, or a user-visible text that stays.
- **Patterns to follow:** the existing naming in `internal/crew` (`RunReleased`, `TakeSettled`, `FailureReportSettled`).
- **Test scenarios:**
  - The pinned wire-name test: a journal line for each of the three events carries `run_judged`, `verdict_moved` or `verdict_dropped` as its type, and a version 2 line in the old spelling still decodes.
  - Test expectation otherwise: none -- a rename; the existing tests, renamed with it, are the proof.
- **Verification:** `go test -race ./...` passes; the TUI golden files and the acceptance snapshots are unchanged.

---

## Verification Contract

| Gate | Command | Applies to |
|---|---|---|
| Format | `gofmt -l cmd internal tools acceptance` prints nothing | every unit |
| Vet | `go vet ./...` | every unit |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | every unit |
| Tests | `go test -race ./...` | every unit |
| Coverage floors | the coverage profile, then `go-test-coverage` (total ≥ 90%) and `tools/diffcover` (changed lines ≥ 90%) as AGENTS.md lists | before the pull request |
| Vulnerabilities | `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` | before the pull request |
| TUI golden files | `go test ./internal/ui/tui` passes without `-update` | U5 |
| Acceptance module | `go -C acceptance vet ./...` and golangci-lint in `acceptance/` | U2, U4 |
| Acceptance suite | `go -C acceptance run ./cmd/acceptance -count=1` passes with no snapshot rewritten | before the pull request |
| Codacy limits | functions ≤ 50 NLOC and complexity ≤ 15, files ≤ 500 NLOC (Lizard's file-nloc) | every new or changed file |

---

## Definition of Done

- Every unit's Verification holds and every gate above passes.
- No golden file, acceptance snapshot, README line or journal wire name changed.
- No code outside `docs/` refers to the run-level names KTD-P1 retires, except the jsonl wire names and the TUI's "judging".
- No core, engine or app code calls the new tracker capabilities, reads a shell status beyond pass and fail, or sets the verdict file.
- Code from abandoned approaches is removed from the diff.
- The pull request body contains `Closes #253`.
