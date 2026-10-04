# AGENTS.md

Guidance for coding agents working in this repository. Claude Code reads it as `CLAUDE.md`, a symlink to this file.

crew is a Go program that polls a tracker (GitHub) and moves each issue through the workflow a repository declares in `.crew/config.yaml`, running one coding-agent session (Claude Code) per action in its own git worktree. Its user is the boss, running it in their own repositories. The docs site is the reference: `docs/guide/crew.mdx` for users, `docs/develop/` for contributors.

## Commands

Run every command from the repository root. Go 1.27 (`go.mod`).

```sh
go build ./cmd/crew   # the binary, at the root (ignored by git)
go test -race ./...   # every test; one package: go test -race ./internal/core; one test: add -run TestName
gofmt -l cmd internal tools # prints the unformatted files; must print nothing
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run   # lint + layering (depguard)
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...   # coverage profile
go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml   # total >= 90%
git diff -U0 origin/main...HEAD | go run ./tools/diffcover -profile coverage.out   # changed lines >= 90%
pnpm install          # once: the docs.page CLI and the Codacy CLIs
pnpm exec codacy-analysis analyze --install-dependencies   # Codacy's Lizard, Opengrep, Trivy, Checkov
pnpm docs:check       # the docs site's links and MDX
pnpm docs:preview     # live preview of the docs
```

- **golangci-lint:** run it through `go run` at v2.14.0, as CI does. A local install older than v2.13.0 cannot lint a `go 1.27` module.
- **CI:** the `go` job in `.github/workflows/ci.yml` runs gofmt, vet, golangci-lint, `go test -race` with coverage, both coverage floors and govulncheck. Its `codacy` job uploads the coverage to Codacy, which analyses the code on its own servers.
- **Quality bar:** zero findings, everywhere. `.golangci.yml` turns on every linter except those it lists with a reason; Codacy's tools and limits are in `.codacy/codacy.config.json`; `docs/develop/quality.mdx` says which tool owns which finding.

## Architecture

Ports and adapters with a pure core; details in `docs/develop/architecture.mdx`.

