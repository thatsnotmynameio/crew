---
title: Install crew into ~/.local/bin without sudo - Plan
type: docs
date: 2026-10-07
topic: install-local-bin
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #267 (part 1 of 2 of #260)
execution: code
---

# Install crew into ~/.local/bin without sudo - Plan

## Goal Capsule

- **Objective:** someone following the README's Quick start installs crew into a directory they own, without being asked for a password, and learns the line to add when that directory is not on their `PATH`.
- **Means:** the README's install snippet installs into `$HOME/.local/bin` (Key Decision; KTD1 to KTD5).
- **Product authority:** issue #267's Product Contract, from the brainstorm of #260. A notice of newer releases (#263), Windows (#264), release gating (#261) and `crew upgrade` itself (the other part of #260) are not in scope.
- **Stop conditions:** stop and report if the snippet cannot keep its fail-fast behaviour (a bad checksum installs nothing) with the new steps, or if it cannot run unchanged under both `sh` (dash) and bash.
- **Execution profile:** one branch, one pull request that closes #267. Docs only: `README.md` changes, and no Go code.

---

## Product Contract

Product Contract preservation: unchanged. KTD4 adds a warning the contract does not name; it is recorded under Assumptions.

### Summary

The README's install snippet moves from `/usr/local/bin` with `sudo` to `~/.local/bin` without `sudo`. It creates the directory when it is missing, and warns with the line to add when `~/.local/bin` is not on `PATH`.

### Problem Frame

crew publishes a release for every pull request that changes `VERSION`, so an installed crew falls behind quickly. The only way to update today is to run the README's install snippet again. That snippet installs into `/usr/local/bin` with `sudo`, so every update asks for a password, and a later `crew upgrade` could not replace the binary there without `sudo`.

### Key Decisions

- **The README installs into `~/.local/bin`.** This is where Claude Code and uv install, and it makes the in-place replacement writable without `sudo`. (session-settled: user-approved — chosen over `~/.crew/bin`, which always needs a `PATH` line, and over keeping `/usr/local/bin`, where `crew upgrade` would fail on every README install.) Governs R14.

### Requirements

**README**

- R14. The README's install snippet installs into `~/.local/bin` without `sudo`, creates the directory when it is missing, and warns with the line to add when `~/.local/bin` is not on `PATH`.

### Acceptance Examples

- AE1. **Covers R14.** Given no `~/.local/bin`, when the snippet runs, it creates the directory, installs `crew` there with mode 0755 and prints the installed crew's version, without calling `sudo`.
- AE2. **Covers R14.** Given `~/.local/bin` is not on `PATH`, when the snippet finishes, it prints a warning on stderr with the line to add to the shell's startup file, and still exits 0.
- AE3. **Covers R14.** Given `~/.local/bin` is on `PATH` and no other `crew` comes before it, when the snippet finishes, it prints no warning.
- AE4. Given an archive whose checksum does not match, when the snippet runs, it exits non-zero and leaves no `crew` in `~/.local/bin`.

### Scope Boundaries

- A notice at startup or in the live view that a newer release exists, and automatic updates: #263.
- Windows: #264.
- How a release is gated and approved: #261.
- Pre-release or release-candidate channels, and a `--check` flag.
- Calling `sudo`, or moving an existing install out of `/usr/local/bin` automatically.
- Package managers such as Homebrew.
- Considered and not built: editing the user's shell startup file to add `~/.local/bin` to `PATH`. The snippet prints the line instead; which file a shell reads differs by shell and system, and writing to it unasked is invasive. A report that users miss the warning would change the call.
- Considered and not built: authenticating the download. The snippet keeps its unauthenticated `curl`, which fails while the repository is private; R14 does not cover it.

### Deferred to Follow-Up Work

