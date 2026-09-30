# Review rules

This repository's rules for `/pr-review` (`SKILL.md` next to this file is the process). `AGENTS.md` at the root is the architecture and the conventions; these rules say what a review flags and how hard. Fill each commented section for this project; until then the rest holds.

## Severity

- **P0**: breaks every user or loses data. <!-- This project's examples: a start that fails for any configuration, a migration that drops data, a release that can't install. -->
- **P1**: a real bug on a normal path. <!-- Examples: wrong state after a restart, a leak of connections or tasks, input the validation accepts and the code then mishandles, a race between two awaits. -->
- **P2**: real but narrow. An edge the docs promise and the code misses, a docs page that now contradicts the code, a contract between modules broken in a way no test catches, at most one missing test per review (only for a path that can regress silently).

A finding needs a concrete failure scenario: which input or state, what happens, what should. "Could be" is not a finding.

## Never flag

- What the linters and CI already report: actionlint, docs.page's link check, SonarQube Cloud's code smells <!-- , and this project's linters, formatter and type checker -->. A real bug is still a finding when a test would catch it: post it, and name the test. The review can't run the tests, so never skip a finding because "CI will catch it".
- Style, naming, wording, formatting, comment density, nits of any kind.
- `docs/superpowers/` (specs and plans): only when the same pull request's code contradicts it.
- Lockfiles and generated files: `pnpm-lock.yaml` <!-- , and this project's -->.
- Anything outside the pull request's changes, unless the change breaks it.

## Worth checking in this repository

Where bugs have been found here; `AGENTS.md` explains each. Add a line when a bug turns up somewhere new.

<!--
- The startup order and what each step may assume.
- Restore and reload: state kept across restarts, listeners and tasks removed on shutdown.
-->
- Docs: a change in behaviour, configuration or messages updates the matching `docs/**/*.mdx` page in the same pull request. MDX: `{` and `<` outside code break the page.
- Releases: `VERSION` must be `MAJOR.MINOR.PATCH` and above the latest release.
- CI: GitHub Actions pinned by SHA, pnpm (never npm), shared workflows called from thatsnotmynameio/.github.

## Extra lenses

Lenses this project needs beyond the process's own (correctness, lifecycle and concurrency, contracts, security, consistency), one line each: its name and what it looks at.

<!-- - **configuration**: the schema and its defaults, what it accepts but the code mishandles, translations. -->

## Risk

How much can go wrong if this change is wrong, by area:

- low: docs, tests, a new optional feature nobody uses yet.
- medium: <!-- this project's ordinary feature code --> feature code.
- high: <!-- this project's startup, persistence, shared core --> shared code everything depends on.
- critical: releases, CI, deletion of what users own, and this review itself (`.agents/skills/pr-review/`).
