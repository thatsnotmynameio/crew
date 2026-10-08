# Changelog

What changed in each version of crew, for the people who use it, newest first. Each version has a section under a `## X.Y.Z` heading, and the Release workflow publishes that section as the version's release text.

## 0.1.1

The first release with crew's prebuilt binaries, for macOS and Linux on amd64 and arm64, with `checksums.txt`. The README's install command downloads it.

- **Rules:** each rule reacts to one label you name in `.crew/config.yaml`. crew runs the rule's actions one after another in one git worktree and branch per run, and each action's verdict either runs the next action or ends the run through one of the rule's routes. A route can comment, post crew's report, run shell steps and functions, then move the issue to another label or close it.
- **Actions:** an action is a headless Claude Code or Codex session, a shell script, or a function built into crew. A failed action resumes in its own worktree.
- **Questions:** a session can wait for answers from the people and agents you trust, and a rule can ask a question, delegate it to the configured answerer and return the answer to the rule that asked.
- **Config:** a global `~/.config/crew/config.yaml` (or under `$XDG_CONFIG_HOME`), then `.crew/config.yaml`, then `.crew/config.local.yaml`, each top-level key replacing the one before it. `.crew/config.example.yaml` documents every key, and `schema/config.schema.json` gives your editor completion.
- **Bots:** `crew bots create <name>` sets up a GitHub App of your own for crew to act as.
- **Live view:** a board you can configure, with a card per issue and a popup with its details, a Bots row, queues and events. Ctrl+P pauses taking new issues and resumes it. `q` or Ctrl+C stops crew only when pressed twice within 3 seconds. `--plain` prints one line per event instead.
- **Scheduling:** crew takes waiting issues by their GitHub Priority first, waits until nothing blocks an issue, runs queues with their own slots, and can stop by itself after `run_time_limit_seconds`.

## 0.1.0

The first tagged version of crew. It was published without binaries.
