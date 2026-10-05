# crew

crew moves your GitHub issues through rules you declare in the repository. Each rule reacts to one label: crew polls for the issues and pull requests that carry it and runs the rule's actions in parallel, each one a headless Claude Code session in its own git worktree and branch. When every action ends, crew moves the issue to the rule's success label, or to its failure label with a comment saying what failed. You name every label in the rules: crew has no fixed ones.

crew only runs sessions and moves labels. Opening pull requests, reviewing and merging are your prompts' job and yours.

## Quick start

On macOS or Linux, on amd64 or arm64, with `gh` and `claude` on your `PATH` and logged in, install the latest release into `/usr/local/bin`. The command checks the download against the release's `checksums.txt`, and `sudo` asks for your password:

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

Commit a `.crew/config.yaml` that declares your agents and rules ([`.crew/config.example.yaml`](.crew/config.example.yaml) is a complete example, and [`schema/config.schema.json`](schema/config.schema.json) gives your editor completion for every key), then run `crew` in the repository's main checkout:

```sh
crew
```

crew shows a live view of the issues it holds and the sessions it runs; `--plain` prints one line per event instead. You act on issues through their labels on GitHub.

## What's inside

| Path | What it does |
| --- | --- |
| `cmd/crew` | The `crew` binary. |
| `internal/` | crew's engine, its adapters (`github`, `claude`, `git`), its TUI, and `bots` for `crew bots create`. See `AGENTS.md`. |
| `.crew/config.example.yaml` | crew's own rules: crew runs on this repository too, with rules for features, bugs, dependency triage of brainstormed features and the hand-offs between them, and labels that start with `crew:`. Copy it to `.crew/config.yaml`, which git ignores, to run crew here. |
| `schema/config.schema.json` | The JSON Schema of `.crew/config.yaml`, for editors that complete and explain its keys. |
| `docs/` | Plans (`docs/plans/`), ideation and documented solutions (`docs/solutions/`). |
| `STRATEGY.md` | What crew is for, who it serves, and its boundaries. |
| `AGENTS.md` (`CLAUDE.md`) | Instructions for coding agents. |
| `.agents/agents/acceptance-tester.md` | The acceptance tester: writes behavior tests from a plan's acceptance examples, without reading the implementation. |
| `.agents/skills/cw-create-issue/` | The `/cw-create-issue` skill: creates an issue with the label and filled template of one of the issue types it lists. This repository's own aid, with its own labels. |
| `.agents/skills/cw-update-issue-plan/` | The `/cw-update-issue-plan` skill: copies the session's plan file into the issue it is working on, and can move it to one of its types' labels. |
| `.agents/skills/cw-brainstorm/` | The `/cw-brainstorm` skill: runs the brainstorm prompt it holds for an issue, in your own session. |
| `.github/ISSUE_TEMPLATE/` | The issue templates of crew's own work, one per kind of work; several labels may share one. |
| `.compound-engineering/` | The Compound Engineering plugin's settings for this repository. |
| `.github/workflows/ci.yml` | Pull requests: `version` (the release rule on `VERSION`) and `actionlint`. Pull requests and pushes to `main`: `go` (gofmt, vet, lint, tests, coverage floors, govulncheck) `codacy` (uploads the coverage to Codacy) and `codacy gate` (repeats Codacy's verdict on a pull request), both when the variable `CODACY_ENABLED` is `true` and skipped for Dependabot. |
| `.github/workflows/codacy-import.yml` | Pushes to `main` that change `.codacy/codacy.config.json`: applies it to Codacy. |
| `.github/workflows/release.yml` | Pushes to `main`: when `VERSION` is new, GoReleaser builds crew and publishes it as `vX.Y.Z`, a GitHub release with the binaries and `checksums.txt`. |
| `.goreleaser.yaml` | What a release builds: crew for macOS and Linux on amd64 and arm64, one archive per platform, and `checksums.txt`. |
| `.github/workflows/claude.yml` | `@claude` in issues, pull requests and reviews. |
| `.github/dependabot.yml` | Weekly updates of the pinned actions, the shared workflows, the Codacy CLIs and the Go modules. |
| `VERSION` | The version; a pull request that bumps it is a release. |

The workflows call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA: shared behaviour changes there, once.

## Symlinks on Windows

`CLAUDE.md`, `.claude/agents` and `.claude/skills` are symlinks. On Windows, clone with `git clone -c core.symlinks=true` (with Developer Mode on, or as an administrator), or they check out as small text files holding the path. crew itself does not run on Windows.
