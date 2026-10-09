---
title: Keeping Codacy's Lizard and golangci-lint on the same complexity limits
date: 2026-10-02
category: tooling-decisions
module: .golangci.yml, .codacy
problem_type: tooling_decision
component: tooling
applies_when:
  - "Changing a complexity, length or parameter limit in .golangci.yml or .codacy/codacy.config.json"
  - "Adding a path exclusion for Lizard or another Codacy engine in .codacy.yaml"
  - "Codacy reports a Lizard finding that golangci-lint does not, or names a function oddly"
tags: [codacy, lizard, golangci-lint, funlen, cyclop, complexity, codacy-analysis-cli, exclude-paths]
---

# Keeping Codacy's Lizard and golangci-lint on the same limits

## Context

crew's quality gate measures function size twice. golangci-lint runs in CI's `go` job, a required check. Codacy's server also runs Lizard, a complexity tool that was kept on the server when the gate was designed. The plan requires that the two never disagree, so an agent that passes the local checks also passes the gate (R1 and R15 in `docs/plans/2026-10-02-2153-feat-codacy-strict-config-plan.md`). Lizard has three quirks that make "the same limits" harder than copying numbers, and none of them shows in the configuration.

## Guidance

- **Count lines the way Lizard counts them.** Lizard's length patterns count non-comment lines (NLOC), not statements. `funlen` therefore counts lines with comments ignored and statements off (`.golangci.yml:76-81`), at the same 50 as `Lizard_nloc-medium` (`.codacy/codacy.config.json:554-556`). Cyclomatic complexity is `cyclop`'s 15 (`.golangci.yml:73`) against `Lizard_ccn-medium` (`.codacy/codacy.config.json:548-550`). The parameter count is revive's `argument-limit` against `Lizard_parameter-count-medium` (8), and file length is revive's `file-length-limit` against `Lizard_file-nloc-medium` (500). Change a limit on both sides in the same commit.
- **Do not relax limits per path; hold tests to the same bar.** Codacy's server honours per-engine `exclude_paths` in `.codacy.yaml`, but the local Codacy Analysis CLI (`@codacy/analysis-cli` 0.24.0) reads only the global `exclude_paths`. It ignores `engines.<tool>.exclude_paths`. A test exclusion for Lizard would apply on Codacy and not locally, so an agent's local run would disagree with the gate. That is why `_test.go` files meet the same complexity limits (`docs/develop/quality.mdx:25`), and why `.codacy.yaml` uses per-engine excludes only for duplication (`.codacy.yaml:13-28`), where golangci-lint's `dupl` skips test files by the same rule (`.golangci.yml:326-329`), so the local check and the gate agree.
- **Expect Lizard to misread Go after a type switch.** Lizard's Go parser loses track of function boundaries after an `x := y.(type)` switch. It merges the following functions into nameless entries or misses them. While crew's debt pass was running, `driver_test.go` showed 9 functions to Lizard against 19 real ones, and `action.go` showed 6 against 7. The merged entries carry the summed length and complexity, so they can produce findings that golangci-lint, which parses Go properly, does not. If such a false finding appears, the R2 answer is to turn Lizard's function patterns off in `.codacy/codacy.config.json`, with that reason, and let golangci-lint own those metrics. It is not to contort the code.
- **Check locally with the same unit.** Running `lizard -l go -L 50` directly measures total function length, blank lines and comments included, which is stricter than Codacy's NLOC pattern. It flagged functions with 47 NLOC that were 55 lines long. `pnpm exec codacy-analysis analyze` applies the committed patterns and is the faithful local check.

## Why This Matters

When the two tools disagree, the gate stops meaning anything. Either an agent pushes code that passed locally and fails on Codacy, or a limit is loosened on one side and the other keeps failing. The first time someone hits one of these quirks, the natural fixes are wrong: excluding tests in `.codacy.yaml`, or splitting a function that only Lizard thinks is long. Each of them quietly breaks the one-owner-per-metric design.

## When to Apply

- Before changing any limit in `.golangci.yml` or `.codacy/codacy.config.json`.
- Before adding a path exclusion to `.codacy.yaml`.
- When Codacy and the local checks disagree on a complexity or length finding.

## Examples

Both sides of the function-length limit, as committed:

```yaml
# .golangci.yml
funlen:
  lines: 50
  statements: -1
  ignore-comments: true
```

```json
{ "patternId": "Lizard_nloc-medium", "parameters": { "threshold": 50 } }
```

The exclusion that looks right but splits local from Codacy. Do not add it:

```yaml
# .codacy.yaml: Codacy would skip tests for Lizard, codacy-analysis would not
engines:
  metric:
    exclude_paths:
      - "**/*_test.go"
```
