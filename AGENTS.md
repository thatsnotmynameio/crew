# AGENTS.md

Guidance for coding agents working in this repository. Claude Code reads it as `CLAUDE.md`, a symlink to this file.

crew is a Go program that polls a tracker (GitHub) and moves each issue through the rules a repository declares on its labels in `.crew/config.yaml`, running one coding-agent session (Claude Code or Codex) per action in its own git worktree. Its users run it in their own repositories, where it takes the issues their code owners and bots opened.

## Commands

Run every command from the repository root. Go 1.27 (`go.mod`).

```sh
go build ./cmd/crew   # the binary, at the root (ignored by git)
go test -race ./...   # every test; one package: go test -race ./internal/core; one test: add -run TestName
gofmt -l cmd internal tools acceptance # prints the unformatted files; must print nothing
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run   # lint + layering (depguard)
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...   # coverage profile
go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml   # total >= 90%
git diff -U0 origin/main...HEAD | go run ./tools/diffcover -profile coverage.out   # changed lines >= 90%
go -C acceptance run ./cmd/acceptance   # builds crew with the release config, runs the acceptance suite against it; go test flags pass through
pnpm install          # once: the Codacy CLIs
pnpm exec codacy-analysis analyze --install-dependencies   # Codacy's Lizard, Opengrep, Trivy, Checkov
```

- **golangci-lint:** run it through `go run` at v2.14.0, as CI does. A local install older than v2.13.0 cannot lint a `go 1.27` module.
- **CI:** the `go` job in `.github/workflows/ci.yml` runs gofmt, vet, golangci-lint, `go test -race` with coverage, both coverage floors and govulncheck. Its `acceptance` job builds crew with GoReleaser (a Linux amd64 snapshot), runs vet, golangci-lint and govulncheck in `acceptance/`, then the suite with `CREW_BIN` set; on failure it uploads the tests' artifacts. Its `codacy` job uploads the coverage to Codacy, which analyses the code on its own servers.
- **Quality bar:** zero findings, everywhere. `.golangci.yml` turns on every linter except those it lists with a reason; Codacy's tools and limits are in `.codacy/codacy.config.json`.

## Architecture

Ports and adapters with a pure core.

- `cmd/crew`: flags, signals, the repository root; builds the `git` workspace, the `shell` checker and `app.Options.Bots` (through `internal/bots`) and calls `app.Run`. `crew bots create <name>` is chosen before the flags and runs the `internal/bots` flow instead.
- `internal/app`: config, registry, engine, renderer, stop signals, exit codes (0 clean, 1 failure or forced, 2 config or environment).
- `internal/crew`: the domain (states, issues, rules with their labels and queues, actions, board columns, outcomes, failure reports, statuses).
- `internal/config`: `.crew/config.yaml`: refuses the old keys with their replacements (`legacy.go`), strict decoding, engine defaults, one file per section (rules, agents, checks, queues, board); resolves each action's agent, check and bot; hands the tracker and each agent's harness its section as a `port.Decode`.
- `internal/port`: `Tracker`, `Harness`, `Workspace`, `Checker`, `Identity`, the optional `Preparer`, `StatusReporter`, `PullRequestReporter`, `Acting`, `CodeOwnerFinder`, `LoginFinder`, `WriterReporter`, `BoardLister`, `Narrator`, `Reopener`, `UsageReporter`, `LastMessageReporter` and `PullRequestFinder`, sentinel errors, factory types.
- `internal/registry`: name to factory; `default.go` is the production list.
- `internal/core`: the pure reducer, (model, input) to (commands, events). No I/O, no clock.
- `internal/engine`: the one loop that owns the core, runs commands through the ports, owns `.crew/logs/`, publishes updates.
- `internal/proc`: the only way to start a child process (own process group, stop with deadline, kill all; `StartDetached` for the browser opener).
- `internal/bots`: `crew bots create`, crew's own GitHub identities (private GitHub Apps): names, manifest, loopback page, GitHub API signed as the bot, the bots' files under the user config dir (`crew/bots`, and the older `crew/mates` read as a fallback); `Act`, which makes the configured bots act: repository tokens renewed in private gh config directories, and the git environment of the co-author hook.
- `internal/adapter/{github,claude,codex,git,shell}`: the adapters.
- `internal/ui/lines`, `internal/ui/tui`: the renderers; they only read engine updates.
- `internal/fake`: in-memory tracker, scripted harness, temp-dir workspace, scripted checker.
- **Layering:** imports point inward, and `depguard` in `.golangci.yml` fails the build otherwise. `crew` imports nothing of crew's; `core` imports only `crew`; `port` imports no `core`, `engine`, `config`, adapter or UI; `engine` imports no adapter or UI; adapters import no `core`, `engine`, `config`, UI or other adapter (their tests may import `config`); only `ui/tui` imports Bubble Tea, Lip Gloss and Bubbles; only tests import `fake`; `bots` imports only the standard library and `proc`, and only `cmd/crew` imports it.
- `acceptance/`: the black-box acceptance suite, a nested Go module outside the layering. It reaches crew only through the built binary and never imports crew's packages (a `depguard` rule denies `internal` and `cmd` there). The root `go test ./...`, lint, coverage floors and `tools/diffcover` stop at its `go.mod`: run its vet, golangci-lint and govulncheck with `go -C acceptance` (`acceptance/README.md`, which documents the doubles without crew's internals).
- **New adapter:** one package under `internal/adapter/` with a `Factory(group)`, plus one entry in `internal/registry/default.go`. Optional capabilities are separate interfaces found by type assertion: never wrap an adapter value, never add "not implemented" stubs.

