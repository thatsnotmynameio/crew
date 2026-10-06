---
title: Go's os.UserConfigDir puts crew's files in Library/Application Support on macOS
date: 2026-10-06
category: integration-issues
module: internal/bots
problem_type: integration_issue
component: bot_store
symptoms:
  - "On macOS, `crew bots create <name>` saves the bot under `~/Library/Application Support/crew/bots`, not `~/.config/crew/bots`"
  - "crew reads bots from `~/Library/Application Support/crew` on macOS, while Linux uses `$XDG_CONFIG_HOME/crew` or `~/.config/crew`"
root_cause: wrong_api
resolution_type: code_fix
severity: medium
tags: [user-config-dir, xdg-config-home, macos, darwin, os-userconfigdir, bots, store, global-config]
---

# Go's os.UserConfigDir puts crew's files in Library/Application Support on macOS

## Problem

crew keeps its user files, the bots' key files and later the global config of #214, in `$XDG_CONFIG_HOME/crew`, else `~/.config/crew`, on Linux and macOS alike. The bots' store built that directory from Go's `os.UserConfigDir()`, which only matches it on Linux. On macOS, `crew bots create` saved bots in `~/Library/Application Support/crew/bots`, and crew read them from there (#215).

## Symptoms

- On macOS, with `XDG_CONFIG_HOME` unset, `crew bots create <name>` finishes, but the bot's file is not under `~/.config/crew/bots`. It is under `~/Library/Application Support/crew/bots`.
- Linux shows nothing wrong, so neither CI (Linux runners) nor the acceptance suite (which sets `HOME` and `XDG_CONFIG_HOME` to empty temporary directories) can see it.

## What Didn't Work

- **Planning the directory as `os.UserConfigDir()`.** `docs/plans/2026-10-03-1605-feat-crew-mates-plan.md` (KTD6, and the store unit's approach) chose `os.UserConfigDir()` and wrote down that it is `~/Library/Application Support` on macOS. `docs/plans/2026-10-04-2304-feat-config-keys-plan.md` (KTD9) kept `<UserConfigDir>/crew/bots`. Both plans still read that way. Plans are not updated after they ship, so this learning is where the reversal is recorded.
- **A test whose expectation came from the function under test.** The old `TestDefaultStoreLivesInTheUserConfigDir` called `os.UserConfigDir()` to compute the path it expected. It would have passed on macOS as well, because the wrong directory was also the expected one.
- **Reproducing on Linux.** On Linux, Go's `os.UserConfigDir` already returns `$XDG_CONFIG_HOME` or `$HOME/.config`. The pre-fix code was correct there, so a regression test that asserts the explicit path passes before the fix on a Linux machine. To see the red run, `DefaultStore` was wired for a moment to a verbatim copy of Go's darwin branch (`$HOME + "/Library/Application Support"`). The tests then failed with the issue's symptom, `Path = .../Library/Application Support/crew/bots/o/n.json, want .../.config/crew/bots/o/n.json`.

## Solution

`DefaultStore` (`internal/bots/store.go:69`) now calls an unexported `configDir(os.Getenv)` (`internal/bots/store.go:80`). It is Go's Linux branch, applied on every OS:

```go
func configDir(getenv func(string) string) (string, error) {
	if dir := getenv("XDG_CONFIG_HOME"); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", errors.New("$XDG_CONFIG_HOME is a relative path")
		}
		return dir, nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", errors.New("neither $XDG_CONFIG_HOME nor $HOME is set")
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("$HOME is a relative path")
	}
	return filepath.Join(home, ".config"), nil
}
```

Linux behaves as before: a relative `XDG_CONFIG_HOME`, or neither variable set, is still an error, which `DefaultStore` wraps as an `EnvError`. The one addition is that a relative `HOME` is an error too, where Go would have used it: the store would otherwise land relative to crew's working directory, which is usually inside the repository (raised in review on #216). Nothing is migrated. A bot saved under `~/Library/Application Support/crew` stays there, and crew no longer reads it, as #215 asked. No released binary is affected, because v0.1.0 predates bots.

## Why This Works

`os.UserConfigDir` follows each platform's convention: `%AppData%` on Windows, `$HOME/Library/Application Support` on darwin, and XDG on other Unix systems (the `runtime.GOOS` switch in `$GOROOT/src/os/file.go`). crew's convention is XDG on every platform it ships for, as with `gh`, whose config directory crew already resolves the XDG way (`loginGhDir`, `internal/bots/gitenv.go:161`). Reading the two variables directly makes the directory independent of `runtime.GOOS`.

## Prevention

- **Never use `os.UserConfigDir()` for crew's own files.** The global config of #214 lives in the same directory (`~/.config/crew/config.yaml`), and its issue is titled "Read a global crew config from the user config dir". Resolve it through the same `$XDG_CONFIG_HOME`, else `$HOME/.config` rule. Once a second package needs it, move `configDir` somewhere both can import, since `internal/bots` is imported only by `cmd/crew`.
- **Assert explicit paths, not the API's answer.** A path test must build its expectation from the environment it set (`HOME`, `XDG_CONFIG_HOME`), never by calling the function the code under test calls. `TestDefaultStoreLivesInDotConfigOnEveryOS` and `TestDefaultStoreLivesInXDGConfigHome` (`internal/bots/store_test.go:167`, `:143`) do that.
- **Make OS-dependent rules testable on Linux.** Take the environment as a `getenv func(string) string` parameter and test the rule as a table (`TestConfigDirIsXDGConfigHomeElseDotConfig`, `internal/bots/store_test.go:180`), so it has no `runtime.GOOS` branch a Linux runner cannot reach. When a fix must be shown red for another OS, copy that OS's branch of the standard library into the seam for one run.
