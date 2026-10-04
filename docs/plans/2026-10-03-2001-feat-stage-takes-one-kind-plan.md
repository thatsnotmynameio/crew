---
title: A stage takes one kind of item - Plan
type: feat
date: 2026-10-03
topic: stage-takes-one-kind
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #92
execution: code
---

# A stage takes one kind of item - Plan

## Goal Capsule

- **Objective:** a stage runs only on the kind of item its prompt was written for, and the boss can see when crew leaves a labeled item alone because of its kind.
- **Means:** each stage in `.crew/config.yaml` declares whether it takes issues or pull requests (KTD1). A stage that declares nothing takes issues.
- **Product authority:** the boss, through the brainstorm of #92. This work replaces the decision "the label alone decides what a stage takes" in `docs/plans/2026-10-03-1708-feat-poll-pull-requests-plan.md` (#89), and #89's AE4. The rest of #89 stands.
- **Open blockers:** none.
- **Execution profile:** a domain field and a config key, one line in the GitHub adapter, the take rule and a once-per-labeling notice in the pure core, one line in the renderer, then the docs. No port changes.
- **Stop conditions:** stop and report if the kind cannot reach the core through `crew.Issue` without changing `port.Tracker` (KTD2), or if showing the notice once needs state outside the core (KTD4).
- **Who ships:** the implementer opens one pull request that closes #92. Merging is the boss's.

---

## Product Contract

Product Contract preservation: unchanged, carried from the body of #92.

### Summary

Each stage declares the one kind of item it takes: issues, the default, or pull requests. crew takes an item only for a stage of its kind. When an item carries the label of a stage of the other kind, crew leaves it alone and shows the boss a notice.

### Problem Frame

Since #89 a stage takes every open item that carries its label, issue or pull request, and the stage's prompt is expected to handle either. Prompts are written for one kind of item. The `development` and `fix` stages run `/compound-engineering:lfg` on an issue, and their checks look for a pull request that closes that issue. A pull request that gets `crew:development:ready`, by hand or through the mirrored label, starts an unattended `lfg` session on a pull request that prompt was never written for.

### Key Decisions

