---
title: The global config path is one rule kept in two places
date: 2026-10-06
category: design-patterns
module: internal/config, cmd/crew, internal/bots
problem_type: design_pattern
component: config_loader
severity: medium
applies_when:
  - "Changing where crew looks for its global config file, or how it reads XDG_CONFIG_HOME or the home directory"
  - "Changing where crew bots create saves bots, or the bots' configDir"
  - "Adding another user-level file under ~/.config/crew"
  - "Adding a config source to config.Load, or a test that loads crew's config"
  - "Reading the #135 plan's Scope Boundaries, which still rule out a user-wide config file"
tags: [config, global-config, xdg-config-home, user-config-dir, macos, bots, depguard, test-isolation]
---

# The global config path is one rule kept in two places

## Context

Since #214, crew reads three config files: the user's global file, then the repository's `.crew/config.yaml`, then `.crew/config.local.yaml`. Each later file's top-level keys replace the earlier ones (`docs/plans/2026-10-06-1609-feat-global-config-file-plan.md`). The #135 plan (`docs/plans/2026-10-05-2054-feat-config-local-file-plan.md`) still lists "no user-wide config" under its Scope Boundaries. Plans are not updated after they ship, so that line is superseded by #214, not current.

The global file lives in the same directory as crew's bots, and two packages compute that directory. Neither can call the other.

## Guidance

- **One rule decides the directory.** It is `$XDG_CONFIG_HOME` when that variable is set, else `$HOME/.config`, on Linux and macOS alike, and the directory must be absolute. `config.GlobalFile` (`internal/config/files.go:29`) applies it to the global file. `configDir` applies it to the bots in `internal/bots/store.go`, through #216 (fix for #215, open as of this writing). Neither uses `os.UserConfigDir`, which returns `~/Library/Application Support` on macOS. The plan's R6 forbids that path, and #216's learning `docs/solutions/integration-issues/user-config-dir-is-library-application-support-on-macos.md` explains it (not on `main` as of this writing).
- **A relative directory means no global file, not a relative path.** crew runs from inside a repository, so a relative `XDG_CONFIG_HOME` or home would resolve against that repository. `GlobalFile` returns `""` for one, and an unknown home gives `""` too (`internal/config/files.go:34-36`). crew then runs without a global file (R8). It does not fall back from a relative `XDG_CONFIG_HOME` to `~/.config`. #216's `configDir` refuses the same cases with an error. The two differ in how they report the case, not in which directory they accept.
- **Keep the two copies identical; depguard keeps them apart.** `.golangci.yml` lets only `cmd/crew` import `internal/bots`, so `internal/config` cannot reuse `configDir`. #216's learning suggests moving `configDir` somewhere both packages can import once a second one needs it. That takes a layering change, not just a move: `internal/bots` may import only the standard library and `internal/proc` (`AGENTS.md`), so a shared package would need its own depguard rule. A change to one copy must change the other in the same pull request, or the bots and the global file split across two directories on some machines.
- **The loader takes the path; only `cmd/crew` reads the environment.** `config.Load(root, global)` reads the global file at `global`, and `""` reads none (`internal/config/config.go:117`). `cmd/crew` resolves the path once and passes it through `app.Options.GlobalConfig` (`cmd/crew/main.go:135`, `internal/app/app.go:219`). A test that does not pass a path cannot read the developer's real `~/.config/crew/config.yaml`, and it needs no `t.Setenv`. Keep any new config source behind the same kind of parameter.
- **An error must not name a file crew will not read.** When no global path resolves, the missing-config error says crew reads no global file and why, rather than telling the user to create `~/.config/crew/config.yaml` (`internal/config/files.go:80-89`). Code review found the first version suggesting that path even though crew would ignore the file. A user who created it would get the same error again.

## Why This Matters

Without this, the likely regressions are quiet ones. Someone "simplifies" `GlobalFile` to `os.UserConfigDir`, and macOS users' global config moves out from under them. Someone fixes the relative-path case in one package only, and bots and config resolve to different directories. Someone reads the environment inside `config.Load`, and every test on a developer's machine starts loading their personal config. None of these fails a test today: `cmd/crew`'s wiring line is covered only by the acceptance scenarios the tester has yet to write for #214's examples.

## When to Apply

- Before touching `GlobalFile`, bots' `configDir`, or anything else that builds a path under the user's config directory.
- Before adding a config source or a config-loading test.
- When a plan or issue cites #135's "no user-wide config" boundary.

## Examples

```go
// cmd/crew/main.go: the one place the environment is read.
GlobalConfig: config.GlobalFile(os.Getenv("XDG_CONFIG_HOME"), home),

// Tests pass the path, or "" for none.
cfg, err := config.Load(root, "")
cfg, err = config.Load(root, filepath.Join(t.TempDir(), "crew", "config.yaml"))
```

| `XDG_CONFIG_HOME` | home | `GlobalFile` |
|---|---|---|
| `/tmp/x` | `/home/u` | `/tmp/x/crew/config.yaml` |
| unset | `/home/u` | `/home/u/.config/crew/config.yaml` |
| `relative/dir` | `/home/u` | `""` (no global file) |
| unset | `relative/home` or unknown | `""` (no global file) |
