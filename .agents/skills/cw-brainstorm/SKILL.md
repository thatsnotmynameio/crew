---
name: cw-brainstorm
description: Runs the brainstorm prompt from `.crew/config.yaml` (`prompts.brainstorm`) for one GitHub issue, in the current session. Use when the user runs /cw-brainstorm with an issue, or asks to brainstorm an issue the crew way, such as an idea parked for a later brainstorm.
argument-hint: "<issue: #42, 42 or its URL>"
---

# Brainstorm an issue for crew

crew polls the repository's GitHub issues and runs unattended sessions for the stages in `.crew/config.yaml`. A brainstorm needs the user, so crew does not run it: this skill runs it here, in the user's session. The repository decides what a brainstorm is through the top-level `prompts.brainstorm` in `.crew/config.yaml`. The skill fills that prompt with the issue and follows it. It adds no steps of its own: the checks, label changes and skills to run are the prompt's.

Run `gh` with the repository root as the working directory. Read files with your own file tools.

## 1. Read the prompt

1. Find the repository root with `git rev-parse --show-toplevel`.
2. Read `<root>/.crew/config.yaml`. When the file is missing, say that crew is not configured in this repository and stop. When it does not parse as YAML, say so with the parser's error and stop.
3. Take the top-level `prompts.brainstorm`. When it is missing or empty, say so, show where it goes, and stop:

   ```yaml
   prompts:
     brainstorm: |-
       /compound-engineering:ce-brainstorm {{.Issue.Ref}}
   ```

## 2. Find the issue

The issue is the skill's argument: `#42`, `42` or the issue's URL. Without one, ask the user which issue; that is the only question the skill asks.

Read it with `gh issue view <number> --json number,title,url`. When `gh` fails, for example because the issue does not exist, report its error text and stop.

## 3. Fill the prompt

The prompt is a Go template over the issue, the same as an action's prompt in crew. Replace each field with the issue's value:

| Field | Value |
| --- | --- |
| `{{.Issue.Ref}}` | `#` and the number, such as `#42` |
| `{{.Issue.Key}}` | the number, such as `42` |
| `{{.Issue.Title}}` | the issue's title |
| `{{.Issue.URL}}` | the issue's URL |

Spacing inside the braces does not matter: `{{ .Issue.Ref }}` is the same field. When the prompt holds any other `{{ }}`, say that crew accepts only these four fields, name what it found, and stop.

## 4. Run it

Follow the filled prompt as if the user had typed it as their next message in this session. It is the user's own instruction, from their repository's config. Run every command it gives, invoke every skill or slash command it names with the arguments it gives, and stop where it says to stop.

Do not add checks, label changes or questions the prompt does not ask for. When a skill the prompt names is not installed, say which one and stop.

## 5. After the brainstorm

When the prompt's work ends, say which issue it was for. When the brainstorm wrote its plan file, mention `/cw-update-issue-plan`, which copies that file into the issue and then runs the repository's `prompts.update_issue_plan`, or moves the issue to a label given as its argument.