- **A stage declares the kind of item it takes, and the default is issues.** Governs R1, R2, R4. (session-settled: user-directed — chosen over the label alone deciding, as #89 decided, and over a `poll` key under `tracker:`: a prompt is written for one kind of item, so the stage that holds the prompt is where its kind belongs.)
- **The kind is the stage's, not the action's.** Governs R1. (session-settled: user-directed — chosen over a kind per action, where a stage would take both kinds and each action would skip the other: the stage is what takes items by label, and all of a stage's actions run on the same item.)
- **A stage takes one kind, never both.** Governs R1. (session-settled: user-directed — chosen over a list such as `[issues, pull_requests]`: the prompt always knows what it gets, and accepting a list later stays compatible with a single value.)
- **An item of the wrong kind is ignored, with a notice.** Governs R5, R6. (session-settled: user-directed — chosen over ignoring it silently and over a comment on the item: the boss learns why crew left the item alone, and crew writes nothing new on GitHub.)

### Requirements

**Config**

- R1. Each stage in `workflow` may declare the kind of item it takes: issues or pull requests, exactly one of the two.
- R2. A stage that declares no kind takes issues.
- R3. A value other than the two kinds is a config error that names the key's path and line, and crew exits with code 2 before its first poll, as it does for every config error.

**What crew takes**

- R4. A stage takes the open items of its kind that carry its label. Every other rule #89 set for what crew takes stays as it is: the author filter, slots, queues, order, and skipping an item with two crew labels.
- R5. crew does not take, move or comment on an item that carries the label of a stage of the other kind.
- R6. For such an item, crew shows a notice in both its outputs (the TUI and the plain line output) naming the item, the label, and the kind that label's stage takes. The notice shows once while the item keeps the label, and shows again if the label is removed and added back.
- R7. The mirrored label works as it does today. A pull request that gets a stage's label through the mirror is taken only when that stage takes pull requests, and otherwise falls under R5 and R6.

**Docs and vocabulary**

- R8. The user guide documents the key, its default, and the notice. Its section "A pull request as the work" says that a stage takes a pull request only when it declares pull requests.
- R9. The `CONCEPTS.md` entries "Stage" and "Mirrored label" say that a stage takes the items of its kind that carry its label.

### Acceptance Examples

- AE1. **Covers R2, R4, R6.** **Given** the `development` stage declares no kind, **when** the boss adds `crew:development:ready` to their open pull request #90, **then** crew does not take #90 and shows a notice that #90 carries the label of a stage that takes issues.
- AE2. **Covers R1, R4.** **Given** a stage with label `crew:fix review:ready` that declares pull requests, **when** the boss adds `crew:fix review:ready` to their open pull request #90, **then** crew takes #90 at its next poll and runs the stage on it, as #89's AE1 describes.
- AE3. **Covers R5, R6.** **Given** the same `crew:fix review:ready` stage, **when** the boss adds `crew:fix review:ready` to their open issue #42, **then** crew does not take, move or comment on #42, and shows the notice once, not again at the next poll while #42 keeps the label.
- AE4. **Covers R3.** **Given** a stage whose kind is set to a value that names neither issues nor pull requests, **when** crew starts, **then** it reports a config error naming that key's path and line and exits with code 2 without polling.
- AE5. **Covers R7.** **Given** issue #42 is closed by pull request #90, and a stage that takes issues watches the label the mirror copies from #42, **when** crew moves #42, **then** #90 gets that label, crew does not take #90, and crew shows the notice for #90. This replaces #89's AE4.

### Scope Boundaries

- No `poll` key under `tracker:`, the shape #92 first proposed. The stage's kind replaces it.
- A stage never takes both kinds.
- No kind on an action.
- No comment on GitHub for an item of the wrong kind.
- No config check against a mirrored label being a stage's label. #89 left that to the boss's config, and it stays there.
- This repository's `.crew/config.yaml` keeps its stages as they are: all of them take issues, the default.
- No new prompt field: a prompt does not see the item's kind. The stage's kind already tells the prompt's author what it gets.
- The two-label skip (`IssueSkipped`) keeps showing at every poll, as today. Making it show once is #49's area, not this work.
- Considered and not built: remembering shown notices across a restart. A restarted crew shows each notice once more, which costs the boss one line; persisting it would need a new file under `.crew/`. Evidence that would change this: a boss reporting repeated notices as noise.

### Dependencies / Assumptions

- The latest release, v0.1.0, predates #89, so no released crew takes pull requests. R2's default changes nothing for a user of a release.

### Sources

- `docs/plans/2026-10-03-1708-feat-poll-pull-requests-plan.md`: the decision this work replaces (Key Decisions, first entry), and AE4, which AE5 replaces. Its KTD3 and KTD6 say no kind travels in `crew.Issue`; KTD2 below changes that for the listing only.
- `docs/solutions/integration-issues/blocked-issues-dispatched-by-github-tracker.md`: the precedent for a listing fact the core decides on, carried as a `crew.Issue` field whose zero value keeps every other tracker working, not as a new port interface.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The key is `takes`, with the values `issues` and `pull_requests`.** It sits on each stage next to `queue`. Left out, or given with no value, it means `issues`, as an empty value means a missing key everywhere in the config (`internal/config/decode.go`, `decodeValue`). Any other value, the empty string and other spellings included, is a `keyError` at `workflow[N].takes` with its line, such as `workflow[1].takes (line 40): "prs" must be issues or pull_requests`. Values are matched exactly, like queue names. Governs R1, R2, R3.
- KTD2. **The item's kind travels in `crew.Issue` as `Kind`, set by the tracker's `List`.** A new `crew.Kind` type has `KindIssue` as its zero value and `KindPullRequest`, so the fake tracker and every existing test keep listing issues. The GitHub adapter sets `KindPullRequest` on the items it builds from the `pullRequests` connection. `crew.Stage` gains `Takes crew.Kind`, filled from KTD1. `Move` and every write stay as #89 left them: they still need no kind, so #89's KTD3 and KTD6 hold for writes. Rejected: passing `List` the labels per kind. Items of the wrong kind would never reach the core, so it could not show R6's notice.
- KTD3. **The core takes an item only for a stage of its kind.** `waiting` in `internal/core/update.go` adds the kind to its existing match: one crew state, the stage's label, not blocked. Priority, stage order, age, queues and slots are untouched (R4).
- KTD4. **The notice is a new core event, emitted once per labeling by the core itself.** The model remembers, by item key, the stage label it last gave a notice for. At each listing it emits the event for an item whose pair is new, then keeps only the pairs that listing found. A label removed and added back is therefore missing from at least one listing in between, and gets a new notice (R6). A failed or skipped listing changes nothing. Both outputs render the core's event stream (`internal/ui/lines/lines.go` `Text`, which the TUI's recent events reuse), so once in the core is once in each output. Rejected: reusing `IssueSkipped`, which fires at every poll and means something else, and deduplicating in each renderer, which would need the same memory in two places.
- KTD5. **The kind notice applies only to an item in exactly one crew state.** An item with two crew labels gets the two-label skip alone, whatever their stages' kinds. It is not taken either way, and the two-label skip already asks the boss to remove a label. Once one is removed, the kind check applies to what remains. This keeps one notice per reason and answers the issue's deferred question on two labels.
- KTD6. **Kind is checked before blocked.** A blocked issue carrying the label of a stage that takes pull requests gets the notice: it would not be taken once unblocked either. An item crew holds is never checked, as `skipped` already does for two labels.
- KTD7. **The event names the item, its kind, the label, the stage and the kind the stage takes.** Directional line text: `left #90 alone: it is a pull request, and crew:development:ready is the label of stage development, which takes issues`. The exact wording is the implementer's, kept to one sentence like the other lines.

### High-Level Technical Design

How one listing is handled once this lands (directional):

```text
for each listed item not held by crew:
  two or more crew states  -> IssueSkipped (unchanged, every poll)
  one state that is stage S's label:
    item.Kind != S.Takes   -> remember (key, label); emit the kind notice if that pair was not remembered before
    item.Kind == S.Takes   -> candidate unless blocked (unchanged ordering and slots)
forget remembered pairs this listing did not find
```

### Assumptions

- The issue's deferred questions are resolved here as planning decisions: the key's name and values (KTD1), how the core learns the kind (KTD2), the two-label case (KTD5), and showing the notice once (KTD4).
- "Shows once" holds within one crew process. A restart shows each standing notice once more (Scope Boundaries).

### Risks

| Risk | Mitigation |
| --- | --- |
| A pull request that already carries an issue stage's mirrored label shows a notice after this lands, where #89 would have taken it. | Intended (R7, AE5). The guide's "A pull request as the work" says so. This repository's stages all take issues and none of them takes the labels the mirror copies today. |
| `internal/core/update.go` is near revive's file-length limit (500 non-comment lines, the same as Codacy's Lizard). | Put the kind check and its memory in a new `internal/core/kind.go`, as `pullrequest.go` and `resume.go` already split the core. |

