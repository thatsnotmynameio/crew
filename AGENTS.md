# AGENTS.md

Guidance for coding agents working in this repository. Claude Code reads it as `CLAUDE.md`, a symlink to this file.

<!-- One paragraph: what this project is and who uses it. The docs site is the user-facing reference; the README is a short entry point that links to it. -->

## Commands

Run every command from the repository root.

<!-- Add the project's commands: install, test (all, one file, one test), lint, format, type check. -->

```sh
pnpm install          # once: the docs.page CLI
pnpm docs:check       # the docs site's links and MDX
pnpm docs:preview     # live preview of the docs
```

## Architecture

<!-- Where the code lives (one line per folder or module), what each part may import, the order things start in and what each step may assume. -->

## Tests

<!-- The fixtures and helpers every test uses, and how to run one test in isolation. -->

## Docs

- **Where:** [docs.page](https://docs.page) serves `docs.json` (tabs and sidebar) and `docs/**/*.mdx` from `main`. Only `.mdx` is published, so `docs/superpowers/` (specs and plans) is not.
- **Two tabs:** `Guide` (`/`) for users and `Develop` (`/develop`) for contributors.
- **Keep it true:** a change in behaviour, configuration or messages updates the matching pages in the same pull request.
- **MDX:** `{` and `<` outside code are JSX, so keep them in backticks or code blocks.
- **Check:** `pnpm install` once, then `pnpm docs:check` (the Docs workflow runs it on every pull request) and `pnpm docs:preview`. Use pnpm, never npm: `package.json` pins the docs.page CLI and pnpm itself (`packageManager`), and `pnpm-lock.yaml` pins them by hash.

## Releases and CI

- **Releases:** the version is `VERSION`, starting at `0.1.0`. A pull request that changes it is a release. After it merges to `main`, the Release workflow tags `vX.Y.Z` and publishes a GitHub release. The version must be `MAJOR.MINOR.PATCH` and not below the latest release (CI's `version` check).
- **Shared workflows:** CI, Docs, SonarQube, Claude Code and the release call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA with the version as a comment; Dependabot bumps them. Change shared behaviour there, not here.
- **CI:** GitHub Actions are pinned by SHA, pnpm packages by hash (`pnpm-lock.yaml`). The `checks` ruleset requires `version`, `actionlint / actionlint` and `docs / docs.page check`. A new required job goes into it through `bootstrap.sh --checks` (in `.github`).
- **Sonar:** off until the repository variable `SONAR_ENABLED` is `true`. A Sonar finding that conflicts with a required signature or convention is suppressed in `sonar-project.properties` (`sonar.issue.ignore.multicriteria`), with a comment giving the reason, not in code.

## Agents

- `AGENTS.md` and `.agents/` are the source; `CLAUDE.md` and `.claude/agents` are symlinks to them. Edit the source.
- `acceptance-tester` (`.agents/agents/acceptance-tester.md`) writes behavior tests from a plan's acceptance examples in its own worktree, without reading the implementation. It is linked into `~/.claude/agents/` to work in any repository; see `docs/guide/acceptance-tester.mdx`.
