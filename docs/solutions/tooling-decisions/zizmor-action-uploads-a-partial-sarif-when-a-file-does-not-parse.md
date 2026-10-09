---
title: zizmor-action uploads a partial SARIF when a file it audits does not parse
date: 2026-10-09
category: tooling-decisions
module: .github/workflows/security.yml
problem_type: tooling_decision
component: tooling
severity: medium
applies_when:
  - "Changing the zizmor job in .github/workflows/security.yml, or bumping zizmorcore/zizmor-action"
  - "Reading #378's KTD5 (docs/plans/2026-10-09-0511-issue-391-plan.md, and #392's plan), which says every job fails on files left unprocessed"
  - "Trusting a green zizmor check, or a closed zizmor alert, after a change to a workflow, a composite action or .github/dependabot.yml"
  - "Wiring any scanner whose default is to skip an input it cannot parse"
symptoms:
  - "zizmor warns 'failed to parse input' or 'failed to validate ... as dependabot config', exits 0 and writes SARIF for the other files only"
  - "The zizmor job is green and code scanning reads every alert in the skipped file as fixed"
root_cause: wrong_api
tags: [zizmor, sarif, code-scanning, strict-collection, github-actions, false-pass, stale-plan, dependabot]
retire_when: "zizmorcore/zizmor-action gains an input for --strict-collection, or zizmor makes strict collection its default; check action.yml's inputs at the tag the workflow pins and `zizmor --help` for the version it maps 'latest' to"
---

# zizmor-action uploads a partial SARIF when a file it audits does not parse

## Context

#392 added the `zizmor` job to `.github/workflows/security.yml` (lines 312-340). It runs `zizmorcore/zizmor-action` v0.6.4, pinned at `cc914d7f3750a2d13d75c7f184a1060aa0e9d482` (`security.yml:336`), with `persona: regular`, `online-audits: true` and `advanced-security: true`. In that mode the action runs zizmor 1.30.1 (its `latest`) with `--format=sarif` and uploads the result under the category `zizmor`.

#378's KTD5, copied into each part's plan (`docs/plans/2026-10-09-0511-issue-391-plan.md:76`, and #392's plan), says every job "reports only a complete scan, and it fails on its own errors", counting "files left unprocessed" as an error, because "a partial SARIF marks every alert it leaves out as fixed". Plans are not updated after they ship, so they still say it. The job's comment (`security.yml:319-320`), the Code scanning bullet in `AGENTS.md` and the `security.yml` row in the README say the job fails on an error, such as a malformed `.github/zizmor.yml`. That is true for some errors but not for a file zizmor cannot parse.

The review of #392 found the gap (P2, adversarial reviewer), and its validator reproduced it. The finding was left unapplied when this learning was written. It went to the pull request as an open choice (session history), and Codacy's review raised it again on #421. The maintainer chose option 2 below: keep the action, which the repository prefers to an inline script, and say what it does.

## Guidance

How zizmor 1.30.1 and the pinned action treat errors. Reproduced on 2026-10-09 in a throwaway repository with `uvx zizmor@1.30.1 --persona=regular --format=sarif .`:

- **A file zizmor cannot parse is skipped with a warning.** Given a workflow with broken YAML and a `.github/dependabot.yml` that fails the schema, zizmor logged `WARN ... failed to parse input` and `WARN ... failed to validate file://./.github/dependabot.yml as dependabot config`, exited 0, and wrote SARIF whose results named only the one good workflow.
- **`--strict-collection` makes that run fail.** The same run with it exited 1 (`fatal: no audit was performed`). `zizmor --help` describes it as "Fail instead of warning on syntax and schema errors in collected inputs".
- **The pinned action cannot pass it.** `action.sh` builds zizmor's arguments only from the action's inputs (persona, format, collect, online audits, severity, confidence, color, config). It passes the `inputs` input after `--`, so it cannot carry a flag. Then it exits with zizmor's own status, except that exit 3 (no inputs) follows `fail-on-no-inputs`. Its `action.yml` uploads the SARIF in a second step whenever `advanced-security` is `true`, so after an exit 0 the partial SARIF reaches code scanning.
- **What does fail the job:** a malformed `.github/zizmor.yml` (exit 1, nothing uploaded, shown by #392's own proof on a throwaway copy) and no input at all (exit 3, which fails by default).
- **Findings never fail it.** In SARIF mode zizmor exits 0 on findings. Blocking on them is #272's part 7 (#384), through a code scanning rule.
- **actionlint does not cover the gap.** The required `actionlint` check rejects most broken workflow YAML before merge. It does not read `.github/dependabot.yml`, and it does not catch syntax that actionlint accepts and zizmor's parser rejects.

So the job, as #392 shipped it, does not meet KTD5 for unparseable inputs. There are two ways to close the gap:

1. **Fail closed, which keeps KTD5.** Run zizmor without the action: its pinned container image or a pinned release, with `--strict-collection --format=sarif`. Upload with `github/codeql-action/upload-sarif` under category `zizmor` only after an exit 0. That is the rule KTD5 sets for the CLI jobs (upload only after a clean or findings exit), and the `grype` job already follows it by failing before its upload. Prove it once on a throwaway branch: a workflow zizmor cannot parse fails the job, and no analysis is uploaded. The cost is that Dependabot no longer bumps zizmor and its image digest together through the action (KTD7). The pin then joins the pins bumped by hand.
2. **Keep the action and say so.** Write in the job's comment, the `AGENTS.md` bullet and the README row that zizmor skips, with a warning, any file it cannot parse, and that actionlint is what guards workflow syntax.

Other zizmor facts #392 verified, which the plan did not have:

- zizmor finds `.github/zizmor.yml` in the checked-out tree on its own. The job passes no `config` input: `--config` pointing at a file that does not exist would fail the run. Because the job checks out the merge commit, a rule turned off there is off in the pull request that does it (AE3).
- At the `regular` persona, 1.30.1 flagged two findings the plan had not foreseen, and both were fixed rather than suppressed: `cache-poisoning` on `release.yml`'s `actions/setup-go` module cache (now `cache: false`), and `dependabot-cooldown` on each Dependabot ecosystem (now `cooldown: default-days: 7`). It did not flag `codacy-import.yml`'s `setup-node` `cache: pnpm`, which the plan had listed as a likely finding.

## Why This Matters

A partial SARIF does more than miss new problems. KTD5 rests on code scanning comparing each analysis with the previous one in its category, so the alerts in a file that dropped out of the scan are closed as fixed (the plan's premise; not tested here). A pull request that breaks one workflow's YAML, or makes `dependabot.yml` invalid, could then close that file's open zizmor alerts while every check is green. Once #384 blocks merges on code scanning alerts, the same pull request would also get past the gate it is meant to face.

Without this note, a reader of the job's comment, `AGENTS.md` and the plan would believe the job already fails closed.

## When to Apply

- Before relying on the `zizmor` check as a merge gate (#272's part 7, #384).
- When bumping `zizmorcore/zizmor-action`: check whether it now has an input for strict collection.
- When adding any scanner to `security.yml`: find out what it does with an input it cannot parse, not only what it does on a crash.

## Examples

Reproduction, in a throwaway repository with a good workflow, a broken one and an invalid `dependabot.yml`:

```sh
uvx zizmor@1.30.1 --no-online-audits --persona=regular --format=sarif . > out.sarif
echo $?   # 0; out.sarif has results for .github/workflows/good.yml only

uvx zizmor@1.30.1 --no-online-audits --persona=regular --format=sarif --strict-collection . > /dev/null
echo $?   # 1; fatal: no audit was performed
```

The shape of the fail-closed job (option 1), following KTD5. The image reference is a placeholder for a pinned version and digest:

```sh
status=0
docker run --rm -v "$PWD:/workspace:ro" -w /workspace -e GH_TOKEN \
  "ghcr.io/zizmorcore/zizmor:<version>@<digest>" \
  --persona=regular --format=sarif --strict-collection . > zizmor.sarif || status=$?
if [ "$status" -ne 0 ]; then
  echo "zizmor failed with exit $status" >&2
  exit "$status"
fi
# then: github/codeql-action/upload-sarif with sarif_file: zizmor.sarif, category: zizmor
```

## Related

- [A failed shellcheck run becomes an empty SARIF](../logic-errors/failed-shellcheck-uploads-an-empty-sarif.md): the same false pass, there through an empty SARIF.
- [dependency-review-action's warn-only still fails its step](dependency-review-warn-only-still-fails-its-step.md): another case where a pinned action does not do what #378's plans assumed.
- [diffcover passes silently on mnemonic diff prefixes](../logic-errors/diffcover-passes-silently-on-mnemonic-diff-prefixes.md): a gate that passed while checking nothing.
- #392 (this job), #378 (the split and its KTD5), #384 (#272's part 7, the code scanning rule a partial SARIF would get past).
