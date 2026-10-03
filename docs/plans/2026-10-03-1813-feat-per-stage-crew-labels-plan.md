---
title: Per-stage crew labels for this repository - Plan
type: feat
date: 2026-10-03
topic: per-stage-crew-labels
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Per-stage crew labels for this repository - Plan

## Goal Capsule

- **Objective:** in this repository, an issue's crew label says which stage it is in and where it stands in that stage, with the same five states in every stage.
- **Means:** rename, delete and create labels on GitHub, and update `.crew/config.yaml`, the issue templates and the guide page that cites this repository. The repository files change in the pull request; the GitHub label changes are a command list the boss runs (KTD1).
- **Product authority:** the boss, through the brainstorm of #83, whose Product Contract is the body of issue #83 and is carried here unchanged. The change is this repository's own setup, not crew's defaults, code or general docs.
- **Stop conditions:** stop if crew's config check rejects the new labels, or if a change would touch crew's code, tests or `docs/guide/crew.mdx`.
- **Who finishes:** the pull request ships U1 and U2. The boss runs the Appendix's label migration (U3) when they choose, relative to the merge, and restarts crew.
- **Open blockers:** none.

---

## Product Contract

Product Contract preservation: Product Contract unchanged (copied from the body of issue #83).

### Summary

Each of this repository's six stages becomes a micro workflow with five labels: `crew:<stage>:ready`, `crew:<stage>:in progress`, `crew:<stage>:waiting review`, `crew:<stage>:done` and `crew:<stage>:failed`. GitHub holds all 30 labels, and the config names only the ones a stage, the extra label or a prompt uses today. The old labels are renamed when one new label replaces them, and deleted otherwise.

### Problem Frame

The labels in `.crew/config.yaml` mix two patterns. `crew:brainstorm:done` names a stage and a state, while `crew:ready for development`, `crew:waiting brainstorm`, `crew:in progress`, `crew:waiting review` and `crew:failed` do not. Every stage shares `crew:in progress`, `crew:waiting review` and `crew:failed`, so an issue carrying one of them does not say which stage it is in.

### Key Decisions

- **Every stage gets its own set of labels, end states included.** (session-settled: user-directed — chosen over per-stage entry and in-progress labels with a shared `crew:waiting review` and `crew:failed`: the stage should show on every label.) Governs R1, R6.
- **The states are `ready`, `in progress`, `waiting review`, `done` and `failed`, the same in every stage.** (session-settled: user-directed — chosen over adding a `paused` state, which the boss dropped.) Governs R1.
- **GitHub gets every label now, the config names only the labels in use.** The unused labels wait on GitHub for when the workflow uses them. (session-settled: user-directed — chosen over creating only the labels the config uses.) Governs R1, R10.
- **Old labels are renamed when one new label replaces them, and deleted otherwise.** Renaming keeps the label on its issues. (session-settled: user-directed — chosen over deleting every old label and creating new ones.) Governs R2, R3, R4.
- **The brainstorm hands the issue straight to triage.** `prompts.update_issue_plan` moves it to `crew:triage:ready`, so triage still starts on its own after a brainstorm, as it does today with `crew:brainstorm:done`. (session-settled: user-approved — chosen over ending the brainstorm at `crew:brainstorm:done`, which no stage would take.) Governs R9.
- **Who moves an issue between states or stages beyond today's transitions is not decided here.** The workflow can stay incomplete: `done` has no writer yet, nor do brainstorm's `waiting review` and `failed`.

### Requirements

**Labels on GitHub**

- R1. GitHub has these 30 labels: for each stage `brainstorm`, `triage`, `development`, `fix`, `ci audit` and `knowledge base`, the labels `crew:<stage>:ready`, `crew:<stage>:in progress`, `crew:<stage>:waiting review`, `crew:<stage>:done` and `crew:<stage>:failed`. The `audit ci` stage's labels say `ci audit`, as its current label does.
- R2. Each old label with a single replacement is renamed, so the issues carrying it carry the new name:

  | Old label | New label |
  |---|---|
  | `crew:waiting brainstorm` | `crew:brainstorm:ready` |
  | `crew:ready for triage` | `crew:triage:ready` |
  | `crew:ready for development` | `crew:development:ready` |
  | `crew:ready for fix` | `crew:fix:ready` |
  | `crew:ready for ci audit` | `crew:ci audit:ready` |
  | `crew:ready for knowledge base` | `crew:knowledge base:ready` |

- R3. `crew:brainstorm:done` stays as it is.
- R4. `crew:in progress`, `crew:waiting review` and `crew:failed` are deleted.
- R5. Afterwards, the only labels on GitHub that start with `crew:` are the 30 of R1.

**The config**

- R6. Each stage in `.crew/config.yaml` takes, moves and ends on its own labels:

  | Stage | `label` | `moves_to` | `on_success` | `on_failure` |
  |---|---|---|---|---|
  | triage | `crew:triage:ready` | `crew:triage:in progress` | `crew:triage:waiting review` | `crew:triage:failed` |
  | development | `crew:development:ready` | `crew:development:in progress` | `crew:development:waiting review` | `crew:development:failed` |
  | fix | `crew:fix:ready` | `crew:fix:in progress` | `crew:fix:waiting review` | `crew:fix:failed` |
  | audit ci | `crew:ci audit:ready` | `crew:ci audit:in progress` | `crew:ci audit:waiting review` | `crew:ci audit:failed` |
  | knowledge base | `crew:knowledge base:ready` | `crew:knowledge base:in progress` | `crew:knowledge base:waiting review` | `crew:knowledge base:failed` |

- R7. The extra label becomes `crew:brainstorm:ready`, with its current description and issue template.
- R8. `prompts.brainstorm` goes on only when the issue carries `crew:brainstorm:ready`, and moves it to `crew:brainstorm:in progress`.
- R9. `prompts.update_issue_plan` moves the issue from `crew:brainstorm:in progress` to `crew:triage:ready`.
- R10. The config names no label beyond those in R6 to R9.
- R11. The config's header comment describes the new labels instead of the old ones.

**Templates and docs**

- R12. Each issue template's `labels` carries the `ready` label of its type: `feature.md` `crew:development:ready`, `bug.md` `crew:fix:ready`, `ci-audit.md` `crew:ci audit:ready`, `knowledge-base.md` `crew:knowledge base:ready` and `idea.md` `crew:brainstorm:ready`.
- R13. `docs/guide/create-issue.mdx` cites the new labels wherever it uses this repository as its example.

### Acceptance Examples

- AE1. **Covers R2.** **Given** #80 carries `crew:waiting brainstorm`, **when** the labels change, **then** #80 carries `crew:brainstorm:ready` and nothing else changed on it.
- AE2. **Covers R4.** **Given** #60 carries `crew:in progress`, **when** the labels change, **then** #60 carries no crew label.
- AE3. **Covers R8, R9, R6.** **Given** an issue carries `crew:brainstorm:ready`, **when** the boss runs `/cw-brainstorm` on it, **then** it moves to `crew:brainstorm:in progress`; **when** the boss then runs `/cw-update-issue-plan` without a label, **then** it moves to `crew:triage:ready`, and crew's triage stage takes it at its next poll.

### Scope Boundaries

- crew's code, its tests and `docs/guide/crew.mdx` keep their labels: they are generic examples, not this repository's setup.
- `docs/solutions/` and `docs/plans/` keep the old labels as history.
- Moving the open issues whose label was deleted (today #35, #60, #67 and #83, all in `crew:in progress`) is out of scope.
- New transitions, such as who sets `done` or moves an issue from `waiting review` to the next stage, are out of scope.
- Considered and not built: a test that every `crew:` label in `.crew/config.yaml` follows `crew:<stage>:<state>`. A label that drifts from the pattern shows on the issue the first time it is used, and the boss reads those labels every day. A second drift in review would change the call.

### Dependencies / Assumptions

- crew's config check accepts per-stage labels. It rejects only two stages taking the same label, a stage ending on its own label, and a `moves_to` that is another stage's label (`internal/config/validate.go`, `checkGraph`). R6 breaks none of these.
- crew reads `.crew/config.yaml` only at start. The boss decides when the GitHub changes (R1 to R5) run relative to the merge, and restarts crew.

### Sources / Research

- `.crew/config.yaml` and `.github/ISSUE_TEMPLATE/*.md` hold every label this change touches. `docs/guide/create-issue.mdx` cites them at the `/cw-update-issue-plan crew:ready for development` example and in the paragraph on this repository's brainstorm prompt.
- Labels on GitHub on 2026-10-03: `crew:ready for development`, `crew:ready for fix`, `crew:ready for knowledge base`, `crew:ready for triage`, `crew:ready for ci audit`, `crew:in progress`, `crew:waiting review`, `crew:failed`, `crew:brainstorm:done` and `crew:waiting brainstorm`.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The GitHub label changes ship as a command list in this plan's Appendix and the pull request body; the implementation does not run them.** The boss owns their timing (Dependencies / Assumptions), and crew is running on #83 while it is implemented: renaming or deleting `crew:in progress` or `crew:waiting review` now would break the move crew makes when this session ends. A committed script was rejected because the migration runs once, and this plan file already keeps the list in the repository. Covers R1 to R5.
- KTD2. **Labels take one colour per state, reusing today's colours:** `ready` `1D76DB`, `in progress` `FBCA04`, `waiting review` `5319E7`, `done` `0E8A16`, `failed` `B60205`. Each state then looks the same in every stage. The renames set the colour and description too, so `crew:waiting brainstorm` (purple, no description) becomes a blue `ready` label like the others. `crew:brainstorm:done` keeps its colour and description (R3).
- KTD3. **The migration runs with crew stopped, after the merge, before anything uses the new config.** At every start, the GitHub tracker's `Prepare` (`internal/adapter/github/tracker.go`) runs `gh label create` for each label the config names that GitHub lacks. `/cw-create-issue` and `/cw-update-issue-plan` also create a missing label. So a crew started on the old config would recreate `crew:in progress`, `crew:waiting review` and `crew:failed` after their deletion, which breaks R5. And a crew started on the new config before the renames, or either skill used then, would create bare `crew:<stage>:ready` labels, on which the renames of step 1 fail, which breaks R2. The order is:
  1. Wait until crew runs no session, then stop it. Stopping crew moves a running session's issue to `on_failure` (`docs/guide/crew.mdx`).
  2. Merge.
  3. Run the list.
  4. Restart crew on the new config.

  Covers R2, R5.
- KTD4. **The existing repository test is the proof for the config and templates.** `TestTheRepositorysIssueTemplatesMatchItsConfig` in `internal/config/config_templates_test.go` loads the repository's own config through `config.Load`, so it runs the graph check, and requires each template's one label to be the label of a type that names it. `feature.md` is named by both triage and development, so `crew:development:ready` satisfies it. No new test is needed.

### Assumptions

- `docs/guide/create-issue.mdx:80` says a `moves_to` label that `/cw-brainstorm` set does not stop `/cw-update-issue-plan`. That sentence describes the skill in general, not this repository, so it stays as it is.
- The labels' descriptions in the Appendix are this plan's wording. GitHub shows them only in the label picker.
- The issue numbers in AE1, AE2 and Scope Boundaries are as the brainstorm saw them. On 2026-10-03, at planning time, `crew:waiting brainstorm` is on #84, #52, #49, #48, #45, #37, #34, #33, #32, #26 and #25. `crew:in progress` is on open #67, #80 and #83: #80 and #83 are crew sessions whose end label step 2 deletes. `crew:waiting review` is only on closed issues, among them #35 and #60. AE1 and AE2 hold for any issue carrying those labels, such as #84 for AE1 and #67 for AE2.

### Open Questions

- Deferred, not blocking: `/cw-update-issue-plan <label>` removes only the crew labels it finds in the config (a stage's four labels and the extras). `crew:brainstorm:in progress` appears only in `prompts`, so running the skill with an explicit label after `/cw-brainstorm` leaves it on the issue next to the new label. Before this change the brainstorm used `crew:in progress`, a stage's `moves_to`, which the skill did remove. Without a label, the default flow of AE3, the prompt removes it. The fix belongs either to the skill (also treat the labels the prompts name as crew's) or to the boss's habit, and R10 keeps it out of the config. This goes to the pull request body for the boss.

---

## Implementation Units

### U1. Config and issue templates on per-stage labels

- **Goal:** `.crew/config.yaml` and the five issue templates name only the new labels.
- **Requirements:** R6, R7, R8, R9, R10, R11, R12; AE3 for the prompts.
- **Dependencies:** none.
- **Files:**
  - `.crew/config.yaml`
  - `.github/ISSUE_TEMPLATE/feature.md`, `bug.md`, `ci-audit.md`, `knowledge-base.md`, `idea.md`
  - Test: `internal/config/config_templates_test.go` (unchanged; it must pass)
- **Approach:**
  1. Each stage's `label`, `moves_to`, `on_success` and `on_failure` take the values in R6's table.
  2. The extra label becomes `crew:brainstorm:ready` and keeps its description and `idea.md` (R7).
  3. `prompts.brainstorm` checks for `crew:brainstorm:ready`, and its `gh issue edit` swaps it for `crew:brainstorm:in progress` (R8). Its text keeps its current structure, with only the label names changed.
  4. `prompts.update_issue_plan` swaps `crew:brainstorm:in progress` for `crew:triage:ready` and says triage takes it at its next poll (R9).
  5. Rewrite the header comment (R11). Each stage takes `crew:<stage>:ready`, moves the issue to `crew:<stage>:in progress`, and ends in `crew:<stage>:waiting review` or `crew:<stage>:failed`. The extra `crew:brainstorm:ready` parks an idea. The brainstorm prompts hand a brainstormed feature to `crew:triage:ready`. GitHub also holds each stage's `done` and the brainstorm's unused states, which the config does not name yet. Keep the comment's other sentences (templates, prompts, checks, queues).
  6. Each template's `labels` takes its `ready` label from R12.
  7. Leave the triage, audit and other action prompts alone. They speak of labels that start with `crew:` in general, which stays true.
- **Patterns to follow:** the current file's quoting (`"crew:..."` in double quotes) and the prose style of its header comment.
- **Test scenarios:**
  - Covers AE3 (static half). `config.Load` on the repository root succeeds with the new labels, so the graph check accepts them.
  - Every template's single label is the label of a type that names it: `feature.md` gives `crew:development:ready`, which is development's label, and `idea.md` gives the extra's `crew:brainstorm:ready`.
  - No string from the old label set (`crew:waiting brainstorm`, `crew:ready for`, `crew:brainstorm:done`, `crew:in progress`, `crew:waiting review`, `crew:failed`) remains in `.crew/config.yaml` or `.github/ISSUE_TEMPLATE/` (search check, not a Go test).
- **Verification:** the config package's tests pass, and a search of the config and templates finds only labels from R6 to R9.

### U2. Guide page cites the new labels

- **Goal:** `docs/guide/create-issue.mdx` uses this repository's new labels wherever it cites this repository.
- **Requirements:** R13.
- **Dependencies:** U1.
- **Files:** `docs/guide/create-issue.mdx`
- **Approach:**
  1. The worked-example paragraph names the extra `crew:brainstorm:ready` instead of `crew:waiting brainstorm`.
  2. The `/cw-update-issue-plan` example passes `crew:development:ready`.
  3. The brainstorm paragraph says the prompt goes on only with `crew:brainstorm:ready`, moves the issue to `crew:brainstorm:in progress`, and that `prompts.update_issue_plan` moves it to `crew:triage:ready`, which hands it to triage. It also says that until then the issue stays in `crew:brainstorm:in progress`, which no stage takes.
  4. Keep the labels in backticks (MDX treats `{` and `<` as JSX).
- **Test expectation:** none -- documentation only; `pnpm docs:check` covers links and MDX.
- **Verification:** `pnpm docs:check` passes, and the page no longer names an old label.

### U3. Label migration for the boss

- **Goal:** the boss has an exact, reviewed command list that brings GitHub's `crew:` labels to R1 to R5.
- **Requirements:** R1, R2, R3, R4, R5; AE1, AE2. KTD1, KTD2, KTD3.
- **Dependencies:** none for the list. Running it waits for the merge (KTD3).
- **Files:** none in the repository beyond this plan's Appendix. The pull request body carries the Appendix's list.
- **Approach:** copy the Appendix's list into the pull request body under its own heading, with KTD3's order. Do not run any `gh label` command that writes.
- **Test expectation:** none -- the list runs outside the repository, by the boss.
- **Verification:** a read-only `gh label list` taken while implementing shows the ten labels the Sources name, and the list touches each one: six renames, three deletes, and `crew:brainstorm:done` left alone. The list creates 23 labels, so 7 kept plus 23 new gives the 30 of R1.

---

## Verification Contract

| Check | Command | Proves |
|---|---|---|
| Config and templates | `go test -race ./internal/config` | U1: `config.Load` accepts the repository's config; templates match their types (KTD4) |
| Whole suite | `go test -race ./...` | nothing else reads the repository's config or templates |
| Old labels gone | a search for the old label strings in `.crew/config.yaml`, `.github/ISSUE_TEMPLATE/` and `docs/guide/create-issue.mdx` | U1, U2; finds nothing |
| Docs | `pnpm docs:check` | U2: links and MDX |
| Lint | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | no Go changed; runs only if Go files change |

## Definition of Done

- U1 and U2 are committed, with no Go code changed.
- The pull request body carries the Appendix's label migration and the Open Question on `/cw-update-issue-plan <label>`.
- No `gh label` command that writes ran during implementation.
- The checks in the Verification Contract pass.
- No dead-end edits remain in the diff.

---

## Appendix

### Label migration (R1 to R5)

Run in the order of KTD3: wait until crew runs no session and stop it, merge the pull request, run the list before crew or the `cw-` skills use the merged config, then restart crew. Run a second time, each command fails without changing anything: the old name is gone, or the new label already exists. So the list can be rerun after a partial run. One exception: when a rename in step 1 fails because its new name already exists while its old name is still there, something created the new label early (KTD3). No issue carries it yet, so delete that new label and rerun the rename.

```sh
# 1. Rename the six labels with one replacement (R2), set to the ready colour (KTD2)
gh label edit "crew:waiting brainstorm"       --name "crew:brainstorm:ready"     --color 1D76DB --description "crew: an idea to brainstorm"
gh label edit "crew:ready for triage"         --name "crew:triage:ready"         --color 1D76DB --description "crew: the triage stage takes it"
gh label edit "crew:ready for development"    --name "crew:development:ready"    --color 1D76DB --description "crew: the development stage takes it"
gh label edit "crew:ready for fix"            --name "crew:fix:ready"            --color 1D76DB --description "crew: the fix stage takes it"
gh label edit "crew:ready for ci audit"       --name "crew:ci audit:ready"       --color 1D76DB --description "crew: the audit ci stage takes it"
gh label edit "crew:ready for knowledge base" --name "crew:knowledge base:ready" --color 1D76DB --description "crew: the knowledge base stage takes it"

# 2. Delete the three shared labels (R4); their issues lose them
gh label delete "crew:in progress" --yes
gh label delete "crew:waiting review" --yes
gh label delete "crew:failed" --yes

# 3. Create the 23 missing labels (R1); crew:brainstorm:done already exists (R3)
for stage in brainstorm triage development fix "ci audit" "knowledge base"; do
  gh label create "crew:$stage:in progress"    --color FBCA04 --description "crew: $stage is running on it"
  gh label create "crew:$stage:waiting review" --color 5319E7 --description "crew: $stage is done, waits for the boss"
  if [ "$stage" != brainstorm ]; then
    gh label create "crew:$stage:done"         --color 0E8A16 --description "crew: $stage is done"
  fi
  gh label create "crew:$stage:failed"         --color B60205 --description "crew: $stage failed, see the comment"
done

# 4. Check (R5): exactly the 30 labels of R1
gh label list --limit 200 --json name --jq '[.[].name | select(startswith("crew:"))] | length, sort[]'

# 5. No stage takes crew:brainstorm:done any more: an open issue still on it needs moving to crew:triage:ready by hand
gh issue list --state open --label "crew:brainstorm:done" --json number --jq '.[].number'
```