- `cmd/crew`: flags, signals, the repository root; builds the `git` workspace, the `shell` checker and `app.Options.Mates` (through `internal/mates`) and calls `app.Run`. `crew mates create <name>` is chosen before the flags and runs the `internal/mates` flow instead; `crew worktrees clean` is chosen the same way and goes through `app.Clean`.
- `internal/app`: config, registry, engine, renderer, stop signals, exit codes (0 clean, 1 failure or forced, 2 config or environment); `Clean` wires `crew worktrees clean`: config, tracker (never prepared, no mates), workspace, the journal's unended runs through `engine.UnendedRuns`.
- `internal/crew`: the domain (states, issues, stages, actions, outcomes, failure reports, statuses).
- `internal/config`: `.crew/config.yaml`, strict decoding, engine defaults, workflow checks; hands each adapter its section as a `port.Decode`.
- `internal/port`: `Tracker`, `Harness`, `Workspace`, `Checker`, `Identity`, the optional `Preparer`, `StatusReporter`, `PullRequestReporter`, `Acting`, `BossFinder`, `Narrator`, `Reopener`, `UsageReporter`, `PullRequestFinder` and `Sweeper`, sentinel errors, factory types.
- `internal/registry`: name to factory; `default.go` is the production list.
- `internal/core`: the pure reducer, (model, input) to (commands, events). No I/O, no clock.
- `internal/engine`: the one loop that owns the core, runs commands through the ports, owns `.crew/logs/`, publishes updates.
- `internal/proc`: the only way to start a child process (own process group, stop with deadline, kill all; `StartDetached` for the browser opener).
- `internal/mates`: `crew mates create`, crew's own GitHub identities (private GitHub Apps): names, manifest, loopback page, GitHub API signed as the mate, the mates' files under the user config dir; `Act`, which makes the configured mates act: repository tokens renewed in private gh config directories, and the git environment of the co-author hook.
- `internal/worktrees`: `crew worktrees clean`'s rule and flow: list crew's worktrees through `port.Sweeper`, decide each one (removed only when its branch's pull request merged; its branch only when nothing is after the merged head), ask, check again, remove, report.
- `internal/adapter/{github,claude,git,shell}`: the adapters.
- `internal/ui/lines`, `internal/ui/tui`: the renderers; they only read engine updates.
- `internal/fake`: in-memory tracker, scripted harness, temp-dir workspace, scripted checker.
- **Layering:** imports point inward, and `depguard` in `.golangci.yml` fails the build otherwise. `crew` imports nothing of crew's; `core` imports only `crew`; `port` imports no `core`, `engine`, `config`, adapter or UI; `engine` imports no adapter or UI; adapters import no `core`, `engine`, `config`, UI or other adapter (their tests may import `config`); only `ui/tui` imports Bubble Tea, Lip Gloss and Bubbles; only tests import `fake`; `mates` imports only the standard library and `proc`, and only `cmd/crew` imports it; `worktrees` imports only `crew` and `port` of crew's.
- **New adapter:** one package under `internal/adapter/` with a `Factory(group)`, plus one entry in `internal/registry/default.go`. Optional capabilities are separate interfaces found by type assertion: never wrap an adapter value, never add "not implemented" stubs.

## Tests

- **Fakes:** `internal/fake`: `NewTracker`, the scripted `NewHarness`, `NewWorkspace(t.TempDir())`, and `TrackerFactory`/`HarnessFactory` to register them. Tests build their own `registry.New` with the fakes, so they go through the same lookup and validation as real adapters.
- **Core:** table tests, no goroutines, no process, network or filesystem.
- **Time:** the engine loop, and `app` where timing matters, run under `testing/synctest` (fake clock, so `time.Sleep` there costs nothing; leaked goroutines fail).
- **Adapters:** scripted `gh` and `git` runners, and recorded `stream-json` fixtures in `internal/adapter/claude/testdata/`.
- **Real git:** only in temporary repositories (`t.TempDir()`, a local bare `origin`), with `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM` set so the user's config cannot leak in.
- **Golden files:** the TUI's views in `internal/ui/tui/testdata/`, with escape codes stripped; rewrite with `go test ./internal/ui/tui -update` and review the diff.

## Docs

- **Where:** [docs.page](https://docs.page) serves `docs.json` (tabs and sidebar) and `docs/**/*.mdx` from `main`. Only `.mdx` is published, so `docs/plans/` and `docs/ideation/` are not.
- **Two tabs:** `Guide` (`/`) for users and `Develop` (`/develop`) for contributors.
- **Keep it true:** a change in behaviour, configuration or messages updates the matching pages in the same pull request.
- **MDX:** `{` and `<` outside code are JSX, so keep them in backticks or code blocks.
- **Learnings:** `docs/solutions/` holds documented solutions to past problems (bugs, best practices, workflow patterns), by category, with YAML frontmatter (`module`, `tags`, `problem_type`). It is Markdown, so it is not published.
- **Capture:** After a solved, verified problem, automatically invoke the `ce-compound` skill with `mode:non-interactive` at the completion checkpoint only when the work produced durable project reasoning that is not readily recoverable from the final code, tests, types, comments, or existing documentation, and losing it would plausibly cause recurrence, material risk, or substantial rediscovery. Apply this counterfactual: if the learning document disappeared, would a future engineer reading the final implementation still be likely to repeat the mistake or redo substantial investigation? If not, do not invoke it. Completion, effort, and diff size alone are not enough. Capture at the checkpoint so a qualifying learning can ship in the PR that produced it, and only where the repository treats captured learnings as tracked, committed knowledge.
- **Check:** `pnpm install` once, then `pnpm docs:check` (the Docs workflow runs it on every pull request) and `pnpm docs:preview`. Use pnpm, never npm: `package.json` pins the docs.page CLI and pnpm itself (`packageManager`), and `pnpm-lock.yaml` pins them by hash.

## Releases and CI

- **Releases:** the version is `VERSION`, starting at `0.1.0`. A pull request that changes it is a release. After it merges to `main`, the Release workflow runs GoReleaser (`.goreleaser.yaml`), which publishes `vX.Y.Z` as a GitHub release with crew's binaries for macOS and Linux and `checksums.txt`, and publishes nothing unless every one built. Unlike the other workflows, it publishes without the shared release action and uses only its `check` mode. The version must be `MAJOR.MINOR.PATCH` and not below the latest release (CI's `version` check).
- **Shared workflows:** CI, Docs, Claude Code and the release call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA with the version as a comment; Dependabot bumps them. Change shared behaviour there, not here.
- **CI:** GitHub Actions are pinned by SHA, pnpm packages by hash (`pnpm-lock.yaml`). The `checks` ruleset requires `version`, `actionlint / actionlint`, `docs / docs.page check` and `go`. A new required job goes into it through `bootstrap.sh --checks` (in `.github`).
- **Codacy:** its jobs are off until the repository variable `CODACY_ENABLED` is `true`. Fix a finding; suppress only a genuine false positive, at the finding, naming the rule and the reason (`//nolint:<linter> // <reason>`). Never exclude crew's own source from analysis. After editing `.codacy.yaml`, run `pnpm exec codacy-analysis update-config` and commit both files. The gates live in Codacy's UI and are recorded in `docs/develop/quality.mdx`.

## Agents

- `AGENTS.md` and `.agents/` are the source; `CLAUDE.md`, `.claude/agents` and `.claude/skills` are symlinks to `AGENTS.md`, `.agents/agents` and `.agents/skills`. Edit the source.
- `acceptance-tester` (`.agents/agents/acceptance-tester.md`) writes behavior tests from a plan's acceptance examples in its own worktree, without reading the implementation. It is linked into `~/.claude/agents/` to work in any repository; see `docs/guide/acceptance-tester.mdx`.
- `cw-create-issue` (`.agents/skills/cw-create-issue/SKILL.md`) is the `/cw-create-issue` skill: it creates a GitHub issue with the label and filled issue template of a stage or extra label from `.crew/config.yaml`. Its directory is linked into `~/.claude/skills/` to work in any repository; see `docs/guide/create-issue.mdx`.
- `cw-update-issue-plan` (`.agents/skills/cw-update-issue-plan/SKILL.md`) is the `/cw-update-issue-plan` skill: it copies the plan file the session's `ce-brainstorm` or `ce-plan` wrote into the body of the issue the session is working on, keeps the old body in a comment, and moves the issue to the label given or, without one, runs `prompts.update_issue_plan`. Linked and documented like `cw-create-issue`.
- `cw-brainstorm` (`.agents/skills/cw-brainstorm/SKILL.md`) is the `/cw-brainstorm` skill: it fills the top-level `prompts.brainstorm` of `.crew/config.yaml` with an issue and runs it in the user's session. crew checks `prompts` at load but never runs them. Linked and documented like `cw-create-issue`.
- **Reports:** Write every report, summary, or handoff to the user through the `ce-noslop` skill. This applies when you are the top-level agent writing to the user, not when you are a subagent reporting to its caller. Do not apply it to code, config, verbatim quotes, or text the user asked to post as written.
