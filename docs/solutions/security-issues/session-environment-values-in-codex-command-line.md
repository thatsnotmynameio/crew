---
title: Session environment values in the Codex command line are readable by every local process
date: 2026-10-05
category: security-issues
module: internal/adapter/codex
problem_type: security_issue
component: harness_adapter
symptoms:
  - "`ps` or `/proc/<pid>/cmdline` of a running `codex exec` shows `-c shell_environment_policy.set.GH_CONFIG_DIR=\"...\"` and every other session variable with its value"
  - "A command built for an Identity whose Env holds `SECRET_PROBE=s3cret` has an argument containing `s3cret`"
root_cause: wrong_api
resolution_type: code_fix
severity: high
retire_when: "Codex's shell_environment_policy gains a way to keep a variable by name only (a name-only `set`, or an include that runs after exclude without a value); check codex-rs/protocol/src/shell_environment.rs populate_env and the shell snapshot's override keys in codex-rs/core/src/tools/runtimes/mod.rs"
tags: [codex, harness, environment, argv, cmdline, shell-environment-policy, identity, bot, secrets]
---

# Session environment values in the Codex command line are readable by every local process

## Problem

The Codex harness passed each variable it gives a session as a `codex exec` argument, `-c shell_environment_policy.set.NAME="value"`. Any process on the machine, another user's included, can read a process's arguments for as long as it runs, so every value a session's environment held was public on the machine (#198). The Claude harness never had this problem: it sets the same variables in the child's environment only.

## Symptoms

- `ps` shows the bot's `GH_CONFIG_DIR`, the co-author hook's `GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n`, `CREW_CODE_OWNERS` and `CREW_BOTS`, values included, in the `codex exec` command line.
- No secret leaked yet, because a `port.Identity` holds only paths, git config entries and logins (`internal/port/port.go:97-98`). The first secret put in a session's environment would have leaked. Per #198, the judge planned in #188 had to move its token into a file for this reason.

## What Didn't Work

- **Pinning every variable with `set`, as the harness plan chose.** KTD3 and KTD4 of `docs/plans/2026-10-05-1822-feat-codex-harness-plan.md` pinned each value with `-c ...set.NAME=<toml value>` and added a TOML encoder (`internal/adapter/codex/toml.go`, deleted by this fix) to write them. The plan said that seeing the values in `ps` "costs nothing" because none is a token. That holds only until the first value that is a secret, and the harness has no protection of its own. The plan still reads that way.
- **Name-only `set`.** Codex has no such thing. Its `set` table maps a name to the value it inserts, and the shell snapshot restore reads that inserted value back from the live environment, so a `set` key always carries its value on the command line.
- **A per-session profile file** (`codex exec -p <name>` layers `$CODEX_HOME/<name>.config.toml`) would keep `set` without values in argv. It was not taken: it writes into the user's Codex home and needs cleanup across crashes.

## Solution

The values reach Codex only through its process environment: `proc.Command.Env`, which `proc` appends to crew's environment minus `Unset` (`internal/proc/proc.go:273-278`). The arguments carry names only (`internal/adapter/codex/command.go:51-59`):

```go
args = append(args,
	"-c", envPolicy+`.inherit="all"`,
	"-c", envPolicy+".ignore_default_excludes=true",
	"-c", envPolicy+".exclude=[]",
	"-c", envPolicy+".include_only=[]",
)
for _, name := range run.Identity.Unset {
	args = append(args, "-c", envPolicy+".set."+name+`=""`)
}
```

The regression test `TestCommandKeepsTheEnvironmentValuesOutOfItsArguments` (`internal/adapter/codex/command_test.go`) adds `SECRET_PROBE=s3cret` to a bot run and fails if any argument contains it or any other session value.

## Why This Works

Codex 0.154.0 builds the environment of each command it runs in this order (`populate_env` in `codex-rs/protocol/src/shell_environment.rs`; the `codex-rs/` paths here are in the openai/codex repository at tag `rust-v0.154.0`, not in crew):

1. `inherit`: `all` (the default), `core` or `none`.
2. The default excludes `*KEY*`, `*SECRET*` and `*TOKEN*`, unless `ignore_default_excludes` is true. It defaults to true (`codex-rs/config/src/shell_environment_policy.rs`, `unwrap_or(true)`), but a user can turn it off.
3. `exclude`.
4. `set`.
5. `include_only`.

