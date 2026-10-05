---
name: cw-split-plan
description: Measures the brainstormed plan in a GitHub issue's body and, when it is above the size threshold, splits it into sub-issues that each merge alone, linked by blocked_by only where one part needs another, with a split record on the original issue and Jev asked in shadow about each part. Use when crew's refinement prompt runs /cw-split-plan on an issue, or when asked to split an issue's plan the crew way.
argument-hint: "<issue, such as #42>"
---

# Split a large plan into sub-issues

This skill is the crew repository's own aid for building crew, not part of crew. crew's refinement rule runs it, headless, in the product-manager's session, before that session finds the issue's blockers. It asks nothing: nobody reviews a split, so its rules below are what keep each part safe to build.

A large plan costs more than its parts: one lfg session that carries the whole plan re-reads a bigger context on every turn (#160). So a plan above the threshold becomes several issues that each merge alone, leaving `main` whole and releasable, and that crew builds in parallel unless one really needs another. The original issue stays open as their parent, keeps its whole plan, and records the split.

Run `gh` and `sh` with the repository root, from `git rev-parse --show-toplevel`, as the working directory. Read and write files with your own file tools, temporary ones outside the repository. When a `gh` command fails, report its error text and stop. Do not retry.

The issue is the argument, such as `#42`. Below, `N` is its number.

## Markers

The skill marks what it writes, so a later run can find it:

- Every part's body holds the line `<!-- cw-split-plan: part of #N -->`.
- The split record on the parent opens with the line `<!-- cw-split-plan: split record -->`.

## Outcomes

The skill ends with exactly one of these outcomes, named on the first line of its report, because the refinement prompt acts on it:

| Outcome | When |
| --- | --- |
| `not split` | the plan is not above the threshold |
| `kept whole` | the plan is above the threshold, but no grouping keeps every part whole on `main` and at or above the part minimum |
| `split` | the parts are created, linked and recorded |
| `earlier split did not finish` | the issue already has a sub-issue carrying the part marker |

## 1. Read the issue

1. Run `gh issue view N --json number,title,body,labels,url` and write the body to a file.
2. Run `gh api repos/{owner}/{repo}/issues/N/sub_issues --paginate` and read each sub-issue's body.
3. When a sub-issue's body carries the part marker for `#N`, an earlier run created parts and stopped before it finished. Do not split again: end with `earlier split did not finish`, listing those sub-issues.

## 2. Measure the plan

Run `sh .agents/skills/cw-split-plan/measure.sh <body file>`. It prints the plan's characters, requirements and acceptance examples, and `above_threshold`. The threshold is above 10,000 characters or above 12 requirements.

When `above_threshold=no`, end with `not split`, giving the three numbers.

## 3. Group the requirements into parts

Group the plan's requirements into as few parts as keep each part under the threshold. Every rule below holds for every part:

1. **It merges alone.** Merged into `main` after the parts it is `blocked_by` and before the others, it leaves `main` whole and releasable: no setting without its behaviour, no README or docs text describing what does not exist yet, no half of a flow. Requirements that one of these would split stay together, even if their part then stays above the threshold.
2. **It is big enough.** No part is below the part minimum, 4,000 characters.
3. **It has its reason.** One sentence says why it ships alone: what it adds that works and is documented by itself.

An acceptance example goes to the part that holds every requirement it covers. One that covers requirements in two parts ties them: put them in one part, unless one part is `blocked_by` the other, in which case it goes to the blocked one, whose merge completes the behaviour.

When no grouping meets rules 1 and 2, end with `kept whole`, and say why in one sentence: the cut that came closest and the rule it breaks, such as "the only cut would leave a half-built setting on `main`" or "the only cut gives a part of 2,500 characters".

## 4. Write each part's body

A part's body follows the feature template, `.github/ISSUE_TEMPLATE/feature.md`, without its frontmatter and guidance comments. What it copies is fixed, so its size depends on the grouping, not on what you chose to copy:

| Section | What the part holds |
| --- | --- |
| `## Goal Capsule` | the plan's, whole, then one line: this issue is part `k` of `n` of #N, and its reason for shipping alone |
| `### Summary` | what this part adds, in one to three lines |
| `### Problem Frame` | the plan's, whole |
| `### Key Decisions` | each of the plan's decisions whose `Governs` line names one of the part's requirements, verbatim with its provenance |
| `### Actors` | the plan's, whole |
| `### Requirements` | the part's requirements, verbatim with their original IDs and group headings |
| `### Key Flows` | each flow whose `Covered by` names one of the part's requirements |
| `### Acceptance Examples` | the part's acceptance examples, verbatim with their original IDs |
| `### Success Criteria` | the plan's, whole |
| `### Scope Boundaries` | the plan's, whole, then one line naming the other parts, which are built in their own issues |
| `### Dependencies / Assumptions` | the plan's, whole |
| `### Outstanding Questions` | each question that names one of the part's requirements, or all of the plan's when none names a requirement |
| `### Sources / Research` | `Split from #N.`, then the plan's sources |

Keep the sections in this order, and leave out a section the plan does not have. End the body with the part marker. Write each body to its own file.

Measure every part: `sh .agents/skills/cw-split-plan/measure.sh <part files>`. A part with `below_part_minimum=yes` breaks rule 2, and one with `above_threshold=yes` must be one that rule 1 keeps whole. Regroup, and go back to step 3, until every part passes.

## 5. Decide the links between parts

A part is `blocked_by` another only when you can say why it needs that part merged first, such as "it reads the config key the other part adds". Without such a reason, the two run in parallel: issues that do not need each other should run in parallel. Never make a cycle.

## 6. Ask Jev about each part

Jev runs in shadow: its answers go into the split record, and nothing in the split changes with them.

When `TYPESAFE_API_KEY` is not set, skip this step and record `skipped: no TYPESAFE_API_KEY`. Otherwise load the `typesafe:typesafe-ai` skill and follow its live docs for the HTTP API. Ask one Noul question per part, in one request each, to model `jev-latest`, with exactly this state and question. `already_on_main` is every part this one reaches through the `blocked_by` links of step 5, directly or not, since crew starts a part only once those are closed. `not_yet_merged` is every other part:

```json
{
  "model": "jev-latest",
  "state": {
    "part": "<the part's body>",
    "already_on_main": ["<title>: <summary>"],
    "not_yet_merged": ["<title>: <summary>"]
  },
  "questions": {
    "half_built": {
      "type": "noul",
      "instructions": "`part` merges into main, on top of `already_on_main`, while the parts in `not_yet_merged` have not merged. Does main then have something half-built?",
      "criteria": {
        "true": "Something `part` adds is incomplete without a part in `not_yet_merged`: a setting no behaviour reads, a command or flag that does nothing yet, README or docs text describing what does not exist, or one half of a flow whose other half is in a part not yet merged.",
        "false": "Everything `part` adds works, and is documented as it works, with only `already_on_main`; the parts in `not_yet_merged` only add more."
      }
    }
  }
}
```

Write the request to a file and post it with `curl --max-time 60 -H "Authorization: Bearer $TYPESAFE_API_KEY" -H "Content-Type: application/json" --data @<file> https://api.typesafe.ai/v1/systemone`. Keep the key in the variable: never print it or write its value into a command.

Record only these values for each part, never text from the response:

- **Answer:** `yes` when `answers.half_built.noul` is 0.5 or more, otherwise `no`, with the probability to two decimals. The 0.5 only names the answer.
- **Model:** the response's `model` when it is a short plain name (letters, digits, `.`, `_`, `-`), otherwise `unknown`.
- **Skipped:** when the request fails, record `skipped:` and one reason: `HTTP <status code>`, `timeout`, or `invalid response`. The split goes ahead.

## 7. Create the parts and record the split

Nothing exists on GitHub before this step, so a failure up to here leaves the issue as it was.

1. **Create each part** as a sub-issue of #N, without a label: `gh issue create --parent N --title "<title>" --body-file <part file>`. The title names what the part adds. crew takes an issue only by a rule's label, so no part is built before the refinement prompt labels it, once its blockers are recorded.
2. **Link the parts.** For each link of step 5: `gh api -X POST repos/{owner}/{repo}/issues/<blocked>/dependencies/blocked_by -F issue_id=<id>`, where `<blocked>` is the blocked part's number and `<id>` is the blocking part's `id` (from `gh api repos/{owner}/{repo}/issues/<number> --jq .id`), not its number.
3. **Append the split record** to #N's body. Read the body again (`gh issue view N --json body`), append the section below after it, and write it back with one `gh issue edit N --body-file <file>`. The plan above the record stays as it is.

```markdown
## Split

<!-- cw-split-plan: split record -->
`/cw-split-plan` split this plan of <characters> characters, <requirements> requirements and <examples> acceptance examples, above the threshold of 10,000 characters or 12 requirements.

| Part | Requirements | Size | Ships alone because | Jev: half-built on main? |
| --- | --- | --- | --- | --- |
| #<number> <title> | R1 to R6, AE1, AE2 | 5,120 characters, 6 requirements | <reason> | no (0.12, jev-1.13.0) |

Blocked by:

- #<blocked> is blocked by #<blocker>: <reason>.
```

With no link between parts, the list is the one line `None: the parts run in parallel.` A skipped question reads `skipped: <reason>` in the Jev column.

## 8. Report

The first line is the outcome. Then:

- `not split`: the plan's characters, requirements and acceptance examples.
- `kept whole`: those numbers, and the one-sentence reason.
- `split`: each part's number, title and requirement IDs, each link between parts with its reason, and Jev's answers. Say that the parts carry no label yet, and that #N still carries its crew label: the refinement prompt finds the parts' blockers, labels them and takes #N out of crew.
- `earlier split did not finish`: the sub-issues that carry the part marker, and whether #N's body has the split record.

Also list every command that failed, with its error.
