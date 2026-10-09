---
title: A failed shellcheck run becomes an empty SARIF that code scanning reads as every alert fixed
date: 2026-10-09
category: logic-errors
module: tools/shellchecksarif
problem_type: logic_error
component: tooling
symptoms:
  - "`shellcheck --format=json1` given a missing file prints `{\"comments\":[]}` on stdout and exits 2"
  - "Piped into tools/shellchecksarif, it gives a valid SARIF run with no results, and the pipeline exits 0"
  - "Code scanning would close every open shellcheck alert while the check passes"
root_cause: logic_error
resolution_type: workflow_improvement
severity: high
retire_when: "shellcheck stops printing a json1 document when it cannot read a file (check `shellcheck --format=json1 /nonexistent.sh; echo $?` on the version CI installs)"
tags: [shellcheck, sarif, code-scanning, pipefail, exit-status, false-pass, shellchecksarif]
---

# A failed shellcheck run becomes an empty SARIF that code scanning reads as every alert fixed

## Problem

`tools/shellchecksarif` (#390) turns shellcheck's `json1` output into SARIF for GitHub code scanning; a later part of #378 runs it in CI. When shellcheck fails, it still prints a well-formed json1 document with no comments. The converter turns that into a valid SARIF run with no results, and the pipeline its package comment shows (`tools/shellchecksarif/main.go:5`) exits 0. Uploaded, that run tells code scanning that every shellcheck alert is fixed, and the check passes.

## Symptoms

Observed with shellcheck 0.11.0 on 2026-10-09:

- `shellcheck --format=json1 /nonexistent.sh` prints `openBinaryFile: does not exist` on stderr, `{"comments":[]}` on stdout, and exits 2.
- `shellcheck --format=json1 /nonexistent.sh | shellchecksarif` writes a SARIF log whose one run has `"rules": []` and `"results": []`. `PIPESTATUS` is `2 0`, so the step succeeds.
- The same happens when a glob such as `tools/*.sh` matches nothing (bash passes the pattern through as a literal file name) or a script directory is renamed.

## What Didn't Work

- Making the converter reject `{"comments":[]}`. An empty array is also what shellcheck prints for clean scripts, which must give an empty run so code scanning closes fixed alerts. The converter cannot tell the two apart; only shellcheck's exit status can.
- `set -o pipefail` alone, the way the `changed lines coverage` step in `.github/workflows/ci.yml` guards `git diff | diffcover` with `shell: bash`. That fits diffcover, whose upstream `git diff` exits 0 on success. shellcheck exits 1 whenever it finds anything, so pipefail fails the step on findings too. The step then fails before the upload, and the findings never reach code scanning.
- Relying on `set -e`. It does not look at a pipeline's left side, and it is ignored for a command left of `&&` or `||` (see [set -e ignored left of &&](set-e-ignored-left-of-and-in-install-command.md)).

## Solution

#378's KTD5 already sets the policy: exit 0 or 1 is a complete run, anything else fails the job and uploads nothing, and #395 tests it with a path that does not exist. What the pipeline needs to keep that policy: run shellcheck on its own, keep its status, fail on 2 or higher, and convert only after that check. Exit 1 (findings) goes on to the upload:

```sh
status=0
shellcheck --format=json1 tools/*.sh > shellcheck.json1 || status=$?
if [ "$status" -gt 1 ]; then
  echo "shellcheck failed with exit $status" >&2
  exit "$status"
fi
go run ./tools/shellchecksarif < shellcheck.json1 > shellcheck.sarif
```

Verified locally on 2026-10-09: with a missing file this exits 2; with a script that has a finding it exits 0 and writes the SARIF. KTD5 also fails the shellcheck job on findings, in a step after the upload, so keep the status (here, exit 1) for that step.

If the step uses `pipefail` instead, the upload step needs to run on failure too (`if: always()` or `!cancelled()`), and a separate check has to tell exit 1 from exit 2. The explicit status check above is simpler.

## Why This Works

shellcheck's exit status is the only signal that separates "checked and found nothing" (0), "checked and found something" (1), and "could not check" (2 and up, such as a missing file or bad options). Its stdout looks the same for 0 and for a failure. Checking the status before converting keeps an empty run for real clean results only.

## Prevention

- In GitHub Actions, a `run:` step with no `shell:` runs under `bash -e {0}`, which has no `pipefail`. `shell: bash` adds `-o pipefail`. Neither tolerates exit 1, so check the status explicitly as above.
- Test the CI step against a missing path once before relying on it: it must fail, not upload.
- Any tool that writes "no findings" on failure has the same risk when its output is converted or uploaded. Check its exit status before trusting an empty report. [diffcover passes silently on mnemonic diff prefixes](diffcover-passes-silently-on-mnemonic-diff-prefixes.md) is another check that passed silently on bad input.

## Related Issues

- #390: adds `tools/shellchecksarif`. Its review raised this as a risk for the CI part (session history).
- #378: the split. Its KTD5 classifies each tool's exit as clean, findings or error, and uploads SARIF only after the first two.
- #395: part 6 of #378's split, which adds the `shellcheck` job to `security.yml`, where this check lands.
- #384: the code scanning rule that an empty SARIF would defeat.
- Unverified: shellcheck's parse-error comments (SC1009, SC1072, SC1073) have zero-width regions (`column` equals `endColumn`). They pass through the converter unchanged. Whether code scanning accepts and shows them is unknown until the CI part uploads one.
