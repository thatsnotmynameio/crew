---
title: The install command installs an unverified binary when its subshell sits left of &&
date: 2026-10-03
category: logic-errors
module: README.md, docs/guide/crew.mdx
problem_type: logic_error
component: documentation
symptoms:
  - "sha256sum prints FAILED for the downloaded archive, yet the command extracts and installs crew anyway"
  - "tar reports a corrupt archive and the command still goes on to the next step"
root_cause: logic_error
resolution_type: documentation_update
severity: high
tags: [install, shell, set-e, subshell, checksum, posix-sh, release]
---

# The install command installs an unverified binary when its subshell sits left of &&

## Problem

The install command in `README.md` and `docs/guide/crew.mdx` runs its steps in a `( set -eu ... )` subshell, so that a failed download or checksum stops it before anything is installed. A draft ended the block with `) && crew --version`. With that one change, a failed checksum no longer stopped anything: the command went on and installed the unverified binary.

## Symptoms

- In a dry run against a local GoReleaser snapshot with a tampered archive, `sha256sum -c` printed `crew_linux_amd64.tar.gz: FAILED`, then `tar` complained about the archive, and the command still printed `crew v0.1.0` and exited 0.
- The same block without the trailing `&& crew --version` stopped at the checksum and exited 1.

## What Didn't Work

- Reading the block and trusting `set -eu` on its first line. The block looks fail-fast, and nothing in its own text shows that the line after the closing `)` changes that.
- Testing only the happy path. A good archive installs either way, so the defect shows only when a step fails.

## Solution

Keep the subshell a standalone command and put everything that must run after a successful install inside it:

```sh
# Wrong: set -e is ignored inside the subshell
(
  set -eu
  ...
  grep " $archive\$" checksums.txt | sha256sum -c -
  tar -xzf "$archive" crew
  sudo install -m 0755 crew /usr/local/bin/crew
) && crew --version

# Right: the subshell stands alone, and the version check is its last command
(
  set -eu
  ...
  grep " $archive\$" checksums.txt | sha256sum -c -
  tar -xzf "$archive" crew
  sudo install -m 0755 crew /usr/local/bin/crew
  crew --version
)
```

`README.md:14-38` and `docs/guide/crew.mdx:17-41` carry the fixed block.

## Why This Works

POSIX `sh` ignores `set -e` for any command that is part of an AND-OR list, except the last one, and that holds for every command inside a compound command in that position. `( ... ) && x` puts the whole subshell on the left of `&&`, so a failing `sha256sum -c` inside it no longer ends the subshell. The subshell's exit status is then that of its last command, the install, which succeeds. bash behaves the same way, and zsh's manual states the same exception for its `ERR_EXIT` option. With the subshell standing alone, `set -e` applies to every command inside it again.

## Prevention

- When a pasted command relies on `set -e`, never put its subshell or function call on the left of `&&` or `||`, or inside an `if` or `while` condition. Put follow-up steps inside the subshell instead.
- Test a fail-fast install command on its failure paths, not only on a good download. The checks that caught this were three dry runs against a local GoReleaser snapshot (`goreleaser release --snapshot`), with the download base pointed at `file://`, `sudo` removed and a temporary directory in place of `/usr/local/bin`:
  - a good archive: installs and prints the version;
  - an archive with one byte appended: exits 1 at the checksum and installs nothing;
  - a missing asset: curl exits 37.
- `shellcheck -s sh` does not flag this pattern. The failure-path dry run is the check.

## Related Issues

- #36: the release binaries and the install command this block belongs to.
