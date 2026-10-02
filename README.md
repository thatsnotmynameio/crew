# crew

crew moves your GitHub issues through a workflow you declare in the repository. It polls for the issues you labeled and runs each stage's actions in parallel, each one a headless Claude Code session in its own git worktree and branch. When every action ends, crew moves the issue to the stage's success label, or to its failure label with a comment saying what failed. You name every label in the workflow: crew has no fixed ones.

crew only runs sessions and moves labels. Opening pull requests, reviewing and merging are your prompts' job and yours.

The [docs](https://docs.page/thatsnotmynameio/crew) are the reference: the [Guide](https://docs.page/thatsnotmynameio/crew/guide/crew) to install, configure and run crew, and the [Develop](https://docs.page/thatsnotmynameio/crew/develop) tab to work on it.

## Quick start

On macOS or Linux, with Go 1.27 or later, and `gh` and `claude` on your `PATH` and logged in:

```sh
go install github.com/thatsnotmynameio/crew/cmd/crew@main
```

Commit a `.crew/config.yaml` that declares your labels and workflow (the Guide has a complete example), then run `crew` in the repository's main checkout:

```sh
crew
```

crew shows a live view of the issues it holds and the sessions it runs; `--plain` prints one line per event instead. You act on issues through their labels on GitHub.

## What's inside

| Path | What it does |
| --- | --- |
| `cmd/crew` | The `crew` binary. |
| `internal/` | crew's engine, its adapters (`github`, `claude`, `git`) and its TUI. See `AGENTS.md` and the Develop tab. |
| `.crew/config.yaml` | crew's own workflow: crew runs on this repository too, with stages for features, bugs, backlog triage, CI audits and learnings, and labels that start with `crew:`. |
| `docs.json`, `docs/` | The docs.page site (Guide and Develop tabs). Plans live in `docs/plans/` and are not published. |
| `STRATEGY.md` | What crew is for, who it serves, and its boundaries. |
| `AGENTS.md` (`CLAUDE.md`) | Instructions for coding agents. |
| `.agents/agents/acceptance-tester.md` | The acceptance tester: writes behavior tests from a plan's acceptance examples, without reading the implementation. See the Guide. |
| `.compound-engineering/` | The Compound Engineering plugin's settings for this repository. |
| `.github/workflows/ci.yml` | Pull requests: `version` (the release rule on `VERSION`), `actionlint`, and `go` (gofmt, vet, lint, tests, govulncheck). |
| `.github/workflows/release.yml` | Pushes to `main`: publishes `VERSION` as `vX.Y.Z` and a GitHub release when it is new. |
| `.github/workflows/docs.yml` | Pull requests: docs.page's check of `docs.json` and `docs/`. |
| `.github/workflows/sonar.yml` | SonarQube Cloud, when the variable `SONAR_ENABLED` is `true`. |
| `.github/workflows/claude.yml` | `@claude` in issues, pull requests and reviews. |
| `.github/dependabot.yml` | Weekly updates of the pinned actions, the shared workflows, the docs CLI and the Go modules. |
| `VERSION` | The version; a pull request that bumps it is a release. |

The workflows call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA: shared behaviour changes there, once.

## Symlinks on Windows

`CLAUDE.md` and `.claude/agents` are symlinks. On Windows, clone with `git clone -c core.symlinks=true` (with Developer Mode on, or as an administrator), or they check out as small text files holding the path. crew itself does not run on Windows.
