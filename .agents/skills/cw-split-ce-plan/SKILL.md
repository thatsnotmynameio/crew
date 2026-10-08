---
name: cw-split-ce-plan
description: Measures the implementation plan (a plan ce-plan enriched with a Planning Contract and implementation units) in a GitHub issue's body and, when it is above the size threshold, splits it along its units into small parts that each merge alone, written as lean files under docs/splitting/plan/issue-N/ with an issues.json of their titles and the links between them. It reads only the issue and changes nothing on GitHub. Use when asked to split an issue's implementation plan, or a plan ce-plan wrote, the crew way. A brainstormed plan without units is /cw-split-brainstorm's.
argument-hint: "<issue>"
---

# Split a large implementation plan into parts

This skill is the crew repository's own aid for building crew, not part of crew. It asks nothing: nobody reviews a split, so its rules below are what keep each part safe to build.

A large plan costs more than its parts: one lfg session that carries the whole plan re-reads a bigger context on every turn (#160). So a plan above the threshold becomes small parts that each merge alone, leaving `main` whole and releasable, and that crew builds in parallel unless one really needs another. Each part is written lean: a session keeps everything it reads in its context until it ends, so a part holds only what its own session needs.

The skill only splits. It reads the issue it is given and nothing else: no other issue, no sub-issue, no comment, no code, no file of the repository. It writes only the part files and `issues.json`. It creates, edits and labels no issue, and asks no model.

Run `gh` and `sh` with the repository root, from `git rev-parse --show-toplevel`, as the working directory. Read and write files with your own file tools, temporary ones outside the repository. When a `gh` command fails, report its error text and stop. Do not retry.

The issue is the argument, such as `#42`. Below, `N` is its number.

It splits a plan that `ce-plan` enriched: a Goal Capsule, a Product Contract, a Planning Contract, `## Implementation Units` (`U1`, `U2`, …, each with its Goal, Requirements, Dependencies, Files, Approach and Verification), a Verification Contract and a Definition of Done. A brainstormed plan, without units, is `/cw-split-brainstorm`'s.

## What it writes

Under `docs/splitting/plan/issue-N/`, which git ignores:

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

Before writing, delete `docs/splitting/plan/issue-N/` when it exists, so the directory holds only this split.

## Outcomes

The skill ends with exactly one of these outcomes, named on the first line of its report:

| Outcome | When |
| --- | --- |
| `not an implementation plan` | the body has no `## Implementation Units` section: `/cw-split-brainstorm` splits it; nothing is written |
| `not split` | the plan is not above the threshold; nothing is written |
| `kept whole` | the plan is above the threshold, but no grouping keeps every part whole on `main`; nothing is written |
| `split` | the part files and `issues.json` are written |

## 1. Read the issue

Run `gh issue view N --json number,title,body` and write the body to a temporary file. When the body holds a section, such as `## Split`, whose first lines carry `<!-- cw-split-plan: split record -->`, a record an earlier split wrote, leave that section out: it is not part of the plan.

When the body has no `## Implementation Units` section, end with `not an implementation plan`, naming `/cw-split-brainstorm`.

## 2. Measure the plan

Run `sh .agents/skills/cw-split-brainstorm/measure.sh <body file>`, the measure `/cw-split-brainstorm` keeps. It prints the plan's characters, requirements and acceptance examples, and `above_threshold`. The threshold is above 10,000 characters or above 12 requirements.

When `above_threshold=no`, end with `not split`, giving the three numbers.

## 3. Group the units into parts

The plan's units are the building blocks: a part is a set of whole units, and a unit is never cut. Make the parts small: each one a quick delivery, its own pull request. When the plan's `Sequencing` groups the units, start from it. Every rule below holds for every part:

1. **It merges alone.** Merged into `main` after the parts that block it and before the others, it leaves `main` whole and releasable: every gate of the Verification Contract passes, no setting without its behaviour, no README or docs text describing what does not exist yet, no half of a flow. Units that one of these would split stay together. A unit that documents behaviour goes with, or after, the units that build it.
2. **It has its reason.** One sentence says why it ships alone: what it adds that works and passes its gates.
3. **It is under the threshold,** unless rule 1 keeps it whole, or its units alone, which are never cut, are above it.

A unit whose `Dependencies` names a unit in another part makes that part block this one.

A requirement goes to the part whose merge completes it: the one holding the last unit, in merge order, that builds it. A unit that only documents a requirement, such as a docs unit, does not count. An earlier part that builds some of it names it in its Context. An acceptance example goes with the unit whose test scenarios cite it; when no unit cites it, to the part that completes every requirement it covers.

When no grouping meets rule 1, end with `kept whole`, and say why in one sentence: the cut that came closest and the rule it breaks, such as "the only cut would leave the README describing a setting no unit before it builds".

## 4. Decide the links between parts

A part is blocked by another when one of its units depends on one of the other's units, or when you can say why else it needs that part merged first. Without such a reason, the two run in parallel. Never make a cycle. Keep only direct links: leave out a link that a chain of other links already implies. Number the parts so that a part's id is greater than the id of every part that blocks it.

## 5. Write each part

A part's file holds what its own session needs, read once: the contract and its units copied verbatim from the plan, the context written for the part, and what the parts it builds on deliver. It names other parts by their ids, since they are not issues yet, and the parent as `#N`. Units keep their original `U` IDs, so a `Dependencies` line that names a unit of another part still reads true: `Builds on` says which part delivers it.

The file is the part's body: its title goes in `issues.json`, naming what the part adds. The body holds, in this order, leaving out a section with nothing to hold:

| Section | What the part holds | How |
| --- | --- | --- |
| `## Goal Capsule` | **Objective:** the outcome this part delivers, in one sentence. **Parent:** `#N, part k of n. Read it only for a question this part does not answer.` **Open blockers:** the plan's. | written |
| `## Builds on` | Only for a part that is blocked. The line `Already on main when this part starts; read the code, not these parts' files.`, then one line per part in its `blocked_by`: `- Part <id> (U<x>, U<y>): <what it delivers>.` | written |
| `## Product Contract` | the heading alone | |
| `### Summary` | what this part adds, in one to three lines | written |
| `### Context` | why this part matters, in one or two lines, in place of the plan's problem frame, and the requirements an earlier or later part completes that this part's units build some of | written |
| `### Key Decisions` | each of the plan's product decisions whose `Governs` names a requirement this part completes or one its units name | verbatim, with provenance |
| `### Actors` | the plan's actors this part involves | verbatim |
| `### Requirements` | the requirements this part completes, with their original IDs and group headings | verbatim |
| `### Key Flows` | each flow whose `Covered by` names one of the part's requirements | verbatim |
| `### Acceptance Examples` | the part's acceptance examples, with their original IDs | verbatim |
| `### Scope Boundaries` | what this part leaves to which other part or to later work, and the plan's boundaries that touch it. When an earlier part documents what this part extends, such as a README section, say this part keeps that documentation true for what it adds | written |
| `### Outstanding Questions` | the plan's questions that concern this part's units or requirements, under their plan heading | verbatim |
| `### Sources / Research` | the line `Split from #N.`, then the plan's sources this part touches as a list, an entry that names several files cut to the files this part touches | trimmed |
| `## Planning Contract` | the heading alone | |
| `### Key Technical Decisions` | each KTD one of the part's units names in its `Requirements`, and each KTD that shapes what the part's units build | verbatim |
| `### High-Level Technical Design` | the design this part's units build, cut to them | trimmed |
| `### Assumptions` | the plan's assumptions that touch this part's units | verbatim |
| `### Deferred to Implementation` | the plan's entries that touch this part's units | verbatim |
| `### Sequencing` | the order of this part's units, in one or two lines | written |
| `## Implementation Units` | the part's units, whole, with their original IDs, in the plan's order | verbatim |
| `## Verification Contract` | the gates that apply to this part's units, a column that names units or files cut to the part's | trimmed |
| `## Definition of Done` | the plan's items this part's merge can make true; an item only the last part can make true goes to the last part | verbatim |
| Any other section of the plan, such as Risks or System-Wide Impact | its entries that touch this part's units, under the plan's heading, where the plan has it | trimmed |

Verbatim is the contract and the work: decisions, requirements, flows, examples, technical decisions, units, done items, questions. A paraphrase there could change what gets built. Trimmed keeps the plan's own words, cut to what touches the part. Written is the context, kept short. Take everything from the issue's body: the skill reads no code, so a file or source the plan does not name is not added.

End the file with the line `<!-- cw-split-ce-plan: part of #N -->`.

Measure every part: `sh .agents/skills/cw-split-brainstorm/measure.sh <part files>`. A part with `above_threshold=yes` must be one that rule 1 keeps whole, or one whose units alone are above the threshold. Otherwise regroup, and go back to step 3.

## 6. Write issues.json

Write `docs/splitting/plan/issue-N/issues.json` from steps 4 and 5: each part's id, its title, and the ids of the parts that block it directly.

## 7. Report

The first line is the outcome. Then:

- `not an implementation plan`: that `/cw-split-brainstorm` splits this issue.
- `not split`: the plan's characters, requirements and acceptance examples.
- `kept whole`: those numbers, and the one-sentence reason.
- `split`: the directory, then each part's id, title, unit IDs, requirement IDs, size and reason for shipping alone; each link between parts with its reason; and the parts kept above the threshold, with why.

Also list every command that failed, with its error.