## Tests

- **Fakes:** `internal/fake`: `NewTracker`, the scripted `NewHarness`, `NewWorkspace(t.TempDir())`, and `TrackerFactory`/`HarnessFactory` to register them. Tests build their own `registry.New` with the fakes, so they go through the same lookup and validation as real adapters.
- **Core:** table tests, no goroutines, no process, network or filesystem.
- **Time:** the engine loop, and `app` where timing matters, run under `testing/synctest` (fake clock, so `time.Sleep` there costs nothing; leaked goroutines fail).
- **Adapters:** scripted `gh` and `git` runners, and recorded `stream-json` fixtures in `internal/adapter/claude/testdata/`.
- **Real git:** only in temporary repositories (`t.TempDir()`, a local bare `origin`), with `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM` set so the user's config cannot leak in.
- **Golden files:** the TUI's views in `internal/ui/tui/testdata/`, with escape codes stripped; rewrite with `go test ./internal/ui/tui -update` and review the diff.
- **Acceptance:** `acceptance/` runs the binary in `CREW_BIN` (unset fails, never skips) against `gh` and `claude` doubles on `PATH`: the test binary itself, through `harness.Main`, answering from a stateful fake GitHub and scripted sessions. Screen scenarios run crew in a pseudo-terminal and compare masked screens with snapshots. Always `-count=1`: a rebuilt binary keeps its mtime. The pull request that changes how crew calls `gh` or `claude` teaches `acceptance/fakegithub` or `acceptance/fakeclaude` the call. Scenarios (`acceptance/scenarios/`) and their snapshots are the tester's: only the tester rewrites snapshots (`-accept-snapshots`).

## Docs

- **Keep it true:** a change in behaviour, configuration or messages updates the README where it describes them, in the same pull request.
- **Learnings:** `docs/solutions/` holds documented solutions to past problems (bugs, best practices, workflow patterns), by category, with YAML frontmatter (`module`, `tags`, `problem_type`).
- **Capture:** After a solved, verified problem, automatically invoke the `ce-compound` skill with `mode:non-interactive` at the completion checkpoint only when the work produced durable project reasoning that is not readily recoverable from the final code, tests, types, comments, or existing documentation, and losing it would plausibly cause recurrence, material risk, or substantial rediscovery. Apply this counterfactual: if the learning document disappeared, would a future engineer reading the final implementation still be likely to repeat the mistake or redo substantial investigation? If not, do not invoke it. Completion, effort, and diff size alone are not enough. Capture at the checkpoint so a qualifying learning can ship in the PR that produced it, and only where the repository treats captured learnings as tracked, committed knowledge.

## Releases and CI

- **Releases:** the version is `VERSION`, starting at `0.1.0`. A pull request that changes it is a release. After it merges to `main`, the Release workflow runs GoReleaser (`.goreleaser.yaml`), which publishes `vX.Y.Z` as a GitHub release with crew's binaries for macOS and Linux and `checksums.txt`, and publishes nothing unless every one built. Unlike the other workflows, it publishes without the shared release action and uses only its `check` mode. The version must be `MAJOR.MINOR.PATCH` and not below the latest release (CI's `version` check).
- **Shared workflows:** CI, Claude Code and the release call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA with the version as a comment; Dependabot bumps them. Change shared behaviour there, not here.
- **CI:** GitHub Actions are pinned by SHA, pnpm packages by hash (`pnpm-lock.yaml`). Use pnpm, never npm: `package.json` pins pnpm itself (`packageManager`). The `checks` ruleset requires `version`, `actionlint / actionlint` and `go`. A new required job goes into it through `bootstrap.sh --checks` (in `.github`); `acceptance` joins it that way once it is on `main`.
- **Codacy:** its jobs are off until the repository variable `CODACY_ENABLED` is `true`. Fix a finding; suppress only a genuine false positive, at the finding, naming the rule and the reason (`//nolint:<linter> // <reason>`). Never exclude crew's own source from analysis. After editing `.codacy.yaml`, run `pnpm exec codacy-analysis update-config` and commit both files. The gates live in Codacy's UI.

