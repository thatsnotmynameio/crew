---
title: Codacy's Python linters parse the judge with an older Python and miss the repository's rules
date: 2026-10-06
category: tooling-decisions
module: typesafe/, .codacy
problem_type: tooling_decision
component: tooling
severity: medium
applies_when:
  - "Adding or changing a Python tool in .codacy/codacy.config.json"
  - "Writing Python syntax or annotations newer than 3.13 in typesafe/"
  - "Moving or renaming typesafe/pyproject.toml, ruff.toml, bandit.yml or .prospector.yaml"
  - "A bot proposes dropping a Codacy tool because CI already runs the same linter"
symptoms:
  - "Hundreds of Pylint E0601 used-before-assignment on names imported under TYPE_CHECKING"
  - "Prospector's pyflakes reports F821 undefined names and `multiple exception types must be parenthesized`"
  - "Bandit's error list holds `syntax error while parsing AST from file` for a judge module while it reports no issue in it"
  - "Codacy reports D203 and D213 while ruff enforces D211 and D212, and S101 or B101 on every pytest assert"
root_cause: tool_limitation
resolution_type: config_change
retire_when: "Codacy's Bandit, Pylint and Prospector run on Python 3.14: a scratch module holding `except A, B:` analyses with no Bandit toolInvoke error and no pyflakes F999 in `pnpm exec codacy-analysis analyze`"
tags: [codacy, python, ruff, bandit, pylint, prospector, pydocstyle, codacy-analysis-cli, python-3.14]
---

# Codacy's Python linters parse the judge with an older Python and miss the repository's rules

## Problem

When `typesafe/` arrived, Codacy turned on Ruff, Bandit, Pylint and Prospector for Python with its own defaults. Their findings could not be fixed in code: they asked for the opposite of what the `python` CI job enforces, or they came from a Python older than the judge's. The first answer was to drop the four tools from `.codacy/codacy.config.json` and leave Python's linting to CI. That was rejected: Codacy is crew's authority on quality, so the tools stay and are held to the same rules as CI.

## What goes wrong

Two causes, reproduced with `pnpm exec codacy-analysis analyze --config-file .codacy/codacy.config.baseline.json --tool Ruff --tool Bandit --tool PyLintPython3 --tool Prospector`:

- **Codacy's defaults are not the repository's rules.** Codacy's Ruff writes its own `ruff.toml` from the enabled patterns, with the default line length of 88 and no per-file ignores: 424 E501 and 682 S101 on pytest asserts. Bandit adds 682 B101. Prospector's pydocstyle asks for D203 and D213, which contradict the D211 and D212 that ruff enforces, so no docstring can satisfy both.
- **The tools run on an older Python.** The local CLI builds the Bandit, Pylint and Prospector venvs from the system's `python3` (3.13 here), and the version on Codacy's servers is not documented. On 3.13, a name imported under `TYPE_CHECKING` and used in an annotation (fine with 3.14's deferred annotations) is Pylint's E0601 (395 findings) and pyflakes' F821. 3.14's `except A, B:` is a syntax error. Bandit then **skips `service.py` entirely** and still reports zero issues for it. The only sign is a `toolInvoke` line in the error list.

## Solution

- **Each tool reads the repository's configuration** (`useLocalConfigurationFile` in `.codacy/codacy.config.json`). Codacy's import only switches the server to "use the configuration file" without passing a path, and the adapters look in the repository root. So the files live there:
  - `ruff.toml` extends `typesafe/pyproject.toml`. Ruff resolves relative paths in an extended file from the extending file's directory, so `typesafe/pyproject.toml` writes its per-file ignores as `**/tests/**` and names its package in `known-first-party`. Both then hold from `typesafe/` and from the root.
  - `bandit.yml` skips B101 only in `typesafe/tests/`. Every other Bandit test runs.
  - `.prospector.yaml` disables D203 and D213, sets line length 100 and complexity 15, and leaves Pylint to Codacy's own Pylint tool.
  - Pylint keeps Codacy's curated patterns: they are error checks that need no alignment.
- **The judge is written in 3.13 syntax.** `target-version = "py313"` in `typesafe/pyproject.toml` makes ruff refuse newer syntax and stops the formatter from removing the parentheses in `except (A, B):`. Every module starts with `from __future__ import annotations`, so older tools read annotations as strings. The judge still requires and runs on 3.14.
- **True false positives are suppressed at the line.** Bandit stops reading test ids at the next `#`, so the reason follows a second `#`: `# noqa: S608  # nosec B608  # a known table`. Writing `# nosec B608 - a known table` makes Bandit read each word as a test name. Bandit warns `nosec encountered ... but no failed test` on two B608 lines even though removing their `nosec` brings the finding back. Pylint takes `# pylint: disable-next=<message>  # <reason>` on the line above.

## Why This Matters

Dropping a Codacy tool because CI runs the same linter removes those findings from Codacy's dashboard, history and gate, where crew decides on quality. Keeping the tool on Codacy's defaults is no better: findings that contradict CI cannot be fixed, and a gate nobody can make green gets bypassed. The version problem is silent. A file Bandit cannot parse gets no security analysis, and the report still says zero issues.

## When to Apply

- Changing which Python tools Codacy runs, or their configuration files.
- Raising the judge's syntax past 3.13, or removing the `__future__` import from a module.
- Reading a Codacy run: a `toolInvoke` error in the error list means a file went unanalysed, even when the count says zero.

## Examples

Checking it: `pnpm exec codacy-analysis analyze . --tool Ruff --tool Bandit --tool PyLintPython3 --tool Prospector` reported 0 issues. The only item in its error list was Bandit's B608 warning. The baseline configuration had reported 2,199 findings and Bandit's parse error on `service.py`. The `python` job's ruff, ruff format, mypy and 340 tests still passed.
