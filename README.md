# crew

[![CI](https://github.com/thatsnotmynameio/crew/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/thatsnotmynameio/crew/actions/workflows/ci.yml)
[![Release](https://github.com/thatsnotmynameio/crew/actions/workflows/release.yml/badge.svg)](https://github.com/thatsnotmynameio/crew/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platforms: Linux and macOS](https://img.shields.io/badge/platforms-linux%20%7C%20macOS-lightgrey)](.goreleaser.yaml)

crew moves your GitHub issues through rules you declare in the repository. Each rule reacts to one label: crew polls for the issues and pull requests that carry it, moves each to the rule's running label and runs the rule's actions one after another, in one git worktree and branch per run. An action is a headless Claude Code or Codex session, or a shell script. Each action ends with a verdict, which either runs the next action or ends the run through one of the rule's routes. A route may comment on the issue, post crew's report and run scripts, then moves the issue to another label or closes it. You name every label in the rules: crew has no fixed ones.

crew only runs sessions and scripts, comments on issues, moves their labels and closes them. Opening pull requests, reviewing and merging are your prompts' job and yours.

## Quick start

On macOS or Linux, on amd64 or arm64, with `gh` and `claude` or `codex` on your `PATH` and logged in, install the latest release into `~/.local/bin`, without `sudo` or a password. The command checks the download against the release's `checksums.txt` and creates `~/.local/bin` when it is missing. When `~/.local/bin` is not on your `PATH`, it prints the line to add to your shell's startup file; when another `crew` comes first on your `PATH`, such as one installed in `/usr/local/bin`, it names that file:

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
  bin="$HOME/.local/bin"
  install -d "$bin"
  install -m 0755 crew "$bin/crew"
  "$bin/crew" --version
  case ":$PATH:" in
    *":$bin:"*)
      hash -r 2>/dev/null || true
      found=$(command -v crew || true)
      if [ -n "$found" ] && [ "$found" != "$bin/crew" ]; then
        echo "warning: crew runs $found, not $bin/crew; remove $found to run the crew just installed" >&2
      fi
      ;;
    *)
      echo "warning: $bin is not on your PATH; add this line to your shell's startup file, such as ~/.profile, ~/.bashrc or ~/.zshrc:" >&2
      echo "  export PATH=\"\$HOME/.local/bin:\$PATH\"" >&2
      ;;
  esac
)
```

With Go 1.27 or later, `go install github.com/thatsnotmynameio/crew/cmd/crew@vX.Y.Z` builds a release instead, where `vX.Y.Z` is its tag from the [releases page](https://github.com/thatsnotmynameio/crew/releases).

Commit a `.crew/config.yaml` that declares your agents and rules ([`.crew/config.example.yaml`](.crew/config.example.yaml) lists every key, commented out and explained: copy it and uncomment what you need, and [`schema/config.schema.json`](schema/config.schema.json) gives your editor completion for every key in any of crew's config files), then run `crew` in the repository's main checkout:

```sh
crew
```

Keep your own settings, such as `poll_interval_seconds` or a `board`, in `.crew/config.local.yaml` beside it, and have git ignore that file. Each top-level key the local file sets replaces that whole key of `config.yaml`: a local `board` replaces the board, and a local `agents` replaces every agent. A local key with no value, such as `board:`, brings back crew's default for it. Keys the local file leaves out keep their value from `config.yaml`. Either file may hold any key. Every config error names the file its key came from.

Settings you share across repositories, such as your agents, bots or board, can live in one global file outside any repository: `~/.config/crew/config.yaml` on Linux and macOS, or `$XDG_CONFIG_HOME/crew/config.yaml` when `XDG_CONFIG_HOME` is set. crew reads it in every repository it runs in, first: each top-level key of `.crew/config.yaml` replaces that whole key of the global file, and each top-level key of `.crew/config.local.yaml` replaces it in both, so a repository's own files always win. The global file takes every key the other two take, and crew needs at least one of the three, so the global file alone is enough: in a repository with no `.crew/` files, its `rules` act on that repository's issues. A relative `XDG_CONFIG_HOME` or home directory, or none, leaves crew without a global file. Config errors name the global file by its full path.

crew shows a live view of the issues it holds and the sessions it runs; `--plain` prints one line per event instead. You act on issues through their labels on GitHub. Under the header, Bots shows a card for each bot crew acts as, then one for you: whether it can act, what it cost this run, what acts as it and what runs as it now. Below them, the board has a card for each issue in each of its columns. Without a `board` in the config, it has one column per rule that has actions, holding the rule's ready and running labels, and a column of its own for each label where the route of a `waiting` verdict leaves the issue, so an issue paused there stays on screen. A card shows the issue's reference and title, then `run` (the action its run is on and how long it has run, how many of the rule's actions are left after it, and the route the run ends through once it chose one, or its state with none: `blocked` when crew does not hold it and an open issue blocks it), `bots` (the bots its running actions act as) and `via` (the queue its actions run in). In each column, the cards of the issues crew holds come first, then the others, each group oldest first. An issue whose rule ended shows only in the columns its labels put it in, and Events says how the rule ended. A held issue that no column shows, such as a pull request, gets a card in a Not on board column after the others. Queues and Events sit under the board. The board has focus when the view opens, with one card highlighted: ↑↓ move the highlight within a column, ←→ move it between columns, and Enter opens a box over the dimmed view with that issue's rule, its labels as chips with a `blocked` chip after them when an open issue blocks it, its kind, priority and URL, its actions with the bot, queue, state and branch of each and the last thing each said or why it failed, and its events. In the box ←→ move to the previous or next card, ↑↓ scroll it, and Esc closes it. Tab cycles the board, Bots and Events, `b` and `e` jump to Bots and Events, and Esc returns to the board; while Bots has focus, ←→ scroll its cards when they do not all fit. `?` lists every key.

## Rules

A rule takes the items that carry its `ready` label, issues by default or pull requests when its `takes` says so, and moves each to its `running` label. It then runs its `actions` one at a time, in the order listed, and ends the run through one of its `routes`:

```yaml
actions:
  tests: go test ./...
  pr-opened: |-
    n=$(gh pr list --head "$CREW_BRANCH" --state open --json number --jq length) || exit 1
    [ "$n" -gt 0 ] || { echo "no open pull request from $CREW_BRANCH"; exit 1; }

rules:
  development:
    labels:
      ready: ready
      running: in progress
    actions:
      - agent: developer
        name: implement
        prompt: |-
          Implement {{.Issue.Ref}}. Open a pull request whose body has the line `Closes {{.Issue.Ref}}`.
        on:
          blocked: blocked
      - tests
      - pr-opened
    routes:
      passed: in review
      failed:
        - report
        - move: failed
      blocked:
        - comment: "{{.Issue.Ref}} is blocked: `{{.Action}}` ended with `{{.Verdict}}`."
        - move: blocked
```

An action is a session or a shell action. A session is written in the rule, with its `prompt` and the `agent` that runs it, which you can leave out when `agents` declares only one. Its prompt is a Go template over the item: `{{.Issue.Ref}}`, `{{.Issue.Key}}`, `{{.Issue.Title}}` and `{{.Issue.URL}}`. A session is named after its agent unless its `name` says otherwise, and no two actions of a rule share a name. A shell action is defined once, under the top-level `actions`, and a rule names it in its list: by its name alone, or as a key left empty, with `on` and `name` beside it. All the actions of a run share one git worktree and branch, `issue-<number>-<rule>`, so each sees what the ones before it left. crew creates the worktree only for a rule with actions.

Every action ends with a verdict. A session that succeeds gives `passed` and one that fails gives `failed`. It may instead end with one of the verdicts its `on` names, by writing it on the first line of the file that `CREW_VERDICT_FILE` names; crew's prompt tells it how, and a first line that is no verdict counts as `failed`. A shell action passes when it exits 0 and fails otherwise, unless its definition's `verdicts` names the verdict of its exit status. A verdict other than `passed`, `failed` and `waiting` that the action's `on` does not name counts as `failed`. A stop, a session or script that cannot start and a prompt that does not render always give `failed`. A verdict's name is a lowercase letter, then lowercase letters, digits, `-` or `_`.

An action's `on` maps each verdict to `next`, which runs the next action, or to one of the rule's routes, which ends the run there: the actions after it do not run. Without an entry, `passed` leads to `next` and every other verdict to `failed`. When the last action's verdict leads to `next`, the run ends through `passed`.

A rule with actions declares the routes `passed` and `failed`, and every route an `on` leads to. A route is a label to move the item to, or a list of steps that run in order:

- `report` posts crew's report on the issue: the action that ended the run, its verdict, the route and the action's log.
- `comment` posts a comment, a Go template that may name `{{.Issue.Ref}}`, `{{.Issue.Key}}`, `{{.Issue.Title}}`, `{{.Issue.URL}}`, `{{.Rule}}`, `{{.Action}}` (the action that ended the run), `{{.Verdict}}`, `{{.Route}}` and `{{.Log}}`, and nothing else.
- The name of a shell action of `actions` runs it, as a step.
- `move` moves the item to a label. `close` closes the issue, takes crew's labels off it and off its pull requests, which stay open, and posts the stop comment on each of them.

The last step, and only it, is a `move` or a `close`. A step that fails shows on the issue's status comment, and the route goes on, so the final move or close still happens. Neither a comment nor a report ever carries what a session or a script printed. A rule without actions declares only `passed`: crew runs it as soon as it takes the item, without a worktree or a session.

crew refuses a config, naming the file, the key and its line, when an `on` names a route the rule does not declare, when nothing leads to a route other than `passed` and `failed`, when a route does not end with `move` or `close` or has one before its last step, when a route moves the item to the rule's own `ready` label or to any rule's `running` label, when two actions of a rule share a name, or when a route is named `next`. A top-level action cannot take a word of the rules' own, such as `agent`, `prompt`, `on`, `move` or `next`, as its name. crew also refuses to start, with exit status 2, when a route comments on or closes issues and the tracker cannot, or when a session may wait for an answer and the tracker cannot list an issue's comments, naming the rule and the action.

When an issue returns to a rule's `ready` label and that rule's last run on it ended through any route other than `passed`, or chose one and never finished it, crew resumes that run: it reopens the run's worktree and starts at the action that ended it. The actions before it do not run again. When that action is a shell action that judged a session before it, crew starts at that session instead, unless the shell action's definition says `resume: self`, as one that checks something outside the worktree, such as CI, would. A resumed session is a new session, whose prompt tells it that it continues an earlier run, which route that run ended through and where its log is. A run that crashed during an action resumes at that action. When the last run chose `passed` but its final move or close never landed, crew runs only the `passed` route again, in that run's worktree when it still exists. When the worktree is gone, crew starts over at the first action in a new one. crew never resumes on its own: only the `ready` label going back on the issue does. Runs that failed under a crew release without routes start over in a new worktree.

A session can ask a question on the issue and wait for an answer. Map `waiting` in its `on`, usually to a route that moves the issue to a label of its own, and set how long it waits with `wait`:

```yaml
rules:
  development:
    labels:
      ready: ready
      running: in progress
    actions:
      - agent: developer
        name: implement
        wait: 30m
        prompt: |-
          Implement {{.Issue.Ref}}. Ask on the issue when a decision is not yours to make.
        on:
          waiting: waiting
    routes:
      passed: in review
      failed:
        - report
        - move: failed
      waiting:
        - comment: "{{.Issue.Ref}} waits for an answer. Answer on the issue, then move it back to `ready`."
        - move: waiting for answer
```

crew's prompt tells such a session how to ask and how to wait. The session asks one question as one comment on the issue, with its own hidden marker in it, `<!-- crew:session run=<run id> action=<action> -->`, and writes `waiting` in the file `CREW_VERDICT_FILE` names as soon as it posts. It then waits up to its `wait`, a Go duration such as `30m` or `1h30m`, or 10 minutes when left out, through short checks of the issue's comments. It reads them only with the one `gh api` command crew writes into its prompt, which prints only the comments that count as answers, so no one else's text reaches the session while it waits. Once an answer counts, the session replaces `waiting` in the file, or empties it, and goes on with the work. When none came, it checks once more and ends with `waiting`, and the run ends through the route `waiting` leads to. Each check is one command of at most 5 minutes. crew raises Claude Code's command timeout above that; a Codex session is asked to set its tool's timeout itself, which has not been tried yet.

Only the code owners, the logins sessions get as `CREW_CODE_OWNERS`, and the Apps on the top-level `answering_apps` list may answer. Without `answering_apps`, that list is crew's bots; a list you write replaces them, and `[]` lets no App answer. Each entry is an App's login, `<slug>[bot]`, such as `claude[bot]`. crew refuses any other entry, and `github-actions[bot]` in any case, since any workflow can post anyone's text as it. The asking session's own login is left off the list. Logins match ignoring case. A comment by a code owner counts only when GitHub does not mark its author as an App, and one by a listed App only when it does. crew ignores a comment by anyone else and reports it nowhere. A comment that holds `<!-- crew:` anywhere never counts, and crew puts its own hidden marker, `<!-- crew:posted -->`, on every comment it posts: its reports, route comments, status comments and stop comments on pull requests.

crew does not watch the issue for answers. Whoever answers moves the issue back to the rule's `ready` label, and crew resumes the run. Before a session starts at an action where an earlier session may have asked a question, crew reads the issue's comments. It finds the question by that session's marker and login, and hands the new session the answers that count, newest first and each whole, up to 32 KiB, with how many older ones it left out on the issue. When no answer came after the question, the prompt says so. The answers go only into the prompt, and the prompt file crew keeps beside the run's log for the shell actions after the session, never into the run journal, a comment or the status comment. When crew cannot read the comments, the prompt says so and gives the session the filtered command to read them with. A question stays open from run to run until a later session at that action succeeds, whatever verdict it then gives; one that ends with `waiting` again leaves its own new question open. A run that ended through `passed` and finished its route leaves no question open.

## Stopping crew

In the live view, `q` or Ctrl+C stops crew only when pressed twice within 3 seconds, in any mix: the first press only says in the footer that another stops crew, so a stray press costs nothing. When 3 seconds pass without a second press, crew carries on and the next press asks again. With `--plain`, Ctrl+C stops crew at once, as SIGINT, SIGTERM and SIGHUP do. crew takes nothing new, starts no other action, and asks each running session and script to stop, giving it up to ten seconds before it kills it. An action crew stopped gives `failed`, and every run whose action ends while crew stops ends through its `failed` route. In the route of a stopping run, crew stops a running shell step, skips the shell steps that have not started and shows them as skipped, and gives each move, close, comment and report its final try. A rule without actions still ends through `passed`. crew exits once every run it held has ended. Once crew is stopping, whatever started the stop, one more `q` or Ctrl+C in the live view, or a second signal, does not wait: it kills every process crew started and exits at once.

`run_time_limit_seconds` ends crew another way. crew takes nothing new, lets each running action finish and starts no other. A run whose action then leads to the next action ends through `failed` instead; one whose action leads to a route ends through that route. Every route runs all its steps, shell steps included, before crew exits. A tracker write that keeps failing does not keep crew past the limit: crew then stops as above. This wind-down is not a stop, so stopping it from the live view still takes two presses.

crew exits 0 after a stop or at its run time limit, 1 when it failed while running or a second stop forced its exit, and 2 on a command line it cannot use or a config or environment error, such as a repository without `.crew/config.yaml`, `.crew/config.local.yaml` or a global config file.

## Shell actions

A shell action is a script you define once under the top-level `actions`, by name, and name in a rule's `actions` or in a route's steps. Its definition is the script itself, or a mapping with its `script`, the `verdicts` its exit statuses give, such as `3: needs_person`, and `resume: self`. It runs with `sh -c` in the run's worktree, with ten minutes to finish, or in an empty temporary directory when the run has no worktree, as in a rule without actions. Its output goes to the run's log, `.crew/logs/<worktree>.log`, after a line naming it.

A shell action reads the issue from `CREW_ISSUE_REF`, `CREW_ISSUE_KEY`, `CREW_ISSUE_URL` and `CREW_BRANCH`, and the logins from `CREW_CODE_OWNERS` and `CREW_BOTS`. `CREW_ACTION` names the run's latest session, so a script that judges a session knows which one. `CREW_PROMPT_FILE` names a file with the prompt that session started with, resume note included, and `CREW_LAST_MESSAGE_FILE` a file with its last message as it wrote it, empty when it ended without one. crew keeps both beside the run's log, as `.crew/logs/<worktree>.prompt` and `.crew/logs/<worktree>.last-message`, so a script resumed after a restart still judges the same session, and hands each script its own copies, which it removes once the script ended. A resumed run starts with the latest session of the run it resumes. In a fresh run, before its first session, `CREW_ACTION` and both files are empty. A shell action acts on GitHub as the latest session's bot, and before any session as crew's own writes do: as `tracker.bot`, or as you. `CREW_COMMENT_MARKER` holds crew's hidden marker, `<!-- crew:posted -->`. A script that comments on the issue should put it in its comments: otherwise a comment it posts as a code owner or as a listed App counts as an answer to a waiting session's question.

A shell action passes when it exits 0 and fails on any other status, unless its `verdicts` names that status. One that cannot start, runs out of time or is stopped fails. The issue's status comment shows each shell action that ran, with crew's words and the last line it printed, such as `the shell action pr-opened exited with status 1: no open pull request from <branch>`. Make that last line your reason: crew never shows what a session itself said. A shell step of a route shows only crew's words.

In this repository's own config, the lfg sessions are followed by `session-finished`, which asks TypeSafe's Jev whether the session's last message says it is still waiting on work it started or stopped without doing it, and fails if so. When the session did its part but a person must act before its result can be used, it exits 3, which its definition maps to the verdict `needs_person`, and the run ends through the `needs-person` route, which comments on the issue and moves it to `crew:<rule>:needs person`. It sends the session's prompt and last message to TypeSafe, whose zero data retention is offered only on its enterprise plan. It needs `TYPESAFE_API_KEY` in crew's environment, and `jq` and `curl` on the `PATH`; without them it fails. When the last message is empty or TypeSafe cannot answer, it passes and says it did not judge.

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
| `.github/workflows/ci.yml` | Pull requests: `version` (the release rule on `VERSION`) and `actionlint`. Pull requests and pushes to `main`: `go` (gofmt, vet, lint, tests, coverage floors, govulncheck), `codacy` (uploads the coverage to Codacy) and `codacy gate` (repeats Codacy's verdict on a pull request), both when the variable `CODACY_ENABLED` is `true` and skipped for Dependabot. The `acceptance` job is temporarily commented out because the suite is too slow; it can still be run locally. |
| `.github/workflows/codacy-import.yml` | Pushes to `main` that change `.codacy/codacy.config.json`: applies it to Codacy. |
| `.github/workflows/release.yml` | Pushes to `main`: when `VERSION` is new, GoReleaser builds crew and publishes it as `vX.Y.Z`, a GitHub release with the binaries and `checksums.txt`. |
| `.goreleaser.yaml` | What a release builds: crew for macOS and Linux on amd64 and arm64, one archive per platform, and `checksums.txt`. |
| `.github/workflows/claude.yml` | `@claude` in issues, pull requests and reviews. |
| `.github/dependabot.yml` | Weekly updates of the pinned actions, the shared workflows, the Codacy CLIs and the Go modules (crew's and the acceptance suite's). |
| `VERSION` | The version; a pull request that bumps it is a release. |

The workflows call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA: shared behaviour changes there, once.

## Symlinks on Windows

`CLAUDE.md`, `.claude/agents` and `.claude/skills` are symlinks. On Windows, clone with `git clone -c core.symlinks=true` (with Developer Mode on, or as an administrator), or they check out as small text files holding the path. crew itself does not run on Windows.
