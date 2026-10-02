---
name: cw-create-issue
description: Creates a GitHub issue for crew in the current repository, with the label of a stage or extra label from `.crew/config.yaml` and a body that follows that type's issue template. Use when the user asks to create, open, record, file or log an issue for crew, to queue work for crew (a feature, a bug fix, an audit, a learning), to park an idea for a later brainstorm, or to turn the plan or brainstorm just written into an issue.
argument-hint: "[what to record, optionally the type or a plan path]"
---

# Create an issue for crew

crew polls the repository's GitHub issues and moves each one through the workflow in `.crew/config.yaml`. A stage takes every open issue carrying its `label`, so the label this skill puts on the issue decides what crew does with it, unattended, within one poll. The skill creates the issue directly, with no preview and no confirmation: a mistake is fixed by editing the issue on GitHub.

Run `gh` with the repository root as the working directory. Read files with your own file tools.

## 1. Read the config

1. Find the repository root with `git rev-parse --show-toplevel`.
2. Read `<root>/.crew/config.yaml`. When the file is missing, say that crew is not configured in this repository and stop. When it does not parse as YAML, say so with the parser's error and stop. Create nothing in either case.
3. Build the types:
   - **Stages:** each entry of `workflow` is a type, with its `label`, `name`, and optional `description` and `issue_template`. Only a stage's `label` is a type; its `moves_to`, `on_success` and `on_failure` labels are not.
   - **Extras:** each entry of the top-level `extra_labels` is a type, with its `label` and optional `description` and `issue_template`. An extra parks an issue: no stage takes it until someone adds a stage's label.
   - A type is shown by its description, or by its name (a stage) or label (an extra) when it has none.
4. When the config names no types, say so and stop, creating nothing.

## 2. Decide what to record

What to record comes from the arguments and the session. With no arguments, record the session's work, such as a plan just written. When the session has nothing to record either, ask the user what to record. That is the only question besides the type.

## 3. Decide the type

The type is clear when:

- the user named it, by its name, label or description, or
- exactly one type's name, label or description fits the request. Session context counts: a requirements plan written in this session points to the type whose description names finished brainstorms.

In any other case, including when two types fit, list the types (each with its description or name, and its label) and ask the user to pick one. A wrong stage label starts an unattended crew run, so ask whenever you doubt.

## 4. Write the body

1. When the type has an `issue_template`, read `<root>/.github/ISSUE_TEMPLATE/<issue_template>`.
2. **Template found:** drop its YAML frontmatter; its `labels`, `title` and `assignees` are not used. Fill each section from what the user wrote and from the session's context, keeping the template's headings. Replace the template's guidance comments with the content they ask for. Do not invent facts: leave a section short when there is little to say.
3. **No template,** because the type declares none or its file is missing: write a free-form body from the same sources. Remember a missing file for the report.

## 5. Append the session's plan

The session's plan is the plan path the user passed, or else the most recent plan this session wrote or enriched under `<docs>/plans/`. `<docs>` is `docs_root` from `<root>/.compound-engineering/config.yaml` when that file sets it, otherwise `docs`. A plan is a file whose frontmatter has `artifact_contract: ce-unified-plan/v1`.

When there is a plan with a `## Product Contract` section, append that whole section after the body, under its own `## Product Contract` heading, so the crew session that takes the issue needs no file outside it. Convert an HTML plan's section to Markdown. A plan without that section counts as no plan.

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
- that the plan's Product Contract was appended, naming the plan, when it was.
