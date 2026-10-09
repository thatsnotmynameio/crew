---
title: golangci-lint's dupl owns duplication in CI; fix repeated mechanisms, suppress variants and data
date: 2026-10-09
category: tooling-decisions
module: .golangci.yml, .codacy.yaml
problem_type: tooling_decision
component: tooling
severity: medium
applies_when:
  - "dupl reports a pair of declarations and you must choose between refactoring and a //nolint:dupl"
  - "Changing dupl's threshold or its exclusions in .golangci.yml, or the duplication engine in .codacy.yaml"
  - "Reading docs/plans/2026-10-02-2153-feat-codacy-strict-config-plan.md, whose KTD3 still says Codacy owns duplication and dupl is off"
  - "Running pnpm exec codacy-analysis update-config after a comment-only edit to .codacy.yaml"
tags: [dupl, duplication, golangci-lint, codacy, nolint, update-config, stale-plan]
retire_when: "#385 removes Codacy; the .codacy.yaml and update-config parts then no longer apply, and the dupl guidance stands alone"
---

# golangci-lint's dupl owns duplication in CI; fix repeated mechanisms, suppress variants and data

## Context

Until #404, `dupl` was off and Codacy's duplication engine was the only check, as KTD3 of `docs/plans/2026-10-02-2153-feat-codacy-strict-config-plan.md` decided ("Duplication belongs to Codacy's duplication engine. golangci-lint's `dupl` is disabled with that reason"). That plan still says so: plans are not updated after they ship. #380, split from #272, reversed it: each area Codacy owned gets one owner in CI, and for duplication that owner is `dupl` (the duplication row of `docs/plans/2026-10-09-0717-issue-397-plan.md`). #404 turned it on. A `go` job or a local `golangci-lint run` now fails on a duplicate.

## Guidance

- **Keep Codacy's bar.** `dupl` runs at 100 tokens (`.golangci.yml:77-79`), Codacy's `minTokenMatch` (`.codacy.yaml:19`). golangci-lint's default of 150 would let through clones Codacy caught.
- **Skip test files, and nothing else.** An exclusion rule skips `dupl` on `_test\.go` (`.golangci.yml:326-329`), as Codacy's engine did (`.codacy.yaml:27-28`): table-driven tests repeat their case shape on purpose. Measured with v2.14.0 on 2026-10-09, the rule hides 3 pairs (6 issues). Codacy also skips the `testdata` directories, but `dupl` needs no rule for them: `./...` never loads a `testdata` directory, and `dupl` reads only Go. `acceptance/` has no `.golangci.yml` of its own; golangci-lint uses the root one there, so `dupl` runs in the nested module too, and reported nothing.
- **Fix a pair that repeats a mechanism.** If the two copies do the same work, such as building a timed context, registering its cancel and posting from a goroutine, extract the shared part. The two such pairs were fixed before `dupl` went on: the engine's timed script and function calls (#407) and the fake tracker's two boards (#409).
- **Suppress a pair that is variants of one family, or data.** If every type, spec and judge differs and only the shape is shared, or the pair is two literals with different values, put `//nolint:dupl // <reason>` on *each* reported declaration. `nolintlint` requires the linter and a reason. A shared helper would only add indirection. The two current suppressions are `ShellEnded.decide` and `FunctionEnded.decide` (`internal/crew/fact_action.go:101`, `:114`), two cases of the sealed fact family, and `darkPalette` and `lightPalette` (`internal/ui/tui/styles.go:25`, `:43`), two palettes. Removing any one of the four markers brings back exactly that declaration's finding, and `nolintlint` fails on a marker that suppresses nothing.
- **Both engines run until #385.** Codacy's duplication engine stays on with the same 100 tokens and test exclusion, so the two agree. The plan for #404 keeps it on deliberately. What would change that: Codacy's gate failing a pull request on a duplicate that `dupl` passes.
- **Do not commit `update-config`'s drift with an unrelated `.codacy.yaml` edit.** AGENTS.md says to run `pnpm exec codacy-analysis update-config` after editing `.codacy.yaml` and commit both files. In #404 the edit was a comment, and the regenerated `.codacy/codacy.config.json` added SQL to the languages and three tools (PMD7, SQLint, SQLFluff, 23 patterns). The cause was `internal/adapter/sqlite/migrations/0001_processes.sql`, added in #364 without a sync; the config was last regenerated in #199. None of the three tools was installed locally, so zero findings could not be proved. The regenerated `codacy.config.json` and `codacy.config.baseline.json` were reverted, and only `.codacy.yaml` was committed. As of #404 that drift is still unsynced. It needs its own change, or goes away with #385.

## Why This Matters

The 2026-10-02 plan's KTD3 still tells a reader that duplication is Codacy's and that `dupl` must stay off. An agent that trusts it could disable `dupl` again, or add duplication rules only in `.codacy.yaml`. Neither the code nor the comments explain the rule for choosing between a fix and a suppression. Without it, the next `dupl` finding gets one of two wrong fixes: a helper forced over two variants of a sealed family, or a blanket suppression over a real repeated mechanism. The `update-config` step looks routine, but here it would have slipped three unproven analysers into a lint change.

## When to Apply

- When `dupl` reports a new pair.
- Before changing `dupl`'s threshold or exclusions, or Codacy's duplication block.
- When the 2026-10-02 Codacy plan and the current `.golangci.yml` disagree about duplication.
- When `update-config` changes more than the edit you made.

## Examples

The suppression shape, on both declarations of a variant pair:

```go
func darkPalette() palette { //nolint:dupl // data: lightPalette's roles with dark-background colours
func lightPalette() palette { //nolint:dupl // data: darkPalette's roles with light-background colours
```

The configuration, as committed:

```yaml
# .golangci.yml
linters:
  settings:
    dupl:
      # Codacy's minTokenMatch: the bar Codacy held for duplication.
      threshold: 100
  exclusions:
    rules:
      - path: _test\.go
        linters: [dupl]
```

Related: `docs/solutions/tooling-decisions/codacy-lizard-and-golangci-lint-limits.md` (keeping Codacy and golangci-lint on the same limits, and why only duplication has per-path excludes).
