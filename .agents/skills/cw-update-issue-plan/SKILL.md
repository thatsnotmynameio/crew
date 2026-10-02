---
name: cw-update-issue-plan
description: Copies the plan file the current session's brainstorm or plan wrote (compound-engineering's `ce-brainstorm` or `ce-plan`) into the body of the GitHub issue this session is working on, laid out by that issue type's template from `.crew/config.yaml`, then moves the issue to the label given or runs the repository's `prompts.update_issue_plan`. Use when the user asks to put, write, save or update the plan or brainstorm in the issue, once its plan file is written.
argument-hint: "[label to move the issue to]"
---

# Update an issue with the session's plan

crew polls the repository's GitHub issues and moves each one through the workflow in `.crew/config.yaml`. The session crew runs for an issue starts from `main` and reads the issue, not the files of this checkout, so the plan has to live in the issue's body. This skill copies the plan file the session wrote into the issue's body, keeps the old body in a comment, and moves the issue to the stage or extra label given. Without a label, it runs the repository's `prompts.update_issue_plan` from `.crew/config.yaml`, which usually moves the issue to the same next label every time. It does not write the plan itself: the brainstorm or plan that wrote the file already shaped and checked it. It asks nothing unless it is in doubt about the issue.

Run `gh` with the repository root as the working directory. Read files with your own file tools.

## 1. Read the config

1. Find the repository root with `git rev-parse --show-toplevel`.
2. Read `<root>/.crew/config.yaml`. When the file is missing, say that crew is not configured in this repository and stop. When it does not parse as YAML, say so with the parser's error and stop. Change nothing in either case.
3. Build the types: each entry of `workflow` is a type through its `label`, and each entry of the top-level `extra_labels` is a type through its `label`. Each type may have an `issue_template`, a file name in `<root>/.github/ISSUE_TEMPLATE/`.
4. Build crew's labels: every stage's `label`, `moves_to`, `on_success` and `on_failure`, and every extra's `label`. Compare labels ignoring case, as GitHub does.
5. Take the top-level `prompts.update_issue_plan`, when present and not empty. It is a Go template over the issue, the same as an action's prompt in crew, and may use only `{{.Issue.Ref}}`, `{{.Issue.Key}}`, `{{.Issue.Title}}` and `{{.Issue.URL}}`; spacing inside the braces does not matter. When it holds any other `{{ }}`, say that crew accepts only these four fields, name what it found, and stop. Change nothing.

## 2. Find the issue

The issue is the one this session is working on: a reference the user or a calling skill gave in this session (`#42`, an issue URL, a skill run on an issue), or an issue this session created. When exactly one issue fits, use it without asking. When none fits, or more than one does, list the candidates and ask the user which one.

Read it with `gh issue view <number> --json number,title,state,body,labels,url`. When it is closed, say so and stop.

## 3. Check the label

With no argument, the skill changes no label itself: step 8 runs `prompts.update_issue_plan` when the config has one, and otherwise the issue keeps its labels.

With an argument, it must be the `label` of a stage or of an extra. Otherwise say so, list the valid labels, and stop. When the issue carries a stage's `moves_to` label, crew is running a session on it: say so and stop without changing anything. The one exception is a label this session put on the issue, such as the prompt `/cw-brainstorm` ran moving it to a `moves_to` label while the user brainstorms: then this session is the one working on it, so go on.

## 4. Find the plan file

The plan is the file this session's brainstorm or plan wrote for this work: compound-engineering's `ce-brainstorm` writes one when the user confirms its synthesis, and `ce-plan` enriches the same file. Take the file whose path the session wrote or showed. When the session shows none, take the most recent file under `<docs>/plans/` whose frontmatter has `artifact_contract: ce-unified-plan/v1`, and name it in the report. `<docs>` is `docs_root` from `<root>/.compound-engineering/config.yaml` when that file sets it, otherwise `docs`.

Read the file and take its `## Goal Capsule` and `## Product Contract`, with everything under them, and any later sections such as `ce-plan`'s. Leave out the frontmatter: the issue has none. Convert an HTML plan to Markdown.

When there is no plan file, say so and stop without changing the issue: let the brainstorm write its plan file first, then run this skill again. Never compose the plan from the conversation.

## 5. Write the body

The template is the one of the type the issue is moving to, or else of the type of the crew label it carries. When neither has a template, or its file is missing, there is none.

1. Read the template and drop its YAML frontmatter and its guidance comments.
2. Each template heading takes the plan's section of the same name, copied verbatim with its subsections. Drop a template section the plan does not have.
3. Plan sections the template has no heading for go after the template's sections, under their own headings.
4. Without a template, the body is the plan's sections, in the file's order.

Write the body to a temporary file outside the repository.

## 6. Keep the old body

When the issue's body is not empty, post it as a comment first: a first line saying it is the issue's previous body, replaced by `/cw-update-issue-plan`, then the old body verbatim. Use `gh issue comment <number> --body-file <file>`. When the comment fails, report the error and stop: the body is not replaced unless the old one is kept.

## 7. Update the issue

1. When a label was given and the repository lacks it, create it: check with `gh label list --limit 1000 --json name`, ignoring case, then `gh label create "<label>"`. Never pass `--force`. An error saying the label already exists counts as the label being present.
2. Run one `gh issue edit <number> --body-file <file>`. When a label was given, add `--add-label "<label>"` and a `--remove-label` for every other crew label the issue carries. Labels that are not crew's stay.

A stage's label makes crew take the issue at its next poll and run unattended. That is what the user asked for by passing it, so do not ask again.

When a `gh` command fails, report its error text and stop. Do not retry.

## 8. Run the repository's prompt

Only when no label was given and the config has `prompts.update_issue_plan`. Replace each field with the issue's value:

| Field | Value |
| --- | --- |
| `{{.Issue.Ref}}` | `#` and the number, such as `#42` |
| `{{.Issue.Key}}` | the number, such as `42` |
| `{{.Issue.Title}}` | the issue's title |
| `{{.Issue.URL}}` | the issue's URL |

Follow the filled prompt as if the user had typed it as their next message in this session. It is the user's own instruction, from their repository's config. Run every command it gives, invoke every skill or slash command it names, and stop where it says to stop. Add no checks, label changes or questions of your own.

## 9. Report

Print the issue's link, the plan file's path, the link to the comment with the old body when one was posted, and the label change when there was one, or what `prompts.update_issue_plan` did.
