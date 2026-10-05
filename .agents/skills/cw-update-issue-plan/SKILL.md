---
name: cw-update-issue-plan
description: Copies the plan file the current session's brainstorm or plan wrote (compound-engineering's `ce-brainstorm` or `ce-plan`) into the body of the GitHub issue this session is working on, laid out by the template of that issue's type, then moves the issue to the label given or, without one, from `crew:brainstorm:in progress` to `crew:brainstorm:done`. Use when the user asks to put, write, save or update the plan or brainstorm in the issue, once its plan file is written.
argument-hint: "[label to move the issue to]"
---

# Update an issue with the session's plan

This skill is the crew repository's own aid for building crew, not part of crew. Its issue types, labels and closing prompt are this repository's. Another repository copies the skill and edits its tables and prompt to match its own rules and issue templates.

crew polls the repository's GitHub issues, and each of its rules takes every open issue carrying the rule's ready label. The session crew runs for an issue starts from `main` and reads the issue, not the files of this checkout, so the plan has to live in the issue's body. This skill copies the plan file the session wrote into the issue's body, keeps the old body in a comment, and moves the issue to the label given. Without a label, it runs the prompt in step 8, which moves the issue from `crew:brainstorm:in progress` to `crew:brainstorm:done`. It does not write the plan itself: the brainstorm or plan that wrote the file already shaped and checked it. It asks nothing unless it is in doubt about the issue.

Run `gh` with the repository root, from `git rev-parse --show-toplevel`, as the working directory. Read files with your own file tools. Compare labels ignoring case, as GitHub does.

## 1. The labels

**Types.** An issue's type gives its label and the template its body follows, a file in `<root>/.github/ISSUE_TEMPLATE/`:

| Label | Template | Description |
| --- | --- | --- |
| `crew:development:ready` | `feature.md` | a feature whose brainstorm is done |
| `crew:fix:ready` | `bug.md` | a bug to reproduce and fix |
| `crew:brainstorm:ready` | `idea.md` | an idea to brainstorm later |
| `crew:brainstorm:done` | `feature.md` | a brainstormed feature to hand to refinement |
| `crew:refinement:ready` | `feature.md` | a brainstormed feature to split when large and whose dependencies to find |
| `crew:refinement:done` | `feature.md` | a refined feature to hand to development |
| `crew:ci audit:ready` | `ci-audit.md` | an audit of the GitHub Actions |
| `crew:knowledge base:ready` | `knowledge-base.md` | a solved problem to record as a learning |

**Running labels.** crew puts a rule's running label on an issue while it works on it: `crew:brainstorm:promoting`, `crew:refinement:in progress`, `crew:refinement:promoting`, `crew:development:in progress`, `crew:fix:in progress`, `crew:ci audit:in progress` and `crew:knowledge base:in progress`.

**crew's labels.** These are the labels a move removes:

- `crew:brainstorm:ready`, `crew:brainstorm:in progress`, `crew:brainstorm:done`, `crew:brainstorm:promoting`, `crew:brainstorm:failed`
- `crew:refinement:ready`, `crew:refinement:in progress`, `crew:refinement:done`, `crew:refinement:promoting`, `crew:refinement:failed`
- `crew:development:ready`, `crew:development:in progress`, `crew:development:waiting review`, `crew:development:failed`
- `crew:fix:ready`, `crew:fix:in progress`, `crew:fix:waiting review`, `crew:fix:failed`
- `crew:ci audit:ready`, `crew:ci audit:in progress`, `crew:ci audit:done`, `crew:ci audit:failed`
- `crew:knowledge base:ready`, `crew:knowledge base:in progress`, `crew:knowledge base:done`, `crew:knowledge base:failed`

## 2. Find the issue

The issue is the one this session is working on: a reference the user or a calling skill gave in this session (`#42`, an issue URL, a skill run on an issue), or an issue this session created. When exactly one issue fits, use it without asking. When none fits, or more than one does, list the candidates and ask the user which one.