## Agents

- `AGENTS.md` and `.agents/` are the source; `CLAUDE.md`, `.claude/agents` and `.claude/skills` are symlinks to `AGENTS.md`, `.agents/agents` and `.agents/skills`. Edit the source.
- `acceptance-tester` (`.agents/agents/acceptance-tester.md`) writes behavior tests from a plan's acceptance examples in its own worktree, without reading the implementation. It is linked into `~/.claude/agents/` to work in any repository.
- `cw-create-issue` (`.agents/skills/cw-create-issue/SKILL.md`) is the `/cw-create-issue` skill: it creates a GitHub issue with the label and filled issue template of one of the issue types its own table lists. The `cw-*` skills are this repository's own aids, not crew defaults: they do not read `.crew/config.yaml`, and their types, labels and prompts, this repository's, live in the skill files. Its directory is linked into `~/.claude/skills/` to work in any repository.
- `cw-update-issue-plan` (`.agents/skills/cw-update-issue-plan/SKILL.md`) is the `/cw-update-issue-plan` skill: it copies the plan file the session's `ce-brainstorm` or `ce-plan` wrote into the body of the issue the session is working on, keeps the old body in a comment, and moves the issue to the label given or, without one, runs the prompt it holds, which moves it from `crew:brainstorm:in progress` to `crew:brainstorm:done`. Linked like `cw-create-issue`.
- `cw-brainstorm` (`.agents/skills/cw-brainstorm/SKILL.md`) is the `/cw-brainstorm` skill: it fills the brainstorm prompt it holds with an issue and runs it in the user's session. Linked like `cw-create-issue`.
- `cw-split-plan` (`.agents/skills/cw-split-plan/SKILL.md`) is the `/cw-split-plan` skill, which crew's refinement prompt runs before it finds blockers: it measures the issue's plan with its `measure.sh` and, above 10,000 characters or 12 requirements, splits it into unlabeled sub-issues that each merge alone, links them by `blocked_by` only where one needs another, records the split on the parent, and asks Jev in shadow about each part. The refine prompt relies on the outcomes and markers it names. It is not linked into `~/.claude/skills/`: it is written for this repository's refinement rule.
- `cw-rank-blockers` (`.agents/skills/cw-rank-blockers/SKILL.md`) is the `/cw-rank-blockers` skill, which crew's refine prompt runs for each issue it refines, reading in full only the issues it lists, or every candidate when it fails: its `rank.sh` takes an issue, asks Jev (`jev-1.13.0`) about each open issue the logins in `$CREW_CODE_OWNERS` and `$CREW_BOTS` opened, one request per pair, and prints the 5 most likely to block it and the 5 it most likely blocks, or one error line naming why it could not judge every candidate. Its `backtest.sh` replays `rank.sh` against every `blocked_by` link GitHub records, each with the issues and bodies of the moment it was recorded, and prints a report ending with #166's gate (the real dependency in the top 5 for at least 70% of the links). Neither records anything. They need `TYPESAFE_API_KEY`, `gh`, `jq` and `curl`. Their tests, `rank_test.sh` and `backtest_test.sh`, use stub `gh` and `curl` and run by hand, not in CI. It is not linked into `~/.claude/skills/`: it reads crew's session environment.
- `cw-tester` (`.agents/skills/cw-tester/SKILL.md`) is the `/cw-tester` skill: it makes the session crew's black-box tester for the areas it is given. It reads only the README, the two config references the README links, `crew --help` and the acceptance doubles' documentation, never crew's code, writes scenarios under `acceptance/scenarios/<area>/` for the README's promises and the edge cases it judges necessary, keeps a scenario the binary fails red, rewrites a TUI snapshot only when the new screen keeps the README's promises, commits only its own packages and ends with a report of the scenarios that fail and the gaps in the README. It is the tester `acceptance/README.md` names, and the developer never edits its scenarios. It is not linked into `~/.claude/skills/`: it is written for this repository's acceptance suite.
- **Reports:** Write every report, summary, or handoff to the user through the `ce-noslop` skill. This applies when you are the top-level agent writing to the user, not when you are a subagent reporting to its caller. Do not apply it to code, config, verbatim quotes, or text the user asked to post as written.
- **Jev:** when a change needs a judgment on free text (such as classifying, routing, verifying, or ranking), consider TypeSafe's Jev model through the [agent skill](https://docs.typesafe.ai/agent-skill) before using string rules or LLM prompts meant for programmatic parsing. Measure its effectiveness first using real data (e.g., `.crew/logs` and repository issues) using these criteria: one narrow judgment per question, only the necessary text in the state, and thresholds chosen from held-out data. If it is not a fit, explain why. This requires `TYPESAFE_API_KEY`; skip Jev if the key is missing.
