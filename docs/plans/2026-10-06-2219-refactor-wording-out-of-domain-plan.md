---
title: Move display wording out of crew's domain into the renderers - Plan
type: refactor
date: 2026-10-06
topic: wording-out-of-domain
artifact_contract: ce-unified-plan/v1
product_contract_source: GitHub issue #239 (part 2 of 6 of #237)
origin: GitHub issue #239
execution: code
---

# Move display wording out of crew's domain into the renderers - Plan

## Goal Capsule

- **Objective:** the database work that follows can store and reload crew's domain types without carrying screen or tracker wording, and nobody using crew sees a difference: the live view, `--plain` and the status comment read word for word as today.
- **Means:** `internal/ui/lines` holds the spend and kind wording the TUI and `--plain` share, the GitHub adapter words its usage line itself, and `internal/crew` loses every formatting method (KTD17, KTD1 to KTD3).
- **Product authority:** the boss, through the brainstorm and planning session of #237. The Product Contract wins on behaviour; the KTDs win on mechanism.
- **Stop conditions:** stop and report when a unit cannot keep the build, the tests, the TUI golden files or the acceptance suite green without changing what users see (R20), or when KTD17 proves unworkable.
- **Execution profile:** one branch, units in order U1, U2, U3 (expand, switch, contract), one pull request whose body carries `Closes #239`. #220 and the other parts of #237 are not part of this work.
- **Part:** this issue is part 2 of 6 of #237. It ships alone because it moves today's wording unchanged and needs no other part.

---

## Product Contract

Product Contract preservation: unchanged from issue #239, with R20 carried from #237 as the issue cites it.

### Summary

`internal/crew` stops wording spend, tokens, kinds and pull requests. `internal/ui/lines` words spend and kinds for the TUI and `--plain`, and the GitHub adapter words the status comment's usage line, word for word as today.

### Problem Frame

`internal/crew` is meant to be crew's domain, but it formats for the screen and the tracker: `Spend.String`, `formatTokens`, `formatCost`, `Kind.String` "for renderers" and `PullRequest.String`. A database comes right after #237, and crew may later run as a server over many repositories. A domain that carries one view's wording ties every later store, adapter and renderer to it.

### Key Decisions

- KTD17. **Wording moves to the renderers.** `Spend.String`, `formatTokens`, `formatCost`, `Kind.String` and `PullRequest.String` leave `internal/crew`. The GitHub adapter words the status comment's usage itself, and `internal/ui/lines` holds the wording the TUI and `--plain` share, word for word as today. (session-settled: user-approved — chosen over keeping the formatting methods on the domain types: the domain must format nothing for a screen or tracker so a later store and server reuse it unchanged.) Governs R17, R20.

### Requirements

**Presentation and boundaries**

- R17. `internal/crew` formats nothing for a screen or a tracker. Each renderer words spend, tokens, kinds and states itself.

**No visible change**

