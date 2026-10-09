---
title: CodeQL's Go manual build extracts no test file unless go test -c compiles them
date: 2026-10-09
category: integration-issues
module: .github/workflows/security.yml
problem_type: integration_issue
component: tooling
symptoms:
  - "A CodeQL Go database built with `go build ./...` under the tracer holds 217 files and no `_test.go` file, with the extractor's `extract_tests` option set"
  - "`go test -run '^$' ./...` under the tracer also yields no `_test.go` file, and `codeql database create` still exits 0"
  - "`go -C acceptance build ./...` under the tracer extracts nothing from `acceptance/`"
root_cause: wrong_api
resolution_type: config_change
severity: medium
framework_version: "CodeQL CLI 2.27.2 (github/codeql-action v4.38.3), Go 1.27.2"
retire_when: "CodeQL's Go extractor honours extract_tests in a traced (manual) build, or passes go test's own flags through without handing them to go list; check the Go extractor's entries in the CodeQL CLI release notes for a version newer than 2.27.2"
tags: [codeql, code-scanning, go-extractor, test-extraction, go-test-c, sarif, security-workflow, false-pass]
---

# CodeQL's Go manual build extracts no test file unless go test -c compiles them

## Problem

`.github/workflows/security.yml` (#391) runs CodeQL for Go with `build-mode: manual`, and the plan required the whole repository, tests included: `_test.go` files are 267 of the 484 tracked Go files and 31 of the 59 in `acceptance/`. CodeQL's Go extractor sees only the files the traced `go` commands compile, and the two ways the plan named to bring in the tests both produced a database with no test file while every command reported success.

## Symptoms

- With `go build ./...` in both modules and `CODEQL_EXTRACTOR_GO_OPTION_EXTRACT_TESTS=true`, the database's `src.zip` held 217 Go files and 0 `_test.go` files.
- With the plan's fallback, `go test -run '^$' ./...` in both modules, the database again held 0 test files, and `codeql database create` exited 0.
- A step written as `go -C acceptance build ./...` extracted none of `acceptance/`.

## What Didn't Work

- **The `extract_tests` option.** It exists in CodeQL 2.27.2, and it works in autobuild, but a traced manual build drops it. Per this run's reading of the Go extractor's source (`go-extractor.go` in the `github/codeql` repository, at its `codeql-cli/v2.27.2` tag), a traced invocation (`--mimic`) ignores the option and extracts tests only when the traced command is itself `go test`.
- **`go test -run '^$'`,** the plan's stated fallback (Deferred to Implementation in `docs/plans/2026-10-09-0511-issue-391-plan.md`). The extractor hands the traced `go test` command's flags to `go list` to find the packages. `go list` rejects `-run` ("flag provided but not defined: -run"), the extractor skips the tests, and nothing fails: the database is created and the analysis runs on the non-test code alone.
- **`go -C <dir>`.** The extractor reads the word after `go` as the subcommand. With `-C` there, it takes the command for a non-build command and skips it.

## Solution

Compile the test binaries with `go test -c`, which takes only build flags and runs no test, from each module's own directory:

```bash
out="$RUNNER_TEMP/codeql-tests"
for module in . acceptance; do
  (
    cd "$module"
    mkdir -p "$out/$module"
    go build ./...
    go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... |
      awk -F/ 'NF { print ++seen[$NF], $0 }' > "$out/$module/packages"
    while read -r batch; do
      mapfile -t packages < <(awk -v batch="$batch" '$1 == batch { print $2 }' "$out/$module/packages")
      go test -c -o "$out/$module/$batch/" "${packages[@]}"
    done < <(cut -d' ' -f1 "$out/$module/packages" | sort -un)
  )
done
```

`go test -c -o dir/` names each binary after its package's last path element, so `cmd/crew` and `internal/crew` would both write `crew.test`. The `awk` counter numbers each package by how many packages before it share its last element, and each batch compiles the packages with one number: no name repeats within a batch. The root module needs 2 batches and `acceptance/` 1. Batched, the build took 1.5 minutes locally; one `go test -c` per package took 4. Since no test runs, no `TestMain` runs and the acceptance suite does not need `CREW_BIN`.

With this build the database held all 484 tracked Go files, the 267 `_test.go` files among them (236 in the root module, 31 in `acceptance/`).

Because a failed test extraction does not fail the database, the job does not trust the build's exit status. `analyze` runs with `upload: never`; a Go-only step then lists the database's `src.zip` and fails unless it holds `acceptance/` sources, `acceptance/` tests and root-module tests; only then does `github/codeql-action/upload-sarif` upload, with the same category. The step failed (exit 1, `::error::`) on the database built with `go build` only and passed on the full one.

## Why This Works

The Go extractor builds its database by watching `go` commands under CodeQL's tracer, and it decides what to extract from the command line it sees. `go test -c` is a `go test` command, so the traced extractor includes test files, and its only flags (`-c`, `-o`) are ones `go list` also accepts, so the package lookup succeeds. Running from the module's directory keeps `test` or `build` as the word right after `go`.

The upload order matters because code scanning compares each analysis with the last one in its category. A SARIF from a database that silently lost the tests would report every alert in test code as fixed. Checking the database before upload means an incomplete extraction fails the job and leaves the previous analysis standing.

## Prevention

- In a CodeQL manual build, check what the database holds, not what the build command returned. The extractor's failures do not reach the exit code of `go` or of `codeql database create`.
- Put no flag on a traced `go test` that `go list` does not accept (`-run`, `-count` and the other flags only `go test` knows). Use `go test -c` to compile tests under the tracer.
- Never write `go -C <dir>` in a traced build; `cd` into the directory in a subshell.
- When a plan names a fallback for a tool's behaviour, verify the fallback on the tool before relying on it: this one was written from the documentation and failed silently.
- When `github/codeql-action` bumps its pinned CodeQL bundle, rebuild a database the workflow's way and count the `_test.go` files in its `src.zip` again: everything above was verified on bundle 2.27.2, locally, from a `git archive` copy of the tree (session history).
- Compiling every test binary locally fills a small `/tmp`; the verification set `GOTMPDIR` under `$HOME/.cache` (session history).
- The check covers only the presence of each kind of file. A batch whose extraction failed alone would still pass it, as long as another batch brought in root-module tests (residual risk in #391's review).

## Related Issues

- #391: the security workflow and its CodeQL jobs; #378 is the parent split.
- `docs/solutions/logic-errors/diffcover-passes-silently-on-mnemonic-diff-prefixes.md`: the same failure class, a gate that exits 0 while checking nothing.
- `docs/solutions/logic-errors/set-e-ignored-left-of-and-in-install-command.md`: the build step keeps its `( cd "$module"; ... )` subshell standalone so `set -e` applies inside it.
