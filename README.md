# crew

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

Commit a `.crew/config.yaml` that declares your agents and rules ([`.crew/config.example.yaml`](.crew/config.example.yaml) lists every key, commented out and explained: copy it and uncomment what you need, and [`schema/config.schema.json`](schema/config.schema.json) gives your editor completion for every key in either config file), then run `crew` in the repository's main checkout:

```sh
crew
```

Keep your own settings, such as `poll_interval_seconds` or a `board`, in `.crew/config.local.yaml` beside it, and have git ignore that file. Each top-level key the local file sets replaces that whole key of `config.yaml`: a local `board` replaces the board, and a local `agents` replaces every agent. A local key with no value, such as `board:`, brings back crew's default for it. Keys the local file leaves out keep their value from `config.yaml`. crew needs at least one of the two files, and either one may hold any key. Every config error names the file its key came from.

crew shows a live view of the issues it holds and the sessions it runs; `--plain` prints one line per event instead. You act on issues through their labels on GitHub. Under the header, Bots shows a card for each bot crew acts as, then one for you: whether it can act, what it cost this run, what acts as it and what runs as it now. Below them, the board has a card for each issue in each of its columns: the issue's reference and title, then `run` (its actions and how long each has run, or its state with none), `bots` (the bots its running actions act as) and `via` (the queue its actions run in). Its last column, Handled, holds a card for each issue crew stopped handling, those needing you first, with why its rule ended. A held issue that no column shows, such as a pull request, gets a card in a Not on board column before it. Queues and Events sit under the board. The board has focus when the view opens, with one card highlighted: ↑↓ move the highlight within a column, ←→ move it between columns, and Enter opens a box over the dimmed view with that issue's rule, labels, kind, priority, whether it is blocked and its URL, its actions with the bot, queue, state and branch of each and the last thing each said or why it failed, and its events; a Handled card's box also shows the issue's cost and the pull request each action opened. In the box ←→ move to the previous or next card, ↑↓ scroll it, and Esc closes it. Tab cycles the board, Bots and Events, `b` and `e` jump to Bots and Events, and Esc returns to the board; while Bots has focus, ←→ scroll its cards when they do not all fit. `?` lists every key.

## Stopping crew

Ctrl+C, or `q` in the live view, stops crew, as SIGINT, SIGTERM and SIGHUP do. crew takes nothing new and asks each running session to stop, giving it up to ten seconds before it kills the session. An action whose session crew stopped fails, so its issue moves to the rule's failure label like any failed action. crew exits once it has judged every issue it held. A second Ctrl+C, `q` or signal does not wait: it kills every process crew started and exits at once. `run_time_limit_seconds` ends a run another way: crew winds down, taking nothing new while its running sessions end on their own.

crew exits 0 after a stop or at its run time limit, 1 when it failed while running or a second stop forced its exit, and 2 on a command line it cannot use or a config or environment error, such as a repository without `.crew/config.yaml`.

## Checks

A check is a shell script you declare under `checks:` and name in an action's `check:`. It runs in the action's worktree after the session succeeded, and decides whether the action succeeded. `check:` takes one name or a list, such as `check: [tests, pr-opened]`. The checks run in that order, each with its own ten minutes. The first that fails, cannot start, runs out of time or is stopped fails the action, and the rest do not run.

A check reads the issue from `CREW_ISSUE_REF`, `CREW_ISSUE_KEY`, `CREW_ISSUE_URL` and `CREW_BRANCH`, the logins from `CREW_CODE_OWNERS` and `CREW_BOTS`, and the action's name from `CREW_ACTION`. `CREW_PROMPT_FILE` names a file with the prompt the session started with, resume note included. `CREW_LAST_MESSAGE_FILE` names a file with the session's last message as it wrote it, which is empty when it ended without one. crew removes both files once the check ended.

The issue's status comment shows each check that ran, with crew's words and the last line the check printed, such as `the check pr-opened failed: no open pull request from <branch>`. Make that last line your reason: crew never shows what the session itself said.