- R20. Nobody using crew sees a difference: the TUI, `--plain` and the status comment print the same text as before, and the acceptance suite and the TUI golden files pass unchanged. (session-settled: user-approved — chosen over rewording output while moving it: this is a pure refactor step of #237.)

### Success Criteria

- `internal/crew` has no `String` method and imports neither `strconv` nor `strings` for formatting; its only `fmt` use is the error wrapping in `Action.Render`.
- The acceptance suite and the TUI golden files pass unchanged.

### Scope Boundaries

- Anything #220 brings, and the other parts of #237: typed identities, session and check text types, the outbox, validated definitions, the rule-run aggregate.
- `Rule.Notify` (a live-view setting on the domain) stays: removing it belongs to another part of #237.
- The "set when" field comments in `internal/crew` (`Usage`, `Status`, `PullRequest`) stay: #237's success criterion on them is met by the parts that replace those fields with typed ones.
- `core.CallKind.String`, `core.Result.String` and `core.ActionPhase.String` live in `internal/core`, not `internal/crew`, and stay.
- Considered and not built: one shared wording package that both `internal/ui/lines` and the GitHub adapter import. KTD17 makes the adapter word its own comment, and the layering forbids adapters from importing UI.

### Dependencies / Assumptions

- No caller outside `internal/crew`'s tests calls `PullRequest.String`: the status comment words the pull request in `internal/adapter/github/status.go` `usage`, and no renderer shows it. It is deleted, not moved.
- `--plain` prints no spend today. `internal/ui/lines` still owns the spend wording, because it is the package the TUI and `--plain` share for wording (KTD17).

### Sources / Research

- `internal/crew/usage.go`: `Spend.String`, `partial`, `formatCost`, `formatTokens`, `compact`, `thousand`, `million`, `PullRequest.String`. `internal/crew/issue.go`: `Kind.String`.
- Callers: `internal/ui/tui/band.go` `spendParts` (used by `header.go` and `bots.go`), `internal/ui/tui/detail.go` (the popup's kind), `internal/ui/lines/lines.go` `issueOfOtherKind` (prints `e.Kind` through `%s`, so it calls `Kind.String` implicitly), `internal/adapter/github/status.go` `usage`.
- Test callers outside `internal/crew`: `internal/core/usage_test.go` (three handled-entry spends) and `internal/adapter/codex/events_test.go` `TestCodexUsageMarksASumWithAClaudeCostPartial` (AE5 of the Codex harness plan) compare `Spend.String` with wording. Neither package may import `internal/ui` (depguard).
- `internal/adapter/github/status_render_test.go`: pins three usage lines of the status comment.
- `.codacy.yaml`: Codacy's duplication engine flags clones of 100 tokens or more in non-test Go files.
- `docs/solutions/tooling-decisions/codacy-lizard-and-golangci-lint-limits.md`: files of 500 non-comment lines; `internal/adapter/github/status.go` is already 602 lines.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **`internal/ui/lines` exports the wording as functions over domain values: `SpendParts` (the cost part and the tokens part, or one part when neither was reported, nothing when no session ended) and a kind name.** The TUI already imports `lines` for `Plural` and `Text`, and it already needs the spend as parts (the header and the bots section drop the tokens before the cost when narrow). Returning parts replaces `spendParts`' split of a joined string on ", ". Governs R17, R20 (session-settled: user-approved, inherited from KTD17).
- KTD2. **The GitHub adapter words its usage line in a new `internal/adapter/github/usage.go`, which takes `usage` out of `status.go`.** The adapter cannot import `internal/ui` (depguard), and `status.go` is already over Lizard's 500-line file limit, so the moved code must not grow it. The adapter's wording is its own code, shaped for the sentence it writes, not a copy of `lines`' code. Governs R17, R20 (session-settled: user-approved, inherited from KTD17).
- KTD3. **Remove the methods outright; no deprecation shim.** `internal/crew` is internal, every caller is in this repository, and `go vet`'s printf check flags any `%s` left on a `crew.Kind` once `String` is gone. Governs R17.

### Assumptions

- Both copies of the spend wording (lines and the GitHub adapter) stay below Codacy's 100-token duplication threshold when each is written for its own caller. If Codacy still reports a clone, restructure one side; do not add a shared package (Scope Boundaries).

### Sequencing

U1 and U2 add the new wording while `internal/crew`'s methods still exist, and switch every caller. U3 then deletes the methods, so each unit builds and passes on its own.

---

## Implementation Units

### U1. Spend and kind wording in `internal/ui/lines`, used by the TUI and `--plain`

- **Goal:** `internal/ui/lines` words a spend and a kind, and every TUI and `--plain` caller uses it instead of `crew`'s methods.
- **Requirements:** R17, R20 (KTD17, KTD1).
- **Dependencies:** none.
- **Files:**
  - Create `internal/ui/lines/wording.go`, `internal/ui/lines/wording_test.go`.
  - Modify `internal/ui/lines/lines.go` (`issueOfOtherKind`), `internal/ui/tui/band.go` (remove `spendParts`), `internal/ui/tui/header.go`, `internal/ui/tui/bots.go`, `internal/ui/tui/detail.go`.
- **Approach:**
  1. Move the spend wording into `wording.go` as `SpendParts`, with the cost, token and partial helpers private to it. Keep the output of today's `Spend.String`, split on ", ", for every input.
  2. Add the kind name: "issue", "pull request", and "unknown" for any other value.
  3. `issueOfOtherKind` passes the kind name, not the `crew.Kind`, to its format string.
  4. The TUI header and bots section call `lines.SpendParts`; the popup calls the kind name.
- **Patterns to follow:** `lines.Plural`, already exported and used by the TUI; the table tests in `internal/crew/usage_test.go` and `internal/crew/issue_test.go`, which move here.
- **Test scenarios:**
  - A spend of no session gives no parts.
  - One session with cost and tokens gives "$1.20" and "17.3M tokens".
  - A sum where one session has no tokens gives "$4.25" and "17.3M tokens (partial)".
  - A sum where one session has no cost gives "$1.20 (partial)" and "34.6M tokens".
  - A session with tokens and no cost gives "cost not reported" and "17.3M tokens".
  - A session that reported neither gives the single part "cost and tokens not reported".
  - A reported zero gives "$0.00" and "0 tokens".
  - Token counts 0, 950, 1,000, 48,210, 999,949, 17,213,000 and 129,000,000 read "0", "950", "1K", "48.2K", "999.9K", "17.2M" and "129M".
  - Costs 12.4, 0.004, 46.9905864 and 0 read "$12.40", "$0.00", "$46.99" and "$0.00".
  - The kind name of an issue, a pull request and `Kind(7)` is "issue", "pull request" and "unknown".
  - The existing `issueOfOtherKind` line tests in `internal/ui/lines/lines_test.go` still pass unchanged.
- **Verification:** the TUI golden files and the `lines` tests pass without `-update`; no TUI or `lines` file calls `Spend.String` or `Kind.String`.

### U2. The GitHub adapter words its usage line itself

- **Goal:** the status comment's " Usage: …. Pull request: …." sentence is built from the adapter's own wording.
- **Requirements:** R17, R20 (KTD17, KTD2).
- **Dependencies:** none.
- **Files:**
  - Create `internal/adapter/github/usage.go`, `internal/adapter/github/usage_test.go`.
  - Modify `internal/adapter/github/status.go` (move `usage` out).
- **Approach:**
  1. Move `usage` from `status.go` to `usage.go`, unchanged except that it words the spend through the adapter's own helper.
  2. Write that helper for the comment's sentence, giving the same text as today's `Spend.String`.
- **Patterns to follow:** `internal/adapter/github/status_render_test.go`, which pins the usage sentence inside whole comments.
- **Test scenarios:**
  - The three usage lines in `status_render_test.go` stay byte for byte as they are.
  - A spend with both values partial words as "$4.25 (partial), 34.6M tokens (partial)".
  - A spend with tokens and no cost words as "cost not reported, 17.3M tokens".
  - A spend with cost and no tokens words as "$1.20, tokens not reported".
  - A status with no session gives no usage sentence.
- **Verification:** the adapter's tests pass, `status.go` is shorter, not longer, and no adapter file calls `Spend.String`.

### U3. `internal/crew` formats nothing

- **Goal:** the domain package loses every formatting method and helper.
- **Requirements:** R17 (KTD17, KTD3).
- **Dependencies:** U1, U2.
- **Files:**
  - Modify `internal/crew/usage.go`, `internal/crew/issue.go`, `internal/crew/usage_test.go`, `internal/crew/issue_test.go`.
  - Modify `internal/core/usage_test.go`, `internal/adapter/codex/events_test.go`.
- **Approach:**
  1. Delete `Spend.String`, `partial`, `formatCost`, `formatTokens`, `compact`, `thousand`, `million`, `Kind.String` and `PullRequest.String`, and the imports they alone used.
  2. Rewrite the doc comments that name a renderer ("for the live view and the status comment", "for renderers") to describe the value, not who shows it.
  3. `TestSpendSumsSessions` checks the summed fields (`Sessions`, `Cost`, `WithCost`, `Tokens`, `WithTokens`) instead of wording; the wording cases now live in U1's tests. Delete `TestKindString`, `TestFormatTokens`, `TestFormatCost` and `TestPullRequestString`.
  4. The tests in `internal/core/usage_test.go` and `TestCodexUsageMarksASumWithAClaudeCostPartial` assert the summed spend's fields instead of its wording, because `internal/core` and the Codex adapter cannot import `internal/ui/lines`. For the Codex test: `Sessions` 2, `WithCost` 1, `Cost` 3.10, `WithTokens` 2, and tokens equal to the Claude session's plus the Codex fixture's.
- **Patterns to follow:** the rest of `internal/crew`, which has no `String` methods.
- **Test scenarios:**
  - Summing two sessions where one has no cost gives `Sessions` 2, `WithCost` 1, `WithTokens` 2 and the summed tokens.
  - Adding a spend of no session leaves the sum unchanged.
  - A usage that reported nothing gives a spend of one session with nothing counted.
  - The zero `Issue` and `Rule` are of kind issue (existing test, unchanged).
- **Verification:** `go vet ./...` reports no printf misuse of `crew.Kind`, a search of `internal/crew` finds no `func (…) String()`, and no file in the repository calls `Spend.String`, `Kind.String` or `PullRequest.String`.

---

## Verification Contract

| Gate | Command | Proves |
|---|---|---|
| Build and tests | `go test -race ./...` | every unit, the TUI golden files unchanged (R20) |
| Format | `gofmt -l cmd internal tools acceptance` prints nothing | repository style |
| Vet | `go vet ./...` | no `%s` left on a `crew.Kind` (KTD3) |
| Lint and layering | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run` | depguard keeps the adapter off `internal/ui` (KTD2) |
| Coverage | the coverage profile, `go-test-coverage` (total ≥ 90%) and `tools/diffcover` (changed lines ≥ 90%) as `AGENTS.md` lists them | the new wording code is tested |
| Acceptance | `go -C acceptance run ./cmd/acceptance -count=1` | the binary's TUI, `--plain` and status comments are unchanged (R20) |
| Codacy | the PR's Codacy check | no duplication or Lizard finding from the two wordings (Assumptions) |

## Definition of Done

- U1 to U3 are merged on one branch, in that order, and every gate above passes.
- No TUI golden file, acceptance snapshot or `status_render_test.go` expectation changed.
- `internal/crew` has no formatting method or helper, and the doc comments of `Usage`, `Spend` and `Kind` no longer name the live view, the status comment or "renderers" as the reason a value exists. `BoardColumn` and `Rule.Notify` keep theirs (Scope Boundaries).
- No dead code from abandoned attempts is left in the diff.
