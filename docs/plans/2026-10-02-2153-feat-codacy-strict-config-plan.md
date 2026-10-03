---
title: Strict Codacy Configuration - Plan
type: feat
date: 2026-10-02
topic: codacy-strict-config
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Strict Codacy Configuration - Plan

## Goal Capsule

- **Objective:** Every pull request to crew meets one strict quality bar on complexity, quality, security and code standards, and nothing merges below it. Each rule in the bar is one we would act on, it holds for the whole repository from the day it is switched on, and an agent can check it locally before pushing.
- **Means:** Codacy Cloud. Its rules are kept in files in the repository and applied to Codacy on merge. Go is analysed in CI and the results are uploaded. The quality gates are set in the Codacy UI and recorded in the docs.
- **Product authority:** this Product Contract, within `STRATEGY.md`. Shared workflows in `thatsnotmynameio/.github` and organization-wide Codacy policies are not active scope.
- **Execution profile:** Standard. Planning decides how the debt pass (R14) splits across pull requests.
- **Open blockers:** none.

---

## Product Contract

### Summary

crew gets a strict Codacy setup. A tool or rule is enabled only when its findings are ones we would fix, and everything enabled is enforced with zero tolerance on every pull request. Tools, rules and thresholds are versioned in the repository. Only the quality gates live in the Codacy UI, and a docs page records their values. SonarQube is removed.

### Problem Frame

crew writes its own code through agent sessions. Today a pull request must pass gofmt, `go vet`, golangci-lint with five linters (depguard, errorlint, gocritic, misspell, revive), `go test -race` and govulncheck. Nothing checks complexity or duplication. Nothing runs static security analysis or scans for secrets. Coverage is never measured, even though the suite reaches 92.6% of statements with `-coverpkg=./...`. SonarQube is wired in but has never been switched on, and its properties file still names the template project.

The strategy rests on quality coming from checks by someone other than the author. An agent that writes a pull request cannot be trusted to judge its own complexity or security. The boss wants these four areas enforced: complexity, quality, security and code standards.

### Key Decisions

- **Existing findings go to zero before the gate is enforced.** The bar applies to the whole repository from day one. Governs R14. (session-settled: user-directed — chosen over gating only new code and over gating new code now with the debt planned as a later delivery: the standard should hold everywhere, not only on new lines)
- **Rigor that makes sense: a rule earns its place.** A tool, linter or pattern is enabled only if we would fix what it reports. Whatever is left out is listed with its reason. Governs R2, R3. (session-settled: user-directed — chosen over enabling everything, over adopting a community golden config as it stands, and over only extending the five current linters: "rigor, but rigor that makes sense")
- **Coverage: 90% of new lines, and the total never below 90%.** Governs R10, R11. (session-settled: user-directed — chosen over 100% of new lines, which breeds tests written for the number, and over only forbidding a drop in total coverage, which blocks refactors that delete tested code)
- **Codacy's rules are versioned in the repository, and the gates are set in the UI.** Codacy's own configuration file plus its Cloud CLI import make tools, patterns and parameters versionable. Quality gates have no file or CLI command. Governs R5, R6, R7. (session-settled: user-directed — chosen over configuring everything in the UI and over a custom script that calls the Codacy API)
- **CI analyses Go, and Codacy's server runs everything else.** golangci-lint, with its security, static-analysis and complexity linters, runs in CI against the repository's own config and uploads its results. Codacy's server runs the scanners that need no build. Governs R1. (session-settled: user-approved — chosen over letting Codacy's server run only its own Go tools next to an independent CI lint, which splits the rules across two places, and over using Codacy only to aggregate CI results, which loses its secret, SAST and dependency scanning)
- **The Codacy config is applied on merge to `main`, not by hand.** The file and Codacy cannot drift apart through changes made in the repository. Governs R6. (session-settled: user-approved — chosen over the boss running the import after each change)
- **Only crew, for now.** Once it works here, it can be promoted to the shared `.github` repository as a separate delivery. (session-settled: user-directed — chosen over shared workflows from the start and over an organization-wide gate policy and coding standard)
- **SonarQube is removed.** One analysis tool, one place to suppress a finding. Governs R16. (session-settled: user-directed — chosen over leaving it switched off and over running both)

### Requirements

**Rules and ownership**

