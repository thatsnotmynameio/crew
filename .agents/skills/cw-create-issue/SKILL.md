---
name: cw-create-issue
description: Creates a GitHub issue for crew in the current repository, with the label of one of the issue types this skill lists and a body that follows that type's issue template. Use when the user asks to create, open, record, file or log an issue for crew, to queue work for crew (a feature, a bug fix, an audit, a learning), to park an idea for a later brainstorm, or to turn the plan or brainstorm just written into an issue.
argument-hint: "[what to record, optionally the type or a plan path]"
---

# Create an issue for crew

This skill is the crew repository's own aid for building crew, not part of crew. Its issue types and labels are this repository's. Another repository copies the skill and edits its table of types to match its own rules and issue templates.

crew polls the repository's GitHub issues, and each of its rules takes every open issue carrying the rule's ready label. The label this skill puts on the issue decides what crew does with it, unattended, within one poll. The skill creates the issue directly, with no preview and no confirmation: a mistake is fixed by editing the issue on GitHub.

Run `gh` with the repository root, from `git rev-parse --show-toplevel`, as the working directory. Read files with your own file tools.

## 1. The types

An issue's type gives its label, the template its body follows (a file in `<root>/.github/ISSUE_TEMPLATE/`), and the description that shows the type to the user:

| Label | Template | Description | What crew does with it |
| --- | --- | --- | --- |
| `crew:development:ready` | `feature.md` | a feature whose brainstorm is done | the development rule builds it and opens a pull request |
| `crew:fix:ready` | `bug.md` | a bug to reproduce and fix | the fix rule reproduces it, fixes it and opens a pull request |
| `crew:brainstorm:ready` | `idea.md` | an idea to brainstorm later | nothing: no rule takes it, it waits for `/cw-brainstorm` |
| `crew:brainstorm:done` | `feature.md` | a brainstormed feature to hand to refinement | the promote brainstorm rule moves it to `crew:refinement:ready` |
| `crew:refinement:ready` | `feature.md` | a brainstormed feature to split when large and whose dependencies to find | the refinement rule splits a large plan into sub-issues and finds what blocks each issue and what it blocks |
| `crew:refinement:done` | `feature.md` | a refined feature to hand to development | the promote refinement rule moves it to `crew:development:ready` |
| `crew:ci audit:ready` | `ci-audit.md` | an audit of the GitHub Actions | the audit ci rule, which is turned off in this repository |
| `crew:knowledge base:ready` | `knowledge-base.md` | a solved problem to record as a learning | the knowledge base rule, which is turned off in this repository |

## 2. Decide what to record

What to record comes from the arguments and the session. With no arguments, record the session's work, such as a plan just written. When the session has nothing to record either, ask the user what to record. That is the only question besides the type.

## 3. Decide the type

The type is clear when:

- the user named it, by its label or description, or by the rule that takes it, or
- exactly one type's label or description fits the request. Session context counts: a requirements plan written in this session points to the type whose description names finished brainstorms.

In any other case, including when two types fit, list the types (each with its description and its label) and ask the user to pick one. A label a rule takes starts an unattended crew run, so ask whenever you doubt.

## 4. Write the body

1. Read the type's template, `<root>/.github/ISSUE_TEMPLATE/<template>`.
2. **Template found:** drop its YAML frontmatter; its `labels`, `title` and `assignees` are not used. When the issue records a plan, step 5 fills the sections. Otherwise fill each section from what the user wrote and from the session's context, keeping the template's headings. Replace the template's guidance comments with the content they ask for. Do not invent facts: leave a section short when there is little to say.
3. **No template,** because its file is missing: write a free-form body from the same sources. Remember the missing file for the report.

## 5. Copy the plan the issue records

An issue records a plan when the user passed the plan's path, ran the skill with no arguments to record the session's plan, or asked in other words to record it. The session's plan is the most recent plan this session wrote or enriched under `<docs>/plans/`. `<docs>` is `docs_root` from `<root>/.compound-engineering/config.yaml` when that file sets it, otherwise `docs`. A plan is a file whose frontmatter has `artifact_contract: ce-unified-plan/v1`: the compound-engineering plugin's plan format, such as the requirements plan its `ce-brainstorm` writes. A repository that does not use that plugin has no plans, and this step does nothing.

When the issue records a plan, its body is the plan's content, so the crew session that takes the issue needs no file outside it:

1. Each template heading takes the plan's section of the same name, copied verbatim, with its subsections. Drop the template's guidance comments, and drop a template section the plan does not have.
2. Plan sections the template has no heading for go after the template's sections, under their own headings.
3. Without a template, the body is the plan's `## Goal Capsule` and `## Product Contract`.

Convert an HTML plan to Markdown. A plan without a `## Product Contract` section counts as no plan.

When the issue does not record a plan, use none, even if the session wrote one: a plan on an unrelated issue would reach an unattended crew run as its scope.

## 6. Ensure the label exists

1. Run `gh label list --limit 1000 --json name` and look for the type's label, ignoring case.
2. When it is missing, run `gh label create "<label>"`. Never pass `--force`: it would overwrite an existing label's color and description. An error saying the label already exists counts as the label being present.

## 7. Create the issue

1. Take the title from what the user wrote: short, and naming the work. Never the template's `title`.
2. Write the body to a temporary file outside the repository.
3. Run `gh issue create --title "<title>" --label "<label>" --body-file <file>`, with exactly the type's label and no other.

When a `gh` command fails, report its error text and stop. Do not retry.

## 8. Report

Print the issue's link and its label. Also say:

- that the template file was missing, naming the path, when it was,
- which plan the body was copied from, when it was,
- that the session wrote a plan the body does not use, naming it, when that happened,
- that the rule that takes the label is turned off in this repository, so crew leaves the issue alone until that rule is turned on, when the type is the CI audit or the knowledge base.