Read it with `gh issue view <number> --json number,title,state,body,labels,url`. When it is closed, say so and stop.

## 3. Check the label

With no argument, the skill changes no label itself: step 8 runs the prompt that moves the issue.

With an argument, it must be one of the types' labels. Otherwise say so, list the valid labels, and stop. When the issue carries a running label, crew is working on it: say so and stop without changing anything. The one exception is a label this session put on the issue: then this session is the one working on it, so go on.

## 4. Find the plan file

The plan is the file this session's brainstorm or plan wrote for this work: compound-engineering's `ce-brainstorm` writes one when the user confirms its synthesis, and `ce-plan` enriches the same file. Take the file whose path the session wrote or showed. When the session shows none, take the most recent file under `<docs>/plans/` whose frontmatter has `artifact_contract: ce-unified-plan/v1`, and name it in the report. `<docs>` is `docs_root` from `<root>/.compound-engineering/config.yaml` when that file sets it, otherwise `docs`.

Read the file and take its `## Goal Capsule` and `## Product Contract`, with everything under them, and any later sections such as `ce-plan`'s. Leave out the frontmatter: the issue has none. Convert an HTML plan to Markdown.

When there is no plan file, say so and stop without changing the issue: let the brainstorm write its plan file first, then run this skill again. Never compose the plan from the conversation.

## 5. Write the body

The template is the one of the type the issue is moving to, or else of the type whose label the issue carries. When there is no such type, or its file is missing, there is none.

1. Read the template and drop its YAML frontmatter and its guidance comments.
2. Each template heading takes the plan's section of the same name, copied verbatim with its subsections. Drop a template section the plan does not have.
3. Plan sections the template has no heading for go after the template's sections, under their own headings.
4. Without a template, the body is the plan's sections, in the file's order.

Write the body to a temporary file outside the repository.

## 6. Keep the old body

When the issue's body is not empty, post it as a comment first: a first line saying it is the issue's previous body, replaced by `/cw-update-issue-plan`, then the old body verbatim. Use `gh issue comment <number> --body-file <file>`. When the comment fails, report the error and stop: the body is not replaced unless the old one is kept.

## 7. Update the issue

1. When a label was given and the repository lacks it, create it: check with `gh label list --limit 1000 --json name`, ignoring case, then `gh label create "<label>"`. Never pass `--force`. An error saying the label already exists counts as the label being present.
2. Run one `gh issue edit <number> --body-file <file>`. When a label was given, add `--add-label "<label>"` and a `--remove-label` for every other one of crew's labels the issue carries. Other labels stay.

A label a rule takes makes crew take the issue at its next poll and run unattended. That is what the user asked for by passing it, so do not ask again.

When a `gh` command fails, report its error text and stop. Do not retry.

## 8. Move the issue on

Only when no label was given. The prompt below moves a brainstormed idea on. Replace each field with the issue's value:

| Field | Value |
| --- | --- |
| `{{.Issue.Ref}}` | `#` and the number, such as `#42` |
| `{{.Issue.Key}}` | the number, such as `42` |
| `{{.Issue.Title}}` | the issue's title |
| `{{.Issue.URL}}` | the issue's URL |

```text
Move {{.Issue.Ref}} to `crew:brainstorm:done`, so crew's promote brainstorm rule hands it to refinement at its next poll: `gh issue edit {{.Issue.Key}} --remove-label "crew:brainstorm:in progress" --add-label "crew:brainstorm:done"`. When that fails, report the error and stop.
```

Follow the filled prompt as if the user had typed it as their next message in this session. Run every command it gives, and stop where it says to stop. Add no checks, label changes or questions of your own.

## 9. Report

Print the issue's link, the plan file's path, the link to the comment with the old body when one was posted, and the label change when there was one, or what the prompt in step 8 did.