- R1. Each kind of finding has one owner. CI owns Go static analysis: style, correctness, security (gosec), staticcheck and per-function complexity. Codacy's server owns these:
  - SAST and secrets (Opengrep);
  - dependency vulnerabilities, malicious packages and secrets (Trivy);
  - duplication;
  - complexity metrics (Lizard);
  - license scanning.

  Where two tools measure the same thing, such as function complexity in golangci-lint and Lizard, their thresholds match, so local lint and the Codacy gate never disagree.
- R2. Every enabled tool, linter and pattern is one whose findings we would fix. Every one left disabled is listed with a written reason next to where it is configured.
- R3. Every threshold, such as complexity, function length, nesting, duplication or line length, is set in a versioned file with the reason for its value.
- R4. A suppression is narrow and explained. An inline suppression names the specific rule and gives a reason, and lint fails on one that does not. A path excluded from analysis carries its reason in the scope file. First-party source is never excluded to hide a finding.

**Configuration in the repository**

- R5. The repository holds every Codacy setting that has a file form:
  - which Codacy tools run, with their patterns and parameters (Codacy's tool configuration file);
  - each tool's own configuration file where Codacy reads one (`.golangci.yml`, and Opengrep's rules file if Opengrep's defaults are not enough);
  - the analysis scope (`.codacy.yaml`).
- R6. A merge to `main` that changes the Codacy tool configuration applies it to Codacy automatically, with no step in the UI.
- R7. Some settings have no file form: the quality gates, "Run analysis on your build server" and the GitHub integration settings. The boss sets them in the Codacy UI. A page in `docs/develop/` records each value and its reason. A change in rigor is proposed as a pull request to that page.

**The gate**

- R8. The pull request gate fails on any of these:
  - a new issue of any severity, Info and up;
  - a new security issue of any severity;
  - new duplication;
  - diff coverage below the R10 floor.

  The commit gate on `main` uses the same thresholds where Codacy offers them.
- R9. Codacy's status checks are required in the `checks` ruleset, next to `go`. They become required only after they have reported the expected result on real pull requests.
- R10. Diff coverage, the share of the lines a pull request adds or changes that tests cover, must be at least 90%.
- R11. Total coverage never drops below 90%. CI checks this on every pull request, because Codacy gates only coverage changes, not absolute totals.
- R12. Every pull request gets a verdict on its merits. One that cannot upload results, such as a Dependabot pull request without repository secrets, does not leave a Codacy check pending forever. One with no coverable lines, such as a docs-only change, does not fail on coverage.
- R13. The golangci-lint version is pinned in one place, so a new release cannot add rules or move the bar silently.

**The debt pass**

- R14. Before R9 makes the checks required, the repository has zero findings under the final configuration. Each finding is fixed, or suppressed per R4 when it is a false positive. Total coverage meets R11.

**Working locally**

- R15. Everything the gate enforces can be run locally with commands listed in `AGENTS.md` and `docs/develop/index.mdx`:
  - Go lint;
  - Codacy's server-side tools, through the Codacy Analysis CLI;
  - coverage with the 90% floor.

  An agent sees the same failure before it pushes.

**SonarQube and docs**

- R16. SonarQube is removed: its workflow, its properties file, the `SONAR_ENABLED` instructions and every mention in `AGENTS.md`, `README.md` and the coverage comment in `.gitignore`.
- R17. `AGENTS.md`, `README.md` and `docs/develop/index.mdx` describe the new checks, the commands and where each kind of rule is configured, and `pnpm docs:check` passes.

### Acceptance Examples

- AE1. **Covers R1, R8, R15.** **Given** a pull request that builds a shell command from user input, **when** the agent runs the local commands, **then** gosec reports it. **When** the pull request is pushed, **then** `Codacy Static Code Analysis` fails on the same finding.
- AE2. **Covers R8, R10.** **Given** a pull request whose changed lines are 85% covered while the total stays at 92%, **when** Codacy analyses it, **then** the diff coverage check fails.
- AE3. **Covers R11.** **Given** a pull request that deletes tests and drops total coverage to 89%, **when** CI runs, **then** the coverage floor fails, even if every changed line is covered.
- AE4. **Covers R12.** **Given** a Dependabot pull request bumping a Go module, **when** CI runs without repository secrets, **then** every Codacy check still reaches a verdict and none waits forever for an upload.
- AE5. **Covers R12.** **Given** a pull request that only changes `docs/**/*.mdx`, **when** Codacy analyses it, **then** no coverage check blocks it.
- AE6. **Covers R6.** **Given** a merged pull request that disables a pattern in the Codacy tool configuration file, **when** the next analysis runs, **then** Codacy no longer reports that pattern, and nobody touched the UI.
- AE7. **Covers R4.** **Given** a `//nolint:gosec` with no reason, **when** lint runs, **then** it fails and asks for the specific rule and a reason.

### Success Criteria

- A red Codacy check on a crew pull request points at something its author agrees to fix. A check that is red on almost every pull request means R2 failed, and the rule behind it is revisited rather than suppressed case by case.
- The checks hold for agent-written pull requests: a crew session finds the same failures locally (R15) and fixes them before pushing.

### Scope Boundaries

**Deferred for later**

- Promoting the workflow and configuration to `thatsnotmynameio/.github`, and organization-wide Codacy gate policies and coding standards.
- Detecting a gate loosened in the Codacy UI without a pull request.
- Codacy's AI features (AI Reviewer, pull request summary, AI false-positive triage).

**Not in this work**

- Container, IaC and DAST scanning: crew ships a binary, not images or infrastructure.
- Fixing the debt by lowering the bar. A threshold changes only through R2 or R3, with a reason.

### Dependencies / Assumptions

- The organization's Codacy plan is Team or higher, which covers private repositories, gates and coverage. crew is private. The boss confirmed the plan.
- The Codacy GitHub app (`codacy-production`) is installed on the organization. The boss adds crew in Codacy and creates the repository token that CI stores as a secret.
- A repository admin can always bypass Codacy's status checks, and that bypass is audited. Codacy offers no setting to disable it.
- Codacy reads `.codacy.yaml` from the default branch only. A change to the analysis scope takes effect after it merges.

### Outstanding Questions

**Deferred to Planning**

- How golangci-lint results reach Codacy: Codacy's `codacy-golangci-lint` converter with the results API, or the Codacy Analysis CLI's SARIF upload. Also, whether the UI's pattern selection filters uploaded golangci-lint results.
- Whether the aggregate complexity gate adds anything on top of the per-function thresholds of R1 and R3, and what value it takes if so.
- How R12 is met. Options include a Dependabot secret or a guarded upload step, and checking whether Codacy marks diff coverage `∅` as passing on a pull request with no coverable lines.
- Which token the import on `main` needs. A repository token is enough unless coding standards must be unlinked, which needs an account token.
- Which starting thresholds survive R2 against crew's code. A starting point is in Sources.
- How the debt pass (R14) is split into pull requests.

### Sources / Research

- Codacy docs, from the `codacy/docs` repository at `248bb9a` (2026-10-02):
  - `docs/getting-started/supported-languages-and-tools.md`: the Go tools, and which of them run client-side;
  - `docs/repositories-configure/configuring-code-patterns.md`: tool configuration files; Opengrep reads `.semgrep.yaml`, revive reads `revive.toml`, and Gosec, Staticcheck, Lizard and Trivy read none;
  - `docs/repositories-configure/codacy-configuration-file.md`: `.codacy.yaml` holds the scope only and cannot enable tools or set gates;
  - `docs/codacy-analysis-cli/index.md`: `.codacy/codacy.config.json`, local analysis and SARIF upload;
  - `docs/codacy-cloud-cli/index.md`: `codacy tools ... --import`;
  - `docs/repositories-configure/adjusting-quality-gates.md` and `docs/repositories-configure/local-analysis/client-side-tools.md`.
- Codacy Cloud CLI, `codacy/codacy-cloud-cli` `CHANGELOG.md`: `tools --import` works with a repository token, while `--force` needs an account token.
- Starting thresholds for strict golangci-lint v2 setups, where maratori's golden config (`maratori/golangci-lint-config`), traefik and golangci-lint's own config agree: gocognit 15–20, gocyclo about 15, funlen at 50 statements with lines off, nestif 4–5, dupl 100, line length 120. Codacy's Lizard defaults flag CCN above 10 and functions over 50 lines.
- Coverage practice: the Google Testing Blog, "Code Coverage Best Practices" (2020), calls 90% a good per-commit floor. Measured on this repository on 2026-10-02: 92.6% with `go test -coverpkg=./... ./...`.
- A known risk with strict gates: a gate that is red on every pull request stops meaning anything (`Ciemaar/PrintQueueManager#106`). This is the reason for R2 and the first Success Criterion.
