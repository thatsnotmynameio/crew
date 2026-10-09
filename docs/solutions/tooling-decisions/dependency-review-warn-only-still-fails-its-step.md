---
title: dependency-review-action's warn-only still fails its step, so the job reads only the forbidden licenses
date: 2026-10-09
category: tooling-decisions
module: .github/workflows/security.yml
problem_type: tooling_decision
component: tooling
severity: medium
applies_when:
  - "Changing the dependency-review job in .github/workflows/security.yml, or its warn-only, deny-packages, deny-groups or allow-licenses inputs"
  - "Bumping actions/dependency-review-action past v5.0.0"
  - "Reading docs/plans/2026-10-09-0717-issue-397-plan.md, whose KTD7 and Assumptions still name denied-changes"
  - "Building a SARIF from another action's outputs, where the action may fail its own step"
tags: [dependency-review, warn-only, sarif, code-scanning, licenses, github-actions, stale-plan, false-pass]
retire_when: "actions/dependency-review-action changes how warn-only treats unresolved licenses and denied packages, or when it sets denied-changes; check src/main.ts at the tag the workflow pins"
---

# dependency-review-action's warn-only still fails its step, so the job reads only the forbidden licenses

## Context

The `dependency-review` job (#397) runs `actions/dependency-review-action` v5.0.0, pinned at `a1d282b36b6f3519aa1f3fc636f609c47dddb294`, with `warn-only: true` (`.github/workflows/security.yml:150`). A `sarif` step then builds a SARIF from the action's outputs (`security.yml:156-188`), the job uploads it under category `dependency-review`, and a `results` step fails the job on the findings (`security.yml:193`).

The #397 plan (`docs/plans/2026-10-09-0717-issue-397-plan.md`) describes this design with two premises that the action's source does not support. Plans are not updated after they ship, so the plan still says them:

- KTD7 step 1: the SARIF step "reads the action's `invalid-license-changes` and `denied-changes` outputs".
- KTD7: "`warn-only: true`, so the action reports violations without failing its step".
- Assumptions: "The action sets its `invalid-license-changes`, `denied-changes` and `vulnerable-changes` outputs under `warn-only: true`, and still fails its step on an error of its own".

The implementation read the action's source at the pinned SHA. The job's code follows the source, not the plan.

## Guidance

What `warn-only` does in v5.0.0, from the action's own `src/main.ts` at the pinned commit (line numbers there):

- **It downgrades only two findings to warnings:** a forbidden license (`printLicensesBlock`, lines 350-359) and a vulnerable dependency (`printVulnerabilitiesBlock`, lines 313-318).
- **An unresolved license still fails the step.** If the action cannot parse a dependency's license, it calls `core.setFailed` whatever `warn-only` says (lines 361-369).
- **A denied package still fails the step.** `printDeniedDependencies` calls `core.setFailed` with no `warn-only` check (lines 490-491).
- **API and other errors fail the step.** A 404, a 403 or any thrown error is caught and passed to `setFailed` (lines 262-287). On `push`, the action throws before it reviews anything, because it finds no base and head refs (the action's `src/git-refs.ts:33-45`). That is why the job runs the action only under `if: github.event_name == 'pull_request'` (`security.yml:134`).
- **An unlicensed dependency only logs.** If GitHub detects no license for a dependency, `printNullLicenses` writes an info line (lines 385-396). The dependency passes and the job never sees it.

So the `sarif` step reads only `.forbidden[]` of `invalid-license-changes` (`security.yml:161`). It has no handler for `denied-changes` or `.unresolved[]`. If either one has entries, the action's step has already failed, so the later steps, the upload included, do not run. With no `deny-packages` or `deny-groups`, `denied-changes` is always `[]`, so a handler there would be dead code.

A first reading of the source, in #397's implementation, concluded that `denied-changes` is set only when a deny list is configured. That is not quite right. The guard is `if (config.deny_packages || config.deny_groups)` (line 230), and the config schema defaults both lists to `[]` (the action's `src/schemas.ts:107-108`). An empty array is truthy in JavaScript, so the action always sets `denied-changes`, to `"[]"` when no deny list is configured. The conclusion still holds: without a deny list, the output can never hold an entry.

Two choices in the SARIF also come from the action's output shape:

- Each result points at line 1 of its manifest, because the action names a manifest but no line. Code scanning tells alerts apart by rule and location. So the rule id is `license/<package name>`, and two forbidden packages in one `go.mod` become two alerts instead of one.
- A vulnerable dependency fails the job but gets no SARIF entry, because Grype already files the same advisory on the same manifest (plan KTD7).

## Why This Matters

- **Plans and code disagree.** Someone who trusts the plan would add a `denied-changes` handler, or expect `warn-only` to keep an unresolved license from failing the step. The handler would be dead code. The expectation would misread a failed run as an error in the job's scripts.
- **The job never uploads an empty SARIF after a failure.** Every failure path calls `setFailed` in the action's own step, so the `sarif` and upload steps are skipped, and code scanning never gets an empty run that closes open alerts. This is the risk in [A failed shellcheck run becomes an empty SARIF](../logic-errors/failed-shellcheck-uploads-an-empty-sarif.md). Here the action's own step failure guards against it, and shellcheck needs an exit-status check instead. Adding `if: always()` or `!cancelled()` to the `sarif` or upload step would remove the guard: a failed review would then upload an empty SARIF.
- **Accepted gaps:**
  - An unlicensed dependency passes the job and code scanning. The action offers no way to fail on it, and treating it as forbidden would fail every pull request that adds or bumps a SHA-pinned GitHub Action, because GitHub detects no license for those today.
  - Each push to `main` uploads a run with no result, which serves as the base analysis. A dependency with a forbidden license that merges anyway, before #384 makes the check required or through a bypass, has its alert closed as fixed while the dependency stays.

## When to Apply

- Before you add an input to the action, check the action's `src/main.ts` at the new pin. Each input can add an output, and a new failure path decides whether the steps after it ever run.
- After you bump the action, check the three `setFailed` calls above against the new source. If one of them starts honouring `warn-only`, the `sarif` step must then read that list. Otherwise the action's step passes and the job passes too, and nothing records the dependency.
- When you build a SARIF from any other action's outputs, find out which of its findings fail its own step under its "report only" mode. The job never reads outputs from a step that failed.

## Examples

The `sarif` step reads one list:

```sh
jq -n --arg changes "$LICENSE_CHANGES" '
  [if $changes == "" then empty else ($changes | fromjson | .forbidden[]?) end] as $forbidden
  ...'
```

The plan's version would have read `denied-changes` too. That output is always `"[]"` here, and when a deny list makes it non-empty, the action has already failed the step:

```sh
# Unreachable with entries: printDeniedDependencies calls setFailed even under warn-only.
DENIED_CHANGES: ${{ steps.review.outputs.denied-changes }}
```

The cases the implementation checked locally (an out-of-repo harness that ran the `sarif` and `results` scripts against outputs shaped like the action's v5.0.0 source) were these. With no changes, the job passes with 0 results. With only allowed licenses, it passes. One MPL-2.0 package gives 1 result at `go.mod:1` and fails the job. Three forbidden packages in two manifests give 3 results under 2 rules. A null license gives 1 result. A GHSA-vulnerable dependency gives 0 results and fails the job. The behaviour on GitHub (the action's outputs under `warn-only`, the skipped upload after an action error, and code scanning accepting the hand-built SARIF from fork and Dependabot pull requests) is not yet checked. That waits for the job's first real runs on #397's pull request.

## Related

- [A failed shellcheck run becomes an empty SARIF](../logic-errors/failed-shellcheck-uploads-an-empty-sarif.md): the same rule that a failed tool never uploads an empty pass, with an exit-status check instead of a step failure.
- [crew's plans assume a private repository](../workflow-issues/plans-assume-a-private-repository-that-is-now-public.md): another plan premise that a shipped `docs/plans` file still states.
- #397 (this job), #379 (its parent), #398 (documents the job in AGENTS.md and the README), #384 (makes `dependency-review` a required check), #395 and #405 (later jobs that upload SARIF to code scanning).
