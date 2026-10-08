---
name: cw-split-brainstorm
description: Measures the brainstormed plan (a Product Contract, no implementation units) in a GitHub issue's body and, when it is above the size threshold, splits it into small parts that each merge alone, written as lean files under docs/splitting/brainstorm/issue-N/ with an issues.json of their titles and the links between them. It reads only the issue and changes nothing on GitHub. Use when asked to split an issue's brainstormed plan the crew way, or when crew's refinement prompt runs /cw-split-brainstorm on an issue. A plan ce-plan enriched with implementation units is /cw-split-ce-plan's.
argument-hint: "<issue>"
---

# Split a large plan into parts

This skill is the crew repository's own aid for building crew, not part of crew. It asks nothing: nobody reviews a split, so its rules below are what keep each part safe to build.

A large plan costs more than its parts: one lfg session that carries the whole plan re-reads a bigger context on every turn (#160). So a plan above the threshold becomes small parts that each merge alone, leaving `main` whole and releasable, and that crew builds in parallel unless one really needs another. Each part is written lean: a session keeps everything it reads in its context until it ends, so a part holds only what its own session needs.

The skill only splits. It reads the issue it is given and nothing else: no other issue, no sub-issue, no comment, no code, no file of the repository. It writes only the part files and `issues.json`. It creates, edits and labels no issue, and asks no model.

Run `gh` and `sh` with the repository root, from `git rev-parse --show-toplevel`, as the working directory. Read and write files with your own file tools, temporary ones outside the repository. When a `gh` command fails, report its error text and stop. Do not retry.

The issue is the argument, such as `#42`. Below, `N` is its number.

It splits a brainstormed plan: a Goal Capsule and a Product Contract, as `ce-brainstorm` writes them. A plan that `ce-plan` enriched has a `## Implementation Units` section, and `/cw-split-ce-plan` splits it, along its units.

## What it writes

Under `docs/splitting/brainstorm/issue-N/`, which git ignores:

- `1.md` to `n.md`: one file per part, its body, `n` being the number of parts. Ids start at 1 and follow the order the parts can merge in.
- `issues.json`: one entry per part, in id order, with its id, its title and the ids of the parts that block it directly, `[]` when none does. Links are only between the parts:

```json
[
  { "id": "1", "title": "Open the store", "blocked_by": [] },
  { "id": "2", "title": "Record issues in the store", "blocked_by": ["1"] },
  { "id": "3", "title": "Record crew's processes in the store", "blocked_by": ["1"] }
]
```

Here parts 2 and 3 are each blocked by part 1, and run in parallel once it merges.

Before writing, delete `docs/splitting/brainstorm/issue-N/` when it exists, so the directory holds only this split.

## Outcomes

The skill ends with exactly one of these outcomes, named on the first line of its report:

| Outcome | When |
| --- | --- |
| `not a brainstormed plan` | the body has a `## Implementation Units` section: `/cw-split-ce-plan` splits it; nothing is written |
| `not split` | the plan is not above the threshold; nothing is written |
| `kept whole` | the plan is above the threshold, but no grouping keeps every part whole on `main`; nothing is written |
| `split` | the part files and `issues.json` are written |

## 1. Read the issue

Run `gh issue view N --json number,title,body` and write the body to a temporary file. When the body holds a section, such as `## Split`, whose first lines carry `<!-- cw-split-plan: split record -->`, a record an earlier version of this skill wrote, leave that section out: it is not part of the plan.

When the body has a `## Implementation Units` section, end with `not a brainstormed plan`, naming `/cw-split-ce-plan`.

## 2. Measure the plan

Run `sh .agents/skills/cw-split-brainstorm/measure.sh <body file>`. It prints the plan's characters, requirements and acceptance examples, and `above_threshold`. The threshold is above 10,000 characters or above 12 requirements.

When `above_threshold=no`, end with `not split`, giving the three numbers.

## 3. Group the requirements into parts

Make the parts small: each one a quick delivery, its own pull request. When the plan has a section that suggests slices, start from it. Every rule below holds for every part:

1. **It merges alone.** Merged into `main` after the parts that block it and before the others, it leaves `main` whole and releasable: no setting without its behaviour, no README or docs text describing what does not exist yet, no half of a flow. Requirements that one of these would split stay together.
2. **It has its reason.** One sentence says why it ships alone: what it adds that works and is documented by itself.
3. **It is under the threshold,** unless rule 1 keeps it whole.

An acceptance example goes to the part that holds every requirement it covers. One that covers requirements in two parts ties them: put them in one part, unless one part is blocked by the other, in which case it goes to the blocked one, whose merge completes the behaviour. When its scenario also needs what a later part adds, such as records that part writes, it goes to that later part, the first one where it can be tested.

When no grouping meets rule 1, end with `kept whole`, and say why in one sentence: the cut that came closest and the rule it breaks, such as "the only cut would leave a half-built setting on `main`".

## 4. Decide the links between parts

A part is blocked by another only when you can say why it needs that part merged first, such as "it reads the config key the other part adds". Without such a reason, the two run in parallel. Never make a cycle. Keep only direct links: leave out a link that a chain of other links already implies. Number the parts so that a part's id is greater than the id of every part that blocks it.

## 5. Write each part

A part's file holds what its own session needs, read once: the contract copied verbatim from the plan, the context written for the part, and what the parts it builds on deliver. It names other parts by their ids, since they are not issues yet, and the parent as `#N`.

The file is the part's body: its title goes in `issues.json`, naming what the part adds. The body holds, in this order, leaving out a section with nothing to hold:

| Section | What the part holds | How |
| --- | --- | --- |
| `## Goal Capsule` | **Objective:** the outcome this part delivers, in one sentence. **Parent:** `#N, part k of n. Read it only for a question this part does not answer.` **Open blockers:** the plan's. | written |
| `## Builds on` | Only for a part that is blocked. The line `Already on main when this part starts; read the code, not these parts' files.`, then one line per part in its `blocked_by`: `- Part <id>: <what it delivers>.` | written |
| `## Product Contract` | the heading alone | |
| `### Summary` | what this part adds, in one to three lines | written |
| `### Context` | why this part matters, in one or two lines, in place of the plan's problem frame | written |
| `### Key Decisions` | each of the plan's decisions whose `Governs` names one of the part's requirements, and each decision that shapes what this part builds for later parts to extend, such as the shape of a type or a port | verbatim, with provenance |
| `### Actors` | the plan's actors this part involves | verbatim |
| `### Requirements` | the part's requirements, with their original IDs and group headings | verbatim |
| `### Key Flows` | each flow whose `Covered by` names one of the part's requirements | verbatim |
| `### Acceptance Examples` | the part's acceptance examples, with their original IDs | verbatim |
| `### Success Criteria` | the plan's criteria this part can meet | verbatim |
| `### Scope Boundaries` | what this part leaves to which other part or to later work, and the plan's boundaries that touch it. When an earlier part documents what this part extends, such as a README section, say this part keeps that documentation true for what it adds | written |
| `### Dependencies / Assumptions` | the plan's entries that touch this part, each cut to what touches it | trimmed |
| `### Outstanding Questions` | the plan's questions that name one of the part's requirements or concern what it builds, under their plan heading | verbatim |
| `### Sources / Research` | the line `Split from #N.`, then the plan's sources this part touches as a list, an entry that names several files cut to the files this part touches | trimmed |

Verbatim is the contract: decisions, requirements, flows, examples, criteria, questions. A paraphrase there could change what gets built. Trimmed keeps the plan's own words, cut to what touches the part. Written is the context, kept short. Take everything from the issue's body: the skill reads no code, so a source the plan does not name is not added.

End the file with the line `<!-- cw-split-brainstorm: part of #N -->`.

Measure every part: `sh .agents/skills/cw-split-brainstorm/measure.sh <part files>`. A part with `above_threshold=yes` must be one that rule 1 keeps whole. Otherwise regroup, and go back to step 3.

## 6. Write issues.json

Write `docs/splitting/brainstorm/issue-N/issues.json` from steps 4 and 5: each part's id, its title, and the ids of the parts that block it directly.

## 7. Report

The first line is the outcome. Then:

- `not a brainstormed plan`: that `/cw-split-ce-plan` splits this issue.
- `not split`: the plan's characters, requirements and acceptance examples.
- `kept whole`: those numbers, and the one-sentence reason.
- `split`: the directory, then each part's id, title, requirement IDs, size and reason for shipping alone; each link between parts with its reason; and the parts kept above the threshold, with why.

Also list every command that failed, with its error.