---

## Implementation Units

### U1. The kind in the domain

- **Goal:** `crew.Kind`, `crew.Issue.Kind` and `crew.Stage.Takes` exist, with issues as the zero value.
- **Requirements:** R1, R2; KTD2.
- **Dependencies:** none.
- **Files:** `internal/crew/issue.go`, `internal/crew/workflow.go`, `internal/crew/issue_test.go` (new) or the nearest existing `internal/crew` test file.
- **Approach:** `Kind` is an int type with `KindIssue` (zero) and `KindPullRequest`, and a `String` that renderers use (`issue`, `pull request`), returning the package's unknown-name convention for other values as `core` does. `Issue.Clone` needs no change. Doc comments say a tracker that knows no pull requests leaves `Kind` zero.
- **Patterns to follow:** `crew.Issue.Blocked` and its comment; `core.Claim.String`.
- **Test scenarios:**
  - `KindIssue.String()` and `KindPullRequest.String()` name each kind, and an out-of-range value names neither.
  - The zero `crew.Issue` and `crew.Stage` are of kind issue.
- **Verification:** the package builds and its tests pass, and no other package changes behaviour yet.

### U2. The `takes` key

- **Goal:** a stage's `takes` decodes into `Stage.Takes`, and a bad value stops crew with a config error.
- **Requirements:** R1, R2, R3; KTD1; AE4.
- **Dependencies:** U1.
- **Files:** `internal/config/validate.go`, `internal/config/config_workflow_test.go`, `internal/config/config_reject_workflow_test.go`, `internal/app/app_startup_test.go` (only if no existing test already proves a workflow config error exits 2 before any poll).
- **Approach:**
  1. `stageDoc` gains `Takes located[string] yaml:"takes"`, and `stageShape` lists `takes` among the optional keys.
  2. `parseStage` fills `p.Takes` through a small helper in the shape of `stageQueue`: line 0 gives `KindIssue`, `issues` and `pull_requests` give their kinds, anything else is the KTD1 `keyError`.