- `crew upgrade` itself (R1 to R13, R15, R16 of #260), in its own issue.

### Sources / Research

- `README.md`, Quick start: the install snippet and the sentence above it that names `/usr/local/bin` and `sudo`.
- `docs/solutions/logic-errors/set-e-ignored-left-of-and-in-install-command.md`: keep the `( set -eu ... )` subshell a standalone command with every follow-up step inside it, and test the snippet on its failure paths with a local archive and a `file://` download base.
- `docs/plans/2026-10-03-0209-feat-release-binaries-plan.md`, KTD8: how the snippet maps OS and architecture and checks `checksums.txt`. Its `/usr/local/bin` choice is the one this plan replaces.
- `.goreleaser.yaml`: version-free archive names (`crew_<os>_<arch>.tar.gz`) and `checksums.txt`.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The directory is `$HOME/.local/bin`, created with `install -d` and no `sudo`.** The snippet sets it once in a variable and uses it for the directory, the install and the checks. It does not read `XDG_BIN_HOME`, which no standard defines; R14 names `~/.local/bin`. Governs R14 (session-settled: user-approved, inherited from the Key Decision).
- KTD2. **The `PATH` check compares `:$PATH:` with `:$HOME/.local/bin:`.** When the directory is missing from `PATH`, the snippet prints to stderr that `~/.local/bin` is not on `PATH` and the line to add to the shell's startup file: `export PATH="$HOME/.local/bin:$PATH"`. It exits 0, because the install succeeded. Governs R14.
- KTD3. **The version check runs the installed file by its full path.** `"$HOME/.local/bin/crew" --version` proves the binary just installed runs, whether or not the directory is on `PATH`, and never runs an older `crew` found elsewhere. It stays the subshell's last install step, inside the subshell, as the learning requires.
- KTD4. **A different `crew` first on `PATH` gets a warning naming it.** When `~/.local/bin` is on `PATH` but `command -v crew` resolves to another file, such as an earlier install in `/usr/local/bin`, the snippet warns on stderr that that file runs instead and can be removed. Without the warning, KTD3 prints the new version while `crew` keeps running the old one, and nothing tells the user. It moves or deletes nothing (Scope Boundaries).
- KTD5. **The new checks cannot trip `set -e`.** A `crew` that `command -v` does not find is a normal outcome here, so its status is consumed by the `if` or `case` that tests it and never ends the subshell.

### Assumptions

- KTD4's warning goes beyond R14's wording. It is a one-line message about a state the change itself creates for anyone who installed with the old snippet; a reviewer can drop it without touching R14.
- The `PATH` match is literal. A `PATH` entry spelled `~/.local/bin` with an unexpanded tilde, or with a trailing slash, counts as missing and gets the warning; the line it prints still works.
- Paste-into-shell is the use: the snippet runs in the user's interactive shell (bash, zsh or a POSIX `sh`) on macOS or Linux, as today.

---

## Implementation Units

### U1. The install snippet installs into ~/.local/bin

- **Goal:** the Quick start installs crew into `~/.local/bin` without `sudo`, and the text above the snippet says so.
- **Requirements:** R14 (Key Decision), KTD1 to KTD5.
- **Dependencies:** none.
- **Files:** `README.md`.
- **Approach:**
  1. Rewrite the sentence before the snippet: it installs the latest release into `~/.local/bin`, checks the download against `checksums.txt`, creates the directory when it is missing and says what to add when it is not on `PATH`. Drop the `sudo` clause.
  2. In the snippet, replace the two `sudo install` lines with the directory variable, `install -d` and `install -m 0755` (KTD1).
  3. Replace `crew --version` with the full-path version check (KTD3), still inside the subshell.
  4. After it, add the `PATH` check (KTD2) and the shadowing check (KTD4), written so neither trips `set -e` (KTD5).
  5. Keep everything else in the snippet as it is: the OS and architecture mapping, the temporary directory and its trap, the checksum step and the standalone subshell.
- **Patterns to follow:** the current snippet's style: POSIX `sh`, short lines, messages to stderr with `>&2`, as its "no build" line does.
- **Execution note:** this is a shell snippet in a doc. Prove it with `shellcheck -s sh` and dry runs against a local archive, not unit tests.
- **Test scenarios:**
  - Covers AE1. `HOME` is an empty temporary directory and the download base points at a `file://` directory holding a good archive and its `checksums.txt`. The snippet creates `$HOME/.local/bin`, installs `crew` there with mode 0755, prints its version and calls no `sudo`.
  - Covers AE2. Same setup, with `PATH` not containing `$HOME/.local/bin`. The snippet prints the `PATH` warning with the `export` line on stderr and exits 0.
  - Covers AE3. `$HOME/.local/bin` exists and is first on `PATH`. The snippet prints no warning and exits 0, and a second run over the installed binary also succeeds.
  - `$HOME/.local/bin` is on `PATH` after another directory holding a different executable `crew`. The snippet warns naming that other file and exits 0, and the other file is untouched.
  - Covers AE4. The archive has one byte appended. The snippet exits non-zero at the checksum and `$HOME/.local/bin/crew` does not exist.
  - Each scenario above runs under dash and bash, and under zsh where it is installed. Confirm the shell under test is really dash, since `/bin/sh` is bash on some systems (Arch among them). When no dash is reachable, such as a container whose `/bin/sh` is dash, run the scenarios under `bash --posix` and say in the pull request that dash was not exercised.
- **Verification:** `shellcheck -s sh` reports nothing on the extracted snippet, and every scenario behaves as written.

---

## Verification Contract

| Check | How | Applies |
|---|---|---|
| Snippet lint | Extract the fenced block from `README.md` and run `shellcheck -s sh` on it | U1 |
| Dry runs | Build `crew` with `go build ./cmd/crew`, pack it as `crew_<os>_<arch>.tar.gz` with a matching `checksums.txt` (or use a GoReleaser snapshot), point a copy of the snippet's download base at that directory with `file://`, set `HOME` to a temporary directory, and run U1's scenarios under each shell U1 names, checking which shell actually ran | U1 |
| Unchanged gates | No Go file changes, so `go test -race ./...`, golangci-lint and the coverage floors are unaffected; CI's `go` job must still pass | PR |

---

## Definition of Done

- The README's Quick start installs into `~/.local/bin` without `sudo`, and its text no longer mentions `sudo` or `/usr/local/bin`.
- Every U1 scenario passed in a dry run, and `shellcheck -s sh` is clean.
- The only edit to the snippet is the install target and the checks after it; the fail-fast structure from the learning is intact.
- No dry-run scripts, archives or temporary files are left in the diff.
