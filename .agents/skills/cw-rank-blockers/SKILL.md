---
name: cw-rank-blockers
description: Ranks, with TypeSafe's Jev, the open issues most likely to block one GitHub issue and the open issues it most likely blocks, among the issues crew's code owners and bots opened, and prints two short lists with Jev's probabilities. It records nothing. Use when crew's refinement needs a shortlist of likely dependencies for an issue, or when asked which open issues likely block, or are blocked by, an issue.
argument-hint: "<issue, such as #42>"
---

# Rank the likely blockers of an issue

This skill is the crew repository's own aid for building crew, not part of crew. Its script, `rank.sh`, asks Jev about each open candidate issue in its own request and prints two ranked lists, so the session that reads them need not read every open issue to find the few that relate.

The lists only order candidates. In #166's backtest the real dependency was in the top 5 for 14 of the 19 links it could measure, and real blockers often scored only 0.1 to 0.2, so a low probability does not rule an issue out. Whoever reads the lists still decides each dependency, and may open any other candidate whose title looks related.

The issue is the argument, such as `#42`.

## What it needs

- `TYPESAFE_API_KEY` in the environment. Keep the key in the variable: never print it or write its value into a command.
- `CREW_CODE_OWNERS` and `CREW_BOTS`, the logins whose open issues are the candidates, separated by spaces. crew sets both in its sessions.
- `gh`, `jq` and `curl`.

## Run it

From the repository root (`git rev-parse --show-toplevel`):

```sh
sh .agents/skills/cw-rank-blockers/rank.sh '#42'
```

The candidates are the open issues those logins opened, without the issue itself, split parents (issues whose body holds `<!-- cw-split-plan: split record -->`) and, when the issue is a part of a split (its body holds `<!-- cw-split-plan: part of #P -->`), its parent #P and the other parts of #P. For each candidate one request holds only the two issues' titles and the first 3,000 characters of their bodies. The script prints, for example:

```text
Likely to block #42 (24 candidates, jev-1.13.0):
1. #31 0.82 Add the config key
2. #17 0.40 Rename the queue
...

Likely blocked by #42 (24 candidates, jev-1.13.0):
1. #50 0.71 Show the key in the live view
...
```

Each list holds the 5 candidates with the highest probability for its direction, or every candidate when there are fewer, and reads `(none)` when there is no candidate.

## When it fails

When the script cannot judge every candidate it prints no list, writes one line starting with `rank.sh:` on standard error, and exits 1. The line names the cause: `TYPESAFE_API_KEY is not set`, both login lists empty, a missing command, a `gh` command that failed with its error, or `Jev failed for #N` with `HTTP <status>`, `timeout`, `connection failed` or `invalid response` and the number of attempts. It retries a busy or failing API up to 3 attempts. A wrong argument prints its usage and exits 2.

Report that line as it is. Do not retry the script, and do not rank the candidates yourself in its place: say the shortlist is unavailable and why. Whoever asked then reads every candidate itself, as the refine prompt in `.crew/config.example.yaml` does.

## Measure it

`backtest.sh`, beside it, measures how often `rank.sh` puts a real dependency in its top 5. It replays `rank.sh` against every `blocked_by` link GitHub records in the repository, each at the moment it was recorded: the issues open then, with their bodies as they read then, from the side whose refinement recorded the link (the blocked issue for a link recorded by hand). It needs the same environment as `rank.sh` and takes no argument:

```sh
sh .agents/skills/cw-rank-blockers/backtest.sh
```

It prints a markdown report of issue numbers, ranks and Jev's probabilities, with no issue text: one row per measured link, the misses with the top 5 shown instead, the links the shortlist could never show and why, and the gate line, which passes when at least 70% of the measured links were found. When `rank.sh` fails for any link, it prints no report and one line starting with `backtest.sh:` on standard error, and exits 1.

## What it never does

It only reads: it records, removes or edits no dependency, label or issue. Report its lists, or its error, and leave every decision to whoever asked.

Its tests are `rank_test.sh` and `backtest_test.sh`, beside it, which run the scripts with stub `gh` and `curl`: `sh .agents/skills/cw-rank-blockers/rank_test.sh` and `sh .agents/skills/cw-rank-blockers/backtest_test.sh`.