- **Patterns to follow:** `stageQueue` in `internal/config/queue.go`; the reject tables in `config_reject_workflow_test.go`.
- **Test scenarios:**
  - A stage without `takes` loads with `Takes == KindIssue`.
  - `takes: issues` loads as `KindIssue`, and `takes: pull_requests` as `KindPullRequest`.
  - `takes:` with no value loads as `KindIssue`.
  - Covers AE4. `takes: prs` is rejected with `workflow[0].takes (line N)` and a message naming both accepted values.
  - `takes: ""`, `takes: Issues` and `takes: [issues, pull_requests]` are each rejected at `workflow[0].takes` with its line.
  - A rejected `takes` is reported together with the stage's other errors, as the other keys are.
- **Verification:** config tests pass; an invalid `takes` exits with code 2 before the first poll through the existing config-error path.

### U3. The GitHub listing marks pull requests

- **Goal:** `List` returns each pull request with `Kind == KindPullRequest` and each issue with `KindIssue`.
- **Requirements:** R4, R7; KTD2; AE5 (adapter side).
- **Dependencies:** U1.
- **Files:** `internal/adapter/github/tracker.go`, `internal/adapter/github/list_test.go`.
- **Approach:** set the kind where `List` appends pull request nodes; the query, the author filter and the sort stay as they are. Update `List`'s doc comment to say each item carries its kind.
- **Patterns to follow:** how `List` sets `Blocked` and `Priority` on issue nodes only.
- **Test scenarios:**
  - A reply with issue #42 and the login's pull request #90 lists #42 as an issue and #90 as a pull request.
  - Covers AE5. A pull request #90 carrying the label the mirror copied from #42 is listed with that state and `KindPullRequest`.
- **Verification:** adapter tests pass, and the scripted gh arguments are unchanged.

### U4. The core takes by kind and gives the notice once

- **Goal:** the core takes an item only for a stage of its kind, and emits the kind notice once per labeling.
- **Requirements:** R4, R5, R6, R7; KTD3, KTD4, KTD5, KTD6, KTD7; AE1, AE2, AE3, AE5.
- **Dependencies:** U1.
- **Files:** `internal/core/kind.go` (new), `internal/core/update.go`, `internal/core/model.go`, `internal/core/event.go`, `internal/core/kind_test.go` (new).
- **Approach:**
  1. `event.go` gains the notice event, its `Time` and `event` methods, with the fields KTD7 names.
  2. `model.go` gains the remembered pairs, a map from item key to stage label, made in `New`.
  3. `listed` calls a new step in `kind.go`, after `skipped` and before `waiting`, that applies KTD4 to KTD6.
  4. `waiting` adds the kind match (KTD3).
- **Patterns to follow:** `skipped` and `IssueSkipped` in `update.go`; the table tests in `internal/core/skip_test.go` and the driver in `driver_test.go`.
- **Test scenarios:**
  - Covers AE1. A stage with the default kind and a listed pull request carrying its label: nothing taken, no command, one notice naming the item, the label and that the stage takes issues.
  - Covers AE2. A stage with `Takes: KindPullRequest` and a listed pull request carrying its label: taken as an issue would be, with `IssueTaken` and the take move.
  - Covers AE3. The same stage and an issue carrying its label: no command for the issue, one notice, and no notice at the next listing that still carries it.
  - A notice shows again once a listing without the item, or with it under another crew state, comes between two listings that carry the wrong-kind label.
  - The item moving from one wrong-kind stage label to another gets a new notice.
  - A failed listing (`ListingFailed`) and a skipped poll leave the remembered pairs as they were, so the next successful listing does not repeat the notice.
  - Covers AE5. An item of kind pull request in the label of a stage that takes issues, alongside the issue that stage takes: the issue is taken, the pull request gets the notice.
  - An item with two crew labels, one a pull-request stage's label, gets `IssueSkipped` and no kind notice (KTD5).
  - A blocked issue in the label of a pull-request stage gets the notice (KTD6).
  - Mixed listing order: wrong-kind items take no slot and do not hold back right-kind items of lower priority.
- **Verification:** core tests pass with no goroutine, clock or I/O, and every existing core test passes unchanged.

### U5. The notice in both outputs

