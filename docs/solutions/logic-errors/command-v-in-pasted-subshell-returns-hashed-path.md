---
title: command -v in the pasted install command returns the shell's hashed path, not the PATH winner
date: 2026-10-07
category: logic-errors
module: README.md
problem_type: logic_error
component: documentation
symptoms:
  - "The install command warns that an old crew in another directory runs instead of ~/.local/bin/crew, although ~/.local/bin comes first on PATH"
  - "A fresh shell runs the new crew, but the warning tells the user to remove the old binary"
root_cause: wrong_api
resolution_type: documentation_update
severity: medium
tags: [install, shell, command-v, hash, path, subshell, posix-sh, readme]
---

# command -v in the pasted install command returns the shell's hashed path, not the PATH winner

## Problem

The install command in the README's Quick start checks whether another `crew` comes before `~/.local/bin` on `PATH`, so a user who installed an older crew into `/usr/local/bin` learns that the old one still runs. The check used `command -v crew`. In a shell that had already run the old crew, it named the old file even when `~/.local/bin` came first, and told the user to remove it.

## Symptoms

- After installing into `~/.local/bin`, which comes first on `PATH`, the command printed `warning: crew runs /usr/local/bin/crew, not ~/.local/bin/crew; remove /usr/local/bin/crew ...`.
- The same command in a new terminal printed no warning.

## What Didn't Work

- Dry runs with a fresh `bash` or `bash --posix` per scenario. A new shell has an empty command hash, so the bug never showed. It shows only when the command is pasted into a shell that already ran `crew`, which is the normal upgrade path.
- Running the dry runs as `sh snippet.sh` to cover dash. On Arch, `/bin/sh` is bash (`readlink -f /bin/sh` prints `/usr/bin/bash`), so that tests bash twice. Neither dash nor zsh was installed, and the Docker socket refused the user, so this work could not exercise either.

## Solution

Clear the hash table right before the lookup, inside the subshell. `README.md` Quick start:

```sh
# Before: may return the path the interactive shell hashed earlier
found=$(command -v crew || true)

# After: the lookup follows PATH order
hash -r 2>/dev/null || true
found=$(command -v crew || true)
```

`|| true` keeps `set -e` from ending the subshell if a shell rejects `hash -r`. The reset only affects the subshell. The user's own shell keeps its hashed old crew until they run `hash -r` or open a new shell.

## Why This Works

The install command is a `( set -eu ... )` block that the user pastes into their interactive shell. A `( )` subshell is a fork of that shell, so it inherits the shell's state, including the table of command paths it has already looked up. bash checks that table before searching `PATH` (zsh hashes commands the same way, though this work could not test it), and `command -v` reports what the shell would run, so it returns the hashed path. Reproduced with bash: `bash -c "hash -p <old>/crew crew; . snippet.sh"`, with `~/.local/bin` first on `PATH`, printed the false warning before the fix and none after.

## Prevention

- In any snippet users paste, treat the subshell as carrying the user's shell state: the hash table, aliases, functions and options. A lookup meant to reflect `PATH` order resets the hash first. An alias or function named `crew` would still make `command -v crew` print it, and the command does not guard against that.
- Simulate the pasted-into shell, not only a fresh one. Seed the state the user has (`hash -p <old>/crew crew`, then source the snippet) as well as running it in a clean `bash`.
- Check which shell `sh` is before calling a run a dash test (`readlink -f /bin/sh`). Where dash is missing, say the run used `bash --posix` instead.
- Keep the fail-fast rules from [the set -e learning](set-e-ignored-left-of-and-in-install-command.md): the subshell stands alone, and a new step that may fail ends in `|| true` only when its failure is expected.

## Related Issues

- #267: the move from `/usr/local/bin` with `sudo` to `~/.local/bin`, whose pull request carries this fix (unmerged as of this writing). Plan: `docs/plans/2026-10-07-1154-docs-install-local-bin-plan.md`.
- #260: a subcommand to update crew (`crew upgrade` in its plan), which will replace the binary in `~/.local/bin` in place.
- `docs/solutions/logic-errors/set-e-ignored-left-of-and-in-install-command.md`: the same install command's fail-fast structure.