In this repository's own config, the lfg actions first run `session-finished`, which asks TypeSafe's Jev whether the session's last message says it is still waiting on work it started or stopped without doing it. It sends the session's prompt and last message to TypeSafe, whose zero data retention is offered only on its enterprise plan. It needs `TYPESAFE_API_KEY` in crew's environment, and `jq` and `curl` on the `PATH`; without the key it fails the action. When the last message is empty or TypeSafe cannot answer, it passes and says it did not judge.

## TypeSafe judge

The TypeSafe judge is a local service that answers the named [TypeSafe](https://docs.typesafe.ai) questions in `.crew/typesafe.yaml`. It records every ask in `.crew/typesafe/ledger.sqlite`. When a question gets an input it already answered, the judge replays the recorded answer instead of asking TypeSafe again. The judge is optional: crew does not start it or use it yet.

It needs [uv](https://docs.astral.sh/uv/), which installs the Python it runs on. Live answers need `TYPESAFE_API_KEY` in its environment. Without the key, recorded answers still replay, and an input never asked answers "cannot judge" with the reason `no key`.

Start it in crew's main checkout. `--root` names the main checkout of the repository whose `.crew/` holds the bank, never one of crew's worktrees, so that one repository keeps one ledger:

```sh
uv run --project typesafe --locked typesafe-judge serve --root .
```

Its files stay in `.crew/typesafe/`, which crew's `.crew/.gitignore` keeps out of git. It listens on `127.0.0.1`, on a port the system picks. Once ready, it prints one JSON line with `instance`, `pid`, `url`, `ask_token_file` and `admin_token_file`, and writes the same fields to `.crew/typesafe/service.json`. Its logs go to stderr. Read the URL from that file, then check that `GET /v1/health` answers with the same `instance`: after a crash, another process may hold the port. When several instances serve one repository, the file names the latest one started.

Each instance writes two tokens to files only you can read, in `.crew/typesafe/run/<instance>/`. Send one as `Authorization: Bearer <token>`. The ask token allows the first five endpoints below, and the admin token allows them all. Read a token from its file into a header file, never into a command's arguments or an environment variable. `printf` is a shell builtin, so here the token never shows in the process list:

```sh
svc=.crew/typesafe/service.json
headers=$(mktemp)
printf 'Authorization: Bearer %s\n' "$(cat "$(jq -r .ask_token_file "$svc")")" > "$headers"
curl -sS -H @"$headers" "$(jq -r .url "$svc")/v1/ask" --json '{
  "questions": ["issue_needs_candidate"],
  "state": {"issue": "Add the judge port", "candidate": "Serve the judge"},
  "identifiers": {"issue": 204, "candidate": 203}
}'
rm "$headers"
```

| Endpoint | Token | What it does |
| --- | --- | --- |
| `GET /v1/health` | ask | The instance, whether the bank file exists and how many questions it holds, and whether the judge has a key. |
| `GET /v1/questions` | ask | Each question's primitive, model, version, and declared and effective stage. |
| `POST /v1/ask` | ask | Asks `questions` about one `state`, with optional `identifiers` and `fresh`. |
| `GET /v1/asks?question=<name>&identifiers=<JSON>` | ask | The question's asks whose identifiers hold the ones given. |
| `POST /v1/decisions` | ask | Records what you did with an answer: `ask_id` and `decision`. |
| `POST /v1/outcomes` | admin | Records the real answer, on an `ask_id` or on a `question` and `identifiers`: `value`, `source` and `strength`. |
| `POST /v1/rechecks` | admin | Rechecks a question's current version: `question`, and optionally `from_version`. |
| `POST /v1/calibrations` | admin | Calibrates a question's current version: `question`, and optionally `split_key` and `strong_only`. |
| `GET /v1/stages` | admin | What each question's current version has earned. |

An ask's `state` is JSON without floats, holding only what the judgment needs. `identifiers` are yours, a flat object of strings and integers, to find the ask later. Each answer gives its `ask_id`, its `status` (`answered` or `cannot_judge`), whether it was `replayed`, the `verdict` read through the question's current bands, the raw `probabilities`, the model asked and the model that answered, and the declared and effective stage. A "cannot judge" answer adds the `reason`, the `attempts`, the question's `default` and `recorded_answer_exists`. `"fresh": true` asks TypeSafe again and records the new answer, but later asks still replay the first one.

Every success carries `instance` and `bank_error`. A failure is `{"error": {"code": ..., "message": ...}}`: 400 for a bad request, which records nothing, 401 for a missing or wrong token, 403 for the ask token on an admin endpoint, and 413 for a body above 1 MiB.

### The question bank

`.crew/typesafe.yaml` maps each question's name to its definition:

```yaml
questions:
  issue_needs_candidate:
    primitive: noul                    # noul, choice or score
    instructions: Does `issue` need `candidate` to merge first?
    criteria: {true: "issue cannot merge before candidate", false: "issue can merge alone"}  # optional for a noul
    model: jev-1.13.0                  # a pinned version, never jev-latest or jev-preview
    bands: {yes_at: 0.7, no_at: 0.3}   # yes at or above yes_at, no at or below no_at, else uncertain
    default: no                        # the verdict when it cannot judge: yes, no or uncertain
    stage: shadow                      # shadow, confirm or act
    bar: {max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}
```

A `choice` lists its options in `criteria`, and a `score` lists 2 to 10 levels, numbered from 0. Both take one confidence `floor` as `bands` and one `max_error` in `bar`, and their `default` is an option, a level or `uncertain`. The judge rereads the file when it changes. Without the file, the bank is empty. An invalid bank stops the start, and an invalid edit while it runs keeps the last valid bank, with `bank_error` saying what is wrong.

### Stages

A question's stage says what its answers are for: in `shadow` they are recorded and ignored, in `confirm` they go to a person, and in `act` a caller can act on them. The caller decides. Record what you did with `POST /v1/decisions`, as `acted`, `sent_to_person` or `ignored_in_shadow`. The judge accepts a decision against the stage, such as `acted` on a shadow answer, and the stage report counts it. Once you know the real answer, record it with `POST /v1/outcomes`: a `value` (true or false for a noul, an option for a choice, a level for a score), a `source` of your choice, and a `strength`, `strong` or `weak`. Recording the same outcome again adds nothing.

A change to a question's instructions, criteria, model, bands, default or bar makes a new version. The first version the ledger sees keeps its declared stage. A later version is in shadow until it earns its declared stage, through either of:

- A passing recheck, from the trusted version: the latest earlier version that held at least that stage. The recheck compares the two versions' verdicts on the states the earlier one answered, and lists each flip with the decisions on it. It passes under the earlier version's bar, with at least `min_examples` states, a flip rate within `max_flip_rate`, and no state left "cannot judge". A bands-only change rereads the recorded answers, and any other change asks TypeSafe and records the answers.
- A passing calibration of the version itself. It takes the states that have outcomes, splits them into dev and held-out by the identifier `split_key` names (or by state), proposes bands from dev, and tests the current bands on held-out against the bar. Adopting the proposed bands makes a new version, which needs its own pass.

Either one counts for the stage the version declared when it ran, or a lower one. Editing only the stage keeps the version, but raising it needs a passing recheck or calibration for the new stage. The judge never edits the bank: `GET /v1/stages` shows what each question has earned, and its `next_stage` names the stage above when the latest calibration passed. Raising the stage is your edit, reviewed like any other.

The ledger lives in one checkout. A fresh clone, or a renamed question, starts with no history, so its first version keeps its declared stage, and the stage report marks it `declared_on_first_sight`.

### Stopping

SIGTERM, SIGINT (Ctrl-C) and SIGHUP stop the judge. It stops accepting requests, waits up to 190 seconds for asks in flight so that their answers are recorded, then removes its instance's directory and points `service.json` at another running instance, or removes it. A second signal stops it at once.

It exits with crew's codes: 0 after a clean stop, 1 on a failure or a forced stop, and 2 for configuration or environment, such as a root without `.crew/`, an invalid bank, a ledger from a newer judge, or an SQLite older than 3.51.3. Each failure prints one line on stderr.

## What's inside

| Path | What it does |
| --- | --- |
| `cmd/crew` | The `crew` binary. |
| `internal/` | crew's engine, its adapters (`github`, `claude`, `codex`, `git`), its TUI, and `bots` for `crew bots create`. See `AGENTS.md`. |
| `acceptance/` | The acceptance suite, a Go module of its own: it runs the `crew` binary that the release config builds against doubles for `gh` and `claude` on its `PATH`, and checks what crew does on GitHub and on the screen. `go -C acceptance run ./cmd/acceptance`, from the repository root, builds crew and runs the suite. See [`acceptance/README.md`](acceptance/README.md). |
| `typesafe/` | The [TypeSafe judge](#typesafe-judge), a Python project of its own run with uv: the `typesafe-judge` service, its tests and `uv.lock`. |
| `.crew/config.yaml` | crew's own rules: crew runs on this repository too, with rules for features, bugs, refinement of brainstormed features (splitting a large plan into sub-issues, then finding their dependencies, reading in full only the open issues Jev's shortlist names, or every open issue when the shortlist is unavailable) and the hand-offs between them, and labels that start with `crew:`. A split plan's issue stays open as the parts' parent and leaves crew, so crew reports its move to done as given up; that is how a split ends. Keep your own settings in `.crew/config.local.yaml`, which git ignores. |
| `.crew/config.example.yaml` | The reference of every key crew's config accepts, commented out, with what each does and its default. |
| `schema/config.schema.json` | The JSON Schema of `.crew/config.yaml` and `.crew/config.local.yaml`, for editors that complete and explain their keys. |
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
| `.github/workflows/ci.yml` | Pull requests: `version` (the release rule on `VERSION`) and `actionlint`. Pull requests and pushes to `main`: `go` (gofmt, vet, lint, tests, coverage floors, govulncheck), `python` (the judge's ruff, mypy, tests, coverage floors, Lizard's limits and pip-audit), `acceptance` (builds crew with the release config and runs the acceptance suite against it), `codacy` (uploads Go's and Python's coverage to Codacy) and `codacy gate` (repeats Codacy's verdict on a pull request), both when the variable `CODACY_ENABLED` is `true` and skipped for Dependabot. |
| `.github/workflows/codacy-import.yml` | Pushes to `main` that change `.codacy/codacy.config.json`: applies it to Codacy. |
| `.github/workflows/release.yml` | Pushes to `main`: when `VERSION` is new, GoReleaser builds crew and publishes it as `vX.Y.Z`, a GitHub release with the binaries and `checksums.txt`. |
| `.goreleaser.yaml` | What a release builds: crew for macOS and Linux on amd64 and arm64, one archive per platform, and `checksums.txt`. |
| `.github/workflows/claude.yml` | `@claude` in issues, pull requests and reviews. |
| `.github/dependabot.yml` | Weekly updates of the pinned actions, the shared workflows, the Codacy CLIs, the Go modules (crew's and the acceptance suite's) and the judge's Python packages (`uv`, which proposes a release once it is a week old; security updates do not wait). |
| `VERSION` | The version; a pull request that bumps it is a release. |

The workflows call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA: shared behaviour changes there, once.

## Symlinks on Windows

`CLAUDE.md`, `.claude/agents` and `.claude/skills` are symlinks. On Windows, clone with `git clone -c core.symlinks=true` (with Developer Mode on, or as an administrator), or they check out as small text files holding the path. crew itself does not run on Windows.