- **Goal:** the line output and the TUI show the notice as one sentence.
- **Requirements:** R6; KTD7.
- **Dependencies:** U4.
- **Files:** `internal/ui/lines/lines.go`, `internal/ui/lines/lines_test.go`, `internal/engine/skip_test.go` or a new `internal/engine/kind_test.go`.
- **Approach:** `issueText` gains a case for the event, with a helper in the style of `issueSkipped`. The TUI needs no change: its recent events use `lines.Text`. An engine test under `synctest`, with the fake tracker and a stage that takes issues, proves the event reaches the subscription once across several polls.
- **Patterns to follow:** `issueSkipped` and its row in `lines_test.go`; the `rig` helpers in `internal/engine/skip_test.go`.
- **Test scenarios:**
  - The line for a pull request in an issue stage's label names the item, the label, the stage and `issues`.
  - The line for an issue in a pull-request stage's label names `pull requests`.
  - Engine: a fake pull request #90 (`Kind: KindPullRequest`) labeled with the issue stage's label, three polls: exactly one notice event published, and no session started for #90.
- **Verification:** lines and engine tests pass, and the TUI golden files are unchanged.

### U6. Docs and vocabulary

- **Goal:** the guide, the glossary and the architecture page describe the key, the take rule and the notice.
- **Requirements:** R8, R9.
- **Dependencies:** U2, U4, U5.
- **Files:** `docs/guide/crew.mdx`, `CONCEPTS.md`, `docs/develop/architecture.mdx`, `.agents/skills/cw-create-issue/SKILL.md`, `.agents/skills/cw-update-issue-plan/SKILL.md`, `docs/guide/create-issue.mdx`.
- **Approach:**
  1. `docs/guide/crew.mdx`: add `takes` to the stage keys table with its default and values. Reword the opening paragraph's "A stage takes a pull request you labeled as it takes an issue". Add a poll step after the two-label skip for an item of the other kind, with an example notice line, saying it shows once while the label stays and again after a restart. In "The pull requests" and "A pull request as the work", say a stage takes a pull request only with `takes: pull_requests`, and that the mirrored label gets a pull request taken only by such a stage, otherwise the notice. Reword "Prompts" ("or over the pull request when the stage took one") to the stage's kind.
  2. `CONCEPTS.md`: "Stage" takes the items of its kind that carry its label. In "Mirrored label", replace "A pull request that carries a stage's label, mirrored or not, is taken by that stage" with the kind rule.
  3. `docs/develop/architecture.mdx`: add the event to the events list, and add `crew.Issue.Kind` and the kind match to the paragraph on what the core takes, next to `crew.Issue.Blocked`.
  4. `/cw-create-issue`, `/cw-update-issue-plan` and `docs/guide/create-issue.mdx`: a stage is an issue type only when it takes issues. A stage with `takes: pull_requests` is left out of the types `/cw-create-issue` offers and out of the stage labels `/cw-update-issue-plan` accepts, since an issue under it is left alone (R5). Reword "A stage takes every open issue carrying its `label`" to the kind rule. Both skills run in other repositories through `~/.claude/skills/`, so this is their only guard.
- **Test expectation:** none -- docs only; `pnpm docs:check` covers links and MDX.
- **Verification:** `pnpm docs:check` passes, and no guide text still says a stage takes any pull request carrying its label.

---

## Verification Contract

| Gate | Command | Applies to |
| --- | --- | --- |
| Tests | `go test -race ./...` | U1-U5 |
| Format | `gofmt -l cmd internal tools` prints nothing | U1-U5 |
| Vet | `go vet ./...` | U1-U5 |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | U1-U5 |
| Coverage | `go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...`, then `go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml` (total at least 90%) and `git diff -U0 origin/main...HEAD \| go run ./tools/diffcover -profile coverage.out` (changed lines at least 90%) | U1-U5 |
| Vulnerabilities | `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` | whole change |
| Docs | `pnpm docs:check` | U6 |

---

## Definition of Done

- AE1 to AE5 each have a named test (U2 for AE4, U3 and U4 for AE5, U4 for AE1 to AE3), and they pass.
- Every gate in the Verification Contract passes with zero findings.
- `.crew/config.yaml` is unchanged.
- The guide, `CONCEPTS.md`, `docs/develop/architecture.mdx`, `docs/guide/create-issue.mdx` and the two issue skills match the behaviour, per U6.
- No abandoned-attempt code, debug output or unused helper is left in the diff.
