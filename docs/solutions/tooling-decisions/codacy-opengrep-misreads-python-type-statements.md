---
title: Codacy's Opengrep only partly scans Python files that use `type` statements
date: 2026-10-06
category: tooling-decisions
module: typesafe/, .codacy
problem_type: tooling_decision
component: tooling
severity: medium
applies_when:
  - "Writing a type alias in crew's Python code (typesafe/)"
  - "Codacy's local analysis lists an Opengrep toolInvoke syntax error while reporting 0 issues"
  - "Changing ruff's rule selection or the Python version typesafe/ targets"
  - "Adding another language to Codacy's analysis with `pnpm exec codacy-analysis update-config`"
symptoms:
  - "`[Opengrep/toolInvoke] <file>: Syntax error `type` was unexpected` in the error list of `pnpm exec codacy-analysis analyze`"
  - "The same run still prints `Analysis complete: 0 issues found`"
root_cause: tool_limitation
resolution_type: config_change
retire_when: "Codacy's Semgrep engine parses PEP 695: a scratch Python file holding `type A = int` analyses with no Opengrep toolInvoke error in `pnpm exec codacy-analysis analyze`"
tags: [codacy, opengrep, semgrep, python, pep-695, type-alias, ruff, up040, codacy-analysis-cli]
---

# Codacy's Opengrep only partly scans Python files that use `type` statements

## Context

The TypeSafe judge (#203) brought Python into crew, under `typesafe/`, targeting Python 3.14. Ruff runs every rule there (`select = ["ALL"]`), and its pyupgrade rule UP040 steers type aliases toward the PEP 695 statement form, `type JSON = ...`. The units were written that way.

`pnpm exec codacy-analysis analyze` then listed 16 errors, 15 of them on the judge's modules and tests: `[Opengrep/toolInvoke] typesafe/src/typesafe_judge/service.py: Syntax error `type` was unexpected`, and the same for `ledger.py`, `bank.py`, `calibrate.py` and the rest. The same run printed `Analysis complete: 0 issues found` and `0 issues` in Semgrep's row of the summary table. Opengrep (1.26.0 in the local CLI) is the engine behind Codacy's Semgrep tool. It cannot parse the `type` statement, so it scans the file only partially: a pattern can still match code it did parse, and code it could not parse goes unchecked. Nothing fails: the only sign is the error list printed after the summary. A scratch run of Opengrep 1.26.0 on a file holding `type A = int` reported it as "only partially analyzed". The sixteenth error, on `.github/workflows/codacy-import.yml`, predates the judge and is a different parse problem.

## Guidance

- **Declare Python type aliases with `TypeAlias`, never with a `type` statement.** Write `JSON: TypeAlias = "bool | int | float | str | list[JSON] | dict[str, JSON] | None"`. A recursive alias goes in quotes, and mypy still resolves it.
- **Keep ruff's UP040 off.** `typesafe/pyproject.toml` ignores it with the reason (`"UP040",  # Opengrep, Codacy's Semgrep, cannot parse `type` statements and scans such a file only partly`). Without the ignore, ruff flags every `TypeAlias` and pushes the code back to the form Opengrep cannot parse.
- **Quote an alias that names a `TYPE_CHECKING`-only import.** A `TypeAlias` value runs at import time, unlike a `type` statement's lazy value, so ruff's TC004 asks to move the import out of the `TYPE_CHECKING` block. Quoting the whole value keeps the import where it is: `Handler: TypeAlias = "Callable[[Call], dict[str, JSON]]"` in `typesafe/src/typesafe_judge/service.py`.
- **Drop `.__value__` when converting.** A `type` statement creates a `TypeAliasType`, so code read its target through `.__value__`. A `TypeAlias` is the target itself: `get_args(DecisionName.__value__)` became `get_args(DecisionName)` in `typesafe/src/typesafe_judge/records.py`.
- **Read the error list, not only the issue count.** After `pnpm exec codacy-analysis analyze`, a `toolInvoke` error means a tool skipped a file. A run is clean only when the error list holds nothing new.

The same change settled which Codacy tools cover Python. `update-config` proposed Ruff, Bandit, PyLintPython3 and Prospector for the new language. They were removed from `.codacy/codacy.config.json`: the `python` CI job's ruff, including its bandit (`S`) rules, and mypy own Python linting. Lizard, Semgrep and Trivy keep covering `typesafe/`, Lizard with the limits `docs/solutions/tooling-decisions/codacy-lizard-and-golangci-lint-limits.md` describes. This follows that learning's rule of one owner per metric.

## Why This Matters

Semgrep is the only Codacy tool that runs security patterns over the judge's Python, and the judge handles bearer tokens and issue text. A file Opengrep cannot fully parse gets incomplete security analysis, on Codacy's server and locally, and both still report zero issues. The failure is silent, and the default lint setup brings it back: every new alias written in the modern form puts another file partly outside the scan.

## When to Apply

- Writing or reviewing any type alias in `typesafe/` or in other Python added to crew.
- Raising ruff's version or Python target, or re-enabling rules, in `typesafe/pyproject.toml`.
- Reading a Codacy run that shows `toolInvoke` errors.
- Running `update-config` for a new language, which proposes that language's linters again.

## Examples

Before (Opengrep scans the file only partially):

```python
type StageName = Literal["shadow", "confirm", "act"]
type Handler = Callable[[Call], dict[str, JSON]]
DECISIONS = get_args(DecisionName.__value__)
```

After (analysed):

```python
StageName: TypeAlias = Literal["shadow", "confirm", "act"]
Handler: TypeAlias = "Callable[[Call], dict[str, JSON]]"
DECISIONS = get_args(DecisionName)
```

Checking it: in the #203 change, the local Codacy run went from 16 Opengrep errors to the one pre-existing error on `.github/workflows/codacy-import.yml`. Ruff, mypy, Lizard and the judge's 341 tests passed unchanged.
