# crew

[![CI](https://github.com/thatsnotmynameio/crew/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/thatsnotmynameio/crew/actions/workflows/ci.yml)
[![Release](https://github.com/thatsnotmynameio/crew/actions/workflows/release.yml/badge.svg)](https://github.com/thatsnotmynameio/crew/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platforms: Linux and macOS](https://img.shields.io/badge/platforms-linux%20%7C%20macOS-lightgrey)](.goreleaser.yaml)

crew moves your GitHub issues through rules you declare in the repository. Each rule reacts to one label: crew polls for the issues and pull requests that carry it and runs the rule's actions in parallel, each one a headless Claude Code or Codex session in its own git worktree and branch. When every action ends, crew moves the issue to the rule's success label, or to its failure label with a comment saying what failed. You name every label in the rules: crew has no fixed ones.

crew only runs sessions and moves labels. Opening pull requests, reviewing and merging are your prompts' job and yours.

## Quick start

On macOS or Linux, on amd64 or arm64, with `gh` and `claude` or `codex` on your `PATH` and logged in, install the latest release into `/usr/local/bin`. The command checks the download against the release's `checksums.txt`, and `sudo` asks for your password:

```sh
(
  set -eu
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case $(uname -m) in
    x86_64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "crew has no build for $(uname -m)" >&2; exit 1 ;;
  esac
  archive="crew_${os}_${arch}.tar.gz"
  url=https://github.com/thatsnotmynameio/crew/releases/latest/download
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  cd "$tmp"
  curl -fsSLO "$url/$archive"
  curl -fsSLO "$url/checksums.txt"
  if command -v sha256sum > /dev/null; then
    grep " $archive\$" checksums.txt | sha256sum -c -
  else
    grep " $archive\$" checksums.txt | shasum -a 256 -c -
  fi
  tar -xzf "$archive" crew
  sudo install -d /usr/local/bin
  sudo install -m 0755 crew /usr/local/bin/crew
  crew --version
)
```

With Go 1.27 or later, `go install github.com/thatsnotmynameio/crew/cmd/crew@vX.Y.Z` builds a release instead, where `vX.Y.Z` is its tag from the [releases page](https://github.com/thatsnotmynameio/crew/releases).

Commit a `.crew/config.yaml` that declares your agents and rules ([`.crew/config.example.yaml`](.crew/config.example.yaml) lists every key, commented out and explained: copy it and uncomment what you need, and [`schema/config.schema.json`](schema/config.schema.json) gives your editor completion for every key in any of crew's config files), then run `crew` in the repository's main checkout:

```sh
crew
```

Keep your own settings, such as `poll_interval_seconds` or a `board`, in `.crew/config.local.yaml` beside it, and have git ignore that file. Each top-level key the local file sets replaces that whole key of `config.yaml`: a local `board` replaces the board, and a local `agents` replaces every agent. A local key with no value, such as `board:`, brings back crew's default for it. Keys the local file leaves out keep their value from `config.yaml`. Either file may hold any key. Every config error names the file its key came from.

Settings you share across repositories, such as your agents, bots or board, can live in one global file outside any repository: `~/.config/crew/config.yaml` on Linux and macOS, or `$XDG_CONFIG_HOME/crew/config.yaml` when `XDG_CONFIG_HOME` is set. crew reads it in every repository it runs in, first: each top-level key of `.crew/config.yaml` replaces that whole key of the global file, and each top-level key of `.crew/config.local.yaml` replaces it in both, so a repository's own files always win. The global file takes every key the other two take, and crew needs at least one of the three, so the global file alone is enough: in a repository with no `.crew/` files, its `rules` act on that repository's issues. A relative `XDG_CONFIG_HOME` or home directory, or none, leaves crew without a global file. Config errors name the global file by its full path.

crew shows a live view of the issues it holds and the sessions it runs; `--plain` prints one line per event instead. You act on issues through their labels on GitHub. Under the header, Bots shows a card for each bot crew acts as, then one for you: whether it can act, what it cost this run, what acts as it and what runs as it now. Below them, the board has a card for each issue in each of its columns: the issue's reference and title, then `run` (its actions and how long each has run, or its state with none: `blocked` when crew does not hold it and an open issue blocks it), `bots` (the bots its running actions act as) and `via` (the queue its actions run in). In each column, the cards of the issues crew holds come first, then the others, each group oldest first. An issue whose rule ended shows only in the columns its labels put it in, and Events says how the rule ended. A held issue that no column shows, such as a pull request, gets a card in a Not on board column after the others. Queues and Events sit under the board. The board has focus when the view opens, with one card highlighted: ↑↓ move the highlight within a column, ←→ move it between columns, and Enter opens a box over the dimmed view with that issue's rule, its labels as chips with a `blocked` chip after them when an open issue blocks it, its kind, priority and URL, its actions with the bot, queue, state and branch of each and the last thing each said or why it failed, and its events. In the box ←→ move to the previous or next card, ↑↓ scroll it, and Esc closes it. Tab cycles the board, Bots and Events, `b` and `e` jump to Bots and Events, and Esc returns to the board; while Bots has focus, ←→ scroll its cards when they do not all fit. `?` lists every key.

## Stopping crew

In the live view, `q` or Ctrl+C stops crew only when pressed twice within 3 seconds, in any mix: the first press only says in the footer that another stops crew, so a stray press costs nothing. When 3 seconds pass without a second press, crew carries on and the next press asks again. With `--plain`, Ctrl+C stops crew at once, as SIGINT, SIGTERM and SIGHUP do. crew takes nothing new and asks each running session to stop, giving it up to ten seconds before it kills the session. An action whose session crew stopped fails, so its issue moves to the rule's failure label like any failed action. crew exits once it has judged every issue it held. Once crew is stopping, whatever started the stop, one more `q` or Ctrl+C in the live view, or a second signal, kills every process crew started and exits at once. `run_time_limit_seconds` ends a run another way: crew winds down, taking nothing new while its running sessions end on their own. A wind-down is not a stop, so stopping it from the live view still takes two presses.

crew exits 0 after a stop or at its run time limit, 1 when it failed while running or a second stop forced its exit, and 2 on a command line it cannot use or a config or environment error, such as a repository without `.crew/config.yaml`, `.crew/config.local.yaml` or a global config file.

## Checks

A check is a shell script you declare under `checks:` and name in an action's `check:`. It runs in the action's worktree after the session succeeded, and decides whether the action succeeded. `check:` takes one name or a list, such as `check: [tests, pr-opened]`. The checks run in that order, each with its own ten minutes. The first that fails, cannot start, runs out of time or is stopped fails the action, and the rest do not run.

A check reads the issue from `CREW_ISSUE_REF`, `CREW_ISSUE_KEY`, `CREW_ISSUE_URL` and `CREW_BRANCH`, the logins from `CREW_CODE_OWNERS` and `CREW_BOTS`, and the action's name from `CREW_ACTION`. `CREW_PROMPT_FILE` names a file with the prompt the session started with, resume note included. `CREW_LAST_MESSAGE_FILE` names a file with the session's last message as it wrote it, which is empty when it ended without one. crew removes both files once the check ended.

The issue's status comment shows each check that ran, with crew's words and the last line the check printed, such as `the check pr-opened failed: no open pull request from <branch>`. Make that last line your reason: crew never shows what the session itself said.

In this repository's own config, the lfg actions first run `session-finished`, which asks TypeSafe's Jev whether the session's last message says it is still waiting on work it started or stopped without doing it. It sends the session's prompt and last message to TypeSafe, whose zero data retention is offered only on its enterprise plan. It needs `TYPESAFE_API_KEY` in crew's environment, and `jq` and `curl` on the `PATH`; without the key it fails the action. When the last message is empty or TypeSafe cannot answer, it passes and says it did not judge.

## A session's task

A coding-agent session crew runs can ask crew what to do next, from the terminal, by its Claude Code or Codex session id:

```sh
crew sessions 0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10 tasks next
```

crew prints the task as one JSON line: its own id, the session's id and a prompt.

```json
{"id":"019a3c51-2b7e-7f10-8c4d-5e6f7a8b9c0d","session_id":"0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10","prompt":"Carry on with the work your session was started with."}
```

For now the captain, which answers, decides nothing: every task carries that same prompt, `tasks current` answers as `tasks next` does, and each answer has a new task id. The session id may be any form of UUID, and crew prints it in its canonical lower-case form; nothing checks that the session exists. The command needs no repository or config, and asks GitHub and the agents nothing. It exits 0 once it printed the task, 2 on a command line it cannot use, such as an id that is not a UUID, and 1 when it failed while running.

## What's inside

| Path | What it does |
| --- | --- |
| `cmd/crew` | The `crew` binary. |
| `internal/` | crew's engine, its adapters (`github`, `claude`, `codex`, `git`), its TUI, `bots` for `crew bots create`, and `captain` for `crew sessions`. See `AGENTS.md`. |
| `acceptance/` | The acceptance suite, a Go module of its own: it runs the `crew` binary that the release config builds against doubles for `gh` and `claude` on its `PATH`, and checks what crew does on GitHub and on the screen. `go -C acceptance run ./cmd/acceptance`, from the repository root, builds crew and runs the suite. See [`acceptance/README.md`](acceptance/README.md). |
| `.crew/config.yaml` | crew's own rules: crew runs on this repository too, with rules for features, bugs, refinement of brainstormed features (splitting a large plan into sub-issues, then finding their dependencies, reading in full only the open issues Jev's shortlist names, or every open issue when the shortlist is unavailable) and the hand-offs between them, and labels that start with `crew:`. A split plan's issue stays open as the parts' parent and leaves crew, so crew reports its move to done as given up; that is how a split ends. Keep your own settings in `.crew/config.local.yaml`, which git ignores. |
| `.crew/config.example.yaml` | The reference of every key crew's config accepts, commented out, with what each does and its default. |
| `schema/config.schema.json` | The JSON Schema of `.crew/config.yaml`, `.crew/config.local.yaml` and the global `~/.config/crew/config.yaml`, for editors that complete and explain their keys. |
| `docs/` | Plans (`docs/plans/`), ideation and documented solutions (`docs/solutions/`). |
| `STRATEGY.md` | What crew is for, who it serves, and its boundaries. |
| `AGENTS.md` (`CLAUDE.md`) | Instructions for coding agents. |
| `.agents/agents/acceptance-tester.md` | The acceptance tester: writes behavior tests from a plan's acceptance examples, without reading the implementation. |
| `.agents/skills/cw-create-issue/` | The `/cw-create-issue` skill: creates an issue with the label and filled template of one of the issue types it lists. This repository's own aid, with its own labels. |
| `.agents/skills/cw-update-issue-plan/` | The `/cw-update-issue-plan` skill: copies the session's plan file into the issue it is working on, and can move it to one of its types' labels. |
| `.agents/skills/cw-brainstorm/` | The `/cw-brainstorm` skill: runs the brainstorm prompt it holds for an issue, in your own session. |
| `.agents/skills/cw-split-plan/` | The `/cw-split-plan` skill, which crew's refinement rule runs: measures an issue's plan with `measure.sh` and splits one above 10,000 characters or 12 requirements into sub-issues that each merge alone. This repository's own aid. |
| `.agents/skills/cw-rank-blockers/` | The `/cw-rank-blockers` skill, which crew's refinement rule runs: its `rank.sh` asks Jev about each open issue crew's code owners and bots opened, and prints the 5 most likely to block an issue and the 5 it most likely blocks. Its `backtest.sh` replays `rank.sh` against the `blocked_by` links GitHub records and reports how often the real dependency made the top 5. Both record nothing. This repository's own aid. |
| `.agents/skills/cw-tester/` | The `/cw-tester` skill: makes the session a black-box tester of crew's binary for an area, writing acceptance scenarios from this README and `crew --help`, never from crew's code, and reporting what fails and what the README leaves undocumented. This repository's own aid. |
| `.github/ISSUE_TEMPLATE/` | The issue templates of crew's own work, one per kind of work; several labels may share one. |
| `.compound-engineering/` | The Compound Engineering plugin's settings for this repository. |
| `.github/workflows/ci.yml` | Pull requests: `version` (the release rule on `VERSION`) and `actionlint`. Pull requests and pushes to `main`: `go` (gofmt, vet, lint, tests, coverage floors, govulncheck), `acceptance` (builds crew with the release config and runs the acceptance suite against it), `codacy` (uploads the coverage to Codacy) and `codacy gate` (repeats Codacy's verdict on a pull request), both when the variable `CODACY_ENABLED` is `true` and skipped for Dependabot. |
| `.github/workflows/codacy-import.yml` | Pushes to `main` that change `.codacy/codacy.config.json`: applies it to Codacy. |
| `.github/workflows/release.yml` | Pushes to `main`: when `VERSION` is new, GoReleaser builds crew and publishes it as `vX.Y.Z`, a GitHub release with the binaries and `checksums.txt`. |
| `.goreleaser.yaml` | What a release builds: crew for macOS and Linux on amd64 and arm64, one archive per platform, and `checksums.txt`. |
| `.github/workflows/claude.yml` | `@claude` in issues, pull requests and reviews. |
| `.github/dependabot.yml` | Weekly updates of the pinned actions, the shared workflows, the Codacy CLIs and the Go modules (crew's and the acceptance suite's). |
| `VERSION` | The version; a pull request that bumps it is a release. |

The workflows call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA: shared behaviour changes there, once.

## Symlinks on Windows

`CLAUDE.md`, `.claude/agents` and `.claude/skills` are symlinks. On Windows, clone with `git clone -c core.symlinks=true` (with Developer Mode on, or as an administrator), or they check out as small text files holding the path. crew itself does not run on Windows.