`set` is the only step that keeps a named variable past the user's filters, and it needs the value. Without `set`, a variable survives only if every filter lets it through, so crew resets each filter by name: `inherit="all"`, `ignore_default_excludes=true`, `exclude=[]` and `include_only=[]`. Step 2 matters for crew in particular: `GIT_CONFIG_KEY_n`, which carries the co-author hook and the bot's credential helper, matches `*KEY*`. A `-c` override clears a user's keyed `[shell_environment_policy.filters]` table as well: setting `exclude` or `include_only` from a higher layer drops it. The adversarial reviewer checked this with `codex sandbox`.

The variables crew unsets (`GH_TOKEN`, `GITHUB_TOKEN` and the others) stay as `set.NAME=""`. Their value is empty, so nothing leaks, and `set` keys are the ones Codex's shell snapshot exports again after it sources the user's login-profile snapshot (`build_override_exports` in `codex-rs/core/src/tools/runtimes/mod.rs`). A `GH_TOKEN` exported by the user's profile therefore cannot come back.

Verified on the real binary without a model or a login, through `codex sandbox`, which runs a command under the shell environment policy:

```sh
env GH_CONFIG_DIR=/probe/gh GIT_CONFIG_KEY_1=hook.x GH_TOKEN=leak codex sandbox \
  -c 'shell_environment_policy.inherit="none"' \
  -c 'shell_environment_policy.ignore_default_excludes=false' \
  -c 'shell_environment_policy.exclude=["GH_*"]' \
  -c 'shell_environment_policy.inherit="all"' -c 'shell_environment_policy.ignore_default_excludes=true' \
  -c 'shell_environment_policy.exclude=[]' -c 'shell_environment_policy.include_only=[]' \
  -c 'shell_environment_policy.set.GH_TOKEN=""' -- env
```

Without the last five overrides, none of the variables comes through. With them, `GH_CONFIG_DIR` and `GIT_CONFIG_KEY_1` come through, and `GH_TOKEN` is empty.

### What the fix costs

These were accepted as matching a Claude session, which gets crew's whole environment. They are recorded on the pull request for the code owner to confirm.

- **The user's own Codex filters stop applying in crew sessions.** A user who kept credentials such as `AWS_*` away from Codex's commands with `inherit = "core"`, an `exclude` list or `ignore_default_excludes = false` no longer has that in a crew session. The review reproduced `AWS_SECRET_ACCESS_KEY` reaching a command. Before the fix, crew cleared only `include_only`.
- **crew's own identity variables are no longer re-exported after the snapshot.** A login profile that exports `GH_CONFIG_DIR` or `GIT_CONFIG_COUNT` now wins over the bot's values in Codex's commands, and so does a `shell_environment_policy.set` entry for those names in the user's Codex config. The session would then act as the user without any error. Tokens are still forced empty by name.

## Prevention

- Never put a value in a child process's arguments. Pass names in arguments, and values through the environment or a file only the user can read (mode 0600). Unlike the arguments, a process's environment is readable by the same user only (`/proc/<pid>/environ`), not by other users.
- When a tool's config can filter the environment its own children see, read its filter order in the source before relying on the environment alone. Name-pattern defaults such as `*KEY*` can drop a variable whose name only looks like a secret.
- Test the argument list for the absence of values, with a probe value the test adds itself, as `TestCommandKeepsTheEnvironmentValuesOutOfItsArguments` does. A test that only checks for expected flags would not have caught this.
- `codex sandbox -c ... -- env` is a cheap way to check Codex's environment handling on the real binary. It needs neither a model nor a login.

## Related Issues

- #198: the report. #188: the judge that, per #198, moved its token into a file because of it.
- #173: the Codex harness that introduced the argument-pinned environment.
- `docs/plans/2026-10-05-1822-feat-codex-harness-plan.md`: KTD3, KTD4 and Approach step 2 still describe the reversed design.
- `docs/solutions/security-issues/session-text-in-public-tracker-comments.md`: another surface where session-held text became readable by others.
- `docs/solutions/runtime-errors/nul-in-gh-argument-freezes-status-comment.md`: what a child process's arguments can safely carry.
