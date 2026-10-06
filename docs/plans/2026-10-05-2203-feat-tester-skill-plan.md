---
title: cw-tester skill and a first scenario per layer - Plan
type: feat
date: 2026-10-05
origin: "GitHub issue #176 (part 2 of 2 of #169)"
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# cw-tester skill and a first scenario per layer - Plan

## Goal Capsule

- **Objective:** a pull request that breaks what crew's released binary does, on the command line, on GitHub or on the TUI screen, fails CI before it merges, so the boss no longer has to run crew by hand to trust a build.
- **Means:** a repo-internal `cw-tester` skill writes black-box scenarios on the acceptance suite from the README, without reading crew's code (KTD1, KTD2), and its first run ships one scenario per layer (KTD5).
- **Product authority:** the boss, through the brainstorm of #169. The Product Contract below is #176's, carried unchanged.
- **Builds on:** #199 (part 1 of #169), merged: `acceptance/` with the harness, the `gh` and `claude` doubles, `cmd/acceptance` and the CI `acceptance` job (R10 to R20).
- **Stop conditions:** stop and report when a first-coverage scenario fails because crew breaks a README promise and the fix is more than a small, obvious change; when the tester run cannot stay blind (it needs crew's source to write a scenario); or when a scenario needs a double the developer cannot add within this work.
- **Execution profile:** the developer role (ce-work) builds the harness guard, the skill and the docs; a fresh tester session, dispatched by the developer and blind to crew's code, writes the scenarios (U4). The developer never edits `acceptance/scenarios/`.
- **Finishes and ships:** lfg, through review and a pull request that carries `Closes #176` and the tester's report.

---

## Product Contract

Product Contract preservation: Product Contract unchanged. R2's reading list is applied as described in KTD2, and AE6 as described in KTD6 (a check of the stamped form, not a comparison with one version), without changing their meaning.

### Summary

A new `cw-*` skill makes an agent session a black-box tester that reads only the README, `crew --help` and the doubles' documentation, writes scenarios on the suite for the README's promises and the edge cases it judges necessary, and rewrites a TUI snapshot only when the new screen keeps those promises. It ships the first scenario per layer: a flag and its exit code, a label move on the fake GitHub, and a TUI screen with its snapshot.

### Problem Frame

Today the boss learns whether a build works by running crew for real in a repository and watching it. The package tests check each package with in-memory fakes. The TUI's golden files are rendered in-process from the view, not from a terminal. Part 1 (#199) added a suite that builds the release binary and runs it against doubles, but its only runs are the developer's smoke runs, which assert no label, comment or layout. So a change can still break a flag, an exit code, a label move or the screen while every test stays green.

No concrete escaped bug was named. The cost today is the manual run before trusting a build, and whatever that run misses.

### Key Decisions

- **The tester is meant to run unattended as a crew rule; the rule and its trigger are left for later.** (session-settled: user-directed — chosen over an agent the boss starts by hand as the end state: the boss set the trigger aside to focus on the skill and the local and CI flow.) Governs R7.
- **A scenario that the binary fails stays red and breaks CI.** (session-settled: user-directed — chosen over marking it a known failure linked to an unlabeled bug issue, and over only reporting the divergence: the tester's pull request merges only after the code or the README is fixed.) Governs R5, R20.
- **The tester also writes the edge-case scenarios it judges necessary, beyond what the README promises.** (session-settled: user-directed — chosen over README promises only: the README proved thin as a specification.) Governs R3.
- **Only the tester rewrites a TUI snapshot.** (session-settled: user-directed — chosen over letting the pull request that changes the TUI rewrite it: nothing the tester owns is edited by another role.) Governs R8, R14.
- **The skill is this repository's own aid, not a generic one.** (session-settled: user-directed — chosen over a generic skill linked into `~/.claude/skills` with a per-repository adapter file: repo-internal tooling.) Governs R1.
- **This work ships one scenario per layer; the rest of the binary is covered later.** (session-settled: user-approved — chosen over covering the whole README now, which makes one large pull request in which every divergence blocks the merge.) Governs R21.

### Actors

- A1. The boss: runs the skill today, decides whether a red scenario is a bug in the code, in the README or in the tester's judgment, and merges.
- A2. The tester: an agent session that runs the skill. It never reads crew's code.
- A3. The developer: builds and maintains the doubles, and may read crew's code. The same role writes the development pull requests that change crew.
- A4. CI: builds the binary with the release config and runs the suite on every pull request.

### Key Flows

- F1. Writing scenarios for an area
  - **Trigger:** the boss runs the skill on an area of crew's behavior, such as the TUI, the config or a rule's label moves.
  - **Actors:** A1, A2
  - **Steps:** the tester reads the README, `crew --help` and the doubles' documentation. It designs scenarios from the README's promises and from the edge cases it judges necessary. It writes them, runs them against the built binary, and commits them on a branch. It reports which scenarios pass, which fail and why, and which undocumented behavior it found.
  - **Outcome:** a pull request that adds scenarios. It is red when the binary fails one of them.
  - **Covered by:** R1 to R7, R9
- F2. Accepting an intended TUI change
  - **Trigger:** a development pull request changes the TUI on purpose, and the snapshot scenario fails.
  - **Actors:** A1, A2, A3
  - **Steps:** the boss runs the skill on that pull request's branch. The tester checks the new screen against the README and its content assertions. When they hold, it rewrites the snapshot and pushes it to the branch. When they do not, it leaves the snapshot alone and reports why.
  - **Outcome:** the pull request turns green only when the new screen keeps what the README promises.
  - **Covered by:** R8, R18

### Requirements

**The tester skill**

- R1. A new `cw-*` skill in `.agents/skills/` makes the session a black-box tester for an area of crew's behavior that the caller names.
- R2. The tester reads only the README, the output of `crew --help`, the doubles' documentation, and the plan of the issue it works on, when there is one. It never reads crew's source, its package tests or older plans in `docs/plans/`.
- R3. A scenario checks either a promise the README makes, citing it, or an edge case the tester judges necessary, stating the expected behavior and why the tester expects it.
- R4. The tester designs scenarios with black-box techniques: equivalence partitions, boundary values, label state transitions, decision tables and error guessing.
- R5. When the binary fails a scenario, the tester keeps the scenario asserting the expected behavior and never weakens it to match the binary.
- R6. When a scenario exercises behavior the README does not document, the tester lists the gap in its report.
- R7. The skill runs to the end without asking anyone, and records its judgments and gaps in the report.
- R8. On a branch whose TUI changed, the tester rewrites a snapshot only when the new screen passes the content assertions and keeps what the README promises.
- R9. The tester's work ends as commits on a branch and a report of the scenarios it added, those that fail and why, and the gaps it found.

**First coverage**

- R21. This work ships at least one scenario per layer, written by the skill: a flag and its exit code, an issue moving from a rule's ready label to its success label on the fake GitHub, and a TUI screen with its snapshot.

R10 to R20 (the doubles, the suite, its local command and its CI job) are part 1's and shipped in #199. The acceptance examples below cite some of them because the scenarios run on that suite.

### Acceptance Examples

- AE1. **Covers R3, R5, R20.** **Given** the README promises a behavior and the binary does something else, **when** the tester writes a scenario for it, **then** the scenario asserts the promised behavior and fails, the CI job is red, and the report names the divergence.
- AE2. **Covers R3, R6.** **Given** the README says nothing about what a second `ctrl+c` does, **when** the tester judges that it must force crew to exit, **then** it writes that scenario with its reason, and the report lists stopping as undocumented.
- AE3. **Covers R8, R18.** **Given** a development pull request changes the TUI on purpose and the snapshot scenario fails, **when** the tester runs on that branch and the content assertions hold, **then** it rewrites the snapshot and pushes it. **When** a content assertion fails, it leaves the snapshot unchanged and reports which promise the new screen breaks.
- AE5. **Covers R10, R11, R21.** **Given** the fake GitHub holds an issue that a code owner opened, labeled with a rule's ready label, and the fake Claude Code is scripted to succeed, **when** the binary runs, **then** the issue ends on the fake GitHub with the rule's success label.
- AE6. **Covers R19, R21.** **Given** a pull request, **when** CI runs the suite, **then** `crew --version` on the binary under test prints the version that the release config stamped.

### Success Criteria

- Breaking a README promise in the binary on purpose (for example, changing an exit code) makes the CI job fail before merge.
- Changing the TUI's layout on purpose makes the snapshot scenario fail until a tester session accepts the new screen.
- The tester's scenarios run against a binary whose source the tester never opened.

### Scope Boundaries

- The crew rule that runs the skill unattended, its label and its trigger, which could be an area issue, a development pull request or a development action.
- Covering the rest of the binary, which follows in area issues, one at a time.
- Running the binary against real GitHub or real Claude Code.
- Testing the macOS and arm64 binaries. CI exercises Linux amd64 only.
- Judging the TUI's look from a rendered image.
- A generic version of the skill for other repositories.
- Changing the `acceptance-tester` agent, which writes behavior tests from a plan before the feature exists.
- The test doubles, the suite, its local command and its CI job (R10 to R20) are part 1 of #169, built in #199.
- Bots that act and `crew bots create`: out of the suite's reach (`acceptance/README.md`, "Bots that act are out of reach"). The skill reports such an area as out of reach instead of writing scenarios for it.
- Considered and not built: a known-failure or quarantine marker for red scenarios. It contradicts the second Key Decision, and nothing here suggests otherwise.
- Considered and not built: a script that enforces the tester's reading list. A session's file access cannot be fenced from inside a skill, and the cost of a leak is a weaker scenario that the boss reviews before merging. A rule run that needs harder isolation can add it with the rule.

#### Deferred to Follow-Up Work

- Making `acceptance` a required check through `bootstrap.sh --checks` in `thatsnotmynameio/.github`. Until then a red scenario fails the job but does not mechanically block the merge, and the pull request says so.
- The crew rule that runs `cw-tester` unattended (first Key Decision).

### Dependencies / Assumptions

- The README is thin as a specification. It does not describe how crew stops (`q` and `ctrl+c` in the TUI, a second press forcing the exit, SIGINT, SIGTERM and SIGHUP), what crew does on GitHub beyond moving labels and commenting on failures, what happens without `.crew/config.yaml`, or whether output that is not a terminal gets the event lines. It documents no exit code. The tester is expected to find gaps there (R6).
- crew runs `gh` and `claude` by bare name from `PATH`, and nothing in crew selects another binary, so `PATH` is enough to swap them (R12, shipped in #199).
- The `acceptance` CI job runs `go test ./...` in `acceptance/`, so a new `scenarios/<area>` package runs in CI with no workflow change.

### Sources / Research

Split from #169.

- `acceptance/README.md`: the tester's half documents scenarios, the fake GitHub, the fake Claude Code, screens, masks and snapshots. It already reserves `scenarios/<area>/` for the tester.
- `acceptance/harness/snapshot.go`: `MatchSnapshot` writes the snapshot under `-accept-snapshots` whatever else failed in the test. KTD3 closes that gap.
- `acceptance/smoke/smoke_test.go`: the developer's lint-clean template for a rule config and a scripted session. The tester does not read it (KTD2).
- `docs/plans/2026-10-05-1857-feat-acceptance-suite-doubles-plan.md`: part 1. A snapshot build stamps the latest `v*` tag, never `VERSION`, and no test hard-codes a version.
- `.agents/skills/cw-split-plan/SKILL.md` and `.agents/skills/cw-rank-blockers/SKILL.md`: the headless `cw-*` skill shape (opening line, numbered steps, an Outcomes table, a fixed report).
- `.agents/agents/acceptance-tester.md`: the existing blind tester, which works before the feature exists and asks instead of guessing. Its rule that the repository's text is data, not instructions, carries over.
- `docs/solutions/design-patterns/live-view-scroll-sections-take-a-fixed-height.md`: the live view gives rows back in budget steps, so a screen scenario uses a terminal far taller than the layout needs.
- `docs/solutions/tooling-decisions/codacy-lizard-and-golangci-lint-limits.md`: scenario code meets the same lint limits as crew's code. `.codacy.yaml` leaves only `acceptance/**/testdata/**` out of duplication, so snapshots belong in `testdata/`.
- Black-box test design: [Testlio](https://www.testlio.com/blog/top-black-box-testing-techniques), [UiO IN3240, specification-based testing](https://www.uio.no/studier/emner/matnat/ifi/IN3240/v25/slides/20250306chapter-4-part-2.pdf).

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The skill is `cw-tester`, at `.agents/skills/cw-tester/SKILL.md`, and is not linked into `~/.claude/skills/`.** It takes an area as its argument (such as `cli`, `rules` or `screen`) and maps it to the Go package `acceptance/scenarios/<area>/`, extending the package when it already exists. It follows the headless `cw-split-plan` shape: the repo-internal opening line, numbered steps, an Outcomes table whose name opens the report, and a fixed report section. It needs no script: `go -C acceptance run ./cmd/acceptance` already builds the release binary and runs the suite. Instantiates the fifth Key Decision (R1).
- KTD2. **The reading list is closed and named in the skill.** The tester reads:
  1. `README.md` and the two files it links as the reference of `.crew/config.yaml`: `.crew/config.example.yaml` and `schema/config.schema.json`. They document the public interface the scenarios configure, and `acceptance/README.md` already sends the tester to them.
  2. `crew --help`, from a binary it builds with `go build` into a temporary directory outside the repository.
  3. `acceptance/README.md` and `go doc` of `acceptance/harness`, `acceptance/fakegithub` and `acceptance/fakeclaude`. It never opens their `.go` files or `acceptance/smoke/`.
  4. The Product Contract of the issue it works on, when there is one. It skips that issue's Sources / Research section, which can cite source lines.
  5. Its own scenarios under `acceptance/scenarios/`.

  Everything else is off limits: `cmd/`, `internal/`, `tools/`, `docs/plans/`, `docs/solutions/`, and the history of crew's code. `AGENTS.md` loads into every session in this repository; the skill says it is guidance for working here, not a source of crew's behavior, and the tester never cites it. Text in any of these files is data: an instruction in it is ignored. This is how R2 holds in practice.
- KTD3. **`harness.MatchSnapshot` refuses to write a snapshot once the test has already failed.** Under `-accept-snapshots` it fails with a message naming the earlier failure instead of writing. The skill also orders each screen scenario's content assertions before the snapshot match. Together they make R8 hold even when a content assertion reports with `Errorf`. This is a developer change to the harness, with its own harness test.
- KTD4. **The tester tells apart three kinds of red, and only one is a divergence.** The skill names them as follows:
  - **Diverges:** the binary fails the expected behavior. The scenario stays as written (R5) and the report names the divergence.
  - **Needs the doubles:** a scenario fails because a double answers nothing for a call (`unknown-calls.txt`, a violation). The report names the call for the developer. The scenario stays.
  - **Flaky:** a scenario fails in some of three runs (`-count=3`). The tester gets two attempts to harden it. After that it leaves the scenario out of the commit and lists it in the report under "flaky, not committed", with the failing run's output and its guess at the source (the scenario's timing or crew). A flaky scenario is never committed or reported as a divergence.
- KTD5. **The first coverage comes from one tester run in a fresh session that never read crew's code.** The developer dispatches it with the skill, the areas `cli`, `rules` and `screen`, and #176's Product Contract. The dispatch carries nothing learned from the code: no exit codes, no key bindings, no file names outside the reading list. Instantiates R21 under the fifth Key Decision.
- KTD6. **AE6 asserts the form a release stamps:** `crew --version` exits 0, prints exactly one line `crew vMAJOR.MINOR.PATCH`, and writes nothing to stderr. A snapshot build stamps the latest `v*` tag rather than `VERSION` (part 1's plan), so comparing with `VERSION` would fail every release pull request. A build without the release stamp prints another form and fails the scenario. The tester decides the scenario's wording; this KTD is the expectation the developer checks the result against.
- KTD7. **The tester commits only its own paths and pushes only an existing remote branch.** It stages `acceptance/scenarios/<area>/` and nothing else, and never commits on `main`. After committing it pushes when the current branch already exists on `origin`, never force-pushes, and reports a rejected push. Under lfg the branch has no remote yet, so lfg's shipping step pushes. It never passes `--no-verify`: when a commit hook refuses the commit, it reports why and stops.
- KTD8. **The tester checks its own code before it commits.** It runs `gofmt`, `go -C acceptance vet ./...` and the pinned golangci-lint in `acceptance/`, because CI lints the module before the suite runs. A finding in its scenario is the tester's to fix.

### High-Level Technical Design

The skill's run, from the area to the report:

```mermaid
flowchart TB
  A[Area named] --> B{Area in reach?}
  B -->|no area| R0[Report: no area]
  B -->|bots that act| R1[Report: out of reach]
  B -->|yes| C[Read the allowed sources and crew --help]
  C --> D[Design scenarios: README promises and edge cases]
  D --> E[Write acceptance/scenarios/area]
  E --> F[Run the area three times]
  F --> G{Result per scenario}
  G -->|passes| H[Keep]
  G -->|flaky, attempts left| D
  G -->|flaky after two attempts| N[Leave out, report flaky, not committed]
  G -->|unknown call| I[Keep, report needs the doubles]
  G -->|diverges| J[Keep asserting the promise, report the divergence]
  H --> K[gofmt, vet, lint]
  I --> K
  J --> K
  N --> K
  K --> L[Commit the area's paths, push if the branch is on origin]
  L --> M[Report]
```

Accepting a snapshot on a branch whose TUI changed (F2, R8):

```mermaid
flowchart TB
  S[Run the screen scenarios without the flag] --> T{Which assertions fail?}
  T -->|none| U[Nothing to accept]
  T -->|a content assertion| V[Leave the snapshot, report the broken promise]
  T -->|only snapshot diffs| W{New screen keeps the README's promises?}
  W -->|no| V
  W -->|yes| X[Rerun those tests with -accept-snapshots]
  X --> Y[Review the snapshot diff, commit and push]
```

### Assumptions

- The tester's report is the session's final message, in the fixed shape the skill defines, so a caller (the boss, lfg, a later crew rule) can paste it into a pull request. lfg puts it in the pull request body.
- If a first-coverage scenario is red, the developer resolves it within this work: a README fix when the binary's behavior is deliberate and undocumented, a small code fix when the binary breaks a README promise, a double when the scenario needs one. The tester then reruns on the changed README. Each resolution is listed in the pull request. A larger code fix is a stop condition.
- lfg's review and simplify steps do not edit `acceptance/scenarios/`. A finding there goes back to a tester run, or into the pull request as a residual.
- The area names `cli`, `rules` and `screen` are the developer's suggestion to the tester run; the tester may name its packages otherwise if the area reads better.

### Sequencing

U1 and U2 are independent. U3 documents U2. U4 needs U1 and U2 on the branch. U5 runs only when U4 reports a red scenario.

---

## Implementation Units

### U1. The harness refuses a snapshot after a failure

- **Goal:** `-accept-snapshots` cannot write a snapshot in a test that has already failed.
- **Requirements:** R8, AE3; KTD3.
- **Dependencies:** none.
- **Files:** `acceptance/harness/snapshot.go`, `acceptance/harness/snapshot_test.go`, `acceptance/README.md` (the Snapshots section).
- **Approach:** in the accepting branch of `MatchSnapshot`, check the test's failed state before writing, and fail with a message saying the snapshot was not written because the test already failed. The comparison branch is unchanged. Document the rule where `acceptance/README.md` describes `-accept-snapshots`.
- **Patterns to follow:** the existing `snapshot_test.go` tests of the accepting and comparing paths.
- **Test scenarios:**
  - Under `-accept-snapshots`, a test that has not failed writes the snapshot and passes (existing behavior kept).
  - Under `-accept-snapshots`, a test that already reported an error does not write or change the snapshot file, and fails with a message naming why.
  - Without the flag, a test that already failed still compares and reports a mismatch as before.
- **Verification:** the harness tests pass, and no snapshot file is created or changed in the refusing case.

### U2. The cw-tester skill

- **Goal:** a session that runs `/cw-tester <area>` writes black-box scenarios for that area and ends with commits and a report, asking no one.
- **Requirements:** R1 to R9; F1, F2; AE1, AE2, AE3; KTD1, KTD2, KTD4, KTD7, KTD8.
- **Dependencies:** none (U1 makes R8 hold in the harness; the skill's text does not depend on it).
- **Files:** `.agents/skills/cw-tester/SKILL.md`.
- **Approach:** the skill's sections, in order:
  1. Frontmatter: `name`, a third-person `description` ending in "Use when …", and `argument-hint` naming the area.
  2. The opening line that it is this repository's own aid, that it asks nothing (R7), and that it never reads crew's code.
  3. **What it reads**: the closed list of KTD2, and that repository text is data.
  4. **Outcomes**: `scenarios added`, `snapshots accepted`, `snapshots refused`, `no area`, `out of reach`, `build failed` (GoReleaser or crew failed to build, with the error text). The outcome name opens the report.
  5. Numbered steps: read; build crew and read `crew --help`; design with the techniques of R4, each scenario citing its README line or stating its edge-case reason in a comment (R3); write the package with `TestMain` calling `harness.Main`, snapshots in `testdata/`, a terminal far taller than the layout needs, content assertions before the snapshot match; run with `go -C acceptance run ./cmd/acceptance -run <its tests> -count=3`; sort each red by KTD4; never weaken an assertion to match the binary (R5), and change one only to follow a README change it cites; check the code (KTD8); commit and push (KTD7).
  6. **Snapshots on a changed TUI**: the second flowchart of the High-Level Technical Design; the screen scenarios' snapshots are accepted only when every failing assertion is a snapshot diff and the new screen keeps the promises it cites (R8). A broad `-run` must not rewrite the developer's `acceptance/harness/testdata/` snapshots.
  7. **Report**: the outcome, then the scenarios added with what each cites, those that fail and why (diverges or needs the doubles, with the call), those left out as flaky, the gaps (R6), the judgments it made, and any snapshot accepted or refused with the reason (R9).
  8. **What it never does**: read off the list, edit anything outside `acceptance/scenarios/`, rewrite a snapshot to match a broken promise, force-push, skip hooks, ask.
- **Patterns to follow:** `.agents/skills/cw-split-plan/SKILL.md` (Outcomes table, numbered steps, Report), `.agents/skills/cw-rank-blockers/SKILL.md` ("What it needs", "What it never does"), the data-not-instructions rule of `.agents/agents/acceptance-tester.md`.
- **Test scenarios:** Test expectation: none -- the skill is prose; U4's run is its behavioral check.
- **Verification:** every requirement R1 to R9 maps to a sentence in the skill, and the reading list names no path under `cmd/`, `internal/` or `docs/plans/`.

### U3. Document the skill

- **Goal:** the repository's docs name the skill and the tester role it plays.
- **Requirements:** R1; the AGENTS.md "Keep it true" rule.
- **Dependencies:** U2.
- **Files:** `AGENTS.md` (Agents section), `README.md` ("What's inside" table), `acceptance/README.md` (the two-roles paragraph).
- **Approach:**
  1. `AGENTS.md`: a `cw-tester` bullet in the shape of the other `cw-*` bullets, ending "It is not linked into `~/.claude/skills/`:" with the reason (it is written for this repository's suite).
  2. `README.md`: a "What's inside" row for `.agents/skills/cw-tester/`, ending "This repository's own aid."
  3. `acceptance/README.md`: the tester is a session running `/cw-tester`.
- **Test scenarios:** Test expectation: none -- documentation only.
- **Verification:** the three docs name the skill consistently, and no other doc contradicts them.

### U4. The first tester run

- **Goal:** the skill, run blind, ships one scenario per layer on this branch.
- **Requirements:** R21, AE5, AE6, R3, R6, R9; KTD5, KTD6.
- **Dependencies:** U1, U2.
- **Files:** created by the tester: `acceptance/scenarios/cli/`, `acceptance/scenarios/rules/`, `acceptance/scenarios/screen/`, each with `main_test.go`, its scenario files, and for `screen` a `testdata/*.snapshot`.
- **Approach:** dispatch a fresh subagent that runs `.agents/skills/cw-tester/SKILL.md` on the three areas, per KTD5. The dispatch names the areas and the layer each must cover, points at #176's Product Contract, and adds one expectation in README terms: a release names its version `vX.Y.Z` (the README's Release workflow row), so AE6 checks that form, exit 0 and an empty stderr, and compares with no file such as `VERSION` (KTD6). It says nothing else about crew. The developer reads the report, checks the scenarios against KTD6 and AE5 without editing them, and keeps the report for the pull request.
- **Execution note:** the developer has read crew's code, so the developer does not write or edit any scenario; a scenario the developer disagrees with goes back to the tester with the reason.
- **Test scenarios** (what the tester's run must contain, in its own wording):
  - Covers AE6. `crew --version` exits 0 and prints one line `crew vMAJOR.MINOR.PATCH`, with nothing on stderr.
  - A flag edge case with its exit code, such as an unknown flag, with the tester's stated reason and the exit codes listed as a gap.
  - Covers AE5. An issue the viewer opened, on a rule's ready label, with a session scripted to succeed, ends on the rule's success label, and crew exits cleanly.
  - A TUI screen after that move, with content assertions on what the README promises the live view shows, then a snapshot match.
  - Any further edge cases the tester judges necessary, such as AE2's second `ctrl+c`, each with its reason.
- **Verification:** the suite passes three times in a row locally, or every red scenario is in the report as a divergence or as needing the doubles; the report lists the README gaps found.

### U5. Resolve the red scenarios of the first run (conditional)

- **Goal:** the pull request is green with every scenario still asserting what the tester expects.
- **Requirements:** R5, R20; the second Key Decision.
- **Dependencies:** U4, and only when U4 reports a red scenario.
- **Files:** depends on the divergence: `README.md` for a deliberate behavior the README does not document; the crew code that breaks a README promise; `acceptance/fakegithub/` or `acceptance/fakeclaude/` for a call the doubles lack, with `acceptance/README.md` where it documents them.
- **Approach:** resolve each red per the Assumptions entry on red first-coverage scenarios, without touching the scenario. After a README change, run the tester again on that area so it can follow the change it cites. Stop per the Goal Capsule when a code fix is not small and obvious.
- **Patterns to follow:** `acceptance/README.md`, "For the developer", for adding a call to a double.
- **Test scenarios:**
  - A call added to a double gets the double's own test, as part 1's doubles have.
  - A README change is checked by the tester's rerun, not by a new test.
- **Verification:** the suite passes three times in a row, and the pull request lists each resolution.

---

## Verification Contract

| Check | Command | Proves |
| --- | --- | --- |
| Harness tests | `go -C acceptance test -race ./harness` | U1 |
| Acceptance suite | `go -C acceptance run ./cmd/acceptance -count=3` | U4, U5, AE5, AE6 |
| Acceptance lint | `go -C acceptance vet ./...` and, in `acceptance/`, `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | scenario and harness code meet the bar |
| Formatting | `gofmt -l cmd internal tools acceptance` prints nothing | all Go code |
| Root suite | `go test -race ./...`, `go vet ./...`, golangci-lint at the root | nothing outside `acceptance/` regressed |
| Acceptance vulnerabilities | `go -C acceptance run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` | as CI runs it |

The coverage floors and `tools/diffcover` stop at `acceptance/go.mod`, so they do not measure this work's Go code.

## Definition of Done

- `.agents/skills/cw-tester/SKILL.md` exists, covers R1 to R9 and names the reading list of KTD2.
- `MatchSnapshot` refuses to write after a failure, with a harness test (U1).
- `acceptance/scenarios/` holds at least one scenario per layer, written by a blind tester run (R21), and the suite passes in CI.
- `AGENTS.md`, `README.md` and `acceptance/README.md` name the skill.
- The pull request body carries `Closes #176`, the tester's report, any U5 resolution, and the note that `acceptance` is not yet a required check.
- No code from abandoned attempts remains in the diff, and no file under `acceptance/scenarios/` was edited by the developer.
